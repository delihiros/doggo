package opt

import (
	"testing"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
)

type fakeResolver map[string]Binding

func (r fakeResolver) ResolveOptimizationSymbol(name string) (Binding, bool) {
	binding, ok := r[name]
	return binding, ok
}

func TestTryOptimizeFormListFusesVectorPipeline(t *testing.T) {
	resolver := fakeResolver{
		"reduce": {Scope: ScopeBuiltin},
		"map":    {Scope: ScopeBuiltin},
	}

	expr := formList(
		formSymbol("reduce"),
		formSymbol("+"),
		formNumber("0"),
		formList(
			formSymbol("map"),
			formSymbol("inc"),
			formVector(formNumber("1"), formNumber("2"), formNumber("3")),
		),
	)

	rewritten, ok := TryOptimizeFormList(expr, DefaultOptimizationConfig(), resolver)
	if !ok {
		t.Fatal("expected optimization")
	}

	list, ok := rewritten.(*sexpr.List)
	if !ok {
		t.Fatalf("rewritten = %T, want *sexpr.List", rewritten)
	}
	if name, ok := formSymbolName(list.Items[0]); !ok || name != "loop" {
		t.Fatalf("rewritten head = %v, want loop", list.Items[0])
	}
}

func TestTryOptimizeFormListNoMutationOnInput(t *testing.T) {
	resolver := fakeResolver{
		"reduce": {Scope: ScopeBuiltin},
		"map":    {Scope: ScopeBuiltin},
	}

	expr := formList(
		formSymbol("reduce"),
		formSymbol("+"),
		formNumber("0"),
		formList(
			formSymbol("map"),
			formSymbol("inc"),
			formVector(formNumber("1"), formNumber("2")),
		),
	)
	before := expr.String()

	_, _ = TryOptimizeFormList(expr, DefaultOptimizationConfig(), resolver)
	if expr.String() != before {
		t.Fatal("TryOptimizeFormList mutated the input expression")
	}
}

func formSymbol(name string) sexpr.Symbol {
	return sexpr.NewSymbol(name, span.Unknown("opt-test"), nil)
}

func formNumber(literal string) sexpr.Number {
	return sexpr.NewNumber(literal, span.Unknown("opt-test"), nil)
}

func formList(items ...sexpr.Form) *sexpr.List {
	return sexpr.NewList(items, span.Unknown("opt-test"), nil)
}

func formVector(items ...sexpr.Form) *sexpr.Vector {
	return sexpr.NewVector(items, span.Unknown("opt-test"), nil)
}
