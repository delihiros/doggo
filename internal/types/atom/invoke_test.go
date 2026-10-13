package atom

import (
	"context"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/coll"
)

func TestKeywordInvoke(t *testing.T) {
	kw := NewKeyword("a")
	m := coll.NewMap(map[types.Value]types.Value{
		NewKeyword("a"): NewNumberInt(1),
		NewKeyword("b"): NewNumberInt(2),
	})

	tests := []struct {
		name    string
		args    []types.Value
		want    types.Value
		wantErr bool
	}{
		{"existing key", []types.Value{m}, NewNumberInt(1), false},
		{"existing key with default", []types.Value{m, NewNumberInt(99)}, NewNumberInt(1), false},
		{"non-existing key with default", []types.Value{coll.NewMap(map[types.Value]types.Value{}), NewNumberInt(99)}, NewNumberInt(99), false},
		{"non-existing key without default", []types.Value{coll.NewMap(map[types.Value]types.Value{})}, nil, false},
		{"no args", []types.Value{}, nil, true},
		{"too many args", []types.Value{m, NewNumberInt(1), NewNumberInt(2)}, nil, true},
		{"non-map arg", []types.Value{NewNumberInt(1)}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := kw.Invoke(context.Background(), tt.args...)
			if (err != nil) != tt.wantErr {
				t.Errorf("Keyword.Invoke() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got == nil {
					if tt.want != nil {
						t.Errorf("Keyword.Invoke() = nil, want %v", tt.want)
					}
				} else if !got.Equals(tt.want) {
					t.Errorf("Keyword.Invoke() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestSymbolInvoke(t *testing.T) {
	sym := types.NewSymbol("a")
	m := coll.NewMap(map[types.Value]types.Value{
		sym:                  NewNumberInt(1),
		types.NewSymbol("b"): NewNumberInt(2),
	})

	tests := []struct {
		name    string
		args    []types.Value
		want    types.Value
		wantErr bool
	}{
		{"existing key", []types.Value{m}, NewNumberInt(1), false},
		{"existing key with default", []types.Value{m, NewNumberInt(99)}, NewNumberInt(1), false},
		{"non-existing key with default", []types.Value{coll.NewMap(map[types.Value]types.Value{}), NewNumberInt(99)}, NewNumberInt(99), false},
		{"non-existing key without default", []types.Value{coll.NewMap(map[types.Value]types.Value{})}, nil, false},
		{"no args", []types.Value{}, nil, true},
		{"too many args", []types.Value{m, NewNumberInt(1), NewNumberInt(2)}, nil, true},
		{"non-map arg", []types.Value{NewNumberInt(1)}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sym.Invoke(context.Background(), tt.args...)
			if (err != nil) != tt.wantErr {
				t.Errorf("Symbol.Invoke() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got == nil {
					if tt.want != nil {
						t.Errorf("Symbol.Invoke() = nil, want %v", tt.want)
					}
				} else if !got.Equals(tt.want) {
					t.Errorf("Symbol.Invoke() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
