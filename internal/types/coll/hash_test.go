package coll_test

import (
	"context"
	"fmt"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	. "doggo/internal/types/coll"
)

type countingValue struct {
	id          int
	comparisons *int
}

func (v *countingValue) String() string { return fmt.Sprintf("counting-%d", v.id) }
func (v *countingValue) Type() string   { return "counting-value" }
func (v *countingValue) Hash() uint32   { return uint32(v.id + 1) }
func (v *countingValue) IsSelfEvaluating() bool {
	return true
}
func (v *countingValue) Equals(other types.Value) bool {
	(*v.comparisons)++
	o, ok := other.(*countingValue)
	return ok && v.id == o.id
}

func TestHashOrdered(t *testing.T) {
	tests := []struct {
		name     string
		items    []types.Value
		expected bool
	}{
		{"empty", []types.Value{}, true},
		{"single item", []types.Value{atom.NewNumberInt(1)}, true},
		{"multiple items", []types.Value{atom.NewNumberInt(1), atom.NewNumberInt(2), atom.NewNumberInt(3)}, true},
		{"with strings", []types.Value{atom.NewString("a"), atom.NewString("b")}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test using List
			list := NewList(tt.items...)
			hash1 := list.Hash()
			hash2 := list.Hash()
			if hash1 != hash2 {
				t.Errorf("List.Hash() = %v, second call = %v, want same value", hash1, hash2)
			}

			// Test using Vector
			vec := NewVector(tt.items...)
			vHash1 := vec.Hash()
			vHash2 := vec.Hash()
			if vHash1 != vHash2 {
				t.Errorf("Vector.Hash() = %v, second call = %v, want same value", vHash1, vHash2)
			}

			if hash1 != vHash1 {
				t.Errorf("List.Hash() %v != Vector.Hash() %v for same items", hash1, vHash1)
			}
		})
	}

	// 順序が異なるとハッシュ値も異なることを確認
	list1 := NewList(atom.NewNumberInt(1), atom.NewNumberInt(2))
	list2 := NewList(atom.NewNumberInt(2), atom.NewNumberInt(1))
	hash1 := list1.Hash()
	hash2 := list2.Hash()
	if hash1 == hash2 {
		t.Errorf("List.Hash() should return different values for different orders, got %v for both", hash1)
	}
}

func TestValueEqualFiniteSeqsShareHash(t *testing.T) {
	items := []types.Value{atom.NewNumberInt(0), atom.NewNumberInt(2)}
	want := NewList(items...)
	seqs := []types.Seq{
		NewRangeLazySeq(0, 4, 2),
		NewMetaSeq(NewList(items...), nil),
		NewConsSeq(items[0], NewList(items[1:]...)),
	}

	for _, seq := range seqs {
		if !want.Equals(seq) || !seq.Equals(want) {
			t.Fatalf("%T is not value-equal to %s", seq, want)
		}
		if got := seq.Hash(); got != want.Hash() {
			t.Fatalf("%T.Hash() = %d, want %d", seq, got, want.Hash())
		}
	}
}

func TestSequentialEqualityIncludesVectors(t *testing.T) {
	items := []types.Value{atom.NewNumberInt(0), atom.NewNumberInt(2)}
	vector := NewVector(items...)
	sequences := []types.Value{
		NewList(items...),
		NewRangeLazySeq(0, 4, 2),
		NewMetaSeq(NewList(items...), nil),
		NewConsSeq(items[0], NewList(items[1:]...)),
	}

	for _, sequence := range sequences {
		for _, pair := range [][2]types.Value{{vector, sequence}, {sequence, vector}} {
			equal, err := ValueEqualsResult(pair[0], pair[1])
			if err != nil {
				t.Fatalf("ValueEqualsResult(%T, %T) returned error: %v", pair[0], pair[1], err)
			}
			if !equal || !pair[0].Equals(pair[1]) {
				t.Fatalf("%T and %T are not symmetrically equal", pair[0], pair[1])
			}

			indexed := NewMapFromPairs([]types.Value{pair[0], atom.NewKeyword("found")})
			got, err := indexed.Invoke(context.Background(), pair[1])
			if err != nil {
				t.Fatalf("map lookup returned error: %v", err)
			}
			if got == nil || !got.Equals(atom.NewKeyword("found")) {
				t.Fatalf("map lookup with %T key and %T query = %v, want :found", pair[0], pair[1], got)
			}
		}
	}
}

