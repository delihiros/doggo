package coll_test

import (
	"sync"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func vectorWithSpareCapacity() *coll.Vector {
	items := make([]types.Value, 3, 8)
	for i := range items {
		items[i] = atom.NewNumberInt(int64(i + 1))
	}
	return coll.NewVector(items...)
}

func TestVectorPersistenceSiblingConj(t *testing.T) {
	base := vectorWithSpareCapacity()
	left := base.Conj(atom.NewNumberInt(4))
	right := base.Conj(atom.NewNumberInt(5))

	if got := base.String(); got != "[1 2 3]" {
		t.Errorf("base changed: %s", got)
	}
	if got := left.String(); got != "[1 2 3 4]" {
		t.Errorf("left branch changed after sibling Conj: %s", got)
	}
	if got := right.String(); got != "[1 2 3 5]" {
		t.Errorf("right branch = %s, want [1 2 3 5]", got)
	}
}

func TestVectorPersistencePopThenConj(t *testing.T) {
	base := vectorWithSpareCapacity()
	parent := base.Conj(atom.NewNumberInt(4)).(*coll.Vector)
	popped := parent.Pop()
	branch := popped.Conj(atom.NewNumberInt(9))

	if got := parent.String(); got != "[1 2 3 4]" {
		t.Errorf("parent changed after Pop then Conj: %s", got)
	}
	if got := popped.String(); got != "[1 2 3]" {
		t.Errorf("popped vector changed: %s", got)
	}
	if got := branch.String(); got != "[1 2 3 9]" {
		t.Errorf("branch = %s, want [1 2 3 9]", got)
	}
}

func TestVectorPersistenceRetainedSeq(t *testing.T) {
	base := vectorWithSpareCapacity()
	left := base.Conj(atom.NewNumberInt(4)).(*coll.Vector)
	view := left.Seq()
	base.Conj(atom.NewNumberInt(5))

	if got := view.String(); got != "(1 2 3 4)" {
		t.Errorf("retained sequence changed after sibling Conj: %s", got)
	}
}

func TestVectorPersistenceHashedKeys(t *testing.T) {
	base := vectorWithSpareCapacity()
	key := base.Conj(atom.NewNumberInt(4)).(*coll.Vector)
	wantKey := coll.NewVector(atom.NewNumberInt(1), atom.NewNumberInt(2), atom.NewNumberInt(3), atom.NewNumberInt(4))
	found := atom.NewKeyword("found")
	index := coll.NewMapFromPairs([]types.Value{key, found})
	members := coll.NewSet(key)
	originalHash := key.Hash()
	base.Conj(atom.NewNumberInt(5))

	if got := key.Hash(); got != originalHash {
		t.Errorf("key hash changed from %d to %d", originalHash, got)
	}
	for name, lookupKey := range map[string]types.Value{"retained key": key, "equivalent key": wantKey} {
		t.Run(name, func(t *testing.T) {
			if got := index.Get(lookupKey); got != found {
				t.Errorf("map lookup = %v, want %v", got, found)
			}
			if !members.Contains(lookupKey) {
				t.Error("set lost the vector member after sibling Conj")
			}
		})
	}
}

func TestVectorPersistenceMetadataView(t *testing.T) {
	base := vectorWithSpareCapacity()
	metadata := coll.NewMapFromPairs([]types.Value{atom.NewKeyword("tag"), atom.NewKeyword("source")})
	decoratedValue, err := coll.CopyWithMetadata(base, metadata)
	if err != nil {
		t.Fatal(err)
	}
	decorated := decoratedValue.(*coll.Vector)
	left := base.Conj(atom.NewNumberInt(4))
	right := decorated.Conj(atom.NewNumberInt(5)).(*coll.Vector)

	if got := left.String(); got != "[1 2 3 4]" {
		t.Errorf("original branch changed after metadata view Conj: %s", got)
	}
	if got := right.String(); got != "[1 2 3 5]" {
		t.Errorf("metadata branch = %s, want [1 2 3 5]", got)
	}
	if decorated.Metadata != metadata {
		t.Error("Conj changed the receiver metadata")
	}
	if right.Metadata != nil {
		t.Error("Conj must retain its existing behavior of dropping result metadata")
	}
}

func TestVectorPersistenceConcurrentConj(t *testing.T) {
	base := vectorWithSpareCapacity()
	const branchCount = 32
	branches := make([]types.Value, branchCount)
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(branchCount)
	for i := range branches {
		go func(i int) {
			defer workers.Done()
			<-start
			branches[i] = base.Conj(atom.NewNumberInt(int64(i + 4)))
		}(i)
	}
	close(start)
	workers.Wait()

	for i, branch := range branches {
		want := coll.NewVector(atom.NewNumberInt(1), atom.NewNumberInt(2), atom.NewNumberInt(3), atom.NewNumberInt(int64(i+4)))
		if !branch.Equals(want) {
			t.Errorf("branch %d = %s, want %s", i, branch, want)
		}
	}
	if got := base.String(); got != "[1 2 3]" {
		t.Errorf("base changed after concurrent Conj: %s", got)
	}
}

func TestVectorPersistenceNilConj(t *testing.T) {
	var base *coll.Vector
	if got := base.Conj(atom.NewNumberInt(4)).String(); got != "[4]" {
		t.Errorf("nil vector Conj = %s, want [4]", got)
	}
}
