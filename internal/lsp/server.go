package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"doggo/internal/lexer"
	"doggo/internal/project"
	"doggo/internal/syntax/frontend"
	"doggo/internal/syntax/span"
	toolingformat "doggo/internal/tooling/format"
	toolindex "doggo/internal/tooling/index"
	toolinglint "doggo/internal/tooling/lint"
	"doggo/internal/types"
	"doggo/internal/types/coll"
	"doggo/stdlib"
)

const (
	lspMethodInitialize                = "initialize"
	lspMethodShutdown                  = "shutdown"
	lspMethodInitialized               = "initialized"
	lspMethodExit                      = "exit"
	lspMethodDidOpen                   = "textDocument/didOpen"
	lspMethodDidChange                 = "textDocument/didChange"
	lspMethodDidClose                  = "textDocument/didClose"
	lspMethodDidSave                   = "textDocument/didSave"
	lspMethodCompletion                = "textDocument/completion"
	lspMethodDefinition                = "textDocument/definition"
	lspMethodReferences                = "textDocument/references"
	lspMethodRename                    = "textDocument/rename"
	lspMethodFormatting                = "textDocument/formatting"
	lspMethodHover                     = "textDocument/hover"
	lspMethodDocumentSymbol            = "textDocument/documentSymbol"
	lspMethodWorkspaceSymbol           = "workspace/symbol"
	lspMethodSemanticTokensFull        = "textDocument/semanticTokens/full"
	lspMethodCodeAction                = "textDocument/codeAction"
	lspMethodPublishDiagnostics        = "textDocument/publishDiagnostics"
	lspErrorMethodNotFound             = -32601
	lspErrorInvalidParams              = -32602
	lspErrorServerNotInitialized       = -32002
	lspDiagnosticSeverityError         = 1
	lspTextDocumentSyncKindIncremental = 2
	lspCompletionTriggerCharacterSlash = "/"
	lspCompletionTriggerCharacterColon = ":"
	lspMaxIncrementalChanges           = 32
	lspSymbolKindFunction              = 12
	lspSymbolKindVariable              = 13
)

type Server struct {
	in  *bufio.Reader
	out *bufio.Writer

	docs       *DocumentStore
	state      *WorkspaceState
	session    *Session
	router     *Router
	projectDir string

	openGenerationSession func(string) (*project.GenerationSession, error)
	generationSession     *project.GenerationSession
	initialized           bool
}

func NewServer(in io.Reader, out io.Writer, openGenerationSession func(string) (*project.GenerationSession, error)) *Server {
	docs := NewDocumentStore()
	state := NewWorkspaceState()
	session := NewSession(docs, state)
	return &Server{
		in:                    bufio.NewReader(in),
		out:                   bufio.NewWriter(out),
		docs:                  docs,
		state:                 state,
		session:               session,
		router:                NewRouter(session),
		openGenerationSession: openGenerationSession,
	}
}

func (s *Server) Run() (err error) {
	defer func() {
		if s.generationSession != nil {
			err = errors.Join(err, s.generationSession.Close())
			s.generationSession = nil
		}
	}()
	for {
		payload, err := readMessage(s.in)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var msg message
		if err := json.Unmarshal(payload, &msg); err != nil {
			return err
		}

		if msg.Method == "" {
			continue
		}

		if msg.ID != nil {
			if err := s.handleRequest(msg); err != nil {
				return err
			}
			continue
		}

		shouldExit, err := s.handleNotification(msg)
		if err != nil {
			return err
		}
		if shouldExit {
			return nil
		}
	}
}

func (s *Server) handleRequest(msg message) error {
	if s.router == nil {
		s.router = NewRouter(NewSession(s.docs, s.state))
	}
	return s.router.HandleRequest(s, msg)
}

func (s *Server) handleNotification(msg message) (bool, error) {
	if !s.initialized && msg.Method != lspMethodExit {
		return false, nil
	}
	switch msg.Method {
	case lspMethodInitialized:
		return false, nil
	case lspMethodDidOpen:
		return false, s.handleDidOpen(msg.Params)
	case lspMethodDidChange:
		return false, s.handleDidChange(msg.Params)
	case lspMethodDidClose:
		return false, s.handleDidClose(msg.Params)
	case lspMethodDidSave:
		return false, s.handleDidSave(msg.Params)
	case lspMethodExit:
		return true, nil
	default:
		return false, nil
	}
}

type initializeParams struct {
	RootURI  string `json:"rootUri"`
	RootPath string `json:"rootPath"`
}

