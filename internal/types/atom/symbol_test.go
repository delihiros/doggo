package atom

import (
	"doggo/internal/types"
	"testing"
)

func TestSymbol(t *testing.T) {
	tests := []struct {
		name     string
		symbol   types.Symbol
		expected string
	}{
		{"simple symbol", types.NewSymbol("foo"), "foo"},
		{"symbol with dash", types.NewSymbol("foo-bar"), "foo-bar"},
		{"symbol with underscore", types.NewSymbol("foo_bar"), "foo_bar"},
		{"empty symbol", types.NewSymbol(""), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.symbol.String() != tt.expected {
				t.Errorf("Symbol.String() = %v, want %v", tt.symbol.String(), tt.expected)
			}
			if tt.symbol.Type() != "symbol" {
				t.Errorf("Symbol.Type() = %v, want symbol", tt.symbol.Type())
			}
		})
	}
}
