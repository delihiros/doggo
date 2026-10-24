package format

import (
	"reflect"
	"testing"

	"doggo/internal/lexer"
	"doggo/internal/vm"
)

func TestFormatWhitespaceCharacterValues(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"space before LF", "[\\ \n]"},
		{"tab before LF", "[\\\t\n]"},
		{"space at EOF", "\\ "},
		{"tab at EOF", "\\\t"},
		{"space before CRLF", "[\\ \r\n]"},
		{"tab with trailing whitespace", "[\\\t \t\r\n]"},
		{"raw LF before CRLF", "[\\\n]\r\n"},
		{"raw LF with CRLF code", "(do\r\n[\\\n 1]\n)\r\n"},
		{"raw CR before LF", "[\\\r\n]\n"},
		{"raw CR before CRLF", "[\\\r\r\n]\n"},
		{"raw CR amid mixed endings", "(do\r\n[\\\r 1]\n)\r\n"},
		{"named characters", "[\\space\n\\tab\n\\newline\n\\return]\r\n"},
		{"strings regexes delimiters comments", "(do\r\n[\\\" \\; \\) \\,] ; \\ \t\n\"a \t\n b\"\n#\"c \t\r\n d\")\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := Format(tc.source, Options{TabSize: 2, InsertSpaces: true})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(significantTokens(t, tc.source), significantTokens(t, got)) {
				t.Errorf("literal token values changed: source=%q formatted=%q", tc.source, got)
			}
			before, after := whitespaceCharacterValue(t, tc.source), whitespaceCharacterValue(t, got)
			if before != after {
				t.Errorf("evaluated values changed: %q -> %q", before, after)
			}
			// Character payload bytes have the same preservation contract as strings.
			beforeChars, afterChars := whitespaceCharacterTokens(t, tc.source), whitespaceCharacterTokens(t, got)
			if !reflect.DeepEqual(beforeChars, afterChars) {
				t.Errorf("character bytes changed: %q -> %q", beforeChars, afterChars)
			}
			second, _, err := Format(got, Options{TabSize: 2, InsertSpaces: true})
			if err != nil || second != got {
				t.Errorf("not idempotent: first=%q second=%q error=%v", got, second, err)
			}
		})
	}
}

func TestSplitSourceLinesPreservesCharacterEndings(t *testing.T) {
	source := "[\\ \n\\\t\r\n\\\n\\\r\n]"
	want := []sourceLine{
		{text: "[\\ ", ending: "\n"},
		{text: "\\\t", ending: "\r\n"},
		{text: "\\", ending: "\n", literalEnding: true},
		{text: "\\", ending: "\r\n", literalEnding: true},
		{text: "]"},
	}
	if got := splitSourceLines(source); !reflect.DeepEqual(got, want) {
		t.Fatalf("split lines=%#v want=%#v", got, want)
	}
}

func TestFormatIncompleteSourceRemainsTolerant(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"(", "(\n"},
		{"\\", "\\\n"},
		{"\\bogus", "\\bogus\n"},
		{"(do\n\"unfinished \t", "(do\n  \"unfinished \t"},
		{"(do\n; \\ \n", "(do\n  ; \\\n"},
	} {
		got, diagnostics, err := Format(tc.source, Options{TabSize: 2, InsertSpaces: true})
		if err != nil || len(diagnostics) != 0 || got != tc.want {
			t.Errorf("Format(%q)=%q, %v, %v; want=%q", tc.source, got, diagnostics, err, tc.want)
		}
		second, _, err := Format(got, Options{TabSize: 2, InsertSpaces: true})
		if err != nil || second != got {
			t.Errorf("incomplete source not idempotent: first=%q second=%q error=%v", got, second, err)
		}
	}
}

func whitespaceCharacterTokens(t *testing.T, source string) []string {
	t.Helper()
	tokens, err := lexer.New(source).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	var chars []string
	for _, tok := range tokens {
		if tok.Type == lexer.CHAR {
			chars = append(chars, source[tok.Offset:tok.EndOffset])
		}
	}
	return chars
}

func whitespaceCharacterValue(t *testing.T, source string) string {
	t.Helper()
	runner, err := vm.NewRunnerWithConfig(vm.RunnerConfig{BootstrapCore: true})
	if err != nil {
		t.Fatal(err)
	}
	value, err := runner.Run(source)
	if err != nil {
		t.Fatalf("evaluating %q: %v", source, err)
	}
	return value.String()
}
