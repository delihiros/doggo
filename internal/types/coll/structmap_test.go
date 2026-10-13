package coll_test

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	. "doggo/internal/types/coll"
)

func TestStructDef(t *testing.T) {
	keys := []types.Value{atom.NewKeyword("a"), atom.NewKeyword("b"), atom.NewKeyword("c")}
	structDef := NewStructDef(keys)

	if structDef == nil {
		t.Errorf("NewStructDef() = nil, want non-nil")
	}

	if len(structDef.Keys()) != 3 {
		t.Errorf("StructDef.Keys() length = %v, want 3", len(structDef.Keys()))
	}

	if structDef.Keys()[0] != atom.NewKeyword("a") {
		t.Errorf("StructDef.Keys()[0] = %v, want :a", structDef.Keys()[0])
	}

	if structDef.String() != "#<struct-def>" {
		t.Errorf("StructDef.String() = %v, want #<struct-def>", structDef.String())
	}

	if structDef.Type() != "struct-def" {
		t.Errorf("StructDef.Type() = %v, want struct-def", structDef.Type())
	}
}

func TestStructMap(t *testing.T) {
	structDef := NewStructDef([]types.Value{atom.NewKeyword("a"), atom.NewKeyword("b")})

	emptyMap, err := NewStructMap(structDef, []types.Value{})
	if err != nil {
		t.Fatalf("NewStructMap() failed: %v", err)
	}
	withValuesMap, err := NewStructMap(structDef, []types.Value{atom.NewNumberInt(1), atom.NewNumberInt(2)})
	if err != nil {
		t.Fatalf("NewStructMap() failed: %v", err)
	}
	tests := []struct {
		name     string
		m        *StructMap
		expected string
	}{
		{"nil map", nil, "{}"},
		{"empty map", emptyMap, "{}"},
		{"with values", withValuesMap, "{:a 1, :b 2}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.m == nil {
				var nilMap *StructMap
				result := nilMap.String()
				if result != tt.expected {
					t.Errorf("StructMap.String() = %v, want %v", result, tt.expected)
				}
				return
			}

			if tt.m.Type() != "struct-map" {
				t.Errorf("StructMap.Type() = %v, want struct-map", tt.m.Type())
			}
		})
	}
}

func TestStructMapMap(t *testing.T) {
	structDef := NewStructDef([]types.Value{atom.NewKeyword("a"), atom.NewKeyword("b")})
	m, err := NewStructMap(structDef, []types.Value{atom.NewNumberInt(1), atom.NewNumberInt(2)})
	if err != nil {
		t.Fatalf("NewStructMap() failed: %v", err)
	}

	// Count
	if m.Count() != 2 {
		t.Errorf("StructMap.Count() = %v, want 2", m.Count())
	}

	// Conj with MapEntry
	newEntry := types.NewMapEntry(atom.NewKeyword("a"), atom.NewNumberInt(99))
	conjed := m.Conj(newEntry)
	conjedMap, ok := conjed.(*StructMap)
	if !ok {
		t.Errorf("StructMap.Conj() returned %T, want *StructMap", conjed)
	} else {
		if !types.Equal(conjedMap.Get(atom.NewKeyword("a")), atom.NewNumberInt(99)) {
			t.Errorf("Conjed StructMap.Get(:a) = %v, want 99", conjedMap.Get(atom.NewKeyword("a")))
		}
	}

	// Seq
	seq := m.Seq()
	if seq == nil {
		t.Errorf("StructMap.Seq() = nil, want non-nil")
	}

	// Assoc
	assoced := m.Assoc(atom.NewKeyword("a"), atom.NewNumberInt(999))
	if !types.Equal(assoced.Get(atom.NewKeyword("a")), atom.NewNumberInt(999)) {
		t.Errorf("Assoced StructMap.Get(:a) = %v, want 999", assoced.Get(atom.NewKeyword("a")))
	}

	// Dissoc - ベースキーは削除できない（Clojure仕様）
	dissoced := m.Dissoc(atom.NewKeyword("a"))
	if dissoced.Get(atom.NewKeyword("a")) == nil {
		t.Errorf("Dissoced StructMap.Get(:a) = nil, but base keys cannot be dissoced")
	}

	// Get
	if !types.Equal(m.Get(atom.NewKeyword("a")), atom.NewNumberInt(1)) {
		t.Errorf("StructMap.Get(:a) = %v, want 1", m.Get(atom.NewKeyword("a")))
	}
	if m.Get(atom.NewKeyword("nonexistent")) != nil {
		t.Errorf("StructMap.Get(:nonexistent) = %v, want nil", m.Get(atom.NewKeyword("nonexistent")))
	}

	// Find
	found := m.Find(atom.NewKeyword("a"))
	if found == nil {
		t.Errorf("StructMap.Find(:a) = nil, want non-nil")
	} else {
		if found.Key != atom.NewKeyword("a") {
			t.Errorf("Find().Key = %v, want :a", found.Key)
		}
		if !types.Equal(found.Value, atom.NewNumberInt(1)) {
			t.Errorf("Find().types.Value = %v, want 1", found.Value)
		}
	}

	// Keys
	keys := m.Keys()
	if keys == nil {
		t.Errorf("StructMap.Keys() = nil, want non-nil")
	}

	// Vals
	vals := m.Vals()
	if vals == nil {
		t.Errorf("StructMap.Vals() = nil, want non-nil")
	}

	// RSeq
	rseq := m.RSeq()
	if rseq == nil {
		t.Errorf("StructMap.RSeq() = nil, want non-nil")
	}
}

