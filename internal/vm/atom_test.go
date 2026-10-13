package vm

import (
	"strings"
	"testing"
)

func TestAtomBuiltins(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		isPrefix bool // trueの場合、expectedをprefixとしてチェック
	}{
		{"create atom", `(atom 42)`, "#<Atom@", true},
		{"atom?", `(atom? (atom 42))`, "true", false},
		{"atom? false", `(atom? 42)`, "false", false},
		{"deref", `(deref (atom 42))`, "42", false},
		{"deref with @", `@(atom 42)`, "42", false},
		{"reset!", `(let [a (atom 0)] (reset! a 42) @a)`, "42", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("runner error: %s", err)
			}

			resultStr := result.String()
			if tt.isPrefix {
				if !strings.HasPrefix(resultStr, tt.expected) {
					t.Errorf("expected string starting with %q, got %q", tt.expected, resultStr)
				}
			} else {
				if resultStr != tt.expected {
					t.Errorf("expected %q, got %q", tt.expected, resultStr)
				}
			}
		})
	}
}

// TestSwapBuiltin tests the swap! builtin function (atomic CAS loop)
func TestSwapBuiltin(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "swap! with inc",
			input:    `(let [a (atom 0)] (swap! a inc) @a)`,
			expected: "1",
		},
		{
			name:     "swap! with add function",
			input:    `(let [a (atom 10)] (swap! a + 5) @a)`,
			expected: "15",
		},
		{
			name:     "swap! with assoc",
			input:    `(let [a (atom {})] (swap! a assoc :foo 1) @a)`,
			expected: "{:foo 1}",
		},
		{
			name:     "multiple swaps",
			input:    `(let [a (atom 0)] (swap! a inc) (swap! a inc) (swap! a inc) @a)`,
			expected: "3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("runner error: %s", err)
			}

			if result.String() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result.String())
			}
		})
	}
}

// TestCompareAndSetBuiltin tests the compare-and-set! builtin function
func TestCompareAndSetBuiltin(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "compare-and-set! success",
			input:    `(let [a (atom 100)] (compare-and-set! a 100 200))`,
			expected: "true",
		},
		{
			name:     "compare-and-set! value after success",
			input:    `(let [a (atom 100)] (compare-and-set! a 100 200) @a)`,
			expected: "200",
		},
		{
			name:     "compare-and-set! failure",
			input:    `(let [a (atom 100)] (compare-and-set! a 99 200))`,
			expected: "false",
		},
		{
			name:     "compare-and-set! value after failure",
			input:    `(let [a (atom 100)] (compare-and-set! a 99 200) @a)`,
			expected: "100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("runner error: %s", err)
			}

			if result.String() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result.String())
			}
		})
	}
}

// TestResetValsBuiltin tests the reset-vals! builtin function
func TestResetValsBuiltin(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "reset-vals! returns [old new]",
			input:    `(let [a (atom 10)] (reset-vals! a 20))`,
			expected: "[10 20]",
		},
		{
			name:     "reset-vals! with map",
			input:    `(let [a (atom {:count 0})] (reset-vals! a {:count 100}))`,
			expected: "[{:count 0} {:count 100}]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("runner error: %s", err)
			}

			if result.String() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result.String())
			}
		})
	}
}

// TestSwapValsBuiltin tests the swap-vals! builtin function
func TestSwapValsBuiltin(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "swap-vals! returns [old new]",
			input:    `(let [a (atom 0)] (swap-vals! a inc))`,
			expected: "[0 1]",
		},
		{
			name:     "swap-vals! with extra args",
			input:    `(let [a (atom 10)] (swap-vals! a + 5))`,
			expected: "[10 15]",
		},
		{
			name:     "swap-vals! with assoc",
			input:    `(let [a (atom {:count 0})] (swap-vals! a assoc :count 1))`,
			expected: "[{:count 0} {:count 1}]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("runner error: %s", err)
			}

			if result.String() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result.String())
			}
		})
	}
}
