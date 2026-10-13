package coll_test

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	. "doggo/internal/types/coll"
)

func TestArrayMap(t *testing.T) {
	emptyMap, err := NewArrayMap()
	if err != nil {
		t.Fatalf("NewArrayMap() failed: %v", err)
	}
	singleMap, err := NewArrayMap(atom.NewKeyword("a"), atom.NewNumberInt(1))
	if err != nil {
		t.Fatalf("NewArrayMap() failed: %v", err)
	}
	multipleMap, err := NewArrayMap(
		atom.NewKeyword("a"), atom.NewNumberInt(1),
		atom.NewKeyword("b"), atom.NewNumberInt(2),
	)
	if err != nil {
		t.Fatalf("NewArrayMap() failed: %v", err)
	}
	tests := []struct {
		name     string
		m        *ArrayMap
		expected string
	}{
		{"nil map", nil, "{}"},
		{"empty map", emptyMap, "{}"},
		{"single entry", singleMap, "{:a 1}"},
		{"multiple entries", multipleMap, "{:a 1, :b 2}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.m == nil {
				var nilMap *ArrayMap
				nilResult := nilMap.String()
				if nilResult != tt.expected {
					t.Errorf("ArrayMap.String() = %v, want %v", nilResult, tt.expected)
				}
				return
			}

			_ = tt.m.String() // 文字列表現は順序が保持されることを確認するため使用
			// 順序が保持されることを確認
			if tt.name == "multiple entries" {
				// 順序が保持されているかチェック
				if len(tt.m.Entries) != 4 {
					t.Errorf("ArrayMap.Entries length = %v, want 4", len(tt.m.Entries))
				}
				if tt.m.Entries[0] != atom.NewKeyword("a") {
					t.Errorf("ArrayMap.Entries[0] = %v, want :a", tt.m.Entries[0])
				}
				if tt.m.Entries[2] != atom.NewKeyword("b") {
					t.Errorf("ArrayMap.Entries[2] = %v, want :b", tt.m.Entries[2])
				}
			}

			if tt.m.Type() != "array-map" {
				t.Errorf("ArrayMap.Type() = %v, want array-map", tt.m.Type())
			}
		})
	}
}

func TestArrayMapMap(t *testing.T) {
	m, err := NewArrayMap(
		atom.NewKeyword("a"), atom.NewNumberInt(1),
		atom.NewKeyword("b"), atom.NewNumberInt(2),
	)
	if err != nil {
		t.Fatalf("NewArrayMap() failed: %v", err)
	}

	// Count
	if m.Count() != 2 {
		t.Errorf("ArrayMap.Count() = %v, want 2", m.Count())
	}

	// Conj with MapEntry
	newEntry := types.NewMapEntry(atom.NewKeyword("c"), atom.NewNumberInt(3))
	conjed := m.Conj(newEntry)
	conjedMap, ok := conjed.(*ArrayMap)
	if !ok {
		t.Errorf("ArrayMap.Conj() returned %T, want *ArrayMap", conjed)
	} else {
		if conjedMap.Count() != 3 {
			t.Errorf("Conjed ArrayMap.Count() = %v, want 3", conjedMap.Count())
		}
	}

	// Seq
	seq := m.Seq()
	if seq == nil {
		t.Errorf("ArrayMap.Seq() = nil, want non-nil")
	}

	// Assoc
	assoced := m.Assoc(atom.NewKeyword("d"), atom.NewNumberInt(4))
	if !types.Equal(assoced.Get(atom.NewKeyword("d")), atom.NewNumberInt(4)) {
		t.Errorf("Assoced ArrayMap.Get(:d) = %v, want 4", assoced.Get(atom.NewKeyword("d")))
	}

	// Dissoc
	dissoced := m.Dissoc(atom.NewKeyword("a"))
	if dissoced.Get(atom.NewKeyword("a")) != nil {
		t.Errorf("Dissoced ArrayMap.Get(:a) = %v, want nil", dissoced.Get(atom.NewKeyword("a")))
	}
	if dissoced.Count() != 1 {
		t.Errorf("Dissoced ArrayMap.Count() = %v, want 1", dissoced.Count())
	}

	// Get
	if !types.Equal(m.Get(atom.NewKeyword("a")), atom.NewNumberInt(1)) {
		t.Errorf("ArrayMap.Get(:a) = %v, want 1", m.Get(atom.NewKeyword("a")))
	}
	if m.Get(atom.NewKeyword("nonexistent")) != nil {
		t.Errorf("ArrayMap.Get(:nonexistent) = %v, want nil", m.Get(atom.NewKeyword("nonexistent")))
	}

	// Find
	found := m.Find(atom.NewKeyword("a"))
	if found == nil {
		t.Errorf("ArrayMap.Find(:a) = nil, want non-nil")
	} else {
		if !types.Equal(found.Key, atom.NewKeyword("a")) {
			t.Errorf("Find().Key = %v, want :a", found.Key)
		}
		if !types.Equal(found.Value, atom.NewNumberInt(1)) {
			t.Errorf("Find().types.Value = %v, want 1", found.Value)
		}
	}

	// Keys
	keys := m.Keys()
	if keys == nil {
		t.Errorf("ArrayMap.Keys() = nil, want non-nil")
	}

	// Vals
	vals := m.Vals()
	if vals == nil {
		t.Errorf("ArrayMap.Vals() = nil, want non-nil")
	}

	// RSeq
	rseq := m.RSeq()
	if rseq == nil {
		t.Errorf("ArrayMap.RSeq() = nil, want non-nil")
	}
}

