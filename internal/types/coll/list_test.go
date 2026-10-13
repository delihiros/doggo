package coll_test

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	. "doggo/internal/types/coll"
)

func TestList(t *testing.T) {
	tests := []struct {
		name     string
		list     *List
		expected string
	}{
		{"nil list", nil, "()"},
		{"empty list", &List{Items: []types.Value{}}, "()"},
		{"single item", NewList(types.NewSymbol("a")), "(a)"},
		{"multiple items", NewList(types.NewSymbol("a"), types.NewSymbol("b"), types.NewSymbol("c")), "(a b c)"},
		{"nested list", NewList(types.NewSymbol("a"), NewList(types.NewSymbol("b"))), "(a (b))"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			if tt.list == nil {
				var nilList *List
				result = nilList.String()
			} else {
				result = tt.list.String()
			}
			if result != tt.expected {
				t.Errorf("List.String() = %v, want %v", result, tt.expected)
			}
			if tt.list != nil && tt.list.Type() != "list" {
				t.Errorf("List.Type() = %v, want list", tt.list.Type())
			}
		})
	}
}

func TestListPersistent(t *testing.T) {
	tests := []struct {
		name string
		list *List
	}{
		{"nil list", nil},
		{"empty list", NewList()},
		{"single item", NewList(atom.NewNumberInt(1))},
		{"multiple items", NewList(atom.NewNumberInt(1), atom.NewNumberInt(2), atom.NewNumberInt(3))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.list == nil {
				if tt.list.Count() != 0 {
					t.Errorf("nil list Count() = %v, want 0", tt.list.Count())
				}
				return
			}

			// Count
			expectedCount := len(tt.list.Items)
			if tt.list.Count() != expectedCount {
				t.Errorf("List.Count() = %v, want %v", tt.list.Count(), expectedCount)
			}

			// Conj
			newItem := atom.NewNumberInt(99)
			conjed := tt.list.Conj(newItem)
			conjedList, ok := conjed.(*List)
			if !ok {
				t.Errorf("List.Conj() returned %T, want *List", conjed)
			} else {
				if conjedList.Count() != expectedCount+1 {
					t.Errorf("Conjed list Count() = %v, want %v", conjedList.Count(), expectedCount+1)
				}
				if conjedList.Items[0] != newItem {
					t.Errorf("Conjed list first item = %v, want %v", conjedList.Items[0], newItem)
				}
			}

			// Seq
			if expectedCount == 0 {
				if tt.list.Seq() != nil {
					t.Errorf("Empty list Seq() = %v, want nil", tt.list.Seq())
				}
			} else {
				if tt.list.Seq() == nil {
					t.Errorf("Non-empty list Seq() = nil, want non-nil")
				}
			}

			// Peek
			if expectedCount > 0 {
				if tt.list.Peek() != tt.list.Items[0] {
					t.Errorf("List.Peek() = %v, want %v", tt.list.Peek(), tt.list.Items[0])
				}
			} else {
				if tt.list.Peek() != nil {
					t.Errorf("Empty list Peek() = %v, want nil", tt.list.Peek())
				}
			}

			// Pop
			if expectedCount > 0 {
				popped := tt.list.Pop()
				if popped.Count() != expectedCount-1 {
					t.Errorf("List.Pop() count = %v, want %v", popped.Count(), expectedCount-1)
				}
			} else {
				popped := tt.list.Pop()
				if popped.Count() != 0 {
					t.Errorf("Empty list Pop() count = %v, want 0", popped.Count())
				}
			}
		})
	}
}
