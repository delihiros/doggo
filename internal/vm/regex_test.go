package vm

import (
	"testing"
)

// TestRegexLiteral は正規表現リテラルのテストです。
// P0-2: #"..." 形式の正規表現リテラルをサポート。
func TestRegexLiteral(t *testing.T) {
	runner := MustNewRunner()

	t.Run("regex?_returns_true_for_regex", func(t *testing.T) {
		assertRunResult(t, runner, `(regex? #"a+")`, "true")
	})

	t.Run("regex?_returns_false_for_string", func(t *testing.T) {
		assertRunResult(t, runner, `(regex? "a+")`, "false")
	})
}

// TestRePattern は re-pattern 関数のテストです。
func TestRePattern(t *testing.T) {
	runner := MustNewRunner()

	t.Run("re-pattern_from_string", func(t *testing.T) {
		assertRunResult(t, runner, `(regex? (re-pattern "a+"))`, "true")
	})

	t.Run("re-pattern_from_regex", func(t *testing.T) {
		assertRunResult(t, runner, `(regex? (re-pattern #"a+"))`, "true")
	})
}

// TestReFind は re-find 関数のテストです。
func TestReFind(t *testing.T) {
	runner := MustNewRunner()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"find_digits", `(re-find #"\d+" "abc123def")`, `"123"`},
		{"find_at_start", `(re-find #"\d+" "123abc")`, `"123"`},
		{"find_at_end", `(re-find #"\d+" "abc123")`, `"123"`},
		{"no_match", `(re-find #"\d+" "abcdef")`, "nil"},
		{"empty_string", `(re-find #"\d+" "")`, "nil"},
		{"word_pattern", `(re-find #"[a-z]+" "ABC123def456")`, `"def"`},
		{"capture_groups", `(re-find #"(\d+)-(\d+)" "x123-456y")`, `["123-456" "123" "456"]`},
		{"unmatched_group", `(re-find #"(a)?b" "b")`, `["b" nil]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestReMatches は re-matches 関数のテストです（全体マッチ）。
func TestReMatches(t *testing.T) {
	runner := MustNewRunner()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"full_match", `(re-matches #"\d+" "123")`, `"123"`},
		{"partial_no_match", `(re-matches #"\d+" "abc123")`, "nil"},
		{"partial_no_match2", `(re-matches #"\d+" "123abc")`, "nil"},
		{"no_match", `(re-matches #"\d+" "abcdef")`, "nil"},
		{"empty_pattern_empty_string", `(re-matches #"" "")`, `""`},
		{"full_match_after_shorter_alternative", `(re-matches #"a|ab" "ab")`, `"ab"`},
		{"capture_groups", `(re-matches #"(\d+)-(\d+)" "123-456")`, `["123-456" "123" "456"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestReSeq は re-seq 関数のテストです（全マッチのシーケンス）。
func TestReSeq(t *testing.T) {
	runner := MustNewRunner()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"multiple_matches", `(re-seq #"\d+" "a1b22c333")`, `["1" "22" "333"]`},
		{"single_match", `(re-seq #"\d+" "abc123def")`, `["123"]`},
		{"no_match", `(re-seq #"\d+" "abcdef")`, `[]`},
		{"empty_string", `(re-seq #"\d+" "")`, `[]`},
		{"capture_groups", `(re-seq #"([a-z])(\d)" "a1b2")`, `[["a1" "a" "1"] ["b2" "b" "2"]]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestRegexInvalidPattern は不正な正規表現パターンのテストです。
func TestRegexInvalidPattern(t *testing.T) {
	runner := MustNewRunner()

	// 不正なパターンでエラーになることを確認
	_, err := runner.Run(`(re-pattern "[")`)
	if err == nil {
		t.Error("expected error for invalid regex pattern, got nil")
	}
}
