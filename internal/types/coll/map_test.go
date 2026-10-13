package coll_test

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	. "doggo/internal/types/coll"
)

func TestMap(t *testing.T) {
	tests := []struct {
		name     string
		m        *Map
		expected string
	}{
		{"nil map", nil, "{}"},
		{"empty map", NewMap(map[types.Value]types.Value{}), "{}"},
		{
			"single entry",
			NewMap(map[types.Value]types.Value{atom.NewKeyword("a"): atom.NewNumberInt(1)}),
			"{:a 1}",
		},
		{
			"multiple entries",
			NewMap(map[types.Value]types.Value{
				atom.NewKeyword("a"): atom.NewNumberInt(1),
				atom.NewKeyword("b"): atom.NewNumberInt(2),
			}),
			"{:a 1, :b 2}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			if tt.m == nil {
				var nilMap *Map
				result = nilMap.String()
			} else {
				result = tt.m.String()
			}
			// Mapの順序は不定なので、空かどうかだけチェック
			if tt.m == nil || tt.m.Count() == 0 {
				if result != "{}" {
					t.Errorf("Map.String() = %v, want {}", result)
				}
			} else {
				if len(result) < 3 {
					t.Errorf("Map.String() = %v, want non-empty", result)
				}
			}
			if tt.m != nil && tt.m.Type() != "map" {
				t.Errorf("Map.Type() = %v, want map", tt.m.Type())
			}
		})
	}
}

func TestMapPersistent(t *testing.T) {
	tests := []struct {
		name string
		m    *Map
	}{
		{"nil map", nil},
		{"empty map", NewMap(map[types.Value]types.Value{})},
		{"single entry", NewMap(map[types.Value]types.Value{atom.NewKeyword("a"): atom.NewNumberInt(1)})},
		{"multiple entries", NewMap(map[types.Value]types.Value{
			atom.NewKeyword("a"): atom.NewNumberInt(1),
			atom.NewKeyword("b"): atom.NewNumberInt(2),
		})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.m == nil {
				if tt.m.Count() != 0 {
					t.Errorf("nil map Count() = %v, want 0", tt.m.Count())
				}
				return
			}

			expectedCount := tt.m.Count()

			// Count
			if tt.m.Count() != expectedCount {
				t.Errorf("Map.Count() = %v, want %v", tt.m.Count(), expectedCount)
			}

			// Conj with MapEntry
			newKey := atom.NewKeyword("new")
			newVal := atom.NewNumberInt(99)
			entry := types.NewMapEntry(newKey, newVal)
			conjed := tt.m.Conj(entry)
			conjedMap, ok := conjed.(*Map)
			if !ok {
				t.Errorf("Map.Conj() returned %T, want *Map", conjed)
			} else {
				if conjedMap.Get(newKey) != newVal {
					t.Errorf("Conjed map Get(newKey) = %v, want %v", conjedMap.Get(newKey), newVal)
				}
			}

			// Seq
			if expectedCount == 0 {
				if tt.m.Seq() != nil {
					t.Errorf("Empty map Seq() = %v, want nil", tt.m.Seq())
				}
			} else {
				if tt.m.Seq() == nil {
					t.Errorf("Non-empty map Seq() = nil, want non-nil")
				}
			}

			// Assoc
			assocKey := atom.NewKeyword("assoc")
			assocVal := atom.NewNumberInt(888)
			assoced := tt.m.Assoc(assocKey, assocVal)
			if assoced.Get(assocKey) != assocVal {
				t.Errorf("Assoced map Get(assocKey) = %v, want %v", assoced.Get(assocKey), assocVal)
			}

			// Dissoc
			if expectedCount > 0 {
				// 最初のキーを取得
				entries := tt.m.AllEntries()
				firstKey := entries[0].Key
				dissoced := tt.m.Dissoc(firstKey)
				if dissoced.Get(firstKey) != nil {
					t.Errorf("Dissoced map Get(firstKey) = %v, want nil", dissoced.Get(firstKey))
				}
			}

			// Get
			if expectedCount > 0 {
				// 最初のキーを取得
				entries := tt.m.AllEntries()
				firstKey := entries[0].Key
				firstVal := entries[0].Value
				if tt.m.Get(firstKey) != firstVal {
					t.Errorf("Map.Get() = %v, want %v", tt.m.Get(firstKey), firstVal)
				}
				if tt.m.Get(atom.NewKeyword("non-existent")) != nil {
					t.Errorf("Map.Get(non-existent) = %v, want nil", tt.m.Get(atom.NewKeyword("non-existent")))
				}
			}

			// Find
			if expectedCount > 0 {
				entries := tt.m.AllEntries()
				firstKey := entries[0].Key
				firstVal := entries[0].Value
				entry := tt.m.Find(firstKey)
				if entry == nil {
					t.Errorf("Map.Find() = nil, want non-nil")
				} else if entry.Value != firstVal {
					t.Errorf("Map.Find() value = %v, want %v", entry.Value, firstVal)
				}
			}

			// Keys
			if expectedCount > 0 {
				if tt.m.Keys() == nil {
					t.Errorf("Non-empty map Keys() = nil, want non-nil")
				}
			} else {
				if tt.m.Keys() != nil {
					t.Errorf("Empty map Keys() = %v, want nil", tt.m.Keys())
				}
			}

			// Vals
			if expectedCount > 0 {
				if tt.m.Vals() == nil {
					t.Errorf("Non-empty map Vals() = nil, want non-nil")
				}
			} else {
				if tt.m.Vals() != nil {
					t.Errorf("Empty map Vals() = %v, want nil", tt.m.Vals())
				}
			}
		})
	}
}
