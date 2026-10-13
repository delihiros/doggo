package lsp

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	toolindex "doggo/internal/tooling/index"
)

func decodeEditorRange(t *testing.T, value any) lspRange {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result lspRange
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRenameEditsPreserveSource(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
		line, character    int
	}{
		{"ASCII", "(ns demo)\n(def target 1)\ntarget\n", "(ns demo)\n(def renamed 1)\nrenamed\n", 1, 5},
		{"UTF16", "(ns demo)\n(println \"😀\") (def target 1)\n(println \"😀\" target)\n", "(ns demo)\n(println \"😀\") (def renamed 1)\n(println \"😀\" renamed)\n", 1, 20},
		{"CRLF", "(ns demo)\r\n(def target 1)\r\ntarget", "(ns demo)\r\n(def renamed 1)\r\nrenamed", 1, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			uri := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
			messages := runServer(t, []map[string]any{
				req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
				notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": uri, "text": tc.source, "version": 1}}),
				req(2, lspMethodRename, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": tc.line, "character": tc.character}, "newName": "renamed"}),
				notify(lspMethodExit, map[string]any{}),
			})
			edits := findResponseByID(t, messages, 2)["result"].(map[string]any)["changes"].(map[string]any)[uri].([]any)
			if len(edits) != 2 {
				t.Fatalf("edits = %v, want declaration and reference", edits)
			}
			actual := tc.source
			for n := len(edits) - 1; n >= 0; n-- {
				edit := edits[n].(map[string]any)
				var err error
				actual, err = applyRangeChange(actual, decodeEditorRange(t, edit["range"]), edit["newText"].(string))
				if err != nil {
					t.Fatal(err)
				}
			}
			if actual != tc.want {
				t.Fatalf("renamed source = %q, want %q", actual, tc.want)
			}
		})
	}
}

func TestFormattingEditsReplaceWholeUTF16Document(t *testing.T) {
	for _, source := range []string{"  (println   \"😀\")", "  (println   \"😀\")\n", "  (println   \"😀\")\r\n", "(ns demo)\r\n  (println   \"😀\")"} {
		t.Run(strings.ReplaceAll(source, "\n", "LF"), func(t *testing.T) {
			root := t.TempDir()
			uri := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
			messages := runServer(t, []map[string]any{
				req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
				notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": uri, "text": source, "version": 1}}),
				req(2, lspMethodFormatting, map[string]any{"textDocument": map[string]any{"uri": uri}, "options": map[string]any{"tabSize": 2, "insertSpaces": true}}),
				notify(lspMethodExit, map[string]any{}),
			})
			edit := findResponseByID(t, messages, 2)["result"].([]any)[0].(map[string]any)
			actual, err := applyRangeChange(source, decodeEditorRange(t, edit["range"]), edit["newText"].(string))
			if err != nil {
				t.Fatal(err)
			}
			if actual != edit["newText"] {
				t.Fatalf("whole replacement left source text: %q", actual)
			}
		})
	}
}

func TestPreinitializeMessagesDoNotDispatchHandlers(t *testing.T) {
	root := t.TempDir()
	uri := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
	messages := runServer(t, []map[string]any{
		req(1, lspMethodCompletion, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 0, "character": 0}}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": uri, "text": "(def ignored 1)", "version": 1}}),
		notify(lspMethodDidChange, map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []any{map[string]any{"text": "(def ignored 2)"}}}),
		notify(lspMethodDidSave, map[string]any{"textDocument": map[string]any{"uri": uri}}),
		notify(lspMethodDidClose, map[string]any{"textDocument": map[string]any{"uri": uri}}),
		req(2, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		req(3, lspMethodDocumentSymbol, map[string]any{"textDocument": map[string]any{"uri": uri}}),
		notify(lspMethodExit, map[string]any{}),
	})
	if len(messages) != 3 {
		t.Fatalf("responses = %#v, want exactly one per request", messages)
	}
	response := findResponseByID(t, messages, 1)
	if got := int(response["error"].(map[string]any)["code"].(float64)); got != lspErrorServerNotInitialized {
		t.Fatalf("error = %d", got)
	}
	if _, ok := findResponseByID(t, messages, 2)["result"]; !ok {
		t.Fatal("initialize did not succeed after ignored notifications")
	}
	if got := findResponseByID(t, messages, 3)["result"].([]any); len(got) != 0 {
		t.Fatalf("ignored documents were stored: %v", got)
	}
}

func TestPreinitializeRequestsReturnOneInitializationError(t *testing.T) {
	for _, method := range []string{lspMethodCompletion, lspMethodShutdown, "unknown/request"} {
		t.Run(method, func(t *testing.T) {
			messages := runServer(t, []map[string]any{req(1, method, map[string]any{}), notify(lspMethodExit, map[string]any{})})
			if len(messages) != 1 {
				t.Fatalf("request produced %d responses", len(messages))
			}
			response := findResponseByID(t, messages, 1)
			errorResponse, ok := response["error"].(map[string]any)
			if !ok || int(errorResponse["code"].(float64)) != lspErrorServerNotInitialized {
				t.Fatalf("preinitialize response = %v", response)
			}
		})
	}
}