func (s *Server) handleInitialize(msg message) (err error) {
	if s.initialized {
		return s.respondError(msg.ID, lspErrorInvalidParams, "server is already initialized")
	}
	params := initializeParams{}
	if len(msg.Params) > 0 && string(msg.Params) != "null" {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return s.respondError(msg.ID, lspErrorInvalidParams, "invalid initialize params")
		}
	}

	root, err := resolveProjectRoot(params)
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	var generationSession *project.GenerationSession
	if s.openGenerationSession != nil {
		generationSession, err = s.openGenerationSession(project.ModPath(root))
		if err != nil {
			return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
		}
		defer func() {
			if generationSession != nil {
				s.generationSession = nil
				err = errors.Join(err, generationSession.Close())
			}
		}()
	}
	s.projectDir = root
	s.generationSession = generationSession
	if err := s.rebuildIndexFull(); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}

	s.initialized = true
	generationSession = nil

	result := map[string]any{
		"capabilities": map[string]any{
			"textDocumentSync": map[string]any{
				"openClose": true,
				"change":    lspTextDocumentSyncKindIncremental,
				"save": map[string]any{
					"includeText": false,
				},
			},
			"completionProvider": map[string]any{
				"triggerCharacters": []string{
					lspCompletionTriggerCharacterSlash,
					lspCompletionTriggerCharacterColon,
				},
			},
			"definitionProvider":         true,
			"referencesProvider":         true,
			"renameProvider":             true,
			"documentFormattingProvider": true,
			"hoverProvider":              true,
			"documentSymbolProvider":     true,
			"workspaceSymbolProvider":    true,
			"codeActionProvider":         true,
			"semanticTokensProvider": map[string]any{
				"legend": map[string]any{
					"tokenTypes":     semanticTokenTypes,
					"tokenModifiers": []string{},
				},
				"full": true,
			},
		},
	}
	return s.respond(msg.ID, result)
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type textDocumentItem struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
	Text    string `json:"text"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

func (s *Server) handleDidOpen(raw json.RawMessage) error {
	params := didOpenParams{}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil
	}
	s.docs.Upsert(params.TextDocument.URI, params.TextDocument.Text, params.TextDocument.Version)
	s.markDirtyURI(params.TextDocument.URI)
	if err := s.refreshIndexIfStale(); err != nil {
		return err
	}
	return s.publishDiagnosticsCurrentIndex(params.TextDocument.URI, params.TextDocument.Text)
}

type textDocumentContentChangeEvent struct {
	Range       *lspRange `json:"range,omitempty"`
	RangeLength *int      `json:"rangeLength,omitempty"`
	Text        string    `json:"text"`
}

type versionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type didChangeParams struct {
	TextDocument   versionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []textDocumentContentChangeEvent `json:"contentChanges"`
}

func (s *Server) handleDidChange(raw json.RawMessage) error {
	params := didChangeParams{}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil
	}
	if len(params.ContentChanges) == 0 {
		return nil
	}
	content := ""
	if current, ok, sourceErr := s.documentSource(params.TextDocument.URI); sourceErr != nil {
		return sourceErr
	} else if ok {
		content = current
	}
	nextContent, err := applyContentChanges(content, params.ContentChanges)
	if err != nil {
		return s.publishIncrementalSyncFailure(params.TextDocument.URI, err)
	}
	content = nextContent
	s.docs.Upsert(params.TextDocument.URI, content, params.TextDocument.Version)
	s.markDirtyURI(params.TextDocument.URI)
	if err := s.refreshIndexIfStale(); err != nil {
		return err
	}
	return s.publishDiagnosticsCurrentIndex(params.TextDocument.URI, content)
}

func applyContentChanges(source string, changes []textDocumentContentChangeEvent) (string, error) {
	if len(changes) == 0 {
		return source, nil
	}
	out := source
	for _, change := range changes {
		if change.Range == nil {
			out = change.Text
			continue
		}
		updated, err := applyRangeChange(out, *change.Range, change.Text)
		if err != nil {
			return "", err
		}
		out = updated
	}
	return out, nil
}

func applyRangeChange(source string, rng lspRange, replacement string) (string, error) {
	start, err := lspPositionToByteOffset(source, rng.Start)
	if err != nil {
		return "", err
	}
	end, err := lspPositionToByteOffset(source, rng.End)
	if err != nil {
		return "", err
	}
	if start > end {
		return "", fmt.Errorf("invalid change range: start after end")
	}
	return source[:start] + replacement + source[end:], nil
}

func lspPositionToByteOffset(source string, pos lspPosition) (int, error) {
	if pos.Line < 0 || pos.Character < 0 {
		return 0, fmt.Errorf("invalid position")
	}

	lineStart, lineText, err := lineTextAtIndex(source, pos.Line)
	if err != nil {
		return 0, err
	}
	return lineStart + utf16ColumnToByteOffset(lineText, pos.Character), nil
}

func lineTextAtIndex(source string, line int) (int, string, error) {
	if line < 0 {
		return 0, "", fmt.Errorf("line out of range")
	}
	offset := 0
	for current := 0; ; current++ {
		if current == line {
			end := strings.IndexByte(source[offset:], '\n')
			if end < 0 {
				lineText := source[offset:]
				lineText = strings.TrimSuffix(lineText, "\r")
				return offset, lineText, nil
			}
			lineText := source[offset : offset+end]
			lineText = strings.TrimSuffix(lineText, "\r")
			return offset, lineText, nil
		}

		next := strings.IndexByte(source[offset:], '\n')
		if next < 0 {
			return 0, "", fmt.Errorf("line out of range")
		}
		offset += next + 1
		if offset > len(source) {
			return 0, "", fmt.Errorf("line out of range")
		}
	}
}

func utf16ColumnToByteOffset(line string, column int) int {
	if column <= 0 {
		return 0
	}

	units := 0
	for i, r := range line {
		width := utf16RuneWidth(r)
		if units+width > column {
			return i
		}
		units += width
		if units == column {
			return i + utf8.RuneLen(r)
		}
	}
	return len(line)
}

func utf16ColumnToRuneIndex(line string, column int) int {
	if column <= 0 {
		return 0
	}
	units := 0
	runes := 0
	for _, r := range line {
		width := utf16RuneWidth(r)
		if units+width > column {
			return runes
		}
		units += width
		runes++
		if units == column {
			return runes
		}
	}
	return runes
}

func utf16RuneWidth(r rune) int {
	if r > 0xFFFF {
		return 2
	}
	return 1
}

type didCloseParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

func (s *Server) handleDidClose(raw json.RawMessage) error {
	params := didCloseParams{}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil
	}
	s.docs.Delete(params.TextDocument.URI)
	s.markDirtyURI(params.TextDocument.URI)
	if err := s.publishDiagnosticItems(params.TextDocument.URI, []lspDiagnostic{}); err != nil {
		return err
	}
	return s.refreshIndexIfStale()
}

type didSaveParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

func (s *Server) handleDidSave(raw json.RawMessage) error {
	params := didSaveParams{}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil
	}
	s.markDirtyURI(params.TextDocument.URI)
	if err := s.refreshIndexIfStale(); err != nil {
		return err
	}
	return s.publishWorkspaceDiagnosticsForSave(params.TextDocument.URI)
}

type completionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     lspPosition            `json:"position"`
}

type definitionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     lspPosition            `json:"position"`
}

type referencesContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

type referencesParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     lspPosition            `json:"position"`
	Context      referencesContext      `json:"context"`
}

type renameParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     lspPosition            `json:"position"`
	NewName      string                 `json:"newName"`
}

type formattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

type formattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Options      formattingOptions      `json:"options"`
}

type symbolOccurrence struct {
	URI     string
	Literal string
	Range   lspRange
}

type semanticTokensParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type codeActionContext struct {
	Diagnostics []lspDiagnostic `json:"diagnostics"`
}

type codeActionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        lspRange               `json:"range"`
	Context      codeActionContext      `json:"context"`
}

func (s *Server) handleCompletion(msg message) error {
	params := completionParams{}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, "invalid completion params")
	}
	if err := s.refreshIndexIfStale(); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}

	source, ok, err := s.documentSource(params.TextDocument.URI)
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	if !ok {
		return s.respond(msg.ID, []any{})
	}

	ns := s.state.index.NamespaceForURI(params.TextDocument.URI)
	prefix := prefixAtPosition(source, params.Position.Line, params.Position.Character)
	candidates := s.state.index.CompletionCandidates(ns, prefix)

	items := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, map[string]any{
			"label": candidate,
		})
	}

	return s.respond(msg.ID, items)
}

func (s *Server) handleDefinition(msg message) error {
	params := definitionParams{}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, "invalid definition params")
	}
	if err := s.refreshIndexIfStale(); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}

	source, ok, err := s.documentSource(params.TextDocument.URI)
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	if !ok {
		return s.respond(msg.ID, []any{})
	}

	symbol, ok := symbolAtPosition(source, params.Position.Line, params.Position.Character)
	if !ok {
		return s.respond(msg.ID, []any{})
	}

	ns := s.state.index.NamespaceForURI(params.TextDocument.URI)
	def, found := s.state.index.ResolveDefinition(ns, symbol)
	if !found {
		return s.respond(msg.ID, []any{})
	}

	location := map[string]any{
		"uri": def.URI,
		"range": lspRange{
			Start: toLSPPosition(def.Range.Start.Line, def.Range.Start.Column),
			End:   toLSPPosition(def.Range.End.Line, def.Range.End.Column),
		},
	}
	return s.respond(msg.ID, []any{location})
}

func (s *Server) handleReferences(msg message) error {
	params := referencesParams{}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, "invalid references params")
	}
	if err := s.refreshIndexIfStale(); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}

	source, ok, err := s.documentSource(params.TextDocument.URI)
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	if !ok {
		return s.respond(msg.ID, []any{})
	}
	symbol, ok := symbolAtPosition(source, params.Position.Line, params.Position.Character)
	if !ok {
		return s.respond(msg.ID, []any{})
	}

	currentNS := s.state.index.NamespaceForURI(params.TextDocument.URI)
	def, found := s.state.index.ResolveDefinition(currentNS, symbol)
	if !found {
		return s.respond(msg.ID, []any{})
	}

	occurrences, err := s.collectSymbolOccurrences(def, params.Context.IncludeDeclaration)
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	locations := make([]map[string]any, 0, len(occurrences))
	for _, occ := range occurrences {
		locations = append(locations, map[string]any{
			"uri":   occ.URI,
			"range": occ.Range,
		})
	}
	return s.respond(msg.ID, locations)
}

func (s *Server) handleRename(msg message) error {
	params := renameParams{}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, "invalid rename params")
	}
	newName := strings.TrimSpace(params.NewName)
	if !isValidRenameName(newName) {
		return s.respondError(msg.ID, lspErrorInvalidParams, "newName must be a valid symbol")
	}
	if err := s.refreshIndexIfStale(); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}

	source, ok, err := s.documentSource(params.TextDocument.URI)
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	if !ok {
		return s.respond(msg.ID, map[string]any{
			"changes": map[string]any{},
		})
	}
	symbol, selectedRange, ok := symbolRangeAtPosition(source, params.Position.Line, params.Position.Character)
	if !ok {
		return s.respond(msg.ID, map[string]any{
			"changes": map[string]any{},
		})
	}

	currentNS := s.state.index.NamespaceForURI(params.TextDocument.URI)
	def, found := s.state.index.ResolveDefinition(currentNS, symbol)
	if !found {
		return s.respond(msg.ID, map[string]any{
			"changes": map[string]any{},
		})
	}
	if strings.HasPrefix(def.URI, "stdlib://") {
		return s.respondError(msg.ID, lspErrorInvalidParams, "cannot rename stdlib symbol")
	}
	if s.state.index.IsGenerationDocument(def.URI) {
		return s.respondError(msg.ID, lspErrorInvalidParams, "cannot rename immutable generation dependency symbol")
	}

	occurrences, err := s.collectSymbolOccurrences(def, true)
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	selectedOccurrence := false
	selectedURI := toolindex.NormalizeURI(params.TextDocument.URI)
	for _, occ := range occurrences {
		if toolindex.NormalizeURI(occ.URI) == selectedURI && occ.Range == selectedRange {
			selectedOccurrence = true
			break
		}
	}
	if !selectedOccurrence {
		return s.respond(msg.ID, map[string]any{"changes": map[string]any{}})
	}
	changes := make(map[string][]map[string]any)
	for _, occ := range occurrences {
		if s.state.index.IsGenerationDocument(occ.URI) {
			return s.respondError(msg.ID, lspErrorInvalidParams, "cannot rename symbol referenced by immutable generation dependency")
		}
		replacement := renameReplacement(occ.Literal, def.Name, newName)
		changes[occ.URI] = append(changes[occ.URI], map[string]any{
			"range":   occ.Range,
			"newText": replacement,
		})
	}

	changeAny := make(map[string]any, len(changes))
	uris := make([]string, 0, len(changes))
	for uri := range changes {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	for _, uri := range uris {
		edits := changes[uri]
		sort.Slice(edits, func(i, j int) bool {
			lhs := edits[i]["range"].(lspRange)
			rhs := edits[j]["range"].(lspRange)
			if lhs.Start.Line != rhs.Start.Line {
				return lhs.Start.Line < rhs.Start.Line
			}
			return lhs.Start.Character < rhs.Start.Character
		})
		items := make([]any, 0, len(edits))
		for _, edit := range edits {
			items = append(items, edit)
		}
		changeAny[uri] = items
	}

	return s.respond(msg.ID, map[string]any{
		"changes": changeAny,
	})
}

func (s *Server) handleFormatting(msg message) error {
	params := formattingParams{}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, "invalid formatting params")
	}
	if err := s.refreshIndexIfStale(); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}

	source, ok, err := s.documentSource(params.TextDocument.URI)
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	if !ok {
		return s.respond(msg.ID, []any{})
	}

	if s.state.index.IsGenerationDocument(params.TextDocument.URI) {
		return s.respond(msg.ID, []any{})
	}
	formatted, _, err := toolingformat.Format(source, toolingformat.Options{
		TabSize:      params.Options.TabSize,
		InsertSpaces: params.Options.InsertSpaces,
	})
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	if formatted == source {
		return s.respond(msg.ID, []any{})
	}

	edit := map[string]any{
		"range":   fullDocumentRange(source),
		"newText": formatted,
	}
	return s.respond(msg.ID, []any{edit})
}

func (s *Server) handleHover(msg message) error {
	params := completionParams{}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, "invalid hover params")
	}
	if err := s.refreshIndexIfStale(); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}

	source, ok, err := s.documentSource(params.TextDocument.URI)
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	if !ok {
		return s.respond(msg.ID, nil)
	}
	symbol, ok := symbolAtPosition(source, params.Position.Line, params.Position.Character)
	if !ok {
		return s.respond(msg.ID, nil)
	}

	currentNS := s.state.index.NamespaceForURI(params.TextDocument.URI)
	hover, ok := s.state.index.ResolveHover(currentNS, symbol)
	if !ok {
		return s.respond(msg.ID, nil)
	}

	location := "<unknown>"
	if hover.URI != "" && hover.Range.Start.Line > 0 {
		location = fmt.Sprintf("%s:%d", hover.URI, hover.Range.Start.Line)
	}

	lines := []string{
		fmt.Sprintf("`%s`", hover.Qualified),
		fmt.Sprintf("- kind: `%s`", hover.Kind),
		fmt.Sprintf("- location: `%s`", location),
	}
	if strings.TrimSpace(hover.Doc) != "" {
		lines = append(lines, "", hover.Doc)
	}

	return s.respond(msg.ID, map[string]any{
		"contents": map[string]any{
			"kind":  "markdown",
			"value": strings.Join(lines, "\n"),
		},
	})
}

func (s *Server) handleDocumentSymbol(msg message) error {
	params := struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
	}{}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, "invalid documentSymbol params")
	}
	if err := s.refreshIndexIfStale(); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}

	symbols := s.state.index.DocumentSymbols(params.TextDocument.URI)
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].Range.Start.Line != symbols[j].Range.Start.Line {
			return symbols[i].Range.Start.Line < symbols[j].Range.Start.Line
		}
		return symbols[i].Range.Start.Column < symbols[j].Range.Start.Column
	})

	items := make([]map[string]any, 0, len(symbols))
	for _, sym := range symbols {
		items = append(items, map[string]any{
			"name": sym.Name,
			"kind": symbolKindFromDef(sym.Kind),
			"range": lspRange{
				Start: toLSPPosition(sym.Range.Start.Line, sym.Range.Start.Column),
				End:   toLSPPosition(sym.Range.End.Line, sym.Range.End.Column),
			},
			"selectionRange": lspRange{
				Start: toLSPPosition(sym.Range.Start.Line, sym.Range.Start.Column),
				End:   toLSPPosition(sym.Range.End.Line, sym.Range.End.Column),
			},
		})
	}
	return s.respond(msg.ID, items)
}

func (s *Server) handleWorkspaceSymbol(msg message) error {
	params := struct {
		Query string `json:"query"`
	}{}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, "invalid workspace/symbol params")
	}
	if err := s.refreshIndexIfStale(); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}

	symbols := s.state.index.WorkspaceSymbols(params.Query)
	items := make([]map[string]any, 0, len(symbols))
	for _, sym := range symbols {
		items = append(items, map[string]any{
			"name":          sym.Name,
			"kind":          symbolKindFromDef(sym.Kind),
			"containerName": sym.Namespace,
			"location": map[string]any{
				"uri": sym.URI,
				"range": lspRange{
					Start: toLSPPosition(sym.Range.Start.Line, sym.Range.Start.Column),
					End:   toLSPPosition(sym.Range.End.Line, sym.Range.End.Column),
				},
			},
		})
	}
	return s.respond(msg.ID, items)
}

func (s *Server) handleSemanticTokensFull(msg message) error {
	params := semanticTokensParams{}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, "invalid semanticTokens params")
	}
	if err := s.refreshIndexIfStale(); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	source, ok, err := s.documentSource(params.TextDocument.URI)
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	if !ok {
		return s.respond(msg.ID, map[string]any{
			"data": []int{},
		})
	}
	data := encodeSemanticTokens(buildSemanticTokens(params.TextDocument.URI, source))
	return s.respond(msg.ID, map[string]any{
		"data": data,
	})
}

func (s *Server) handleCodeAction(msg message) error {
	params := codeActionParams{}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, "invalid codeAction params")
	}
	if err := s.refreshIndexIfStale(); err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}

	source, ok, err := s.documentSource(params.TextDocument.URI)
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	if !ok {
		return s.respond(msg.ID, []any{})
	}
	if s.state.index.IsGenerationDocument(params.TextDocument.URI) {
		return s.respond(msg.ID, []any{})
	}

	actions := make([]map[string]any, 0)
	actions = append(actions, s.buildImportCodeActions(params.TextDocument.URI, source, params.Range)...)
	unusedActions, err := s.buildUnusedDefinitionCodeActions(params.TextDocument.URI, source, params.Range)
	if err != nil {
		return s.respondError(msg.ID, lspErrorInvalidParams, err.Error())
	}
	actions = append(actions, unusedActions...)
	if len(actions) == 0 {
		return s.respond(msg.ID, []any{})
	}

	out := make([]any, 0, len(actions))
	for _, action := range actions {
		out = append(out, action)
	}
	return s.respond(msg.ID, out)
}

func (s *Server) buildImportCodeActions(uri, source string, target lspRange) []map[string]any {
	candidates := findImportActionCandidates(source)
	actions := make([]map[string]any, 0)
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		tokenWidth := utf8.RuneCountInString(candidate.literal)
		if tokenWidth <= 0 {
			tokenWidth = 1
		}
		editRange := lspRange{
			Start: toLSPPosition(candidate.line, candidate.column),
			End:   toLSPPosition(candidate.line, candidate.column+tokenWidth),
		}
		if !rangeIntersects(editRange, target) {
			continue
		}
		key := fmt.Sprintf("%d:%d:%d:%d", editRange.Start.Line, editRange.Start.Character, editRange.End.Line, editRange.End.Character)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		actions = append(actions, map[string]any{
			"title": "Fix import argument (string literal)",
			"kind":  "quickfix",
			"edit": map[string]any{
				"changes": map[string]any{
					uri: []any{
						map[string]any{
							"range":   editRange,
							"newText": fmt.Sprintf("%q", candidate.literal),
						},
					},
				},
			},
		})
	}
	return actions
}

func (s *Server) buildUnusedDefinitionCodeActions(uri, source string, target lspRange) ([]map[string]any, error) {
	if s.state == nil || s.state.index == nil {
		return []map[string]any{}, nil
	}
	currentNS := s.state.index.NamespaceForURI(uri)
	symbols := s.state.index.DocumentSymbols(uri)
	if len(symbols) == 0 {
		return []map[string]any{}, nil
	}

	spansByName := topLevelDefinitionSpans(uri, source)
	actions := make([]map[string]any, 0)

	for _, sym := range symbols {
		def, ok := s.state.index.ResolveDefinition(currentNS, sym.Name)
		if !ok || def.URI != uri {
			continue
		}
		occurrences, err := s.collectSymbolOccurrences(def, true)
		if err != nil {
			return nil, err
		}
		if len(occurrences) > 1 {
			continue
		}

		formSpan, ok := findDefinitionSpan(spansByName[sym.Name], sym.Range.Start.Line)
		if !ok {
			continue
		}
		editRange := lspRange{
			Start: toLSPPosition(formSpan.Start.Line, formSpan.Start.Column),
			End:   toLSPPosition(formSpan.End.Line, formSpan.End.Column),
		}
		if !rangeIntersects(editRange, target) {
			continue
		}

		actions = append(actions, map[string]any{
			"title": fmt.Sprintf("Remove unused definition '%s'", sym.Name),
			"kind":  "source.cleanup",
			"edit": map[string]any{
				"changes": map[string]any{
					uri: []any{
						map[string]any{
							"range":   editRange,
							"newText": "",
						},
					},
				},
			},
		})
	}

	sort.Slice(actions, func(i, j int) bool {
		return actions[i]["title"].(string) < actions[j]["title"].(string)
	})
	return actions, nil
}

func topLevelDefinitionSpans(path, source string) map[string][]span.Span {
	parsed := frontend.Parse(path, source)
	out := make(map[string][]span.Span)
	for idx, value := range parsed.LoweredForms {
		list, ok := value.(*coll.List)
		if !ok || len(list.Items) < 2 {
			continue
		}
		head, ok := types.SymbolName(list.Items[0])
		if !ok {
			continue
		}
		switch head {
		case "def", "defn", "defmacro", "defonce":
		default:
			continue
		}
		name, ok := types.SymbolName(list.Items[1])
		if !ok || name == "" {
			continue
		}
		if idx < len(parsed.Forms) {
			out[name] = append(out[name], parsed.Forms[idx].Span)
			continue
		}
		out[name] = append(out[name], span.Span{})
	}
	return out
}

func findDefinitionSpan(spans []span.Span, line int) (span.Span, bool) {
	for _, sp := range spans {
		if sp.Start.Line == line {
			return sp, true
		}
	}
	if len(spans) == 0 {
		return span.Span{}, false
	}
	return spans[0], true
}

func looksLikeImportSymbol(v string) bool {
	v = strings.TrimSpace(v)
	return frontend.IsSingleIdentifierToken(v)
}

type importActionCandidate struct {
	line    int
	column  int
	literal string
}

type importActionFinder struct {
	tokens     []lexer.Token
	pos        int
	candidates []importActionCandidate
}

func findImportActionCandidates(source string) []importActionCandidate {
	tokens, err := frontend.Tokenize(source)
	if err != nil || len(tokens) == 0 {
		return []importActionCandidate{}
	}
	f := &importActionFinder{
		tokens:     tokens,
		candidates: make([]importActionCandidate, 0),
	}
	for !f.done() {
		f.parseForm(true)
	}
	return f.candidates
}

func (f *importActionFinder) done() bool {
	return f.pos >= len(f.tokens) || f.peek().Type == lexer.EOF
}

func (f *importActionFinder) peek() lexer.Token {
	if f.pos >= len(f.tokens) {
		return lexer.Token{Type: lexer.EOF}
	}
	return f.tokens[f.pos]
}

func (f *importActionFinder) next() {
	if f.pos < len(f.tokens) {
		f.pos++
	}
}

func (f *importActionFinder) parseForm(active bool) {
	tok := f.peek()
	switch tok.Type {
	case lexer.EOF:
		return
	case lexer.LPAREN:
		f.parseList(active)
	case lexer.LBRACKET:
		f.parseDelimited(active, lexer.LBRACKET, lexer.RBRACKET)
	case lexer.LBRACE:
		f.parseDelimited(active, lexer.LBRACE, lexer.RBRACE)
	case lexer.HASHSET:
		f.parseDelimited(active, lexer.HASHSET, lexer.RBRACE)
	case lexer.HASHFN:
		f.parseDelimited(active, lexer.HASHFN, lexer.RPAREN)
	case lexer.QUOTE, lexer.QUASIQUOTE, lexer.VAR_QUOTE:
		f.next()
		f.parseForm(false)
	case lexer.DISCARD:
		f.next()
		f.parseForm(false)
	case lexer.METADATA:
		// metadata form itself is not executable code, but the target form is.
		f.next()
		f.parseForm(false)
		f.parseForm(active)
	case lexer.UNQUOTE, lexer.SPLICE, lexer.DEREF:
		f.next()
		f.parseForm(active)
	default:
		f.next()
	}
}

func (f *importActionFinder) parseDelimited(active bool, open, close lexer.TokenType) {
	if f.peek().Type == open {
		f.next()
	}
	for !f.done() && f.peek().Type != close {
		f.parseForm(active)
	}
	if f.peek().Type == close {
		f.next()
	}
}

func (f *importActionFinder) parseList(active bool) {
	if f.peek().Type == lexer.LPAREN {
		f.next()
	}
	itemCount := 0
	importHead := false
	importArgToken := lexer.Token{}
	hasImportArg := false

	for !f.done() && f.peek().Type != lexer.RPAREN {
		tok := f.peek()
		if itemCount == 0 && tok.Type == lexer.IDENT && tok.Literal == "import" {
			importHead = true
		}
		if itemCount == 1 && tok.Type == lexer.IDENT {
			importArgToken = tok
			hasImportArg = true
		}
		f.parseForm(active)
		itemCount++
	}
	if f.peek().Type == lexer.RPAREN {
		f.next()
	}
	if active && importHead && itemCount == 2 && hasImportArg && looksLikeImportSymbol(importArgToken.Literal) {
		f.candidates = append(f.candidates, importActionCandidate{
			line:    importArgToken.Line,
			column:  importArgToken.Column,
			literal: importArgToken.Literal,
		})
	}
}

func rangeIntersects(a, b lspRange) bool {
	if lessPosition(a.End, b.Start) {
		return false
	}
	if lessPosition(b.End, a.Start) {
		return false
	}
	return true
}

func lessPosition(a, b lspPosition) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Character < b.Character
}

type semanticToken struct {
	line      int
	start     int
	length    int
	tokenType int
	modifier  int
}

var semanticTokenTypes = []string{
	"namespace",
	"function",
	"macro",
	"keyword",
	"string",
	"number",
	"comment",
	"operator",
	"variable",
}

const (
	semanticNamespace = iota
	semanticFunction
	semanticMacro
	semanticKeyword
	semanticString
	semanticNumber
	semanticComment
	semanticOperator
	semanticVariable
)

func buildSemanticTokens(path, source string) []semanticToken {
	parsed := frontend.Parse(path, source)
	if len(parsed.Diagnostics) > 0 {
		return []semanticToken{}
	}

	tokens := make([]semanticToken, 0, len(parsed.Tokens))
	seenComments := make(map[string]struct{})
	for _, tok := range parsed.Tokens {
		for _, trivia := range tok.LeadingTrivia {
			if trivia.Kind != "comment" {
				continue
			}
			key := spanKey(trivia.Span)
			if _, exists := seenComments[key]; exists {
				continue
			}
			seenComments[key] = struct{}{}
			tokens = append(tokens, semanticTokensFromSpan(source, trivia.Span, semanticComment)...)
		}
		for _, trivia := range tok.TrailingTrivia {
			if trivia.Kind != "comment" {
				continue
			}
			key := spanKey(trivia.Span)
			if _, exists := seenComments[key]; exists {
				continue
			}
			seenComments[key] = struct{}{}
			tokens = append(tokens, semanticTokensFromSpan(source, trivia.Span, semanticComment)...)
		}

		tt, ok := classifySemanticToken(tok.Kind, tok.Lexeme)
		if !ok {
			continue
		}
		tokens = append(tokens, semanticTokensFromSpan(source, tok.Span, tt)...)
	}

	sort.Slice(tokens, func(i, j int) bool {
		if tokens[i].line != tokens[j].line {
			return tokens[i].line < tokens[j].line
		}
		if tokens[i].start != tokens[j].start {
			return tokens[i].start < tokens[j].start
		}
		if tokens[i].length != tokens[j].length {
			return tokens[i].length < tokens[j].length
		}
		return tokens[i].tokenType < tokens[j].tokenType
	})
	return tokens
}

func spanKey(sp span.Span) string {
	return fmt.Sprintf("%s:%d:%d:%d:%d", sp.File, sp.Start.Line, sp.Start.Column, sp.End.Line, sp.End.Column)
}

func semanticTokensFromSpan(source string, sp span.Span, tokenType int) []semanticToken {
	start := sourceRunePositionOffset(source, sp.Start)
	end := sourceRunePositionOffset(source, sp.End)
	tokens := make([]semanticToken, 0, max(1, sp.End.Line-sp.Start.Line+1))
	for start < end {
		lineEnd := end
		if newline := strings.IndexByte(source[start:end], '\n'); newline >= 0 {
			lineEnd = start + newline
		}
		contentEnd := lineEnd
		if contentEnd > start && source[contentEnd-1] == '\r' {
			contentEnd--
		}
		first := byteOffsetToLSPPosition(source, start)
		last := byteOffsetToLSPPosition(source, contentEnd)
		if last.Character > first.Character {
			tokens = append(tokens, semanticToken{line: first.Line, start: first.Character, length: last.Character - first.Character, tokenType: tokenType})
		}
		start = lineEnd + 1
	}
	return tokens
}

func sourceRunePositionOffset(source string, pos span.Position) int {
	start, line, err := lineTextAtIndex(source, max(0, pos.Line-1))
	if err != nil {
		return len(source)
	}
	column := max(0, pos.Column-1)
	for offset := range line {
		if column == 0 {
			return start + offset
		}
		column--
	}
	return start + len(line)
}

func byteOffsetToLSPPosition(source string, offset int) lspPosition {
	pos := toolindex.PositionFromOffset(source, offset)
	return toLSPPosition(pos.Line, pos.Column)
}

func classifySemanticToken(kind, lexeme string) (int, bool) {
	switch kind {
	case "string", "regex", "char":
		return semanticString, true
	case "number":
		return semanticNumber, true
	case "keyword":
		return semanticKeyword, true
	case "ident":
		if isSemanticKeyword(lexeme) {
			return semanticKeyword, true
		}
		if isOperatorSymbol(lexeme) {
			return semanticOperator, true
		}
		return semanticVariable, true
	default:
		return 0, false
	}
}

func isSemanticKeyword(symbol string) bool {
	switch symbol {
	case "if", "do", "let", "loop", "recur", "throw", "try", "catch", "finally",
		"fn", "def", "defn", "defmacro", "defonce", "quote", "syntax-quote", "quasiquote",
		"unquote", "unquote-splicing", "var", "set!", "import", "require", "ns", "in-ns",
		"when", "when-let", "if-let", "doseq", "for":
		return true
	default:
		return false
	}
}

func isOperatorSymbol(symbol string) bool {
	switch symbol {
	case "+", "-", "*", "/", "=", "not=", ">", ">=", "<", "<=", "and", "or", "not":
		return true
	default:
		return false
	}
}

func encodeSemanticTokens(tokens []semanticToken) []int {
	if len(tokens) == 0 {
		return []int{}
	}
	out := make([]int, 0, len(tokens)*5)
	prevLine := 0
	prevStart := 0
	for i, tok := range tokens {
		deltaLine := tok.line - prevLine
		deltaStart := tok.start
		if i > 0 && deltaLine == 0 {
			deltaStart = tok.start - prevStart
		}
		out = append(out, deltaLine, deltaStart, tok.length, tok.tokenType, tok.modifier)
		prevLine = tok.line
		prevStart = tok.start
	}
	return out
}

func (s *Server) publishDiagnosticsCurrentIndex(uri, source string) error {
	filePath := uri
	if path, err := toolindex.FileURIToPath(uri); err == nil {
		filePath = path
	}
	currentNS := "user"
	if s.state.index != nil {
		currentNS = s.state.index.NamespaceForURI(uri)
	}
	issues := toolinglint.AnalyzeWithOptions(filePath, source, toolinglint.AnalyzeOptions{
		Index:                 s.state.index,
		CurrentNamespace:      currentNS,
		EnableUndefinedSymbol: true,
	})
	diagnostics := make([]lspDiagnostic, 0, len(issues))
	for _, issue := range issues {
		diagnostics = append(diagnostics, lspDiagnostic{
			Range: lspRange{
				Start: toLSPPosition(issue.Line, issue.Column),
				End:   toLSPPosition(issue.Line, issue.Column+1),
			},
			Severity: lspDiagnosticSeverityError,
			Source:   "doggo-lint",
			Message:  issue.Message,
		})
	}
	return s.publishDiagnosticItems(uri, diagnostics)
}

func (s *Server) publishWorkspaceDiagnosticsForSave(savedURI string) error {
	targets := s.diagnosticsTargetsForSave(savedURI)
	for _, uri := range targets {
		source, ok, err := s.documentSource(uri)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if err := s.publishDiagnosticsCurrentIndex(uri, source); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) diagnosticsTargetsForSave(savedURI string) []string {
	set := make(map[string]struct{})
	add := func(uri string) {
		if strings.TrimSpace(uri) == "" {
			return
		}
		set[uri] = struct{}{}
	}

	add(savedURI)
	if s.docs != nil {
		for _, uri := range s.docs.URIs() {
			add(uri)
		}
	}
	if s.state == nil || s.state.index == nil {
		return sortedURISet(set)
	}

	savedNS := s.state.index.NamespaceForURI(savedURI)
	relatedNamespaces := dependentNamespaces(s.state.index, savedNS)
	for uri, nsName := range s.state.index.NamespaceByURI {
		if _, ok := relatedNamespaces[nsName]; ok {
			add(uri)
		}
	}

	return sortedURISet(set)
}

func dependentNamespaces(idx *toolindex.WorkspaceIndex, rootNS string) map[string]struct{} {
	out := make(map[string]struct{})
	rootNS = strings.TrimSpace(rootNS)
	if idx == nil || rootNS == "" {
		return out
	}
	queue := []string{rootNS}
	out[rootNS] = struct{}{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for nsName, info := range idx.Namespaces {
			if _, seen := out[nsName]; seen {
				continue
			}
			if namespaceDependsOn(info, current) {
				out[nsName] = struct{}{}
				queue = append(queue, nsName)
			}
		}
	}
	return out
}

func namespaceDependsOn(info *toolindex.NamespaceInfo, targetNS string) bool {
	if info == nil || strings.TrimSpace(targetNS) == "" {
		return false
	}
	for _, ns := range info.Aliases {
		if ns == targetNS {
			return true
		}
	}
	for _, ns := range info.Refers {
		if ns == targetNS {
			return true
		}
	}
	for _, ns := range info.ReferAll {
		if ns == targetNS {
			return true
		}
	}
	return false
}

func sortedURISet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for uri := range set {
		out = append(out, uri)
	}
	sort.Strings(out)
	return out
}

func (s *Server) publishIncrementalSyncFailure(uri string, cause error) error {
	msg := "incremental sync failed; document state may be stale. retry edit or reopen file"
	if cause != nil {
		msg = msg + ": " + cause.Error()
	}
	diag := lspDiagnostic{
		Range: lspRange{
			Start: lspPosition{Line: 0, Character: 0},
			End:   lspPosition{Line: 0, Character: 1},
		},
		Severity: lspDiagnosticSeverityError,
		Source:   "doggo-lsp",
		Message:  msg,
	}
	return s.publishDiagnosticItems(uri, []lspDiagnostic{diag})
}

func (s *Server) publishDiagnosticItems(uri string, diagnostics []lspDiagnostic) error {
	params := map[string]any{
		"uri":         uri,
		"diagnostics": diagnostics,
	}
	return s.notify(lspMethodPublishDiagnostics, params)
}

func (s *Server) markDirtyURI(uri string) {
	if strings.TrimSpace(uri) == "" || s.state == nil {
		return
	}
	if path, err := toolindex.FileURIToPath(uri); err == nil {
		abs, absErr := filepath.Abs(path)
		if absErr == nil {
			s.state.MarkDirty(filepath.Clean(abs))
			return
		}
		s.state.MarkDirty(filepath.Clean(path))
		return
	}
	s.state.MarkDirty(uri)
}

func (s *Server) refreshIndexIfStale() error {
	if strings.TrimSpace(s.projectDir) == "" {
		return fmt.Errorf("project root is not initialized")
	}
	if s.state == nil {
		s.state = NewWorkspaceState()
		if s.session != nil {
			s.session.workspace = s.state
		}
	}

	if s.state.index == nil {
		return s.rebuildIndexFull()
	}
	if err := s.state.index.ValidateGenerationSources(); err != nil {
		return err
	}

	stamps, err := collectWorkspaceFileStamps(s.projectDir)
	if err != nil {
		return s.rebuildIndexFull()
	}

	changedPaths := make(map[string]struct{})
	changedURIs := make(map[string]struct{})

	for path, stamp := range stamps {
		if prev, ok := s.state.fileStamps[path]; !ok || prev.size != stamp.size || !prev.modTime.Equal(stamp.modTime) {
			changedPaths[path] = struct{}{}
		}
	}
	for path := range s.state.fileStamps {
		if _, ok := stamps[path]; !ok {
			changedPaths[path] = struct{}{}
		}
	}
	for dirty := range s.state.dirtyFiles {
		if strings.HasPrefix(dirty, "stdlib://") {
			changedURIs[dirty] = struct{}{}
			continue
		}
		if strings.HasPrefix(dirty, "file://") {
			if path, err := toolindex.FileURIToPath(dirty); err == nil {
				abs, absErr := filepath.Abs(path)
				if absErr == nil {
					changedPaths[filepath.Clean(abs)] = struct{}{}
				} else {
					changedPaths[filepath.Clean(path)] = struct{}{}
				}
				continue
			}
		}
		if filepath.IsAbs(dirty) {
			changedPaths[filepath.Clean(dirty)] = struct{}{}
			continue
		}
		changedURIs[dirty] = struct{}{}
	}

	if len(changedPaths)+len(changedURIs) == 0 {
		return nil
	}
	if len(changedPaths)+len(changedURIs) > lspMaxIncrementalChanges {
		return s.rebuildIndexFull()
	}

	if err := s.applyIncrementalChanges(changedPaths, changedURIs); err != nil {
		return s.rebuildIndexFull()
	}

	s.state.fileStamps = stamps
	s.state.dirtyFiles = make(map[string]struct{})
	return nil
}

func (s *Server) rebuildIndexFull() error {
	var generationSources []*project.GenerationSource
	if s.generationSession != nil {
		generationSources = s.generationSession.Config.GenerationSources
	}
	idx, err := toolindex.BuildWorkspaceIndex(toolindex.BuildOptions{
		ProjectRoot:       s.projectDir,
		Overlay:           s.docs.FilePathOverlay(),
		GenerationSources: generationSources,
	})
	if err != nil {
		return err
	}

	stamps, err := collectWorkspaceFileStamps(s.projectDir)
	if err != nil {
		return err
	}

	s.state.index = idx
	s.state.fileStamps = stamps
	s.state.dirtyFiles = make(map[string]struct{})
	return nil
}

func (s *Server) applyIncrementalChanges(changedPaths map[string]struct{}, changedURIs map[string]struct{}) error {
	paths := make([]string, 0, len(changedPaths))
	for path := range changedPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		uri := toolindex.PathToFileURI(path)
		if _, err, retained := s.state.index.GenerationDocumentSource(uri); retained {
			if err != nil {
				return err
			}
			continue
		}
		if doc, ok := s.docs.Get(uri); ok {
			if err := s.state.index.IndexDocument(uri, doc.Text); err != nil {
				return err
			}
			continue
		}

		data, err := os.ReadFile(path)
		if err == nil {
			if err := s.state.index.IndexDocument(uri, string(data)); err != nil {
				return err
			}
			continue
		}
		if errors.Is(err, os.ErrNotExist) {
			if err := s.state.index.RemoveDocument(uri); err != nil {
				return err
			}
			continue
		}
		return err
	}

	uris := make([]string, 0, len(changedURIs))
	for uri := range changedURIs {
		uris = append(uris, uri)
	}
	sort.Strings(uris)

	for _, uri := range uris {
		if doc, ok := s.docs.Get(uri); ok {
			if err := s.state.index.IndexDocument(uri, doc.Text); err != nil {
				return err
			}
			continue
		}
		if err := s.state.index.RemoveDocument(uri); err != nil {
			return err
		}
	}

	return nil
}

func collectWorkspaceFileStamps(projectDir string) (map[string]fileStamp, error) {
	files, err := toolindex.CollectWorkspaceFiles(projectDir)
	if err != nil {
		return nil, err
	}
	stamps := make(map[string]fileStamp, len(files))
	for _, path := range files {
		st, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		stamps[path] = fileStamp{
			modTime: st.ModTime().UTC(),
			size:    st.Size(),
		}
	}
	return stamps, nil
}

func (s *Server) respond(id *json.RawMessage, result any) error {
	if result == nil {
		// JSON-RPC response must include either result or error.
		// Keep explicit null so the "result" property is always present.
		result = json.RawMessage("null")
	}
	resp := message{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Result:  result,
	}
	return writeMessage(s.out, resp)
}

func (s *Server) respondError(id *json.RawMessage, code int, msg string) error {
	resp := message{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error: &responseError{
			Code:    code,
			Message: msg,
		},
	}
	return writeMessage(s.out, resp)
}

func (s *Server) notify(method string, params any) error {
	msg := message{
		JSONRPC: jsonRPCVersion,
		Method:  method,
		Params:  mustMarshalRaw(params),
	}
	return writeMessage(s.out, msg)
}

func mustMarshalRaw(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(data)
}

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

type lspDiagnostic struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity"`
	Source   string   `json:"source"`
	Message  string   `json:"message"`
}

