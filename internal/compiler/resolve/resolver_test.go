package resolve

import (
	"testing"

	"doggo/internal/compiler/hir"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/frontend"
)

func TestResolverBuildsLexicalScopes(t *testing.T) {
	file := mustReadFile(t, "(let [x 1] (let [x 2] x) x)")

	prog, err := File(Config{}, file)
	if err != nil {
		t.Fatal(err)
	}

	xs := bindingsNamed(prog, "x")
	if len(xs) != 2 {
		t.Fatalf("expected two x bindings, got %#v", xs)
	}
	if xs[0].ID == xs[1].ID {
		t.Fatalf("shadowed bindings must have distinct IDs: %#v", xs)
	}
	if xs[0].ScopeID == xs[1].ScopeID {
		t.Fatalf("shadowed bindings must live in distinct scopes: %#v", xs)
	}
}

func TestResolverSeparatesSiblingFunctionScopes(t *testing.T) {
	file := mustReadFile(t, "[(fn [x] x) (fn [x] x)]")

	prog, err := File(Config{}, file)
	if err != nil {
		t.Fatal(err)
	}

	xs := bindingsNamed(prog, "x")
	if len(xs) != 2 {
		t.Fatalf("expected two parameter bindings, got %#v", xs)
	}
	if xs[0].Kind != hir.ScopeParam || xs[1].Kind != hir.ScopeParam {
		t.Fatalf("unexpected binding kinds: %#v", xs)
	}
	if xs[0].Depth != 1 || xs[1].Depth != 1 {
		t.Fatalf("unexpected function depths: %#v", xs)
	}
	if xs[0].ScopeID == xs[1].ScopeID {
		t.Fatalf("sibling functions must not share scope IDs: %#v", xs)
	}
	if xs[0].Slot != 0 || xs[1].Slot != 0 {
		t.Fatalf("sibling function params should restart slots at 0: %#v", xs)
	}
}

func TestResolverIgnoresVariadicMarkersInBindingPatterns(t *testing.T) {
	file := mustReadFile(t, "(fn [x & rest] rest)")

	prog, err := File(Config{}, file)
	if err != nil {
		t.Fatal(err)
	}

	if markers := bindingsNamed(prog, "&"); len(markers) != 0 {
		t.Fatalf("variadic marker must not be recorded as a binding: %#v", markers)
	}
	if xs := bindingsNamed(prog, "x"); len(xs) != 1 {
		t.Fatalf("expected x binding, got %#v", xs)
	}
	if rests := bindingsNamed(prog, "rest"); len(rests) != 1 {
		t.Fatalf("expected rest binding, got %#v", rests)
	}
}

func TestResolverIgnoresMapDestructureDefaults(t *testing.T) {
	file := mustReadFile(t, "(let [{:keys [a] :or {a 1}} {}] a)")

	prog, err := File(Config{}, file)
	if err != nil {
		t.Fatal(err)
	}

	if as := bindingsNamed(prog, "a"); len(as) != 1 {
		t.Fatalf("expected a to be bound once, got %#v", as)
	}
}

func TestResolverTracksNamespaceAliasesAndCanonicalizesForms(t *testing.T) {
	file := mustReadFile(t, "(ns demo.core (:require [doggo.core :as dg])) (dg/+ 1 2)")

	prog, err := File(Config{}, file)
	if err != nil {
		t.Fatal(err)
	}

	if prog.Namespace != "demo.core" {
		t.Fatalf("unexpected namespace: %s", prog.Namespace)
	}
	if got := prog.Aliases["dg"]; got != "doggo.core" {
		t.Fatalf("unexpected alias target: %q", got)
	}

	call, ok := prog.Forms[1].Form().(*sexpr.List)
	if !ok {
		t.Fatalf("expected second form to be a list, got %T", prog.Forms[1].Form())
	}
	head, ok := call.Items[0].(sexpr.Symbol)
	if !ok {
		t.Fatalf("expected call head to be a symbol, got %T", call.Items[0])
	}
	if head.Name != "doggo.core/+" {
		t.Fatalf("expected canonicalized alias target, got %q", head.Name)
	}
}

func bindingsNamed(prog *hir.Program, name string) []hir.Binding {
	var out []hir.Binding
	for _, binding := range prog.Bindings {
		if binding.Name == name {
			out = append(out, binding)
		}
	}
	return out
}

func mustReadFile(t *testing.T, source string) *sexpr.File {
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
	return file
}