func TestMapEntrySharesVectorEqualityAndHash(t *testing.T) {
	entry := types.NewMapEntry(atom.NewKeyword("a"), atom.NewNumberInt(1))
	vector := NewVector(entry.Key, entry.Value)

	for _, pair := range [][2]types.Value{{entry, vector}, {vector, entry}} {
		equal, err := ValueEqualsResult(pair[0], pair[1])
		if err != nil {
			t.Fatalf("ValueEqualsResult(%T, %T) returned error: %v", pair[0], pair[1], err)
		}
		if !equal || !pair[0].Equals(pair[1]) {
			t.Fatalf("%T and %T are not symmetrically equal", pair[0], pair[1])
		}
		if pair[0].Hash() != pair[1].Hash() {
			t.Fatalf("equal %T and %T have different hashes", pair[0], pair[1])
		}

		indexed := NewMapFromPairs([]types.Value{pair[0], atom.NewKeyword("found")})
		got, err := indexed.Invoke(context.Background(), pair[1])
		if err != nil {
			t.Fatalf("map lookup returned error: %v", err)
		}
		if got == nil || !got.Equals(atom.NewKeyword("found")) {
			t.Fatalf("map lookup with %T key and %T query = %v, want :found", pair[0], pair[1], got)
		}
	}
}

func TestEmptyLazySeqSharesEmptySequenceContract(t *testing.T) {
	want := NewList()
	lazy := NewLazySeq(func(types.Environment) (types.Value, error) { return nil, nil }, nil)
	if !want.Equals(lazy) || !lazy.Equals(want) {
		t.Fatal("empty lazy seq is not value-equal to empty list")
	}
	if got := lazy.Hash(); got != want.Hash() {
		t.Fatalf("LazySeq.Hash() = %d, want %d", got, want.Hash())
	}
}

func TestRecordSeqSharesSequenceContract(t *testing.T) {
	name := atom.NewKeyword("name")
	age := atom.NewKeyword("age")
	record := NewRecord(
		NewRecordDef("User", []types.Value{name, age}),
		[]types.Value{atom.NewString("Ada"), atom.NewNumberInt(42)},
	)
	seq := record.Seq().(types.Seq)
	want := NewList(
		&types.MapEntry{Key: name, Value: atom.NewString("Ada")},
		&types.MapEntry{Key: age, Value: atom.NewNumberInt(42)},
	)
	if !want.Equals(seq) || !seq.Equals(want) {
		t.Fatal("record seq is not value-equal to its entry list")
	}
	if got := seq.Hash(); got != want.Hash() {
		t.Fatalf("recordSeq.Hash() = %d, want %d", got, want.Hash())
	}
}

func TestHashUnorderedMap(t *testing.T) {
	tests := []struct {
		name     string
		m        *Map
		expected bool
	}{
		{"nil map", nil, true},
		{"empty map", NewMap(map[types.Value]types.Value{}), true},
		{"single entry", NewMap(map[types.Value]types.Value{atom.NewKeyword("a"): atom.NewNumberInt(1)}), true},
		{"multiple entries", NewMap(map[types.Value]types.Value{
			atom.NewKeyword("a"): atom.NewNumberInt(1),
			atom.NewKeyword("b"): atom.NewNumberInt(2),
		}), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := tt.m.Hash()
			hash2 := tt.m.Hash()
			if hash1 != hash2 {
				t.Errorf("Map.Hash() = %v, second call = %v, want same value", hash1, hash2)
			}
		})
	}

	// 順序が異なっても同じマップは同じハッシュ値を返すことを確認
	m1 := NewMap(map[types.Value]types.Value{
		atom.NewKeyword("a"): atom.NewNumberInt(1),
		atom.NewKeyword("b"): atom.NewNumberInt(2),
	})
	// Reconstruct to ensure randomness? Or just check consistency.
	h1 := m1.Hash()

	// Construct another map with same entries
	m2 := NewMap(map[types.Value]types.Value{
		atom.NewKeyword("b"): atom.NewNumberInt(2),
		atom.NewKeyword("a"): atom.NewNumberInt(1),
	})
	if h1 != m2.Hash() {
		t.Errorf("Map.Hash() should be equal for same content")
	}
}

func TestHashUnorderedSet(t *testing.T) {
	tests := []struct {
		name     string
		s        *Set
		expected bool
	}{
		{"nil set", nil, true},
		{"empty set", NewSet(), true},
		{"single element", NewSet(atom.NewKeyword("a")), true},
		{"multiple elements", NewSet(atom.NewKeyword("a"), atom.NewKeyword("b")), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := tt.s.Hash()
			hash2 := tt.s.Hash()
			if hash1 != hash2 {
				t.Errorf("Set.Hash() = %v, second call = %v, want same value", hash1, hash2)
			}
		})
	}
}

