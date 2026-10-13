package vm

import (
	"testing"

	"doggo/internal/types/atom"
)

// TestCharacterLiteral は文字リテラルのテストです。
// P1-2: \a, \newline, \space, \tab, \return 形式の文字リテラルをサポート。
func TestCharacterLiteral(t *testing.T) {
	runner := MustNewRunner()

	tests := []struct {
		name     string
		input    string
		wantRune rune
		wantStr  string
	}{
		{"single_char_a", `\a`, 'a', `\a`},
		{"single_char_z", `\z`, 'z', `\z`},
		{"newline", `\newline`, '\n', `\newline`},
		{"space", `\space`, ' ', `\space`},
		{"tab", `\tab`, '\t', `\tab`},
		{"return", `\return`, '\r', `\return`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			ch, ok := result.(atom.Character)
			if !ok {
				t.Fatalf("expected Character, got %T", result)
			}
			if rune(ch) != tt.wantRune {
				t.Errorf("rune = %v, want %v", rune(ch), tt.wantRune)
			}
			if ch.String() != tt.wantStr {
				t.Errorf("String() = %v, want %v", ch.String(), tt.wantStr)
			}
		})
	}
}

// TestCharacterEquality は文字リテラルの等価性テストです。
func TestCharacterEquality(t *testing.T) {
	runner := MustNewRunner()

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"same_char_equal", `(= \a \a)`, "true"},
		{"different_char_not_equal", `(= \a \b)`, "false"},
		{"special_char_equal", `(= \newline \newline)`, "true"},
		{"space_equal", `(= \space \space)`, "true"},
		{"tab_equal", `(= \tab \tab)`, "true"},
		{"return_equal", `(= \return \return)`, "true"},
		{"different_special_not_equal", `(= \newline \space)`, "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, runner, tt.input, tt.expect)
		})
	}
}

// TestCharacterInCollection は文字リテラルをコレクション内で使用するテストです。
func TestCharacterInCollection(t *testing.T) {
	runner := MustNewRunner()

	t.Run("char_in_vector", func(t *testing.T) {
		result, err := runner.Run(`(first [\a \b \c])`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ch, ok := result.(atom.Character)
		if !ok {
			t.Fatalf("expected Character, got %T", result)
		}
		if rune(ch) != 'a' {
			t.Errorf("expected 'a', got %c", rune(ch))
		}
	})

	t.Run("char_in_list", func(t *testing.T) {
		result, err := runner.Run(`(first (rest '(\a \b \c)))`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ch, ok := result.(atom.Character)
		if !ok {
			t.Fatalf("expected Character, got %T", result)
		}
		if rune(ch) != 'b' {
			t.Errorf("expected 'b', got %c", rune(ch))
		}
	})

	t.Run("char_in_set", func(t *testing.T) {
		assertRunResult(t, runner, `(contains? #{\a \b \c} \b)`, "true")
	})
}

// TestCharacterStr は文字リテラルのstr変換テストです。
func TestCharacterStr(t *testing.T) {
	runner := MustNewRunner()

	t.Run("str_returns_char_string", func(t *testing.T) {
		assertRunResult(t, runner, `(str \a)`, `"a"`)
	})

	t.Run("str_newline_returns_actual_newline", func(t *testing.T) {
		result, err := runner.Run(`(str \newline)`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// str returns the actual character, so newline becomes "\n"
		if result.String() != "\"\n\"" {
			t.Errorf("expected newline in string, got %s", result.String())
		}
	})
}
