package lexer

import (
	"errors"
	"strings"
	"testing"

	doggoerr "doggo/internal/errors"
)

func TestTokenizeRejectsRawNULWithPosition(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		line, column int
	}{
		{"only byte", "\x00", 1, 1},
		{"between forms", "(def visible 1)\x00(def hidden 2)\n", 1, 16},
		{"last byte", "42\x00", 1, 3},
		{"second line", "42\n\x00(\n", 2, 1},
		{"CRLF", "42\r\n \x00", 2, 2},
		{"comment", "; hidden\x00 ignored\n42", 1, 9},
		{"string", "\"a\x00b\"", 1, 3},
		{"regex", "#\"a\x00b\"", 1, 4},
		{"Unicode column", "\"😀\" \x00", 1, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokens, err := New(tc.source).Tokenize()
			var syntaxErr *doggoerr.DoggoError
			if !errors.As(err, &syntaxErr) || syntaxErr.Kind != doggoerr.KindLexer || !strings.Contains(syntaxErr.Message, "NUL") {
				t.Fatalf("Tokenize(%q) = %v, %v; want NUL lexer error", tc.source, tokens, err)
			}
			if syntaxErr.Position.Line != tc.line || syntaxErr.Position.Column != tc.column {
				t.Fatalf("position = %v, want %d:%d", syntaxErr.Position, tc.line, tc.column)
			}
			if len(tokens) != 0 {
				t.Fatalf("invalid source returned partial tokens: %v", tokens)
			}
		})
	}
}

func TestNextTokenDoesNotTreatRawNULAsEOF(t *testing.T) {
	for _, source := range []string{"\x00 after", "1\x00 after", "; before\x00 after"} {
		t.Run(source, func(t *testing.T) {
			reader := New(source)
			for n := 0; n <= len(source); n++ {
				token := reader.NextToken()
				if token.Type == ILLEGAL {
					if token.Offset != strings.IndexByte(source, 0) {
						t.Fatalf("illegal token offset = %d", token.Offset)
					}
					return
				}
				if token.Type == EOF {
					t.Fatalf("raw NUL exposed as EOF at offset %d before physical end %d", token.Offset, len(source))
				}
			}
			t.Fatal("no invalid token")
		})
	}
}

func TestTokenizePreservesEscapedNULAndPhysicalEOF(t *testing.T) {
	for _, source := range []string{`"\u0000"`, `"\000"`} {
		tokens, err := New(source).Tokenize()
		if err != nil || len(tokens) != 2 || tokens[0].Type != STRING || tokens[0].Literal != "\x00" {
			t.Fatalf("escaped NUL %q = %v, %v", source, tokens, err)
		}
		if tokens[1].Type != EOF || tokens[1].Offset != len(source) {
			t.Fatalf("physical EOF = %v", tokens[1])
		}
	}
	for _, source := range []string{"", "42", "42\n", "; comment at physical end"} {
		tokens, err := New(source).Tokenize()
		if err != nil || len(tokens) == 0 || tokens[len(tokens)-1].Type != EOF || tokens[len(tokens)-1].Offset != len(source) {
			t.Fatalf("valid source %q = %v, %v", source, tokens, err)
		}
	}
}
