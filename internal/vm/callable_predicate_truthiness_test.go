package vm

import "testing"

func TestCallablePredicatesTreatNilAsFalse(t *testing.T) {
	runner := MustNewRunner()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"filter_empty_map", `(pr-str (filter {} ["a" "b"]))`, `"()"`},
		{"filter_map_lookup", `(pr-str (filter {"a" true} ["a" "b"]))`, `"(\"a\")"`},
		{"every_empty_map", `(every? {} ["a"])`, `false`},
		{"some_empty_map", `(pr-str (some {} ["a"]))`, `"nil"`},
		{"remove_empty_map", `(pr-str (remove {} ["a"]))`, `"(\"a\")"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("runner error: %v", err)
			}
			if got := result.String(); got != tt.expected {
				t.Fatalf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}
