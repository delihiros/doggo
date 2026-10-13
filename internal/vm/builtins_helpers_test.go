package vm

import "testing"

func TestMaterializingBuiltinsRejectNonCollections(t *testing.T) {
	runner := MustNewRunner()
	for _, expression := range []string{
		`(second 42)`,
		`(last 42)`,
		`(distinct 42)`,
		`(sort 42)`,
		`(group-by identity 42)`,
		`(zipmap 42 [1])`,
	} {
		assertRunErrorContains(t, runner, expression, "expected a collection, got number")
	}
}

func TestVmErrorToDoggoError(t *testing.T) {
	tests := []struct {
		name     string
		vmError  *Error
		expected string
	}{
		{
			name: "type error",
			vmError: &Error{
				Message:  "split: expected string, got number",
				Kind:     "type",
				Context:  "split",
				Expected: "string",
				Got:      "number",
			},
			expected: "type error: split: expected string, got number",
		},
		{
			name: "argument error",
			vmError: &Error{
				Message: "split: wrong number of arguments",
				Kind:    "argument",
				Context: "split",
			},
			expected: "argument error: wrong number of arguments",
		},
		{
			name: "runtime error",
			vmError: &Error{
				Message: "range: step cannot be zero",
				Kind:    "runtime",
				Context: "range",
			},
			expected: "evaluator error: range: step cannot be zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := vmErrorToDoggoError(tt.vmError)
			if result == nil {
				t.Fatal("vmErrorToDoggoError() returned nil")
			}
			if result.Error() != tt.expected {
				t.Errorf("vmErrorToDoggoError().Error() = %q, want %q", result.Error(), tt.expected)
			}
		})
	}
}
