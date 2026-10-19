package format

import (
	"reflect"
	"testing"

	"doggo/internal/lexer"
	"doggo/internal/parser"
)

func TestFormatCharacterLiteralsPreserveMultilineLiterals(t *testing.T) {
	characters := []struct{ name, raw string }{
		{"quote", `\"`},
		{"semicolon", `\;`},
		{"open paren", `\(`},
		{"close paren", `\)`},
		{"open bracket", `\[`},
		{"close bracket", `\]`},
		{"open brace", `\{`},
		{"close brace", `\}`},
		{"comma", `\,`},
		{"backslash", `\\`},
		{"ordinary control", `\a`},
		{"named control", `\newline`},
	}
	for _, character := range characters {
		for _, newline := range []struct{ name, raw string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
			for _, kind := range []struct {
				name, prefix string
				tokenType    lexer.TokenType
			}{{"STRING", "", lexer.STRING}, {"REGEX", "#", lexer.REGEX}} {
				for _, sameLine := range []bool{false, true} {
					placement := "next line"
					separator, wantSeparator := newline.raw, newline.raw+"  "
					if sameLine {
						placement = "same line"
						separator, wantSeparator = " ", " "
					}
					t.Run(character.name+"/"+newline.name+"/"+kind.name+"/"+placement, func(t *testing.T) {
						literal := kind.prefix + `"alpha` + " \t \n" + ` beta\"; ` + "\t\rgamma" + `\\` + " \t\r\n" + ` tail  "`
						characterForm := "(def c " + character.raw + ")"
						literalForm := "(def value " + literal + ")"
						source := "(do" + newline.raw + " \t" + characterForm + separator + literalForm + " \t" + newline.raw +
							" \t(println value) \t" + newline.raw + " )" + newline.raw
						want := "(do" + newline.raw + "  " + characterForm + wantSeparator + literalForm + newline.raw +
							"  (println value)" + newline.raw + ")" + newline.raw

						before := literalTokens(t, source)
						if len(before) != 1 || before[0].Type != kind.tokenType {
							t.Fatalf("fixture literal tokens = %v, want one %s", before, kind.name)
						}
						tokens, err := lexer.New(source).Tokenize()
						if err != nil {
							t.Fatal(err)
						}
						if _, err := parser.New(tokens, source).ParseAll(); err != nil {
							t.Fatalf("original fixture is invalid: %v", err)
						}

						got, _, err := Format(source, Options{TabSize: 2, InsertSpaces: true})
						if err != nil {
							t.Fatal(err)
						}
						if got != want {
							t.Errorf("formatted output\nwant=%q\ngot =%q", want, got)
						}
						after := literalTokens(t, got)
						if len(after) != 1 || after[0].Type != kind.tokenType {
							t.Fatalf("literal token changed: %v", after)
						}
						if after[0].Literal != before[0].Literal {
							t.Errorf("literal value changed: %q to %q", before[0].Literal, after[0].Literal)
						}
						if raw := got[after[0].Offset:after[0].EndOffset]; raw != literal {
							t.Errorf("literal bytes changed: %q to %q", literal, raw)
						}
						if !reflect.DeepEqual(significantTokens(t, source), significantTokens(t, got)) {
							t.Errorf("format changed significant tokens")
						}
						second, _, err := Format(got, Options{TabSize: 2, InsertSpaces: true})
						if err != nil || second != got {
							t.Errorf("format not idempotent: first=%q second=%q error=%v", got, second, err)
						}
					})
				}
			}
		}
	}
}

func TestSplitSourceLinesCharacterLiteralsRespectCommentCR(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		want   []sourceLine
	}{
		{
			name:   "semicolon character before bare CR code",
			source: "(def c \\;)\r(def value \"alpha   \r\nbeta\")\r; hidden\r\" ) [\n(def tail 1)\n",
			want: []sourceLine{
				{text: `(def c \;)`, ending: "\r"},
				{text: `(def value "alpha   `, ending: "\r\n"},
				{text: `beta")`, ending: "\r"},
				{text: "; hidden\r\" ) [", ending: "\n"},
				{text: "(def tail 1)", ending: "\n"},
			},
		},
		{
			name:   "quote character before real comment",
			source: "(def c \\\") ; hidden\r\" ) [\n(def value \"alpha   \r\nbeta\")\n",
			want: []sourceLine{
				{text: "(def c \\\") ; hidden\r\" ) [", ending: "\n"},
				{text: `(def value "alpha   `, ending: "\r\n"},
				{text: `beta")`, ending: "\n"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := splitSourceLines(tc.source); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitSourceLines\nwant=%q\ngot =%q", tc.want, got)
			}
		})
	}
}

func TestFormatPreservesRawCRCharacterBeforeLF(t *testing.T) {
	source := "[\\\r\n]\n"
	got, _, err := Format(source, Options{TabSize: 2, InsertSpaces: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := "[\\\r\n]\r\n"; got != want {
		t.Errorf("formatted output\nwant=%q\ngot =%q", want, got)
	}
	if !reflect.DeepEqual(significantTokens(t, source), significantTokens(t, got)) {
		t.Errorf("format changed raw CR character")
	}
	second, _, err := Format(got, Options{TabSize: 2, InsertSpaces: true})
	if err != nil || second != got {
		t.Errorf("format not idempotent: first=%q second=%q error=%v", got, second, err)
	}
}