func TestMapAndRecordEqualityAndHashAreSymmetric(t *testing.T) {
	name := atom.NewKeyword("name")
	age := atom.NewKeyword("age")
	record := NewRecord(
		NewRecordDef("User", []types.Value{name}),
		[]types.Value{atom.NewString("Ada")},
	).Assoc(age, atom.NewNumberInt(42)).(*Record)
	plainMap := NewMapFromPairs([]types.Value{
		name, atom.NewString("Ada"),
		age, atom.NewNumberInt(42),
	})

	for _, pair := range [][2]types.Value{{record, plainMap}, {plainMap, record}} {
		equal, err := ValueEqualsResult(pair[0], pair[1])
		if err != nil {
			t.Fatalf("ValueEqualsResult(%T, %T) returned error: %v", pair[0], pair[1], err)
		}
		if !equal {
			t.Fatalf("ValueEqualsResult(%T, %T) = false, want true", pair[0], pair[1])
		}
		if !pair[0].Equals(pair[1]) {
			t.Fatalf("%T.Equals(%T) = false, want true", pair[0], pair[1])
		}
	}

	recordHash, err := ValueHashResult(record)
	if err != nil {
		t.Fatalf("ValueHashResult(record) returned error: %v", err)
	}
	mapHash, err := ValueHashResult(plainMap)
	if err != nil {
		t.Fatalf("ValueHashResult(map) returned error: %v", err)
	}
	if recordHash != mapHash || record.Hash() != plainMap.Hash() {
		t.Fatalf("equal record and map hashes differ: result=(%d, %d), Hash=(%d, %d)", recordHash, mapHash, record.Hash(), plainMap.Hash())
	}

	for _, tc := range []struct {
		name  string
		key   types.Value
		query types.Value
	}{
		{name: "record key queried by map", key: record, query: plainMap},
		{name: "map key queried by record", key: plainMap, query: record},
	} {
		t.Run(tc.name, func(t *testing.T) {
			indexed := NewMapFromPairs([]types.Value{tc.key, atom.NewKeyword("found")})
			got, err := indexed.Invoke(context.Background(), tc.query)
			if err != nil {
				t.Fatalf("map lookup returned error: %v", err)
			}
			if got == nil || !got.Equals(atom.NewKeyword("found")) {
				t.Fatalf("map lookup = %v, want :found", got)
			}
		})
	}
}

func TestHashCollectionEqualityUsesBucketLookup(t *testing.T) {
	const size = 128

	t.Run("map", func(t *testing.T) {
		comparisons := 0
		leftEntries := make(map[types.Value]types.Value, size)
		rightEntries := make(map[types.Value]types.Value, size)
		for i := 0; i < size; i++ {
			leftEntries[&countingValue{id: i, comparisons: &comparisons}] = atom.NewNumberInt(int64(i))
			rightEntries[&countingValue{id: i, comparisons: &comparisons}] = atom.NewNumberInt(int64(i))
		}
		left := NewMap(leftEntries)
		right := NewMap(rightEntries)
		comparisons = 0

		equal, err := ValueEqualsResult(left, right)
		if err != nil || !equal {
			t.Fatalf("map equality = (%v, %v), want (true, nil)", equal, err)
		}
		if comparisons > size*2 {
			t.Fatalf("map equality used %d key comparisons for %d entries", comparisons, size)
		}
	})

	t.Run("set", func(t *testing.T) {
		comparisons := 0
		leftValues := make([]types.Value, 0, size)
		rightValues := make([]types.Value, 0, size)
		for i := 0; i < size; i++ {
			leftValues = append(leftValues, &countingValue{id: i, comparisons: &comparisons})
			rightValues = append(rightValues, &countingValue{id: i, comparisons: &comparisons})
		}
		left := NewSet(leftValues...)
		right := NewSet(rightValues...)
		comparisons = 0

		equal, err := ValueEqualsResult(left, right)
		if err != nil || !equal {
			t.Fatalf("set equality = (%v, %v), want (true, nil)", equal, err)
		}
		if comparisons > size*2 {
			t.Fatalf("set equality used %d key comparisons for %d entries", comparisons, size)
		}
	})

	t.Run("map and record in both directions", func(t *testing.T) {
		comparisons := 0
		name := atom.NewKeyword("name")
		var record types.Map = NewRecord(
			NewRecordDef("Counted", []types.Value{name}),
			[]types.Value{atom.NewString("value")},
		)
		mapEntries := map[types.Value]types.Value{name: atom.NewString("value")}
		for i := 0; i < size; i++ {
			recordKey := &countingValue{id: i, comparisons: &comparisons}
			mapKey := &countingValue{id: i, comparisons: &comparisons}
			value := atom.NewNumberInt(int64(i))
			record = record.Assoc(recordKey, value)
			mapEntries[mapKey] = value
		}
		plainMap := NewMap(mapEntries)

		for _, pair := range [][2]types.Value{{record, plainMap}, {plainMap, record}} {
			comparisons = 0
			equal, err := ValueEqualsResult(pair[0], pair[1])
			if err != nil || !equal {
				t.Fatalf("%T/%T equality = (%v, %v), want (true, nil)", pair[0], pair[1], equal, err)
			}
			if comparisons > size*2 {
				t.Fatalf("%T/%T equality used %d key comparisons for %d extra entries", pair[0], pair[1], comparisons, size)
			}
		}
	})
}
