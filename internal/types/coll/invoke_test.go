package coll_test

import (
	"context"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	. "doggo/internal/types/coll"
)

func TestVectorInvoke(t *testing.T) {
	vec := NewVector(atom.NewNumberInt(10), atom.NewNumberInt(20), atom.NewNumberInt(30))

	tests := []struct {
		name    string
		args    []types.Value
		want    types.Value
		wantErr bool
	}{
		{"valid index", []types.Value{atom.NewNumberInt(0)}, atom.NewNumberInt(10), false},
		{"valid index middle", []types.Value{atom.NewNumberInt(1)}, atom.NewNumberInt(20), false},
		{"valid index last", []types.Value{atom.NewNumberInt(2)}, atom.NewNumberInt(30), false},
		{"out of bounds with default", []types.Value{atom.NewNumberInt(10), atom.NewNumberInt(99)}, atom.NewNumberInt(99), false},
		{"out of bounds without default", []types.Value{atom.NewNumberInt(10)}, nil, false},
		{"negative index with default", []types.Value{atom.NewNumberInt(-1), atom.NewNumberInt(99)}, atom.NewNumberInt(99), false},
		{"no args", []types.Value{}, nil, true},
		{"too many args", []types.Value{atom.NewNumberInt(0), atom.NewNumberInt(1), atom.NewNumberInt(2)}, nil, true},
		{"non-number index", []types.Value{atom.NewString("0")}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := vec.Invoke(context.Background(), tt.args...)
			if (err != nil) != tt.wantErr {
				t.Errorf("Vector.Invoke() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got == nil {
					if tt.want != nil {
						t.Errorf("Vector.Invoke() = nil, want %v", tt.want)
					}
				} else if !got.Equals(tt.want) {
					t.Errorf("Vector.Invoke() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestMapInvoke(t *testing.T) {
	m := NewMap(map[types.Value]types.Value{
		atom.NewKeyword("a"): atom.NewNumberInt(1),
		atom.NewKeyword("b"): atom.NewNumberInt(2),
	})

	tests := []struct {
		name    string
		args    []types.Value
		want    types.Value
		wantErr bool
	}{
		{"existing key", []types.Value{atom.NewKeyword("a")}, atom.NewNumberInt(1), false},
		{"existing key with default", []types.Value{atom.NewKeyword("a"), atom.NewNumberInt(99)}, atom.NewNumberInt(1), false},
		{"non-existing key with default", []types.Value{atom.NewKeyword("c"), atom.NewNumberInt(99)}, atom.NewNumberInt(99), false},
		{"non-existing key without default", []types.Value{atom.NewKeyword("c")}, nil, false},
		{"no args", []types.Value{}, nil, true},
		{"too many args", []types.Value{atom.NewKeyword("a"), atom.NewNumberInt(1), atom.NewNumberInt(2)}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := m.Invoke(context.Background(), tt.args...)
			if (err != nil) != tt.wantErr {
				t.Errorf("Map.Invoke() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got == nil {
					if tt.want != nil {
						t.Errorf("Map.Invoke() = nil, want %v", tt.want)
					}
				} else if !got.Equals(tt.want) {
					t.Errorf("Map.Invoke() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestDirectMapInvokeArityErrors(t *testing.T) {
	type directInvoker interface {
		Invoke(args ...types.Value) (types.Value, error)
	}

	arrayMap, err := NewArrayMap(atom.NewKeyword("a"), atom.NewNumberInt(1))
	if err != nil {
		t.Fatalf("NewArrayMap() failed: %v", err)
	}
	sortedMap, err := NewSortedMap(atom.NewKeyword("a"), atom.NewNumberInt(1))
	if err != nil {
		t.Fatalf("NewSortedMap() failed: %v", err)
	}
	structMap, err := NewStructMap(NewStructDef([]types.Value{atom.NewKeyword("a")}), []types.Value{atom.NewNumberInt(1)})
	if err != nil {
		t.Fatalf("NewStructMap() failed: %v", err)
	}

	for _, tt := range []struct {
		name       string
		m          directInvoker
		noArgsErr  string
		tooManyErr string
	}{
		{
			name:       "array map",
			m:          arrayMap,
			noArgsErr:  "array-map invocation requires at least 1 argument (key)",
			tooManyErr: "array-map invocation expects 1 or 2 arguments (key, default?), got 3",
		},
		{
			name:       "sorted map",
			m:          sortedMap,
			noArgsErr:  "sorted-map invocation requires at least 1 argument (key)",
			tooManyErr: "sorted-map invocation expects 1 or 2 arguments (key, default?), got 3",
		},
		{
			name:       "struct map",
			m:          structMap,
			noArgsErr:  "struct-map invocation requires at least 1 argument (key)",
			tooManyErr: "struct-map invocation expects 1 or 2 arguments (key, default?), got 3",
		},
	} {
		t.Run(tt.name+"/no args", func(t *testing.T) {
			_, err := tt.m.Invoke()
			if err == nil {
				t.Fatal("Invoke() succeeded unexpectedly")
			}
			if err.Error() != tt.noArgsErr {
				t.Fatalf("Invoke() error = %q, want %q", err.Error(), tt.noArgsErr)
			}
		})
		t.Run(tt.name+"/too many args", func(t *testing.T) {
			_, err := tt.m.Invoke(atom.NewKeyword("a"), atom.NewNumberInt(1), atom.NewNumberInt(2))
			if err == nil {
				t.Fatal("Invoke() succeeded unexpectedly")
			}
			if err.Error() != tt.tooManyErr {
				t.Fatalf("Invoke() error = %q, want %q", err.Error(), tt.tooManyErr)
			}
		})
	}
}

func TestSetInvoke(t *testing.T) {
	s := NewSet(atom.NewKeyword("a"), atom.NewKeyword("b"), atom.NewKeyword("c"))

	tests := []struct {
		name    string
		args    []types.Value
		want    types.Value
		wantErr bool
	}{
		{"existing member", []types.Value{atom.NewKeyword("a")}, atom.NewKeyword("a"), false},
		{"existing member b", []types.Value{atom.NewKeyword("b")}, atom.NewKeyword("b"), false},
		{"non-existing member", []types.Value{atom.NewKeyword("d")}, nil, false},
		{"no args", []types.Value{}, nil, true},
		{"too many args", []types.Value{atom.NewKeyword("a"), atom.NewKeyword("b")}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Invoke(context.Background(), tt.args...)
			if (err != nil) != tt.wantErr {
				t.Errorf("Set.Invoke() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got == nil {
					if tt.want != nil {
						t.Errorf("Set.Invoke() = nil, want %v", tt.want)
					}
				} else if !got.Equals(tt.want) {
					t.Errorf("Set.Invoke() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
