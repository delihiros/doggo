package vm

import (
	"testing"
)

// TestExponentialNotation は指数表記の数値リテラルをテストします。
// 1.0e-4 など、小数と符号付き指数を含む指数表記をサポートします。
func TestExponentialNotation(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string // 数値の文字列表現
	}{
		{"simple_exponent", "1e3", "1000"},
		{"exponent_with_decimal", "1.5e2", "150"},
		{"negative_exponent", "1e-2", "0.01"},
		{"uppercase_E", "1E3", "1000"},
		{"exponent_with_plus", "1e+3", "1000"},
		{"zero_exponent", "1e0", "1"},
		{"spec_example", "1.0e-4", "0.0001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			// 数値の文字列表現を比較
			if result.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result.String())
			}
		})
	}
}

// TestExponentialArithmetic は指数表記の数値が正しく演算されることをテストします。
func TestExponentialArithmetic(t *testing.T) {
	runner := MustNewRunner()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"add_exp", "(+ 1e3 1e2)", "1100"},
		{"mul_exp", "(* 1e2 1e2)", "10000"},
		{"compare_exp", "(= 1e3 1000)", "true"},
		{"def_exp", "(do (def x 1.0e-4) (* x 10000))", "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}