func symbolKindFromDef(kind string) int {
	switch kind {
	case "defn", "defmacro":
		return lspSymbolKindFunction
	default:
		return lspSymbolKindVariable
	}
}

func toLSPPosition(line, column int) lspPosition {
	if line <= 0 {
		line = 1
	}
	if column <= 0 {
		column = 1
	}
	return lspPosition{
		Line:      line - 1,
		Character: column - 1,
	}
}

func resolveProjectRoot(params initializeParams) (string, error) {
	if strings.TrimSpace(params.RootURI) != "" {
		path, err := toolindex.FileURIToPath(params.RootURI)
		if err == nil {
			return filepath.Abs(path)
		}
	}
	if strings.TrimSpace(params.RootPath) != "" {
		return filepath.Abs(params.RootPath)
	}
	return os.Getwd()
}

func (s *Server) documentSource(uri string) (string, bool, error) {
	if s.state != nil && s.state.index != nil {
		if content, err, retained := s.state.index.GenerationDocumentSource(uri); retained {
			return content, err == nil, err
		}
	}
	if doc, ok := s.docs.Get(uri); ok {
		return doc.Text, true, nil
	}
	if strings.HasPrefix(uri, "stdlib://") {
		path := strings.TrimPrefix(uri, "stdlib://")
		content, err := fs.ReadFile(stdlib.StdlibFS, path)
		if err != nil {
			return "", false, nil
		}
		return string(content), true, nil
	}
	path, err := toolindex.FileURIToPath(uri)
	if err != nil {
		return "", false, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", false, nil
	}
	return string(content), true, nil
}