func TestStructMapFn(t *testing.T) {
	structDef := NewStructDef([]types.Value{atom.NewKeyword("a"), atom.NewKeyword("b")})
	m, err := NewStructMap(structDef, []types.Value{atom.NewNumberInt(1), atom.NewNumberInt(2)})
	if err != nil {
		t.Fatalf("NewStructMap() failed: %v", err)
	}

	// Invoke with existing key
	result, err := m.Invoke(atom.NewKeyword("a"))
	if err != nil {
		t.Errorf("StructMap.Invoke() error = %v", err)
	}
	if !types.Equal(result, atom.NewNumberInt(1)) {
		t.Errorf("StructMap.Invoke(:a) = %v, want 1", result)
	}

	// Invoke with non-existing key and default
	result, err = m.Invoke(atom.NewKeyword("c"), atom.NewNumberInt(99))
	if err != nil {
		t.Errorf("StructMap.Invoke() error = %v", err)
	}
	if !types.Equal(result, atom.NewNumberInt(99)) {
		t.Errorf("StructMap.Invoke(:c, 99) = %v, want 99", result)
	}

	// Invoke with non-existing key without default
	result, err = m.Invoke(atom.NewKeyword("c"))
	if err != nil {
		t.Errorf("StructMap.Invoke() error = %v", err)
	}
	if result != nil {
		t.Errorf("StructMap.Invoke(:c) = %v, want nil", result)
	}
}

func TestStructMapBaseKeysCannotBeDissoced(t *testing.T) {
	structDef := NewStructDef([]types.Value{atom.NewKeyword("a"), atom.NewKeyword("b")})
	m, err := NewStructMap(structDef, []types.Value{atom.NewNumberInt(1), atom.NewNumberInt(2)})
	if err != nil {
		t.Fatalf("NewStructMap() failed: %v", err)
	}

	// ベースキーを削除しようとしても削除されない
	dissoced := m.Dissoc(atom.NewKeyword("a"))
	if dissoced.Get(atom.NewKeyword("a")) == nil {
		t.Errorf("Base key :a was dissoced, but it should not be")
	}

	dissoced2 := m.Dissoc(atom.NewKeyword("b"))
	if dissoced2.Get(atom.NewKeyword("b")) == nil {
		t.Errorf("Base key :b was dissoced, but it should not be")
	}
}
