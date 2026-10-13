package vm

import (
	"strings"
	"testing"
)

// TestStackTraceFunctionName tests that function names appear in stack traces
func TestStackTraceFunctionName(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectInTrace  []string // strings that should appear in the error message
		expectNotTrace []string // strings that should NOT appear
	}{
		{
			name: "def_with_anonymous_fn_shows_def_name",
			input: `
(def foo (fn [] (throw (ex-info "error in foo" {:kind :test}))))
(foo)
`,
			expectInTrace: []string{"in foo"},
		},
		{
			name: "def_with_named_fn_shows_fn_name",
			input: `
(def foo (fn bar [] (throw (ex-info "error in bar" {:kind :test}))))
(foo)
`,
			expectInTrace: []string{"in bar"},
		},
		{
			name: "nested_calls_show_multiple_function_names",
			input: `
(def inner (fn [] (throw (ex-info "deep error" {:kind :test}))))
(def outer (fn [] (inner)))
(outer)
`,
			expectInTrace: []string{"in inner", "in outer"},
		},
		{
			name: "defn_shows_function_name",
			input: `
(defn my-func [] (throw (ex-info "error" {:kind :test})))
(my-func)
`,
			expectInTrace: []string{"in my-func"},
		},
		{
			name: "deeply_nested_calls",
			input: `
(def a (fn [] (throw (ex-info "bottom" {:kind :test}))))
(def b (fn [] (a)))
(def c (fn [] (b)))
(c)
`,
			expectInTrace: []string{"in a", "in b", "in c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			_, err := runner.Run(tt.input)

			if err == nil {
				t.Fatal("expected an error, got nil")
			}

			errStr := err.Error()

			// Check expected strings are present
			for _, expect := range tt.expectInTrace {
				if !strings.Contains(errStr, expect) {
					t.Errorf("expected error to contain %q, got:\n%s", expect, errStr)
				}
			}

			// Check unexpected strings are absent
			for _, notExpect := range tt.expectNotTrace {
				if strings.Contains(errStr, notExpect) {
					t.Errorf("expected error NOT to contain %q, got:\n%s", notExpect, errStr)
				}
			}
		})
	}
}

// TestStackTraceRuntimeError tests RuntimeError structure with function names
func TestStackTraceRuntimeError(t *testing.T) {
	runner := MustNewRunner()
	_, err := runner.Run(`
(def foo (fn [] (throw (ex-info "test error" {:kind :test}))))
(def bar (fn [] (foo)))
(bar)
`)

	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	// Check that it's a RuntimeError with proper stack trace
	rtErr, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("expected *RuntimeError, got %T", err)
	}

	// Stack trace should have at least 2 frames (foo and bar)
	if len(rtErr.StackTrace) < 2 {
		t.Errorf("expected at least 2 stack frames, got %d", len(rtErr.StackTrace))
	}

	// Check that function names are present in frames
	foundFoo := false
	foundBar := false
	for _, frame := range rtErr.StackTrace {
		if frame.Name == "foo" {
			foundFoo = true
		}
		if frame.Name == "bar" {
			foundBar = true
		}
	}

	if !foundFoo {
		t.Error("expected to find 'foo' in stack trace frames")
	}
	if !foundBar {
		t.Error("expected to find 'bar' in stack trace frames")
	}
}