func prefixAtPosition(source string, line, character int) string {
	_, lineText, err := lineTextAtIndex(source, line)
	if err != nil {
		return ""
	}
	character = utf16ColumnToRuneIndex(lineText, character)
	runes := []rune(lineText)
	if character <= 0 || len(runes) == 0 {
		return ""
	}
	if character > len(runes) {
		character = len(runes)
	}

	i := character - 1
	for i >= 0 && isSymbolRune(runes[i]) {
		i--
	}
	return string(runes[i+1 : character])
}

func symbolAtPosition(source string, line, character int) (string, bool) {
	symbol, _, ok := symbolRangeAtPosition(source, line, character)
	return symbol, ok
}

func symbolRangeAtPosition(source string, line, character int) (string, lspRange, bool) {
	lineStart, lineText, err := lineTextAtIndex(source, line)
	if err != nil {
		return "", lspRange{}, false
	}
	character = utf16ColumnToRuneIndex(lineText, character)
	runes := []rune(lineText)
	if len(runes) == 0 {
		return "", lspRange{}, false
	}

	if character < 0 {
		return "", lspRange{}, false
	}
	if character >= len(runes) {
		character = len(runes) - 1
	}

	if !isSymbolRune(runes[character]) {
		if character > 0 && isSymbolRune(runes[character-1]) {
			character--
		} else {
			return "", lspRange{}, false
		}
	}

	left := character
	for left > 0 && isSymbolRune(runes[left-1]) {
		left--
	}
	right := character + 1
	for right < len(runes) && isSymbolRune(runes[right]) {
		right++
	}

	token := string(runes[left:right])
	if token == "" || strings.HasPrefix(token, ":") {
		return "", lspRange{}, false
	}
	return token, lspRange{
		Start: byteOffsetToLSPPosition(source, lineStart+len(string(runes[:left]))),
		End:   byteOffsetToLSPPosition(source, lineStart+len(string(runes[:right]))),
	}, true
}

