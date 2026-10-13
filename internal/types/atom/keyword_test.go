package atom

import "testing"

func TestKeyword(t *testing.T) {
	tests := []struct {
		name     string
		keyword  Keyword
		expected string
	}{
		{"simple keyword", Keyword("foo"), ":foo"},
		{"keyword with dash", Keyword("foo-bar"), ":foo-bar"},
		{"empty keyword", Keyword(""), ":"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.keyword.String() != tt.expected {
				t.Errorf("Keyword.String() = %v, want %v", tt.keyword.String(), tt.expected)
			}
			if tt.keyword.Type() != "keyword" {
				t.Errorf("Keyword.Type() = %v, want keyword", tt.keyword.Type())
			}
		})
	}
}
