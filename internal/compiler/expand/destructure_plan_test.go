package expand

import (
	"testing"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
)

func TestDestructurePlanPreservesNestedVectorAndMapPatterns(t *testing.T) {
	innerMap := sexpr.NewMap([]sexpr.MapEntry{
		{
			Key:   sexpr.NewSymbol("nested-key", span.Unknown("<test>"), nil),
			Value: sexpr.NewSymbol("nested-value", span.Unknown("<test>"), nil),
		},
	}, span.Unknown("<test>"), nil)

	outerMap := sexpr.NewMap([]sexpr.MapEntry{
		{
			Key: sexpr.NewKeyword("keys", span.Unknown("<test>"), nil),
			Value: sexpr.NewVector([]sexpr.Form{
				sexpr.NewSymbol("map-key", span.Unknown("<test>"), nil),
			}, span.Unknown("<test>"), nil),
		},
		{
			Key:   sexpr.NewKeyword("as", span.Unknown("<test>"), nil),
			Value: sexpr.NewSymbol("whole-map", span.Unknown("<test>"), nil),
		},
	}, span.Unknown("<test>"), nil)

	form := sexpr.NewVector([]sexpr.Form{
		sexpr.NewSymbol("head", span.Unknown("<test>"), nil),
		sexpr.NewVector([]sexpr.Form{
			sexpr.NewSymbol("inner", span.Unknown("<test>"), nil),
			innerMap,
		}, span.Unknown("<test>"), nil),
		outerMap,
		sexpr.NewKeyword("as", span.Unknown("<test>"), nil),
		sexpr.NewSymbol("all", span.Unknown("<test>"), nil),
		sexpr.NewSymbol("&", span.Unknown("<test>"), nil),
		sexpr.NewSymbol("rest", span.Unknown("<test>"), nil),
	}, span.Unknown("<test>"), nil)

	plan, err := BuildDestructurePlan(form)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || plan.Vector == nil {
		t.Fatalf("expected vector destructure plan, got %#v", plan)
	}
	if len(plan.Vector.Items) != 3 {
		t.Fatalf("vector item count = %d, want 3", len(plan.Vector.Items))
	}
	if plan.Vector.Items[1] == nil || plan.Vector.Items[1].Vector == nil {
		t.Fatalf("expected nested vector pattern to be preserved, got %#v", plan.Vector.Items[1])
	}
	if plan.Vector.Items[2] == nil || plan.Vector.Items[2].Map == nil {
		t.Fatalf("expected nested map pattern to be preserved, got %#v", plan.Vector.Items[2])
	}
	if plan.Vector.As == nil || plan.Vector.As.Name != "all" {
		t.Fatalf("vector :as binding = %#v, want all", plan.Vector.As)
	}
	if plan.Vector.Rest == nil || plan.Vector.Rest.Name != "rest" {
		t.Fatalf("vector rest binding = %#v, want rest", plan.Vector.Rest)
	}
	if plan.Vector.Items[2].Map.As == nil || plan.Vector.Items[2].Map.As.Name != "whole-map" {
		t.Fatalf("map :as binding = %#v, want whole-map", plan.Vector.Items[2].Map.As)
	}
	if len(plan.Vector.Items[2].Map.Keys) != 1 {
		t.Fatalf("map :keys item count = %d, want 1", len(plan.Vector.Items[2].Map.Keys))
	}
}
