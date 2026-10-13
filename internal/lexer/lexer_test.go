package lexer

import (
	"fmt"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	l := New("test")
	if l == nil {
		t.Fatal("New() returned nil")
	}
	if l.input != "test" {
		t.Errorf("New().input = %v, want test", l.input)
	}
	if l.line != 1 {
		t.Errorf("New().line = %v, want 1", l.line)
	}
	// columnは最初の文字を読み込んだ後なので2になる
	if l.column != 2 {
		t.Errorf("New().column = %v, want 2", l.column)
	}
}

func TestStringEscapes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "backspace", input: `"a\b"`, expected: "a\b"},
		{name: "formfeed", input: `"a\f"`, expected: "a\f"},
		{name: "unicode", input: `"A\u2603Z"`, expected: "A\u2603Z"},
		{name: "unicode surrogate pair", input: `"\uD834\uDD1E"`, expected: "\U0001D11E"},
		{name: "octal", input: `"\101"`, expected: "A"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := New(tt.input).NextToken()
			if token.Type != STRING {
				t.Fatalf("expected STRING token, got %v", token.Type)
			}
			if token.Literal != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, token.Literal)
			}
		})
	}
}

func TestStringEscapesRejectLoneSurrogates(t *testing.T) {
	for _, input := range []string{`"\uDEAD"`, `"\uD834x"`, `"\uD834\u0041"`} {
		if _, err := New(input).Tokenize(); err == nil {
			t.Errorf("expected lone surrogate rejection for %s", input)
		}
	}
}

