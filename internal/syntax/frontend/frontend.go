package frontend

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	doggoerr "doggo/internal/errors"
	"doggo/internal/lexer"
	"doggo/internal/syntax/form"
	"doggo/internal/syntax/lower"
	"doggo/internal/syntax/span"
	syntoken "doggo/internal/syntax/token"
	"doggo/internal/types"
)

var lowerForms = lower.ToValues

// Diagnostic is a parse/lower diagnostic.
type Diagnostic struct {
	Span    span.Span
	Message string
}

// ParseResult contains the source forms and their derived artifacts.
type ParseResult struct {
	Tokens       []syntoken.Token
	Forms        []*form.Node
	LoweredForms []types.Value
	Diagnostics  []Diagnostic
}

// Parse reads source into forms, tokens, lowered values, and diagnostics.
func Parse(path, source string) ParseResult {
	lineOffsets := buildLineOffsets(source)
	reader, err := NewReader(path, source)
	if err != nil {
		return ParseResult{
			Diagnostics: []Diagnostic{errorToDiagnostic(path, err)},
		}
	}

	legacyTokens := reader.Tokens()
	toks := buildSyntaxTokens(path, source, lineOffsets, legacyTokens)
	nodes, err := reader.ReadSyntaxAll()
	if err != nil {
		return ParseResult{
			Tokens:      toks,
			Diagnostics: []Diagnostic{errorToDiagnostic(path, err)},
		}
	}

	return parseResultFromNodes(path, toks, nodes)
}

func parseResultFromNodes(path string, toks []syntoken.Token, nodes []*form.Node) ParseResult {
	result := ParseResult{
		Tokens: toks,
		Forms:  nodes,
	}

	loweredForms, err := lowerForms(nodes)
	if err != nil {
		result.Diagnostics = []Diagnostic{errorToDiagnostic(path, err)}
		return result
	}
	result.LoweredForms = loweredForms
	return result
}

func buildSyntaxTokens(path, source string, lineOffsets []int, tokens []lexer.Token) []syntoken.Token {
	if len(tokens) == 0 {
		return nil
	}
	type rawToken struct {
		tok      lexer.Token
		startOff int
		endOff   int
	}

	raw := make([]rawToken, 0, len(tokens))
	for _, tok := range tokens {
		if tok.Type == lexer.EOF {
			continue
		}
		raw = append(raw, rawToken{tok: tok, startOff: tok.Offset, endOff: tok.EndOffset})
	}

	sort.Slice(raw, func(i, j int) bool {
		return raw[i].startOff < raw[j].startOff
	})

	out := make([]syntoken.Token, 0, len(raw))
	prevEnd := 0
	for i, item := range raw {
		start := clamp(item.startOff, 0, len(source))
		end := clamp(item.endOff, start, len(source))
		nextStart := len(source)
		if i+1 < len(raw) {
			nextStart = clamp(raw[i+1].startOff, end, len(source))
		}
		leadingText := substring(source, prevEnd, start)
		trailingText := substring(source, end, nextStart)

		startLine, startCol := offsetToLineCol(lineOffsets, source, start)
		endLine, endCol := offsetToLineCol(lineOffsets, source, max(start, end))

		out = append(out, syntoken.Token{
			Kind:           tokenKind(item.tok.Type),
			Lexeme:         substring(source, start, end),
			Span:           span.New(path, startLine, startCol, endLine, endCol),
			LeadingTrivia:  splitTrivia(path, source, lineOffsets, prevEnd, leadingText),
			TrailingTrivia: splitTrivia(path, source, lineOffsets, end, trailingText),
		})
		prevEnd = nextStart
	}
	return out
}

func splitTrivia(path, source string, lineOffsets []int, baseOffset int, text string) []syntoken.Trivia {
	if text == "" {
		return nil
	}
	out := make([]syntoken.Trivia, 0, 2)
	i := 0
	for i < len(text) {
		start := i
		kind := syntoken.TriviaWhitespace
		if text[i] == ';' {
			kind = syntoken.TriviaComment
			for i < len(text) && text[i] != '\n' {
				i++
			}
		} else {
			for i < len(text) && text[i] != ';' {
				i++
			}
		}
		part := text[start:i]
		if part == "" {
			continue
		}
		absStart := baseOffset + start
		absEnd := baseOffset + i
		sl, sc := offsetToLineCol(lineOffsets, source, absStart)
		el, ec := offsetToLineCol(lineOffsets, source, absEnd)
		out = append(out, syntoken.Trivia{
			Kind: kind,
			Text: part,
			Span: span.New(path, sl, sc, el, ec),
		})
	}
	return out
}

func tokenKind(kind lexer.TokenType) string {
	switch kind {
	case lexer.IDENT:
		return "ident"
	case lexer.NUMBER:
		return "number"
	case lexer.STRING:
		return "string"
	case lexer.KEYWORD:
		return "keyword"
	case lexer.LPAREN:
		return "lparen"
	case lexer.RPAREN:
		return "rparen"
	case lexer.LBRACKET:
		return "lbracket"
	case lexer.RBRACKET:
		return "rbracket"
	case lexer.LBRACE:
		return "lbrace"
	case lexer.RBRACE:
		return "rbrace"
	case lexer.QUOTE:
		return "quote"
	case lexer.QUASIQUOTE:
		return "quasiquote"
	case lexer.UNQUOTE:
		return "unquote"
	case lexer.SPLICE:
		return "splice"
	case lexer.DEREF:
		return "deref"
	case lexer.DOT:
		return "dot"
	case lexer.HASHSET:
		return "hashset"
	case lexer.HASHFN:
		return "hashfn"
	case lexer.VAR_QUOTE:
		return "var-quote"
	case lexer.METADATA:
		return "metadata"
	case lexer.REGEX:
		return "regex"
	case lexer.CHAR:
		return "char"
	case lexer.DISCARD:
		return "discard"
	case lexer.ILLEGAL:
		return "illegal"
	default:
		return "unknown"
	}
}

func buildLineOffsets(source string) []int {
	offsets := []int{0}
	for i := 0; i < len(source); i++ {
		if source[i] == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

func offsetToLineCol(lineOffsets []int, source string, offset int) (int, int) {
	offset = clamp(offset, 0, len(source))
	lineIdx := sort.Search(len(lineOffsets), func(i int) bool { return lineOffsets[i] > offset }) - 1
	if lineIdx < 0 {
		lineIdx = 0
	}
	lineStart := lineOffsets[lineIdx]
	col := utf8.RuneCountInString(source[lineStart:offset]) + 1
	return lineIdx + 1, col
}

func errorToDiagnostic(path string, err error) Diagnostic {
	var de *doggoerr.DoggoError
	if errors.As(err, &de) {
		return Diagnostic{
			Span:    span.New(path, de.Position.Line, de.Position.Column, de.Position.Line, de.Position.Column),
			Message: de.Message,
		}
	}
	return Diagnostic{
		Span:    span.Unknown(path),
		Message: strings.TrimSpace(err.Error()),
	}
}

func substring(source string, start, end int) string {
	start = clamp(start, 0, len(source))
	end = clamp(end, start, len(source))
	if start == end {
		return ""
	}
	return source[start:end]
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
