package vm

import (
	"strings"
	"testing"
)

// TestReplNeedsMoreInput_Continuation tests cases where REPL should continue reading
func TestReplNeedsMoreInput_Continuation(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		// 未閉じリスト
		{"unclosed list", "(+ 1 2"},
		{"unclosed nested list", "(+ (* 2"},
		{"unclosed vector", "[1 2"},
		{"unclosed map", "{:a 1"},
		{"unclosed set", "#{1 2"},

		// 未閉じ文字列
		{"unclosed string", `"hello`},
		{"unclosed string with escaped quote", `"hello\"`},

		// 未閉じ正規表現
		{"unclosed regex", `#"abc`},

		// メタデータの後にフォームがない
		{"metadata without form - map", "^{:a 1}"},
		{"metadata without form - keyword", "^:private"},

		// クォート系の後にフォームがない
		{"quote without form", "'"},
		{"quasiquote without form", "`"},
		{"unquote without form", "~"},
		{"splice without form", "~@"},
		{"deref without form", "@"},
		{"var-quote without form", "#'"},

		// 複数行のケース
		{"multiline unclosed list", "(defn foo\n  [x]\n  (+ x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needsMore, err := replNeedsMoreInput(tt.input)
			if err != nil {
				t.Errorf("replNeedsMoreInput(%q) returned unexpected error: %v", tt.input, err)
			}
			if !needsMore {
				t.Errorf("replNeedsMoreInput(%q) = false, want true (should continue)", tt.input)
			}
		})
	}
}

// TestReplNeedsMoreInput_Complete tests cases where input is complete
func TestReplNeedsMoreInput_Complete(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		// 完全な式
		{"simple expression", "(+ 1 2)"},
		{"nested expression", "(+ (* 2 3) 4)"},
		{"vector", "[1 2 3]"},
		{"map", "{:a 1 :b 2}"},
		{"set", "#{1 2 3}"},
		{"string", `"hello"`},
		{"regex", `#"\\d+"`},
		{"keyword", ":foo"},
		{"number", "42"},
		{"symbol", "foo"},

		// コメント内の括弧は無視される
		{"brackets in comment", "(+ 1 2) ; ) ] }"},
		{"brackets in comment multiline", "(+ 1 2)\n; (unclosed"},

		// 文字列内の括弧は無視される
		{"brackets in string", `(str ")")`},
		{"brackets in string 2", `(println "[{}")`},

		// メタデータ付き完全式
		{"metadata with form", "^{:a 1} [1 2]"},
		{"metadata keyword with form", "^:private (defn foo [])"},

		// 複数の式
		{"multiple expressions", "(+ 1 2) (* 3 4)"},

		// 空入力は完了扱い（REPLでは空行は無視される）
		{"empty input", ""},
		{"whitespace only", "   \n  "},
		{"comment only", "; comment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needsMore, err := replNeedsMoreInput(tt.input)
			if err != nil {
				t.Errorf("replNeedsMoreInput(%q) returned unexpected error: %v", tt.input, err)
			}
			if needsMore {
				t.Errorf("replNeedsMoreInput(%q) = true, want false (should be complete)", tt.input)
			}
		})
	}
}

// TestReplNeedsMoreInput_Error tests cases where input has a definite error
func TestReplNeedsMoreInput_Error(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		errorContains string
	}{
		// 閉じ括弧の種別違い
		{"mismatched paren-brace", "(+ 1 2}", "unexpected token"},
		{"mismatched bracket-paren", "[1 2)", "unexpected token"},

		// 余分な閉じ括弧
		{"extra close paren", "(+ 1 2))", "unexpected token"},
		{"extra close bracket", "[1 2]]", "unexpected token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needsMore, err := replNeedsMoreInput(tt.input)
			if needsMore {
				t.Errorf("replNeedsMoreInput(%q) = true, want false (should be error)", tt.input)
			}
			if err == nil {
				t.Errorf("replNeedsMoreInput(%q) returned no error, want error containing %q", tt.input, tt.errorContains)
			} else if !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("replNeedsMoreInput(%q) error = %v, want error containing %q", tt.input, err, tt.errorContains)
			}
		})
	}
}

// TestReplNeedsMoreInput_EdgeCases tests edge cases
func TestReplNeedsMoreInput_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		needsMore bool
		hasError  bool
	}{
		// 特殊文字
		{"character literal", `\a`, false, false},
		{"character newline", `\newline`, false, false},

		// 深くネストした式
		{"deeply nested complete", "((((((1))))))", false, false},
		{"deeply nested incomplete", "(((((1))))", true, false},

		// 混合ケース
		{"list with string containing parens", `(println "(")`, false, false},
		{"incomplete after complete", "(+ 1 2) (", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needsMore, err := replNeedsMoreInput(tt.input)
			if needsMore != tt.needsMore {
				t.Errorf("replNeedsMoreInput(%q) needsMore = %v, want %v", tt.input, needsMore, tt.needsMore)
			}
			if tt.hasError && err == nil {
				t.Errorf("replNeedsMoreInput(%q) expected error but got none", tt.input)
			}
			if !tt.hasError && err != nil {
				t.Errorf("replNeedsMoreInput(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}
