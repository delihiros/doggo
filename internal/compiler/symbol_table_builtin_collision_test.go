package compiler

import "testing"

func TestSymbolTableCoreBuiltinSurvivesNamespaceGlobalCollision(t *testing.T) {
	st := NewSymbolTable()
	builtin := st.DefineBuiltin(17, "format")

	st.SetCurrentNamespace("scratch.lib")
	global := st.DefineGlobal("format")

	st.SetCurrentNamespace(DefaultNamespace)
	resolved, ok := st.Resolve("format")
	if !ok || resolved.Scope != BuiltinScope || resolved.Index != builtin.Index {
		t.Fatalf("Resolve(format) = %#v, %v; want builtin index %d", resolved, ok, builtin.Index)
	}

	st.SetCurrentNamespace("scratch.lib")
	resolved, ok = st.Resolve("format")
	if !ok || resolved != global {
		t.Fatalf("Resolve(format) in scratch.lib = %#v, %v; want %#v", resolved, ok, global)
	}

	st.SetCurrentNamespace(DefaultNamespace)
	resolved, ok = st.Resolve("scratch.lib/format")
	if !ok || resolved != global {
		t.Fatalf("Resolve(scratch.lib/format) = %#v, %v; want %#v", resolved, ok, global)
	}

	resolved, ok = st.Resolve(StdlibCoreNamespace + "/format")
	if !ok || resolved.Scope != BuiltinScope || resolved.Index != builtin.Index {
		t.Fatalf("Resolve(doggo.core/format) = %#v, %v; want builtin index %d", resolved, ok, builtin.Index)
	}

	st.ExcludeCoreSymbol(DefaultNamespace, "format")
	if resolved, ok = st.Resolve("format"); ok {
		t.Fatalf("Resolve(format) after core exclusion = %#v, true; want unresolved", resolved)
	}
}

func TestSymbolTableResolveBuiltinSurvivesNamespaceGlobalCollision(t *testing.T) {
	st := NewSymbolTable()
	st.DefineBuiltin(17, "format")

	st.SetCurrentNamespace("scratch.lib")
	st.DefineGlobal("format")
	st.SetCurrentNamespace(DefaultNamespace)

	index, ok := st.ResolveBuiltin("format")
	if !ok || index != 17 {
		t.Fatalf("ResolveBuiltin(format) = %d, %v; want 17, true", index, ok)
	}
}

func TestSymbolTableValidateVarGlobalAllowedRejectsProtectedGlobalWithoutMutation(t *testing.T) {
	st := NewSymbolTable()
	protected, err := st.DefineProtectedGlobal("reserved")
	if err != nil {
		t.Fatal(err)
	}
	definitionsBefore := st.numDefinitions

	if err := st.ValidateVarGlobalAllowed("reserved"); err == nil {
		t.Fatal("expected protected global collision")
	}
	if st.numDefinitions != definitionsBefore {
		t.Fatal("validation mutated the symbol table")
	}
	if resolved, ok := st.Resolve("reserved"); !ok || resolved != protected {
		t.Fatalf("protected symbol changed: got %#v, %v; want %#v, true", resolved, ok, protected)
	}
}