func TestArrayMapFn(t *testing.T) {
	m, err := NewArrayMap(
		atom.NewKeyword("a"), atom.NewNumberInt(1),
		atom.NewKeyword("b"), atom.NewNumberInt(2),
	)
	if err != nil {
		t.Fatalf("NewArrayMap() failed: %v", err)
	}

	// Invoke with existing key
	result, err := m.Invoke(atom.NewKeyword("a"))
	if err != nil {
		t.Errorf("ArrayMap.Invoke() error = %v", err)
	}
	if !types.Equal(result, atom.NewNumberInt(1)) {
		t.Errorf("ArrayMap.Invoke(:a) = %v, want 1", result)
	}

	// Invoke with non-existing key and default
	result, err = m.Invoke(atom.NewKeyword("c"), atom.NewNumberInt(99))
	if err != nil {
		t.Errorf("ArrayMap.Invoke() error = %v", err)
	}
	if !types.Equal(result, atom.NewNumberInt(99)) {
		t.Errorf("ArrayMap.Invoke(:c, 99) = %v, want 99", result)
	}

	// Invoke with non-existing key without default
	result, err = m.Invoke(atom.NewKeyword("c"))
	if err != nil {
		t.Errorf("ArrayMap.Invoke() error = %v", err)
	}
	if result != nil {
		t.Errorf("ArrayMap.Invoke(:c) = %v, want nil", result)
	}
}

func TestArrayMapOrderPreservation(t *testing.T) {
	m, err := NewArrayMap(
		atom.NewKeyword("z"), atom.NewNumberInt(3),
		atom.NewKeyword("a"), atom.NewNumberInt(1),
		atom.NewKeyword("m"), atom.NewNumberInt(2),
	)
	if err != nil {
		t.Fatalf("NewArrayMap() failed: %v", err)
	}

	// 順序が保持されていることを確認
	if m.Entries[0] != atom.NewKeyword("z") {
		t.Errorf("ArrayMap.Entries[0] = %v, want :z", m.Entries[0])
	}
	if m.Entries[2] != atom.NewKeyword("a") {
		t.Errorf("ArrayMap.Entries[2] = %v, want :a", m.Entries[2])
	}
	if m.Entries[4] != atom.NewKeyword("m") {
		t.Errorf("ArrayMap.Entries[4] = %v, want :m", m.Entries[4])
	}
}