func TestInitializeAndOverlayUseStandardFileURI(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main file.dog")
	mustWrite(t, file, "(ns disk)\n(def old 1)\n")
	standardURI := func(path string) string {
		p := filepath.ToSlash(path)
		if filepath.VolumeName(path) != "" {
			p = "/" + p
		}
		return (&url.URL{Scheme: "file", Path: p}).String()
	}
	uri := standardURI(file)
	alias := strings.Replace(uri, "main", "%6dain", 1)
	messages := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": standardURI(root)}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": alias, "text": "(ns overlay)\n(def updated 1)\n", "version": 1}}),
		req(2, lspMethodDocumentSymbol, map[string]any{"textDocument": map[string]any{"uri": uri}}),
		req(3, lspMethodCompletion, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 1, "character": 12}}),
		notify(lspMethodExit, map[string]any{}),
	})
	if _, ok := findResponseByID(t, messages, 1)["result"]; !ok {
		t.Fatalf("standard URI initialize failed: %#v", messages)
	}
	symbols := findResponseByID(t, messages, 2)["result"].([]any)
	if len(symbols) != 1 || symbols[0].(map[string]any)["name"] != "updated" {
		t.Fatalf("overlay symbols = %v", symbols)
	}
	items := findResponseByID(t, messages, 3)["result"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["label"] != "updated" {
		t.Fatalf("completion did not read same overlay: %v", items)
	}
}

func TestDefinitionReferencesAndSymbolsUseUTF16NameRanges(t *testing.T) {
	root := t.TempDir()
	uri := toolindex.PathToFileURI(filepath.Join(root, "main.dog"))
	source := "(ns demo)\n(println \"😀\") (def target 1)\n(println \"😀\" target)\n"
	params := map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 2, "character": 14}}
	messages := runServer(t, []map[string]any{
		req(1, lspMethodInitialize, map[string]any{"rootUri": toolindex.PathToFileURI(root)}),
		notify(lspMethodDidOpen, map[string]any{"textDocument": map[string]any{"uri": uri, "text": source, "version": 1}}),
		req(2, lspMethodDefinition, params),
		req(3, lspMethodReferences, map[string]any{"textDocument": params["textDocument"], "position": params["position"], "context": map[string]any{"includeDeclaration": true}}),
		req(4, lspMethodDocumentSymbol, map[string]any{"textDocument": params["textDocument"]}),
		req(5, lspMethodWorkspaceSymbol, map[string]any{"query": "demo/target"}),
		req(6, lspMethodHover, params),
		notify(lspMethodExit, map[string]any{}),
	})
	wantDeclaration := lspRange{Start: lspPosition{Line: 1, Character: 20}, End: lspPosition{Line: 1, Character: 26}}
	definition := findResponseByID(t, messages, 2)["result"].([]any)
	if len(definition) != 1 || decodeEditorRange(t, definition[0].(map[string]any)["range"]) != wantDeclaration {
		t.Fatalf("definition = %v", definition)
	}
	refs := findResponseByID(t, messages, 3)["result"].([]any)
	if len(refs) != 2 {
		t.Fatalf("references = %v", refs)
	}
	for i, want := range []lspRange{wantDeclaration, {Start: lspPosition{Line: 2, Character: 14}, End: lspPosition{Line: 2, Character: 20}}} {
		if actual := decodeEditorRange(t, refs[i].(map[string]any)["range"]); actual != want {
			t.Fatalf("reference %d = %+v, want %+v", i, actual, want)
		}
	}
	docSymbols := findResponseByID(t, messages, 4)["result"].([]any)
	if actual := decodeEditorRange(t, docSymbols[0].(map[string]any)["selectionRange"]); actual != wantDeclaration {
		t.Fatalf("document symbol = %+v", actual)
	}
	workspaceSymbols := findResponseByID(t, messages, 5)["result"].([]any)
	if actual := decodeEditorRange(t, workspaceSymbols[0].(map[string]any)["location"].(map[string]any)["range"]); actual != wantDeclaration {
		t.Fatalf("workspace symbol = %+v", actual)
	}
	hover := findResponseByID(t, messages, 6)["result"].(map[string]any)["contents"].(map[string]any)["value"].(string)
	if !strings.Contains(hover, "demo/target") || !strings.Contains(hover, uri+":2") {
		t.Fatalf("hover = %q", hover)
	}
}

func TestSemanticTokensUseUTF16ColumnsAndLengths(t *testing.T) {
	tokens := buildSemanticTokens("main.dog", "(println \"😀\" target) ; 😀 comment\n")
	for _, want := range []semanticToken{
		{line: 0, start: 9, length: 4, tokenType: semanticString},
		{line: 0, start: 14, length: 6, tokenType: semanticVariable},
		{line: 0, start: 22, length: 12, tokenType: semanticComment},
	} {
		found := false
		for _, actual := range tokens {
			if actual == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %+v in %+v", want, tokens)
		}
	}
}
