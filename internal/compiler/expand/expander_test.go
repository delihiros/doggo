package expand

import (
	"testing"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/frontend"
)

type stubRuntimeMacroExpander struct {
	expand func(handle RuntimeMacroHandle, args []sexpr.Form) (sexpr.Form, error)
}

func (e stubRuntimeMacroExpander) ExpandRuntimeMacro(handle RuntimeMacroHandle, args []sexpr.Form) (sexpr.Form, error) {
	if e.expand == nil {
		return nil, nil
	}
	return e.expand(handle, args)
}

func TestExpanderMacroexpand1MatchesClojureWhen(t *testing.T) {
	form := mustReadForm(t, "(when ok? (println :x) :done)")

	got, err := Expander{
		LookupMacro: func(sym sexpr.Symbol) (Macro, bool) {
			if sym.String() != "when" {
				return nil, false
			}
			return func(args []sexpr.Form) (sexpr.Form, error) {
				return mustReadForm(t, "(if ok? (do (println :x) :done))"), nil
			}, true
		},
	}.Macroexpand1(form)
	if err != nil {
		t.Fatal(err)
	}

	want := mustReadForm(t, "(if ok? (do (println :x) :done))")
	if got.String() != want.String() {
		t.Fatalf("unexpected expansion:\nwant=%s\ngot=%s", want, got)
	}
}

func TestExpanderUsesRuntimeMacroExpanderForRuntimeBackedMacro(t *testing.T) {
	var expanderCalled bool
	form := mustReadForm(t, "(runtime-macro ok? :done)")

	got, err := Expander{
		LookupMacro: func(sym sexpr.Symbol) (Macro, bool) {
			return nil, false
		},
		LookupRuntimeMacro: func(sym sexpr.Symbol) (RuntimeMacroHandle, bool) {
			if sym.Name != "runtime-macro" {
				return nil, false
			}
			return sym, true
		},
		RuntimeMacros: stubRuntimeMacroExpander{
			expand: func(handle RuntimeMacroHandle, args []sexpr.Form) (sexpr.Form, error) {
				expanderCalled = true
				sym, ok := handle.(sexpr.Symbol)
				if !ok || sym.Name != "runtime-macro" {
					t.Fatalf("unexpected runtime macro handle: %#v", handle)
				}
				if len(args) != 2 {
					t.Fatalf("unexpected arg count: %d", len(args))
				}
				return mustReadForm(t, "(expanded ok? :done)"), nil
			},
		},
	}.Macroexpand1(form)
	if err != nil {
		t.Fatal(err)
	}
	if !expanderCalled {
		t.Fatal("expected runtime macro expander to be invoked")
	}

	want := mustReadForm(t, "(expanded ok? :done)")
	if got.String() != want.String() {
		t.Fatalf("unexpected expansion:\nwant=%s\ngot=%s", want, got)
	}
}

func mustReadForm(t *testing.T, source string) sexpr.Form {
	t.Helper()

	reader, err := frontend.NewReader("<test>", source)
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}
	nodes, err := reader.ReadSyntaxAll()
	if err != nil {
		t.Fatalf("ReadSyntaxAll failed: %v", err)
	}

	file, err := sexpr.LowerFile(nodes)
	if err != nil {
		t.Fatalf("LowerFile failed: %v", err)
	}
	if len(file.Forms) != 1 {
		t.Fatalf("expected 1 form, got %d", len(file.Forms))
	}
	return file.Forms[0]
}