func TestNextToken(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Token
	}{
		{
			"empty string",
			"",
			[]Token{{Type: EOF, Literal: ""}},
		},
		{
			"identifiers",
			"foo bar baz",
			[]Token{
				{Type: IDENT, Literal: "foo"},
				{Type: IDENT, Literal: "bar"},
				{Type: IDENT, Literal: "baz"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"numbers",
			"123 456 3.14",
			[]Token{
				{Type: NUMBER, Literal: "123"},
				{Type: NUMBER, Literal: "456"},
				{Type: NUMBER, Literal: "3.14"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"negative numbers",
			"-10 -2.5 -0.5",
			[]Token{
				{Type: NUMBER, Literal: "-10"},
				{Type: NUMBER, Literal: "-2.5"},
				{Type: NUMBER, Literal: "-0.5"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"exponential notation",
			"1e3 1.0e-4 1E+2 0e0 2.5E10",
			[]Token{
				{Type: NUMBER, Literal: "1e3"},
				{Type: NUMBER, Literal: "1.0e-4"},
				{Type: NUMBER, Literal: "1E+2"},
				{Type: NUMBER, Literal: "0e0"},
				{Type: NUMBER, Literal: "2.5E10"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"negative exponential",
			"-1e3 -2.5e-4",
			[]Token{
				{Type: NUMBER, Literal: "-1e3"},
				{Type: NUMBER, Literal: "-2.5e-4"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"strings",
			`"hello" "world"`,
			[]Token{
				{Type: STRING, Literal: "hello"},
				{Type: STRING, Literal: "world"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"keywords",
			":foo :bar :",
			[]Token{
				{Type: KEYWORD, Literal: "foo"},
				{Type: KEYWORD, Literal: "bar"},
				{Type: KEYWORD, Literal: ""},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"namespaced keyword with dotted namespace",
			":doggo.schema/invalid",
			[]Token{
				{Type: KEYWORD, Literal: "doggo.schema/invalid"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"parentheses",
			"()",
			[]Token{
				{Type: LPAREN, Literal: "("},
				{Type: RPAREN, Literal: ")"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"brackets",
			"[]",
			[]Token{
				{Type: LBRACKET, Literal: "["},
				{Type: RBRACKET, Literal: "]"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"braces",
			"{}",
			[]Token{
				{Type: LBRACE, Literal: "{"},
				{Type: RBRACE, Literal: "}"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"hashset",
			"#{}",
			[]Token{
				{Type: HASHSET, Literal: "#{"},
				{Type: RBRACE, Literal: "}"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"hashset with content",
			"#{:a :b}",
			[]Token{
				{Type: HASHSET, Literal: "#{"},
				{Type: KEYWORD, Literal: "a"},
				{Type: KEYWORD, Literal: "b"},
				{Type: RBRACE, Literal: "}"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"regex literal simple",
			`#"\d+"`,
			[]Token{
				{Type: REGEX, Literal: `\d+`},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"regex literal with escapes",
			`#"a\nb"`,
			[]Token{
				{Type: REGEX, Literal: `a\nb`},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"regex in expression",
			`(re-find #"\d+" "abc123")`,
			[]Token{
				{Type: LPAREN, Literal: "("},
				{Type: IDENT, Literal: "re-find"},
				{Type: REGEX, Literal: `\d+`},
				{Type: STRING, Literal: "abc123"},
				{Type: RPAREN, Literal: ")"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"discard macro simple",
			`#_ foo`,
			[]Token{
				{Type: DISCARD, Literal: "#_"},
				{Type: IDENT, Literal: "foo"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"discard macro in expression",
			`(+ 1 #_ 2 3)`,
			[]Token{
				{Type: LPAREN, Literal: "("},
				{Type: IDENT, Literal: "+"},
				{Type: NUMBER, Literal: "1"},
				{Type: DISCARD, Literal: "#_"},
				{Type: NUMBER, Literal: "2"},
				{Type: NUMBER, Literal: "3"},
				{Type: RPAREN, Literal: ")"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"discard macro with list",
			`#_ (expensive-call) result`,
			[]Token{
				{Type: DISCARD, Literal: "#_"},
				{Type: LPAREN, Literal: "("},
				{Type: IDENT, Literal: "expensive-call"},
				{Type: RPAREN, Literal: ")"},
				{Type: IDENT, Literal: "result"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"char literal single",
			`\a \b \c`,
			[]Token{
				{Type: CHAR, Literal: "a"},
				{Type: CHAR, Literal: "b"},
				{Type: CHAR, Literal: "c"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"char literal special names",
			`\newline \space \tab \return`,
			[]Token{
				{Type: CHAR, Literal: "newline"},
				{Type: CHAR, Literal: "space"},
				{Type: CHAR, Literal: "tab"},
				{Type: CHAR, Literal: "return"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"char literal in expression",
			`(= \a \b)`,
			[]Token{
				{Type: LPAREN, Literal: "("},
				{Type: IDENT, Literal: "="},
				{Type: CHAR, Literal: "a"},
				{Type: CHAR, Literal: "b"},
				{Type: RPAREN, Literal: ")"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"char literal in vector",
			`[\a \newline]`,
			[]Token{
				{Type: LBRACKET, Literal: "["},
				{Type: CHAR, Literal: "a"},
				{Type: CHAR, Literal: "newline"},
				{Type: RBRACKET, Literal: "]"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"quotes",
			"'`~",
			[]Token{
				{Type: QUOTE, Literal: "'"},
				{Type: QUASIQUOTE, Literal: "`"},
				{Type: UNQUOTE, Literal: "~"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"splice",
			"~@",
			[]Token{
				{Type: SPLICE, Literal: "~@"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"dot",
			".",
			[]Token{
				{Type: DOT, Literal: "."},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"dot method syntax",
			".WriteString",
			[]Token{
				{Type: IDENT, Literal: ".WriteString"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"dot method syntax in list",
			"(.WriteString builder \"hello\")",
			[]Token{
				{Type: LPAREN, Literal: "("},
				{Type: IDENT, Literal: ".WriteString"},
				{Type: IDENT, Literal: "builder"},
				{Type: STRING, Literal: "hello"},
				{Type: RPAREN, Literal: ")"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"double dot macro",
			".. obj method",
			[]Token{
				{Type: IDENT, Literal: ".."},
				{Type: IDENT, Literal: "obj"},
				{Type: IDENT, Literal: "method"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"dot followed by number stays DOT",
			". 123",
			[]Token{
				{Type: DOT, Literal: "."},
				{Type: NUMBER, Literal: "123"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"method call with dot special form",
			"(. obj WriteString)",
			[]Token{
				{Type: LPAREN, Literal: "("},
				{Type: DOT, Literal: "."},
				{Type: IDENT, Literal: "obj"},
				{Type: IDENT, Literal: "WriteString"},
				{Type: RPAREN, Literal: ")"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"special chars in identifier",
			"foo-bar foo_bar + - * / = < > ! ?",
			[]Token{
				{Type: IDENT, Literal: "foo-bar"},
				{Type: IDENT, Literal: "foo_bar"},
				{Type: IDENT, Literal: "+"},
				{Type: IDENT, Literal: "-"},
				{Type: IDENT, Literal: "*"},
				{Type: IDENT, Literal: "/"},
				{Type: IDENT, Literal: "="},
				{Type: IDENT, Literal: "<"},
				{Type: IDENT, Literal: ">"},
				{Type: IDENT, Literal: "!"},
				{Type: IDENT, Literal: "?"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"comment",
			"foo\nbar",
			[]Token{
				{Type: IDENT, Literal: "foo"},
				{Type: IDENT, Literal: "bar"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"whitespace",
			"  \t\n  foo  \t\n  ",
			[]Token{
				{Type: IDENT, Literal: "foo"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"list expression",
			"(+ 1 2)",
			[]Token{
				{Type: LPAREN, Literal: "("},
				{Type: IDENT, Literal: "+"},
				{Type: NUMBER, Literal: "1"},
				{Type: NUMBER, Literal: "2"},
				{Type: RPAREN, Literal: ")"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"vector expression",
			"[1 2 3]",
			[]Token{
				{Type: LBRACKET, Literal: "["},
				{Type: NUMBER, Literal: "1"},
				{Type: NUMBER, Literal: "2"},
				{Type: NUMBER, Literal: "3"},
				{Type: RBRACKET, Literal: "]"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"map expression",
			"{:a 1 :b 2}",
			[]Token{
				{Type: LBRACE, Literal: "{"},
				{Type: KEYWORD, Literal: "a"},
				{Type: NUMBER, Literal: "1"},
				{Type: KEYWORD, Literal: "b"},
				{Type: NUMBER, Literal: "2"},
				{Type: RBRACE, Literal: "}"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"comma ignored",
			"foo, bar",
			[]Token{
				{Type: IDENT, Literal: "foo"},
				{Type: IDENT, Literal: "bar"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"comma in map",
			"{:tag Boolean, :added \"1.0\"}",
			[]Token{
				{Type: LBRACE, Literal: "{"},
				{Type: KEYWORD, Literal: "tag"},
				{Type: IDENT, Literal: "Boolean"},
				{Type: KEYWORD, Literal: "added"},
				{Type: STRING, Literal: "1.0"},
				{Type: RBRACE, Literal: "}"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"multiple commas",
			"a, b, c",
			[]Token{
				{Type: IDENT, Literal: "a"},
				{Type: IDENT, Literal: "b"},
				{Type: IDENT, Literal: "c"},
				{Type: EOF, Literal: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(tt.input)
			for i, expected := range tt.expected {
				tok := l.NextToken()
				if tok.Type != expected.Type {
					t.Errorf("token[%d].Type = %v, want %v", i, tok.Type, expected.Type)
				}
				if tok.Literal != expected.Literal {
					t.Errorf("token[%d].Literal = %v, want %v", i, tok.Literal, expected.Literal)
				}
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"simple", "foo"},
		{"expression", "(+ 1 2)"},
		{"multiple lines", "foo\nbar\nbaz"},
		{"with comments", "; comment\nfoo ; another comment\nbar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(tt.input)
			tokens, err := l.Tokenize()
			if err != nil {
				t.Fatalf("Tokenize() error = %v", err)
			}
			if len(tokens) == 0 {
				t.Error("Tokenize() returned empty slice")
			}
			last := tokens[len(tokens)-1]
			if last.Type != EOF {
				t.Errorf("last token type = %v, want EOF", last.Type)
			}
		})
	}
}

func TestLineColumn(t *testing.T) {
	// 改行を含む文字列でテスト
	l := New("foo\n  bar")
	tokens, _ := l.Tokenize()

	// fooは1行目にあるはず（NextTokenでskipWhitespaceの後、位置を保存するため）
	if tokens[0].Line != 1 {
		t.Errorf("foo token line = %v, want 1", tokens[0].Line)
	}

	// barは2行目にあるはず
	barToken := tokens[1]
	if barToken.Line != 2 {
		t.Errorf("bar token line = %v, want 2", barToken.Line)
	}
}

func TestStringEscapeSequences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"newline", `"hello\nworld"`, "hello\nworld"},
		{"tab", `"hello\tworld"`, "hello\tworld"},
		{"carriage return", `"hello\rworld"`, "hello\rworld"},
		{"backslash", `"hello\\world"`, "hello\\world"},
		{"quote", `"hello\"world"`, "hello\"world"},
		{"mixed", `"hello\n\tworld"`, "hello\n\tworld"},
		{"no escape", `"hello world"`, "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(tt.input)
			tok := l.NextToken()
			if tok.Type != STRING {
				t.Errorf("token type = %v, want STRING", tok.Type)
			}
			if tok.Literal != tt.expected {
				t.Errorf("token literal = %q, want %q", tok.Literal, tt.expected)
			}
		})
	}
}

func TestStringUnclosed(t *testing.T) {
	// 未閉じの文字列リテラル
	l := New(`"hello world`)
	tokens, err := l.Tokenize()
	if err == nil {
		t.Error("Tokenize() should return error for unclosed string")
	}
	// エラーメッセージに行番号情報が含まれていることを確認
	if err != nil && !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error message should contain line number, got: %v", err)
	}
	// エラーが返された場合、トークンは空または部分的に生成される可能性がある
	if err == nil && len(tokens) == 0 {
		t.Error("Tokenize() returned empty slice")
	}
}

func TestStringIncompleteEscape(t *testing.T) {
	// 不完全なエスケープシーケンス（文字列の終端でバックスラッシュ）
	l := New(`"hello\`)
	tokens, err := l.Tokenize()
	if err == nil {
		t.Error("Tokenize() should return error for incomplete escape sequence")
	}
	// エラーメッセージに行番号情報が含まれていることを確認
	if err != nil && !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error message should contain line number, got: %v", err)
	}
	// エラーが返された場合、トークンは空または部分的に生成される可能性がある
	if err == nil && len(tokens) == 0 {
		t.Error("Tokenize() returned empty slice")
	}
}

func TestStringUnknownEscape(t *testing.T) {
	l := New(`"hello\zworld"`)
	_, err := l.Tokenize()
	if err == nil {
		t.Fatal("Tokenize() should return error for unknown escape sequence")
	}
	if !strings.Contains(err.Error(), "unknown escape sequence") {
		t.Fatalf("expected unknown escape sequence error, got %v", err)
	}
}

func TestCommentHandling(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			"comment at start",
			"; comment\nfoo",
			[]Token{
				{Type: IDENT, Literal: "foo"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"comment after code",
			"foo ; comment",
			[]Token{
				{Type: IDENT, Literal: "foo"},
				{Type: EOF, Literal: ""},
			},
		},
		{
			"multiple comments",
			"; first\nfoo ; second\nbar",
			[]Token{
				{Type: IDENT, Literal: "foo"},
				{Type: IDENT, Literal: "bar"},
				{Type: EOF, Literal: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(tt.input)
			tokens, err := l.Tokenize()
			if err != nil {
				t.Fatalf("Tokenize() error = %v", err)
			}

			if len(tokens) != len(tt.want) {
				t.Errorf("Tokenize() returned %d tokens, want %d", len(tokens), len(tt.want))
				return
			}

			for i, expected := range tt.want {
				if tokens[i].Type != expected.Type {
					t.Errorf("token[%d].Type = %v, want %v", i, tokens[i].Type, expected.Type)
				}
				if tokens[i].Literal != expected.Literal {
					t.Errorf("token[%d].Literal = %q, want %q", i, tokens[i].Literal, expected.Literal)
				}
			}
		})
	}
}

func TestIllegalToken(t *testing.T) {
	// バックスラッシュ単独は文字リテラルとしてパースされるが、
	// パーサーレベルで「無効な文字リテラル」エラーになる。
	// Lexer レベルでは CHAR トークンとして返される。
	l := New("\\")
	tok := l.NextToken()
	if tok.Type != CHAR {
		t.Errorf("token type = %v, want CHAR", tok.Type)
	}
	if tok.Literal != "" {
		t.Errorf("token literal = %q, want empty", tok.Literal)
	}
}

func TestDerefToken(t *testing.T) {
	// @はderefリーダーマクロとして有効なトークン
	l := New("@")
	tok := l.NextToken()
	if tok.Type != DEREF {
		t.Errorf("token type = %v, want DEREF", tok.Type)
	}
}

// TestLexerErrorMessages はlexerのエラーメッセージに行番号とカラム番号が含まれることを確認します。
func TestLexerErrorMessages(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedLine   int
		expectedColumn int
		errorContains  string
	}{
		{
			name:           "unclosed string single line",
			input:          `"hello world`,
			expectedLine:   1,
			expectedColumn: 2,
			errorContains:  "unclosed string",
		},
		{
			name:           "unclosed string multi line",
			input:          "foo\nbar\n\"unclosed string",
			expectedLine:   3,
			expectedColumn: 2,
			errorContains:  "unclosed string",
		},
		{
			name:           "incomplete escape sequence",
			input:          `"hello\`,
			expectedLine:   1,
			expectedColumn: 9, // エスケープシーケンス検出時の位置
			errorContains:  "incomplete escape sequence",
		},
		// 注意: バックスラッシュ（\）は文字リテラルとして扱われるようになったため、
		// 「illegal character」テストは削除されました。
		// バックスラッシュ単独（\\）は CHAR トークンとして lexer を通過し、
		// parser レベルで「invalid character literal」エラーになります。
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(tt.input)
			_, err := l.Tokenize()
			if err == nil {
				t.Fatal("expected error but got nil")
			}

			errMsg := err.Error()
			// エラーメッセージに行番号が含まれることを確認
			if !strings.Contains(errMsg, fmt.Sprintf("line %d", tt.expectedLine)) {
				t.Errorf("error message should contain 'line %d', got: %s", tt.expectedLine, errMsg)
			}
			// エラーメッセージにカラム番号が含まれることを確認
			if !strings.Contains(errMsg, fmt.Sprintf("column %d", tt.expectedColumn)) {
				t.Errorf("error message should contain 'column %d', got: %s", tt.expectedColumn, errMsg)
			}
			// エラーメッセージに期待する文字列が含まれることを確認
			if !strings.Contains(errMsg, tt.errorContains) {
				t.Errorf("error message should contain '%s', got: %s", tt.errorContains, errMsg)
			}
		})
	}
}

func FuzzLexerDoesNotPanic(f *testing.F) {
	for _, seed := range []string{
		"",
		`(+ 1 2)`,
		`{:message "hello" :values [1 2 3]}`,
		`#_discarded #'qualified/name`,
		"\x00\xff\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		_, _ = New(source).Tokenize()
	})
}
