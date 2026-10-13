package compiler

import "testing"

func TestSymbolTablePrivateGlobalsAreExcludedFromPublicEnumerationAndQualifiedAccess(t *testing.T) {
	st := NewSymbolTable()
	st.SetCurrentNamespace("doggo.observability")
	st.DefineGlobalInNamespace("doggo.observability", "-log!")
	st.SetGlobalPrivateInNamespace("doggo.observability", "-log!", true)
	st.DefineGlobalInNamespace("doggo.observability", "log!")

	public := st.GetPublicSymbolsInNamespace("doggo.observability")
	if len(public) != 1 || public[0] != "log!" {
		t.Fatalf("expected only public log!, got %#v", public)
	}

	st.SetCurrentNamespace("app")
	if _, ok := st.Resolve("doggo.observability/-log!"); ok {
		t.Fatal("expected qualified access to private global to fail from another namespace")
	}

	st.SetCurrentNamespace("doggo.observability")
	if _, ok := st.Resolve("doggo.observability/-log!"); !ok {
		t.Fatal("expected defining namespace to retain access to its private global")
	}
}

func TestSymbolTableAddReferRejectsPrivateGlobals(t *testing.T) {
	st := NewSymbolTable()
	st.SetCurrentNamespace("doggo.observability")
	st.DefineGlobalInNamespace("doggo.observability", "-log!")
	st.SetGlobalPrivateInNamespace("doggo.observability", "-log!", true)

	st.SetCurrentNamespace("app")
	if err := st.AddRefer("doggo.observability", "-log!", "-log!"); err == nil {
		t.Fatal("expected refer of private global to fail")
	}
}