func (s *Server) collectSymbolOccurrences(def toolindex.SymbolDef, includeDeclaration bool) ([]symbolOccurrence, error) {
	if s.state == nil || s.state.index == nil {
		return []symbolOccurrence{}, nil
	}

	uris := s.workspaceDocumentURIs()
	occurrences := make([]symbolOccurrence, 0)

	for _, uri := range uris {
		source, ok, err := s.documentSource(uri)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		candidates := s.referenceCandidatesForURI(uri, def)
		if len(candidates) == 0 {
			continue
		}

		tokens, err := frontend.Tokenize(source)
		if err != nil {
			continue
		}
		collector := newOccurrenceCollector(
			uri,
			source,
			tokens,
			s.state.index,
			s.state.index.NamespaceForURI(uri),
			def,
			candidates,
			includeDeclaration,
		)
		occurrences = append(occurrences, collector.collect()...)
	}

	sort.Slice(occurrences, func(i, j int) bool {
		if occurrences[i].URI != occurrences[j].URI {
			return occurrences[i].URI < occurrences[j].URI
		}
		if occurrences[i].Range.Start.Line != occurrences[j].Range.Start.Line {
			return occurrences[i].Range.Start.Line < occurrences[j].Range.Start.Line
		}
		return occurrences[i].Range.Start.Character < occurrences[j].Range.Start.Character
	})
	return occurrences, nil
}

