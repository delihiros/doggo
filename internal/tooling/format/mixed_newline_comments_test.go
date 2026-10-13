package format

import (
	"reflect"
	"testing"

	"doggo/internal/lexer"
	"doggo/internal/parser"
)

func TestFormatMixedNewlineCommentsPreservesForms(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"reported", "; hidden\r(def hidden 1)\n(def live 2)\n"},
		{"comment delimiters and quotes", "(do\r; hidden\r) \"unclosed [ {\n(def live 2)\n)\n"},
		{"CRLF comment terminator", "; hidden\r(def hidden 1)\r\n(def live 2)\r\n"},
		{"comment at EOF", "(def live 2)\r; hidden\r(def hidden 1)"},
		{"terminal CR comment at EOF", "(def live 2)\n; hidden\r"},
		{"terminal CRs comment at EOF", "(def live 2)\n; hidden\r\r"},
		{"comment trailing CR and spaces", "(def live 2)\n; hidden\r \t\n"},
		{"bare CR code", "(do\r(def live 2)\r(println live))\r"},
		{"quoted semicolon", "(def text \"; literal\rbody\nend\")\r; hidden\r(def hidden 1)\n(def live 2)\n"},
		{"escaped quote", "(def text \"a\\\"; literal\rbody\nend\")\r; hidden\r(def hidden 1)\n(def live 2)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := significantTokens(t, tc.source)
			beforeForms, err := parser.New(before, tc.source).ParseAll()
			if err != nil {
				t.Fatalf("original parse: %v", err)
			}
			formatted, _, err := Format(tc.source, Options{TabSize: 2, InsertSpaces: true})
			if err != nil {
				t.Fatal(err)
			}
			after := significantTokens(t, formatted)
			if !reflect.DeepEqual(before, after) {
				t.Errorf("format changed tokens: before=%v after=%v; formatted=%q", before, after, formatted)
			}
			beforeLiterals := literalTokens(t, tc.source)
			afterLiterals := literalTokens(t, formatted)
			for i := 0; i < min(len(beforeLiterals), len(afterLiterals)); i++ {
				original := beforeLiterals[i]
				result := afterLiterals[i]
				if tc.source[original.Offset:original.EndOffset] != formatted[result.Offset:result.EndOffset] {
					t.Errorf("literal %d raw bytes changed", i)
				}
			}
			afterForms, err := parser.New(after, formatted).ParseAll()
			if err != nil {
				t.Fatalf("formatted parse: %v; formatted=%q", err, formatted)
			}
			if len(beforeForms) != len(afterForms) {
				t.Errorf("parsed form count changed: %d => %d", len(beforeForms), len(afterForms))
			}
			for i := 0; i < min(len(beforeForms), len(afterForms)); i++ {
				if beforeForms[i].String() != afterForms[i].String() {
					t.Errorf("parsed form %d changed: %s => %s", i, beforeForms[i], afterForms[i])
				}
			}
			second, _, err := Format(formatted, Options{TabSize: 2, InsertSpaces: true})
			if err != nil || second != formatted {
				t.Errorf("format not idempotent: first=%q second=%q error=%v", formatted, second, err)
			}
		})
	}
}

func significantTokens(t *testing.T, source string) []lexer.Token {
	t.Helper()
	tokens, err := lexer.New(source).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	for i := range tokens {
		tokens[i].Line, tokens[i].Column = 0, 0
		tokens[i].Offset, tokens[i].EndOffset = 0, 0
	}
	return tokens
}
