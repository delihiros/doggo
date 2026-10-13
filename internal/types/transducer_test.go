package types_test

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestReducedUsesIdentityEquality(t *testing.T) {
	first := &types.Reduced{Val: atom.NewNumberInt(1)}
	second := &types.Reduced{Val: atom.NewNumberInt(1)}

	if !first.Equals(first) {
		t.Fatal("Reduced value must equal itself")
	}
	if first.Equals(second) {
		t.Fatal("separately allocated Reduced values must not compare equal")
	}
	if !coll.NewSet(first).Contains(first) {
		t.Fatal("Reduced value must be retrievable as a collection key")
	}
}
