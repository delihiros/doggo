package format

import (
	"testing"

	"doggo/internal/lexer"
)

func TestFormatOnlyDedentsLeadingClosingDelimiters(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"(def labels\n{\"one\" 1\n\"two\" 2})\n", "(def labels\n  {\"one\" 1\n    \"two\" 2})\n"},
		{"(str\n\"a\"\n\"b\")\n", "(str\n  \"a\"\n  \"b\")\n"},
	} {
		got, _, err := Format(tc.source, Options{TabSize: 2, InsertSpaces: true})
		if err != nil || got != tc.want {
			t.Errorf("Format(%q) = %q, %v; want %q", tc.source, got, err, tc.want)
		}
	}
}

func TestFormatPreservesLiteralBytes(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "opening line trailing spaces",
			source: "(def s \"a   \nb\")\n",
			want:   "(def s \"a   \nb\")\n",
		},
		{
			name:   "continuation and closing line whitespace",
			source: "(do\n(def s \"a \t \n  b \t \n c  \"  ) \t \n)\n",
			want:   "(do\n  (def s \"a \t \n  b \t \n c  \"  )\n)\n",
		},
		{
			name:   "escaped quotes and backslashes",
			source: "(do\n(def s \"a\\\"   \n \tb\\\\   \nc  \") \t\n)\n",
			want:   "(do\n  (def s \"a\\\"   \n \tb\\\\   \nc  \")\n)\n",
		},
		{
			name:   "mixed literal newlines",
			source: "(do\r\n(def s \"a   \nb \t\r\nc\rd\") \t\r\n)\r\n",
			want:   "(do\r\n  (def s \"a   \nb \t\r\nc\rd\")\r\n)\r\n",
		},
		{
			name:   "literal CRLF does not choose external newline",
			source: "(do\n(def s \"a\r\nb\") \n)\n",
			want:   "(do\n  (def s \"a\r\nb\")\n)\n",
		},
		{
			name:   "literal CR with external CRLF",
			source: "(do\r\n(def s \"a\rb\")\r\n)\r\n",
			want:   "(do\r\n  (def s \"a\rb\")\r\n)\r\n",
		},
		{
			name:   "normalize mixed external newlines only",
			source: "(do\r\n(def s \"a\nb\")\r)\n",
			want:   "(do\r\n  (def s \"a\nb\")\r\n)\r\n",
		},
		{
			name:   "regex literal",
			source: "(do\n(def pattern #\"a   \r\nb\") \t\n)\n",
			want:   "(do\n  (def pattern #\"a   \r\nb\")\n)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := Format(tt.source, Options{TabSize: 2, InsertSpaces: true})
			if err != nil {
				t.Fatalf("Format failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("formatted output\nwant=%q\ngot =%q", tt.want, got)
			}
			before := literalTokens(t, tt.source)
			after := literalTokens(t, got)
			if len(before) != len(after) {
				t.Fatalf("literal count changed: %d to %d", len(before), len(after))
			}
			for i, tok := range before {
				if after[i].Literal != tok.Literal {
					t.Errorf("literal %d value changed: %q to %q", i, tok.Literal, after[i].Literal)
				}
				wantRaw := tt.source[tok.Offset:tok.EndOffset]
				gotRaw := got[after[i].Offset:after[i].EndOffset]
				if gotRaw != wantRaw {
					t.Errorf("literal %d bytes changed: %q to %q", i, wantRaw, gotRaw)
				}
			}
			second, _, err := Format(got, Options{TabSize: 2, InsertSpaces: true})
			if err != nil {
				t.Fatalf("second Format failed: %v", err)
			}
			if second != got {
				t.Errorf("format not idempotent\nfirst =%q\nsecond=%q", got, second)
			}
		})
	}
}

func literalTokens(t *testing.T, source string) []lexer.Token {
	t.Helper()
	tokens, err := lexer.New(source).Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}
	var literals []lexer.Token
	for _, tok := range tokens {
		if tok.Type == lexer.STRING || tok.Type == lexer.REGEX {
			literals = append(literals, tok)
		}
	}
	return literals
}
