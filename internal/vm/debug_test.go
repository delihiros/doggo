package vm

import (
	"strings"
	"testing"
)

func TestRuntimeErrorFormat(t *testing.T) {
	err := &RuntimeError{
		Message: "test error",
		Line:    42,
		StackTrace: []StackFrame{
			{Line: 42},
			{Line: 10},
			{Line: 1},
		},
	}

	errString := err.Error()
	for _, want := range []string{":42:", "test error", "Stack trace"} {
		if !strings.Contains(errString, want) {
			t.Errorf("RuntimeError.Error() = %q, want substring %q", errString, want)
		}
	}
}