type occurrenceCollector struct {
	uri                string
	source             string
	tokens             []lexer.Token
	pos                int
	idx                *toolindex.WorkspaceIndex
	currentNS          string
	target             toolindex.SymbolDef
	candidates         map[string]struct{}
	includeDeclaration bool
	occurrences        []symbolOccurrence
	seen               map[string]struct{}
	quasiquoteDepth    int
}

type occurrenceScope struct {
	parent  *occurrenceScope
	symbols map[string]struct{}
}

func newOccurrenceCollector(
	uri string,
	source string,
	tokens []lexer.Token,
	idx *toolindex.WorkspaceIndex,
	currentNS string,
	target toolindex.SymbolDef,
	candidates map[string]struct{},
	includeDeclaration bool,
) *occurrenceCollector {
	return &occurrenceCollector{
		uri:                uri,
		source:             source,
		tokens:             tokens,
		idx:                idx,
		currentNS:          currentNS,
		target:             target,
		candidates:         candidates,
		includeDeclaration: includeDeclaration,
		occurrences:        make([]symbolOccurrence, 0),
		seen:               make(map[string]struct{}),
	}
}

func newOccurrenceScope(parent *occurrenceScope) *occurrenceScope {
	return &occurrenceScope{
		parent:  parent,
		symbols: make(map[string]struct{}),
	}
}

func (s *occurrenceScope) define(name string) {
	if s == nil || name == "" || name == "&" {
		return
	}
	s.symbols[name] = struct{}{}
}

func (s *occurrenceScope) resolve(name string) bool {
	for scope := s; scope != nil; scope = scope.parent {
		if _, ok := scope.symbols[name]; ok {
			return true
		}
	}
	return false
}

func (c *occurrenceCollector) collect() []symbolOccurrence {
	root := newOccurrenceScope(nil)
	for !c.done() {
		c.parseForm(root, false)
	}
	return c.occurrences
}

func (c *occurrenceCollector) done() bool {
	return c.pos >= len(c.tokens) || c.peek().Type == lexer.EOF
}

func (c *occurrenceCollector) peek() lexer.Token {
	if c.pos >= len(c.tokens) {
		return lexer.Token{Type: lexer.EOF}
	}
	return c.tokens[c.pos]
}

func (c *occurrenceCollector) next() lexer.Token {
	tok := c.peek()
	if c.pos < len(c.tokens) {
		c.pos++
	}
	return tok
}

func (c *occurrenceCollector) parseForm(scope *occurrenceScope, quoted bool) {
	tok := c.peek()
	switch tok.Type {
	case lexer.EOF:
		return
	case lexer.IDENT:
		c.next()
		c.handleIdentifier(tok, scope, quoted, false, false)
	case lexer.LPAREN:
		c.parseList(scope, quoted)
	case lexer.LBRACKET:
		c.parseDelimited(scope, quoted, lexer.LBRACKET, lexer.RBRACKET)
	case lexer.LBRACE:
		c.parseDelimited(scope, quoted, lexer.LBRACE, lexer.RBRACE)
	case lexer.QUOTE:
		c.next()
		// Within quasiquote, quote is data whose children still participate
		// in quasiquote expansion, including active unquotes.
		c.parseForm(scope, quoted || c.quasiquoteDepth == 0)
	case lexer.QUASIQUOTE:
		c.next()
		c.parseReaderForm(scope, quoted, 1)
	case lexer.UNQUOTE, lexer.SPLICE:
		c.next()
		if c.quasiquoteDepth > 0 {
			c.parseReaderForm(scope, quoted, -1)
		} else {
			c.parseForm(scope, true) // invalid outside quasiquote, not an evaluated reference
		}
	case lexer.VAR_QUOTE, lexer.DEREF:
		c.next()
		c.parseForm(scope, quoted)
	case lexer.METADATA:
		c.next()
		// Ordinary metadata is literal data. Quasiquote expands metadata
		// recursively, so active unquotes retain their evaluation context.
		c.parseForm(scope, quoted || c.quasiquoteDepth == 0)
		c.parseForm(scope, quoted)
	case lexer.DISCARD:
		c.next()
		c.parseForm(scope, true)
	case lexer.HASHSET:
		c.next()
		c.parseUntil(scope, quoted, lexer.RBRACE)
	case lexer.HASHFN:
		c.next()
		c.parseUntil(scope, quoted, lexer.RPAREN)
	default:
		c.next()
	}
}

func (c *occurrenceCollector) parseReaderForm(scope *occurrenceScope, quoted bool, depthChange int) {
	previousDepth := c.quasiquoteDepth
	c.quasiquoteDepth += depthChange
	c.parseForm(scope, quoted)
	c.quasiquoteDepth = previousDepth
}

