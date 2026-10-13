package coll_test

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	. "doggo/internal/types/coll"
)

func TestVector(t *testing.T) {
	tests := []struct {
		name     string
		vector   *Vector
		expected string
	}{
		{"nil vector", nil, "[]"},
		{"empty vector", &Vector{Items: []types.Value{}}, "[]"},
		{"single item", NewVector(types.NewSymbol("a")), "[a]"},
		{"multiple items", NewVector(types.NewSymbol("a"), types.NewSymbol("b"), types.NewSymbol("c")), "[a b c]"},
		{"nested vector", NewVector(types.NewSymbol("a"), NewVector(types.NewSymbol("b"))), "[a [b]]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			if tt.vector == nil {
				var nilVector *Vector
				result = nilVector.String()
			} else {
				result = tt.vector.String()
			}
			if result != tt.expected {
				t.Errorf("Vector.String() = %v, want %v", result, tt.expected)
			}
			if tt.vector != nil && tt.vector.Type() != "vector" {
				t.Errorf("Vector.Type() = %v, want vector", tt.vector.Type())
			}
		})
	}
}

func TestVectorPersistent(t *testing.T) {
	tests := []struct {
		name   string
		vector *Vector
	}{
		{"nil vector", nil},
		{"empty vector", NewVector()},
		{"single item", NewVector(atom.NewNumberInt(1))},
		{"multiple items", NewVector(atom.NewNumberInt(1), atom.NewNumberInt(2), atom.NewNumberInt(3))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.vector == nil {
				if tt.vector.Count() != 0 {
					t.Errorf("nil vector Count() = %v, want 0", tt.vector.Count())
				}
				return
			}

			expectedCount := len(tt.vector.Items)

			// Count
			if tt.vector.Count() != expectedCount {
				t.Errorf("Vector.Count() = %v, want %v", tt.vector.Count(), expectedCount)
			}

			// Conj
			newItem := atom.NewNumberInt(99)
			conjed := tt.vector.Conj(newItem)
			conjedVector, ok := conjed.(*Vector)
			if !ok {
				t.Errorf("Vector.Conj() returned %T, want *Vector", conjed)
			} else {
				if conjedVector.Count() != expectedCount+1 {
					t.Errorf("Conjed vector Count() = %v, want %v", conjedVector.Count(), expectedCount+1)
				}
				if conjedVector.Items[expectedCount] != newItem {
					t.Errorf("Conjed vector last item = %v, want %v", conjedVector.Items[expectedCount], newItem)
				}
			}

			// Seq
			if expectedCount == 0 {
				if tt.vector.Seq() != nil {
					t.Errorf("Empty vector Seq() = %v, want nil", tt.vector.Seq())
				}
			} else {
				if tt.vector.Seq() == nil {
					t.Errorf("Non-empty vector Seq() = nil, want non-nil")
				}
			}

			// Nth
			if expectedCount > 0 {
				if tt.vector.Nth(0) != tt.vector.Items[0] {
					t.Errorf("Vector.Nth(0) = %v, want %v", tt.vector.Nth(0), tt.vector.Items[0])
				}
				if tt.vector.Nth(expectedCount) != nil {
					t.Errorf("Vector.Nth(out of bounds) = %v, want nil", tt.vector.Nth(expectedCount))
				}
			}

			// Assoc
			if expectedCount > 0 {
				newVal := atom.NewNumberInt(999)
				assoced := tt.vector.Assoc(0, newVal)
				if assoced.Nth(0) != newVal {
					t.Errorf("Vector.Assoc(0) = %v, want %v", assoced.Nth(0), newVal)
				}
			}

			// Pop
			if expectedCount > 0 {
				popped := tt.vector.Pop()
				if popped.Count() != expectedCount-1 {
					t.Errorf("Vector.Pop() count = %v, want %v", popped.Count(), expectedCount-1)
				}
			} else {
				popped := tt.vector.Pop()
				if popped.Count() != 0 {
					t.Errorf("Empty vector Pop() count = %v, want 0", popped.Count())
				}
			}

			// RSeq
			if expectedCount > 0 {
				rseq := tt.vector.RSeq()
				if rseq == nil {
					t.Errorf("Non-empty vector RSeq() = nil, want non-nil")
				}
				if list, ok := rseq.(*List); ok {
					if list.Items[0] != tt.vector.Items[expectedCount-1] {
						t.Errorf("RSeq first item = %v, want %v", list.Items[0], tt.vector.Items[expectedCount-1])
					}
				}
			} else {
				if tt.vector.RSeq() != nil {
					t.Errorf("Empty vector RSeq() = %v, want nil", tt.vector.RSeq())
				}
			}
		})
	}
}
