package expand

import (
	"testing"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
)

func TestQuotedLiteralPlanPreservesResolvedSymbol(t *testing.T) {
	form := sexpr.NewResolvedSymbol(
		"user/foo",
		sexpr.SymbolResolution{
			Kind:      sexpr.BuiltinSymbolResolution,
			Index:     7,
			Namespace: "doggo.core",
		},
		span.Unknown("<test>"),
		nil,
	)

	plan, err := BuildQuotedLiteralPlan(form)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil {
		t.Fatal("expected quoted literal plan")
	}
	if plan.ResolvedSymbol == nil {
		t.Fatal("expected resolved symbol plan variant")
	}
	if plan.Symbol != nil {
		t.Fatalf("expected resolved symbol variant only, got plain symbol plan: %#v", plan.Symbol)
	}
	if plan.ResolvedSymbol.Name != "user/foo" {
		t.Fatalf("resolved symbol name = %q, want %q", plan.ResolvedSymbol.Name, "user/foo")
	}
	if plan.ResolvedSymbol.Resolution.Kind != sexpr.BuiltinSymbolResolution {
		t.Fatalf("resolution kind = %q, want %q", plan.ResolvedSymbol.Resolution.Kind, sexpr.BuiltinSymbolResolution)
	}
	if plan.ResolvedSymbol.Resolution.Index != 7 {
		t.Fatalf("resolution index = %d, want 7", plan.ResolvedSymbol.Resolution.Index)
	}
	if plan.ResolvedSymbol.Resolution.Namespace != "doggo.core" {
		t.Fatalf("resolution namespace = %q, want %q", plan.ResolvedSymbol.Resolution.Namespace, "doggo.core")
	}
}