func (c *occurrenceCollector) parseReaderList(scope *occurrenceScope, quoted bool, depthChange int) {
	c.next() // reader form head
	previousDepth := c.quasiquoteDepth
	c.quasiquoteDepth += depthChange
	c.parseUntil(scope, quoted, lexer.RPAREN)
	c.quasiquoteDepth = previousDepth
}

func (c *occurrenceCollector) parseDelimited(scope *occurrenceScope, quoted bool, open, close lexer.TokenType) {
	if c.peek().Type == open {
		c.next()
	}
	c.parseUntil(scope, quoted, close)
}

func (c *occurrenceCollector) parseUntil(scope *occurrenceScope, quoted bool, close lexer.TokenType) {
	for !c.done() {
		if c.peek().Type == close {
			c.next()
			return
		}
		c.parseForm(scope, quoted)
	}
}

func (c *occurrenceCollector) parseList(scope *occurrenceScope, quoted bool) {
	if c.peek().Type == lexer.LPAREN {
		c.next()
	}
	if c.done() {
		return
	}
	if c.peek().Type == lexer.RPAREN {
		c.next()
		return
	}
	if quoted {
		c.parseUntil(scope, true, lexer.RPAREN)
		return
	}

	head := c.peek()
	if c.quasiquoteDepth > 0 {
		if head.Type == lexer.IDENT {
			switch head.Literal {
			case "quasiquote":
				c.parseReaderList(scope, false, 1)
				return
			case "unquote", "unquote-splicing", "splice-unquote":
				c.parseReaderList(scope, false, -1)
				return
			}
		}
		c.parseUntil(scope, false, lexer.RPAREN)
		return
	}
	if head.Type != lexer.IDENT {
		c.parseUntil(scope, quoted, lexer.RPAREN)
		return
	}

	switch head.Literal {
	case "def", "defonce":
		c.parseDefLike(scope, quoted)
	case "defn", "defmacro":
		c.parseDefnLike(scope, quoted)
	case "fn":
		c.parseFnLike(scope, quoted)
	case "letfn":
		c.parseLetfnLike(scope, quoted)
	case "let", "loop", "doseq", "for":
		c.parseLetLike(scope, quoted)
	case "if-let", "when-let":
		c.parseIfWhenLet(scope, quoted, head.Literal)
	case "catch":
		c.parseCatchLike(scope, quoted)
	case "quasiquote":
		c.parseReaderList(scope, false, 1)
	case "quote", "syntax-quote", "unquote", "unquote-splicing", "splice-unquote":
		c.next() // head
		c.parseUntil(scope, true, lexer.RPAREN)
	case "ns", "require", "import":
		c.next() // head
		c.parseUntil(scope, quoted, lexer.RPAREN)
	default:
		c.parseUntil(scope, quoted, lexer.RPAREN)
	}
}

func (c *occurrenceCollector) parseDefLike(scope *occurrenceScope, quoted bool) {
	c.next() // def/defonce
	c.consumeMetadataPrefixes()
	if c.peek().Type == lexer.IDENT {
		nameTok := c.next()
		c.handleIdentifier(nameTok, scope, quoted, true, false)
	}
	c.parseUntil(scope, quoted, lexer.RPAREN)
}

func (c *occurrenceCollector) parseDefnLike(scope *occurrenceScope, quoted bool) {
	c.next() // defn/defmacro
	c.consumeMetadataPrefixes()
	if c.peek().Type == lexer.IDENT {
		nameTok := c.next()
		c.handleIdentifier(nameTok, scope, quoted, true, false)
	}
	if c.peek().Type == lexer.STRING {
		c.next()
	}
	if c.peek().Type == lexer.LBRACE {
		c.parseDelimited(scope, quoted, lexer.LBRACE, lexer.RBRACE)
	}
	c.consumeMetadataPrefixes()

	if c.peek().Type == lexer.LBRACKET {
		fnScope := newOccurrenceScope(scope)
		c.parseParamVector(fnScope)
		c.parseUntil(fnScope, quoted, lexer.RPAREN)
		return
	}

	if c.peek().Type == lexer.LPAREN {
		for !c.done() && c.peek().Type == lexer.LPAREN {
			c.next() // arity list open
			fnScope := newOccurrenceScope(scope)
			c.consumeMetadataPrefixes()
			if c.peek().Type == lexer.LBRACKET {
				c.parseParamVector(fnScope)
			}
			c.parseUntil(fnScope, quoted, lexer.RPAREN)
			c.consumeMetadataPrefixes()
		}
		if c.peek().Type == lexer.RPAREN {
			c.next()
		}
		return
	}

	c.parseUntil(scope, quoted, lexer.RPAREN)
}

func (c *occurrenceCollector) parseFnLike(scope *occurrenceScope, quoted bool) {
	c.next() // fn
	fnName := ""
	c.consumeMetadataPrefixes()
	if c.peek().Type == lexer.IDENT {
		nameTok := c.next()
		fnName = nameTok.Literal
	}
	c.consumeMetadataPrefixes()

	if c.peek().Type == lexer.LBRACKET {
		fnScope := newOccurrenceScope(scope)
		fnScope.define(fnName)
		c.parseParamVector(fnScope)
		c.parseUntil(fnScope, quoted, lexer.RPAREN)
		return
	}

	if c.peek().Type == lexer.LPAREN {
		for !c.done() && c.peek().Type == lexer.LPAREN {
			c.next()
			fnScope := newOccurrenceScope(scope)
			fnScope.define(fnName)
			c.consumeMetadataPrefixes()
			if c.peek().Type == lexer.LBRACKET {
				c.parseParamVector(fnScope)
			}
			c.parseUntil(fnScope, quoted, lexer.RPAREN)
			c.consumeMetadataPrefixes()
		}
		if c.peek().Type == lexer.RPAREN {
			c.next()
		}
		return
	}

	c.parseUntil(scope, quoted, lexer.RPAREN)
}

func (c *occurrenceCollector) parseParamVector(scope *occurrenceScope) {
	if c.peek().Type != lexer.LBRACKET {
		return
	}
	c.next() // [
	for !c.done() {
		if c.peek().Type == lexer.RBRACKET {
			c.next()
			return
		}
		c.parseBindingPattern(scope)
	}
}

func (c *occurrenceCollector) parseLetLike(scope *occurrenceScope, quoted bool) {
	c.next() // let/loop/doseq/for
	child := newOccurrenceScope(scope)
	c.consumeMetadataPrefixes()
	if c.peek().Type == lexer.LBRACKET {
		c.parseBindingVector(child, quoted)
	}
	c.parseUntil(child, quoted, lexer.RPAREN)
}

func (c *occurrenceCollector) parseLetfnLike(scope *occurrenceScope, quoted bool) {
	c.next() // letfn
	child := newOccurrenceScope(scope)
	c.consumeMetadataPrefixes()
	if c.peek().Type == lexer.LBRACKET {
		c.parseLetfnBindings(child, quoted)
	}
	c.parseUntil(child, quoted, lexer.RPAREN)
}

func (c *occurrenceCollector) parseLetfnBindings(scope *occurrenceScope, quoted bool) {
	if c.peek().Type != lexer.LBRACKET {
		return
	}
	for _, name := range c.collectLetfnBindingNames(c.pos) {
		scope.define(name)
	}

	c.next() // [
	for !c.done() {
		if c.peek().Type == lexer.RBRACKET {
			c.next()
			return
		}
		c.parseLetfnBinding(scope, quoted)
	}
}

func (c *occurrenceCollector) parseLetfnBinding(scope *occurrenceScope, quoted bool) {
	c.consumeMetadataPrefixes()
	switch c.peek().Type {
	case lexer.LPAREN:
		c.parseLetfnBindingList(scope, quoted)
	case lexer.IDENT:
		nameTok := c.next()
		scope.define(nameTok.Literal)
		if c.peek().Type != lexer.RBRACKET {
			c.parseForm(scope, quoted)
		}
	default:
		c.parseForm(scope, quoted)
	}
}

func (c *occurrenceCollector) parseLetfnBindingList(scope *occurrenceScope, quoted bool) {
	if c.peek().Type != lexer.LPAREN {
		return
	}
	c.next() // (
	c.consumeMetadataPrefixes()
	if c.peek().Type == lexer.IDENT {
		nameTok := c.next()
		scope.define(nameTok.Literal)
	}
	c.consumeMetadataPrefixes()

	fnScope := newOccurrenceScope(scope)
	if c.peek().Type == lexer.LBRACKET {
		c.parseParamVector(fnScope)
		c.parseUntil(fnScope, quoted, lexer.RPAREN)
		return
	}

	if c.peek().Type == lexer.LPAREN {
		for !c.done() && c.peek().Type == lexer.LPAREN {
			c.next() // arity list open
			arityScope := newOccurrenceScope(fnScope)
			c.consumeMetadataPrefixes()
			if c.peek().Type == lexer.LBRACKET {
				c.parseParamVector(arityScope)
			}
			c.parseUntil(arityScope, quoted, lexer.RPAREN)
			c.consumeMetadataPrefixes()
		}
		if c.peek().Type == lexer.RPAREN {
			c.next()
		}
		return
	}

	c.parseUntil(fnScope, quoted, lexer.RPAREN)
}

