package macroconvert

import (
	"testing"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/frontend"
	"doggo/internal/types"
)

func TestMacroConversionRoundTripPreservesMeta(t *testing.T) {
	form := mustReadForm(t, "^{:private true} (when ok? :yes)")

	value, err := FormToValue(form)
	if err != nil {
		t.Fatal(err)
	}

	roundTrip, err := ValueToForm(value)
	if err != nil {
		t.Fatal(err)
	}

	if !roundTrip.Meta().Get("private").Truthy() {
		t.Fatalf("metadata lost: %#v", roundTrip.Meta())
	}
	if got, want := roundTrip.String(), form.String(); got != want {
		t.Fatalf("unexpected round trip:\nwant=%s\ngot=%s", want, got)
	}
}

func TestMacroConversionRoundTripPreservesResolvedSymbol(t *testing.T) {
	form, err := ValueToForm(types.NewResolvedSymbol("doggo.core/new-lazy-seq", types.SymbolResolution{
		Kind:      types.BuiltinSymbolResolution,
		Index:     7,
		Namespace: "doggo.core",
	}))
	if err != nil {
		t.Fatal(err)
	}

	value, err := FormToValue(form)
	if err != nil {
		t.Fatal(err)
	}

	resolved, ok := value.(*types.ResolvedSymbol)
	if !ok {
		t.Fatalf("expected resolved symbol, got %T", value)
	}
	if resolved.Resolution.Kind != types.BuiltinSymbolResolution {
		t.Fatalf("unexpected resolution kind: %#v", resolved.Resolution)
	}
	if resolved.Resolution.Index != 7 {
		t.Fatalf("unexpected resolution index: %#v", resolved.Resolution)
	}
	if resolved.Name != "doggo.core/new-lazy-seq" {
		t.Fatalf("unexpected symbol name: %s", resolved.Name)
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
