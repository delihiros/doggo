package coll

import (
	"testing"

	"doggo/internal/types"
)

// TestCount はCountヘルパー関数をテストします
func TestCount(t *testing.T) {
	tests := []struct {
		name     string
		input    types.Value
		expected int64
		ok       bool
	}{
		{"nil", nil, 0, true},
		{"empty vector", NewVector(), 0, true},
		{"vector with items", NewVector(mockValue("a"), mockValue("b"), mockValue("c")), 3, true},
		{"empty list", NewList(), 0, true},
		{"list with items", NewList(mockValue("a"), mockValue("b")), 2, true},
		{"empty map", NewMap(nil), 0, true},
		{"map with items", NewMap(map[types.Value]types.Value{mockValue("k"): mockValue("v")}), 1, true},
		{"empty set", NewSet(), 0, true},
		{"set with items", NewSet(mockValue("a"), mockValue("b")), 2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Count(tt.input)
			if result.Ok != tt.ok {
				t.Errorf("Count(%v).Ok = %v, want %v", tt.input, result.Ok, tt.ok)
			}
			if result.Count != tt.expected {
				t.Errorf("Count(%v).Count = %d, want %d", tt.input, result.Count, tt.expected)
			}
		})
	}
}

// TestFirst はFirstヘルパー関数をテストします
func TestFirst(t *testing.T) {
	tests := []struct {
		name     string
		input    types.Value
		expected types.Value
	}{
		{"nil", nil, nil},
		{"empty vector", NewVector(), nil},
		{"vector with items", NewVector(mockValue("a"), mockValue("b")), mockValue("a")},
		{"empty list", NewList(), nil},
		{"list with items", NewList(mockValue("a"), mockValue("b")), mockValue("a")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := First(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("First(%v) = %v, want nil", tt.input, result)
				}
			} else if result == nil || !result.Equals(tt.expected) {
				t.Errorf("First(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestRest はRestヘルパー関数をテストします
func TestRest(t *testing.T) {
	t.Run("empty vector", func(t *testing.T) {
		result := Rest(NewVector())
		// rest of empty vector should return empty seq (list)
		if l, ok := result.(*List); !ok || len(l.Items) != 0 {
			t.Errorf("Rest(empty vector) should return empty list (seq), got %T", result)
		}
	})

	t.Run("vector with items", func(t *testing.T) {
		result := Rest(NewVector(mockValue("a"), mockValue("b"), mockValue("c")))
		// rest of vector should return seq (list), not vector
		if l, ok := result.(*List); !ok || len(l.Items) != 2 {
			t.Errorf("Rest(vector) should return list (seq) with 2 items, got %T", result)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		result := Rest(NewList())
		if l, ok := result.(*List); !ok || len(l.Items) != 0 {
			t.Errorf("Rest(empty list) should return empty list")
		}
	})

	t.Run("list with items", func(t *testing.T) {
		result := Rest(NewList(mockValue("a"), mockValue("b"), mockValue("c")))
		if l, ok := result.(*List); !ok || len(l.Items) != 2 {
			t.Errorf("Rest(list) should return list with 2 items, got %v", result)
		}
	})
}

// TestToSeq はToSeqヘルパー関数をテストします
func TestToSeq(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		result := ToSeq(nil)
		if !result.Ok || result.Seq != nil {
			t.Errorf("ToSeq(nil) should return Ok=true, Seq=nil")
		}
	})

	t.Run("empty vector", func(t *testing.T) {
		result := ToSeq(NewVector())
		if !result.Ok || result.Seq != nil {
			t.Errorf("ToSeq(empty vector) should return Ok=true, Seq=nil")
		}
	})

	t.Run("vector with items", func(t *testing.T) {
		result := ToSeq(NewVector(mockValue("a"), mockValue("b")))
		if !result.Ok || result.Seq == nil {
			t.Errorf("ToSeq(vector) should return Ok=true, non-nil Seq")
		}
	})

	t.Run("metadata seq unwraps at coercion boundary", func(t *testing.T) {
		base := NewList(mockValue("a"), mockValue("b"))
		metaSeq := NewMetaSeq(base, NewMap(nil))
		result := ToSeq(metaSeq)
		if !result.Ok || result.Seq == nil {
			t.Fatalf("ToSeq(meta seq) should return Ok=true, non-nil Seq")
		}
		if result.Seq == metaSeq {
			t.Fatalf("ToSeq(meta seq) should unwrap metadata wrapper")
		}
		if result.Seq != base {
			t.Fatalf("ToSeq(meta seq) should return wrapped base seq")
		}
	})
}

// TestAsSeq はAsSeqヘルパー関数をテストします
func TestAsSeq(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		result := AsSeq(nil)
		if result != nil {
			t.Errorf("AsSeq(nil) should return nil")
		}
	})

	t.Run("empty vector", func(t *testing.T) {
		result := AsSeq(NewVector())
		if result != nil {
			t.Errorf("AsSeq(empty vector) should return nil")
		}
	})

	t.Run("vector with items", func(t *testing.T) {
		result := AsSeq(NewVector(mockValue("a"), mockValue("b")))
		if result == nil {
			t.Errorf("AsSeq(vector) should return non-nil Seq")
		}
		if result.First() == nil || !result.First().Equals(mockValue("a")) {
			t.Errorf("AsSeq(vector).First() should return first element")
		}
	})

	t.Run("empty list", func(t *testing.T) {
		result := AsSeq(NewList())
		if result != nil {
			t.Errorf("AsSeq(empty list) should return nil")
		}
	})

	t.Run("list with items", func(t *testing.T) {
		list := NewList(mockValue("a"), mockValue("b"))
		result := AsSeq(list)
		if result == nil {
			t.Errorf("AsSeq(list) should return non-nil Seq")
		}
		// List の場合は自分自身を返す
		if result != list {
			t.Errorf("AsSeq(list) should return the list itself")
		}
	})

	t.Run("set with items", func(t *testing.T) {
		result := AsSeq(NewSet(mockValue("a"), mockValue("b")))
		if result == nil {
			t.Errorf("AsSeq(set) should return non-nil Seq")
		}
	})

	t.Run("empty set", func(t *testing.T) {
		result := AsSeq(NewSet())
		if result != nil {
			t.Errorf("AsSeq(empty set) should return nil")
		}
	})

	t.Run("metadata seq unwraps at iteration boundary", func(t *testing.T) {
		base := NewList(mockValue("a"), mockValue("b"))
		metaSeq := NewMetaSeq(base, NewMap(nil))
		result := AsSeq(metaSeq)
		if result == nil {
			t.Fatalf("AsSeq(meta seq) should return non-nil Seq")
		}
		if result == metaSeq {
			t.Fatalf("AsSeq(meta seq) should unwrap metadata wrapper")
		}
		if result != base {
			t.Fatalf("AsSeq(meta seq) should return wrapped base seq")
		}
	})
}

// TestToVector はToVectorヘルパー関数をテストします
func TestToVector(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		result := ToVector(nil)
		if len(result.Items) != 0 {
			t.Errorf("ToVector(nil) should return empty vector")
		}
	})

	t.Run("list seq", func(t *testing.T) {
		list := NewList(mockValue("a"), mockValue("b"), mockValue("c"))
		result := ToVector(list)
		if len(result.Items) != 3 {
			t.Errorf("ToVector(list) should return vector with 3 items, got %d", len(result.Items))
		}
	})
}

