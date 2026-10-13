package coll_test

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	. "doggo/internal/types/coll"
)

func TestSortedMap(t *testing.T) {
	emptyMap, err := NewSortedMap()
	if err != nil {
		t.Fatalf("NewSortedMap() failed: %v", err)
	}
	singleMap, err := NewSortedMap(atom.NewKeyword("a"), atom.NewNumberInt(1))
	if err != nil {
		t.Fatalf("NewSortedMap() failed: %v", err)
	}
	multipleMap, err := NewSortedMap(
		atom.NewKeyword("z"), atom.NewNumberInt(3),
		atom.NewKeyword("a"), atom.NewNumberInt(1),
		atom.NewKeyword("m"), atom.NewNumberInt(2),
	)
	if err != nil {
		t.Fatalf("NewSortedMap() failed: %v", err)
	}
	tests := []struct {
		name     string
		m        *SortedMap
		expected string
	}{
		{"nil map", nil, "{}"},
		{"empty map", emptyMap, "{}"},
		{"single entry", singleMap, "{:a 1}"},
		{"multiple entries", multipleMap, "{:a 1, :m 2, :z 3}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.m == nil {
				var nilMap *SortedMap
				result := nilMap.String()
				if result != tt.expected {
					t.Errorf("SortedMap.String() = %v, want %v", result, tt.expected)
				}
				return
			}

			if tt.m.Type() != "sorted-map" {
				t.Errorf("SortedMap.Type() = %v, want sorted-map", tt.m.Type())
			}

			// ソート順が維持されていることを確認
			if tt.name == "multiple entries" {
				if len(tt.m.Entries) != 3 {
					t.Errorf("SortedMap.Entries length = %v, want 3", len(tt.m.Entries))
				} else {
					// キーがソートされていることを確認
					if !types.Equal(tt.m.Entries[0].Key, atom.NewKeyword("a")) {
						t.Errorf("SortedMap.Entries[0].Key = %v, want :a", tt.m.Entries[0].Key)
					}
					if !types.Equal(tt.m.Entries[1].Key, atom.NewKeyword("m")) {
						t.Errorf("SortedMap.Entries[1].Key = %v, want :m", tt.m.Entries[1].Key)
					}
					if !types.Equal(tt.m.Entries[2].Key, atom.NewKeyword("z")) {
						t.Errorf("SortedMap.Entries[2].Key = %v, want :z", tt.m.Entries[2].Key)
					}
				}
			}
		})
	}
}

func TestSortedMapBy(t *testing.T) {
	// カスタム比較関数（数値の降順）
	comparator := func(a, b types.Value) int {
		na, ok1 := a.(*atom.Number)
		nb, ok2 := b.(*atom.Number)
		if !ok1 || !ok2 {
			return 0
		}
		aVal := na.ToFloat64()
		bVal := nb.ToFloat64()
		if aVal > bVal {
			return -1
		}
		if aVal < bVal {
			return 1
		}
		return 0
	}

	m, err := NewSortedMapBy(comparator,
		atom.NewNumberInt(1), atom.NewString("one"),
		atom.NewNumberInt(3), atom.NewString("three"),
		atom.NewNumberInt(2), atom.NewString("two"),
	)
	if err != nil {
		t.Fatalf("NewSortedMapBy() failed: %v", err)
	}

	// 数値の降順でソートされていることを確認
	if len(m.Entries) != 3 {
		t.Errorf("SortedMap.Entries length = %v, want 3", len(m.Entries))
	} else {
		if !types.Equal(m.Entries[0].Key, atom.NewNumberInt(3)) {
			t.Errorf("SortedMap.Entries[0].Key = %v, want 3", m.Entries[0].Key)
		}
		if !types.Equal(m.Entries[1].Key, atom.NewNumberInt(2)) {
			t.Errorf("SortedMap.Entries[1].Key = %v, want 2", m.Entries[1].Key)
		}
		if !types.Equal(m.Entries[2].Key, atom.NewNumberInt(1)) {
			t.Errorf("SortedMap.Entries[2].Key = %v, want 1", m.Entries[2].Key)
		}
	}
}

