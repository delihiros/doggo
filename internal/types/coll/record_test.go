package coll_test

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	. "doggo/internal/types/coll"
)

func TestRecordToGoMapIncludesFieldAndExtraEntries(t *testing.T) {
	nameKey := atom.NewKeyword("name")
	ageKey := atom.NewKeyword("age")
	def := NewRecordDef("User", []types.Value{nameKey})
	record := NewRecord(def, []types.Value{atom.NewString("Ada")})
	recordWithExtra := record.Assoc(ageKey, atom.NewNumberInt(42)).(*Record)

	got := recordWithExtra.ToGoMap()
	if got == nil {
		t.Fatal("ToGoMap returned nil")
	}
	if value := got[nameKey]; value == nil || !value.Equals(atom.NewString("Ada")) {
		t.Fatalf("field value = %v, want Ada", value)
	}
	if value := got[ageKey]; value == nil || !value.Equals(atom.NewNumberInt(42)) {
		t.Fatalf("extra value = %v, want 42", value)
	}
}

func TestRecordRSeqReturnsEntriesInReverseOrder(t *testing.T) {
	nameKey := atom.NewKeyword("name")
	ageKey := atom.NewKeyword("age")
	roleKey := atom.NewKeyword("role")
	def := NewRecordDef("User", []types.Value{nameKey, ageKey})
	record := NewRecord(def, []types.Value{atom.NewString("Ada"), atom.NewNumberInt(42)})
	record = record.Assoc(roleKey, atom.NewString("admin")).(*Record)

	seq, ok := record.RSeq().Seq().(types.Seq)
	if !ok {
		t.Fatal("RSeq did not return a sequence")
	}

	want := []types.Value{roleKey, ageKey, nameKey}
	for i, wantKey := range want {
		if seq == nil {
			t.Fatalf("RSeq ended at index %d", i)
		}
		entry, ok := seq.First().(*types.MapEntry)
		if !ok {
			t.Fatalf("RSeq item %d is %T, want *types.MapEntry", i, seq.First())
		}
		if !entry.Key.Equals(wantKey) {
			t.Fatalf("RSeq key %d = %v, want %v", i, entry.Key, wantKey)
		}
		seq = seq.Next()
	}
	if seq != nil {
		t.Fatal("RSeq returned extra entries")
	}
}