func (c *occurrenceCollector) collectLetfnBindingNames(start int) []string {
	if start < 0 || start >= len(c.tokens) || c.tokens[start].Type != lexer.LBRACKET {
		return []string{}
	}

	names := make([]string, 0)
	seen := make(map[string]struct{})

	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || name == "_" || name == "&" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	i := start + 1
	for i < len(c.tokens) {
		if c.tokens[i].Type == lexer.RBRACKET {
			return names
		}

		for i < len(c.tokens) && c.tokens[i].Type == lexer.METADATA {
			i++
			i = c.skipOneFormFrom(i)
		}
		if i >= len(c.tokens) || c.tokens[i].Type == lexer.RBRACKET {
			return names
		}

		switch c.tokens[i].Type {
		case lexer.LPAREN:
			j := i + 1
			for j < len(c.tokens) && c.tokens[j].Type == lexer.METADATA {
				j++
				j = c.skipOneFormFrom(j)
			}
			if j < len(c.tokens) && c.tokens[j].Type == lexer.IDENT {
				add(c.tokens[j].Literal)
			}
			i = c.skipOneFormFrom(i)
		case lexer.IDENT:
			add(c.tokens[i].Literal)
			i++
			i = c.skipOneFormFrom(i)
		default:
			i = c.skipOneFormFrom(i)
		}
	}
	return names
}

func (c *occurrenceCollector) consumeMetadataPrefixes() {
	for c.peek().Type == lexer.METADATA {
		c.next()
		c.pos = c.skipOneFormFrom(c.pos)
	}
}

func (c *occurrenceCollector) skipOneFormFrom(start int) int {
	if start < 0 {
		start = 0
	}
	if start >= len(c.tokens) {
		return start
	}

	tmp := *c
	tmp.pos = start
	tmp.occurrences = nil
	tmp.seen = nil
	tmp.parseForm(newOccurrenceScope(nil), true)
	if tmp.pos <= start {
		return start + 1
	}
	return tmp.pos
}

func (c *occurrenceCollector) parseBindingVector(scope *occurrenceScope, quoted bool) {
	c.next() // [
	for !c.done() {
		if c.peek().Type == lexer.RBRACKET {
			c.next()
			return
		}
		names := c.parseBindingPatternNames()
		if c.peek().Type == lexer.RBRACKET {
			for _, name := range names {
				scope.define(name)
			}
			c.next()
			return
		}
		c.parseForm(scope, quoted) // binding expr uses previously defined names
		for _, name := range names {
			scope.define(name)
		}
	}
}

func (c *occurrenceCollector) parseIfWhenLet(scope *occurrenceScope, quoted bool, head string) {
	c.next() // if-let/when-let
	child := newOccurrenceScope(scope)
	c.consumeMetadataPrefixes()
	if c.peek().Type == lexer.LBRACKET {
		c.next() // [
		names := c.parseBindingPatternNames()
		if c.peek().Type != lexer.RBRACKET {
			c.parseForm(scope, quoted) // binding expr in outer scope
		}
		if c.peek().Type == lexer.RBRACKET {
			c.next()
		}
		for _, name := range names {
			child.define(name)
		}
	}
	if c.peek().Type != lexer.RPAREN {
		c.parseForm(child, quoted) // then/body
	}
	if head == "if-let" && c.peek().Type != lexer.RPAREN {
		c.parseForm(scope, quoted) // else
	}
	c.parseUntil(scope, quoted, lexer.RPAREN)
}

func (c *occurrenceCollector) parseCatchLike(scope *occurrenceScope, quoted bool) {
	c.next() // catch
	if c.peek().Type != lexer.RPAREN {
		c.parseForm(scope, quoted) // exception type
	}
	child := newOccurrenceScope(scope)
	if c.peek().Type == lexer.IDENT {
		nameTok := c.next()
		child.define(nameTok.Literal)
	}
	c.parseUntil(child, quoted, lexer.RPAREN)
}

func (c *occurrenceCollector) parseBindingPatternNames() []string {
	names := make([]string, 0)
	c.parseBindingPatternInto(&names)
	return names
}

func (c *occurrenceCollector) parseBindingPattern(scope *occurrenceScope) {
	names := c.parseBindingPatternNames()
	for _, name := range names {
		scope.define(name)
	}
}

func (c *occurrenceCollector) parseBindingPatternInto(out *[]string) {
	c.consumeMetadataPrefixes()
	if c.done() {
		return
	}
	tok := c.peek()
	switch tok.Type {
	case lexer.IDENT:
		c.next()
		if tok.Literal != "&" && tok.Literal != "_" {
			*out = append(*out, tok.Literal)
		}
	case lexer.LBRACKET:
		c.next()
		for !c.done() && c.peek().Type != lexer.RBRACKET {
			c.parseBindingPatternInto(out)
		}
		if c.peek().Type == lexer.RBRACKET {
			c.next()
		}
	case lexer.LBRACE:
		c.parseMapBindingPatternInto(out)
	default:
		c.parseForm(newOccurrenceScope(nil), true)
	}
}

func (c *occurrenceCollector) parseMapBindingPatternInto(out *[]string) {
	c.next() // {
	for !c.done() && c.peek().Type != lexer.RBRACE {
		c.consumeMetadataPrefixes()
		key := c.peek()
		if key.Type == lexer.KEYWORD {
			c.next()
			switch key.Literal {
			case "keys", "strs", "as":
				c.parseBindingPatternInto(out)
			default:
				// :or defaults and unrecognized directives introduce no bindings.
				c.pos = c.skipOneFormFrom(c.pos)
			}
			continue
		}
		if key.Type == lexer.IDENT {
			c.parseBindingPatternInto(out) // {local-name literal-lookup-key}
		} else {
			c.pos = c.skipOneFormFrom(c.pos)
		}
		if !c.done() && c.peek().Type != lexer.RBRACE {
			c.pos = c.skipOneFormFrom(c.pos) // lookup keys are literal data
		}
	}
	if c.peek().Type == lexer.RBRACE {
		c.next()
	}
}

func (c *occurrenceCollector) handleIdentifier(tok lexer.Token, scope *occurrenceScope, quoted, isDeclaration, isBinding bool) {
	if quoted || c.quasiquoteDepth > 0 {
		return
	}
	literal := strings.TrimSpace(tok.Literal)
	if literal == "" {
		return
	}
	if isBinding {
		scope.define(literal)
		return
	}
	if _, ok := c.candidates[literal]; !ok {
		return
	}
	if !strings.Contains(literal, "/") && scope.resolve(literal) {
		return
	}
	def, ok := c.idx.ResolveDefinition(c.currentNS, literal)
	if !ok {
		return
	}
	if def.Namespace != c.target.Namespace || def.Name != c.target.Name {
		return
	}
	if isDeclaration && !c.includeDeclaration {
		return
	}

	start := byteOffsetToLSPPosition(c.source, tok.Offset)
	end := byteOffsetToLSPPosition(c.source, tok.EndOffset)
	key := fmt.Sprintf("%s:%d:%d:%s", c.uri, tok.Line, tok.Column, literal)
	if _, exists := c.seen[key]; exists {
		return
	}
	c.seen[key] = struct{}{}
	c.occurrences = append(c.occurrences, symbolOccurrence{
		URI:     c.uri,
		Literal: literal,
		Range: lspRange{
			Start: start,
			End:   end,
		},
	})
}

func (s *Server) workspaceDocumentURIs() []string {
	set := make(map[string]struct{})
	if s.state != nil && s.state.index != nil {
		for uri := range s.state.index.NamespaceByURI {
			set[uri] = struct{}{}
		}
		for uri := range s.state.index.DocumentByURI {
			set[uri] = struct{}{}
		}
	}
	if s.docs != nil {
		for _, uri := range s.docs.URIs() {
			set[uri] = struct{}{}
		}
	}
	if s.state != nil {
		for path := range s.state.fileStamps {
			set[toolindex.PathToFileURI(path)] = struct{}{}
		}
	}

	out := make([]string, 0, len(set))
	for uri := range set {
		out = append(out, uri)
	}
	sort.Strings(out)
	return out
}

func (s *Server) referenceCandidatesForURI(uri string, def toolindex.SymbolDef) map[string]struct{} {
	out := map[string]struct{}{
		compilerQualified(def.Namespace, def.Name): {},
	}

	currentNS := s.state.index.NamespaceForURI(uri)
	if currentNS == def.Namespace || uri == def.URI {
		out[def.Name] = struct{}{}
	}

	info := s.state.index.NamespaceInfo(currentNS)
	if info == nil {
		return out
	}

	if targetNS, ok := info.Refers[def.Name]; ok && targetNS == def.Namespace {
		out[def.Name] = struct{}{}
	}
	for _, ns := range info.ReferAll {
		if ns == def.Namespace {
			out[def.Name] = struct{}{}
			break
		}
	}
	for alias, targetNS := range info.Aliases {
		if targetNS == def.Namespace && alias != "" {
			out[alias+"/"+def.Name] = struct{}{}
		}
	}

	return out
}

func compilerQualified(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + "/" + name
}

func renameReplacement(literal, oldName, newName string) string {
	if literal == oldName {
		return newName
	}
	oldSuffix := "/" + oldName
	if strings.HasSuffix(literal, oldSuffix) {
		return strings.TrimSuffix(literal, oldSuffix) + "/" + newName
	}
	return newName
}

func isValidRenameName(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	return frontend.IsSimpleSymbol(name)
}

func fullDocumentRange(source string) lspRange {
	return lspRange{
		Start: lspPosition{Line: 0, Character: 0},
		End:   byteOffsetToLSPPosition(source, len(source)),
	}
}

func isSymbolRune(r rune) bool {
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '-', '_', '+', '*', '/', '=', '<', '>', '!', '?', '&', '|', '%', '$', '#', '.':
		return true
	default:
		return false
	}
}