func TestSortedMapMap(t *testing.T) {
	m, err := NewSortedMap(
		atom.NewKeyword("a"), atom.NewNumberInt(1),
		atom.NewKeyword("b"), atom.NewNumberInt(2),
	)
	if err != nil {
		t.Fatalf("NewSortedMap() failed: %v", err)
	}

	// Count
	if m.Count() != 2 {
		t.Errorf("SortedMap.Count() = %v, want 2", m.Count())
	}

	// Conj with MapEntry
	newEntry := types.NewMapEntry(atom.NewKeyword("c"), atom.NewNumberInt(3))
	conjed := m.Conj(newEntry)
	conjedMap, ok := conjed.(*SortedMap)
	if !ok {
		t.Errorf("SortedMap.Conj() returned %T, want *SortedMap", conjed)
	} else {
		if conjedMap.Count() != 3 {
			t.Errorf("Conjed SortedMap.Count() = %v, want 3", conjedMap.Count())
		}
		// ソート順が維持されていることを確認
		if len(conjedMap.Entries) > 2 && !types.Equal(conjedMap.Entries[2].Key, atom.NewKeyword("c")) {
			t.Errorf("Conjed SortedMap.Entries[2].Key = %v, want :c", conjedMap.Entries[2].Key)
		}
	}

	// Seq
	seq := m.Seq()
	if seq == nil {
		t.Errorf("SortedMap.Seq() = nil, want non-nil")
	}

	// Assoc
	assoced := m.Assoc(atom.NewKeyword("d"), atom.NewNumberInt(4))
	if !types.Equal(assoced.Get(atom.NewKeyword("d")), atom.NewNumberInt(4)) {
		t.Errorf("Assoced SortedMap.Get(:d) = %v, want 4", assoced.Get(atom.NewKeyword("d")))
	}
	// ソート順が維持されていることを確認
	assocedMap := assoced.(*SortedMap)
	if len(assocedMap.Entries) != 3 {
		t.Errorf("Assoced SortedMap.Entries length = %v, want 3", len(assocedMap.Entries))
	}
	// エントリが正しく追加されていることを確認（キー:dは最後に追加される）
	if len(assocedMap.Entries) > 0 && !types.Equal(assocedMap.Entries[len(assocedMap.Entries)-1].Key, atom.NewKeyword("d")) {
		t.Errorf("Assoced SortedMap.Entries[%d].Key = %v, want :d", len(assocedMap.Entries)-1, assocedMap.Entries[len(assocedMap.Entries)-1].Key)
	}

	// Dissoc
	dissoced := m.Dissoc(atom.NewKeyword("a"))
	if dissoced.Get(atom.NewKeyword("a")) != nil {
		t.Errorf("Dissoced SortedMap.Get(:a) = %v, want nil", dissoced.Get(atom.NewKeyword("a")))
	}
	if dissoced.Count() != 1 {
		t.Errorf("Dissoced SortedMap.Count() = %v, want 1", dissoced.Count())
	}

	// Get
	if !types.Equal(m.Get(atom.NewKeyword("a")), atom.NewNumberInt(1)) {
		t.Errorf("SortedMap.Get(:a) = %v, want 1", m.Get(atom.NewKeyword("a")))
	}
	if m.Get(atom.NewKeyword("nonexistent")) != nil {
		t.Errorf("SortedMap.Get(:nonexistent) = %v, want nil", m.Get(atom.NewKeyword("nonexistent")))
	}

	// Find
	found := m.Find(atom.NewKeyword("a"))
	if found == nil {
		t.Errorf("SortedMap.Find(:a) = nil, want non-nil")
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
		t.Errorf("SortedMap.Keys() = nil, want non-nil")
	}

	// Vals
	vals := m.Vals()
	if vals == nil {
		t.Errorf("SortedMap.Vals() = nil, want non-nil")
	}

	// RSeq
	rseq := m.RSeq()
	if rseq == nil {
		t.Errorf("SortedMap.RSeq() = nil, want non-nil")
	}
}

func TestSortedMapFn(t *testing.T) {
	m, err := NewSortedMap(
		atom.NewKeyword("a"), atom.NewNumberInt(1),
		atom.NewKeyword("b"), atom.NewNumberInt(2),
	)
	if err != nil {
		t.Fatalf("NewSortedMap() error = %v", err)
	}

	// Invoke with existing key
	result, err := m.Invoke(atom.NewKeyword("a"))
	if err != nil {
		t.Errorf("SortedMap.Invoke() error = %v", err)
	}
	if !types.Equal(result, atom.NewNumberInt(1)) {
		t.Errorf("SortedMap.Invoke(:a) = %v, want 1", result)
	}

	// Invoke with non-existing key and default
	result, err = m.Invoke(atom.NewKeyword("c"), atom.NewNumberInt(99))
	if err != nil {
		t.Errorf("SortedMap.Invoke() error = %v", err)
	}
	if !types.Equal(result, atom.NewNumberInt(99)) {
		t.Errorf("SortedMap.Invoke(:c, 99) = %v, want 99", result)
	}

	// Invoke with non-existing key without default
	result, err = m.Invoke(atom.NewKeyword("c"))
	if err != nil {
		t.Errorf("SortedMap.Invoke() error = %v", err)
	}
	if result != nil {
		t.Errorf("SortedMap.Invoke(:c) = %v, want nil", result)
	}
}

func TestSortedMapSortOrder(t *testing.T) {
	// キーをランダムな順序で追加
	m, err := NewSortedMap(
		atom.NewKeyword("z"), atom.NewNumberInt(3),
		atom.NewKeyword("a"), atom.NewNumberInt(1),
		atom.NewKeyword("m"), atom.NewNumberInt(2),
		atom.NewKeyword("b"), atom.NewNumberInt(4),
	)
	if err != nil {
		t.Fatalf("NewSortedMap() failed: %v", err)
	}

	// ソート順が維持されていることを確認
	if len(m.Entries) != 4 {
		t.Errorf("SortedMap.Entries length = %v, want 4", len(m.Entries))
	} else {
		expectedKeys := []types.Value{
			atom.NewKeyword("a"),
			atom.NewKeyword("b"),
			atom.NewKeyword("m"),
			atom.NewKeyword("z"),
		}
		for i, expectedKey := range expectedKeys {
			if m.Entries[i].Key != expectedKey {
				t.Errorf("SortedMap.Entries[%d].Key = %v, want %v", i, m.Entries[i].Key, expectedKey)
			}
		}
	}
}