// TestFirstSupported はFirstSupportedヘルパー関数をテストします
func TestFirstSupported(t *testing.T) {
	tests := []struct {
		name     string
		input    types.Value
		expected bool
	}{
		{"nil", nil, false},
		{"vector", NewVector(), true},
		{"list", NewList(), true},
		{"map", NewMap(nil), true},
		{"set", NewSet(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FirstSupported(tt.input)
			if result != tt.expected {
				t.Errorf("FirstSupported(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestRestSupported はRestSupportedヘルパー関数をテストします
func TestRestSupported(t *testing.T) {
	tests := []struct {
		name     string
		input    types.Value
		expected bool
	}{
		{"nil", nil, false},
		{"vector", NewVector(), true},
		{"list", NewList(), true},
		{"map", NewMap(nil), true},
		{"set", NewSet(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RestSupported(tt.input)
			if result != tt.expected {
				t.Errorf("RestSupported(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// mockValue はテスト用のモック値を作成します
type mockValueType struct {
	val string
}

func mockValue(s string) *mockValueType {
	return &mockValueType{val: s}
}

func (m *mockValueType) String() string {
	return m.val
}

func (m *mockValueType) Type() string {
	return "mock"
}

func (m *mockValueType) Hash() uint32 {
	return 0
}

func (m *mockValueType) Equals(other types.Value) bool {
	if o, ok := other.(*mockValueType); ok {
		return m.val == o.val
	}
	return false
}

func (m *mockValueType) IsSelfEvaluating() bool {
	return true
}
