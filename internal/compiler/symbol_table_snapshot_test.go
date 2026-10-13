package compiler

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestSymbolTableSnapshotRestoreRestoresNamespaceState(t *testing.T) {
	st := NewSymbolTable()
	st.SetCurrentNamespace("base")
	st.SetNamespaceMetadata("base", coll.NewMap(map[types.Value]types.Value{
		atom.NewKeyword("doc"): atom.String("base"),
	}))
	st.DefineGlobalInNamespace("base", "stable")

	snapshot := st.Snapshot()

	st.SetCurrentNamespace("leaked.ns")
	st.SetNamespaceMetadata("leaked.ns", coll.NewMap(map[types.Value]types.Value{
		atom.NewKeyword("doc"): atom.String("leaked"),
	}))
	st.DefineGlobalInNamespace("leaked.ns", "tmp")

	st.Restore(snapshot)

	if got := st.CurrentNamespace(); got != "base" {
		t.Fatalf("expected current namespace to roll back to base, got %q", got)
	}
	if meta := st.GetNamespaceMetadata("leaked.ns"); meta != nil {
		t.Fatalf("expected leaked namespace metadata to be removed, got %v", meta)
	}
	if _, ok := st.Resolve("tmp"); ok {
		t.Fatal("expected leaked symbol definition to be removed")
	}
}

func TestSymbolTableSnapshotRestoreRestoresDefinitionCounter(t *testing.T) {
	st := NewSymbolTable()
	st.DefineGlobalInNamespace("base", "stable")
	snapshot := st.Snapshot()

	st.DefineGlobalInNamespace("base", "tmp")
	if st.numDefinitions < 2 {
		t.Fatalf("expected tmp definition to increment numDefinitions, got %d", st.numDefinitions)
	}

	st.Restore(snapshot)
	if got := st.numDefinitions; got != 1 {
		t.Fatalf("expected numDefinitions to roll back to 1, got %d", got)
	}
}
