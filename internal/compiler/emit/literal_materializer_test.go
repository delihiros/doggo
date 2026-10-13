package emit

import (
	"testing"

	"doggo/internal/compiler/expand"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func TestMaterializeQuotedLiteralPreservesResolvedSymbol(t *testing.T) {
	plan, err := expand.BuildQuotedLiteralPlan(sexpr.NewResolvedSymbol(
		"user/foo",
		sexpr.SymbolResolution{
			Kind:      sexpr.GlobalSymbolResolution,
			Index:     3,
			Namespace: "demo.ns",
		},
		span.Unknown("<test>"),
		nil,
	))
	if err != nil {
		t.Fatal(err)
	}

	value, err := MaterializeQuotedLiteral(plan)
	if err != nil {
		t.Fatal(err)
	}

	resolved, ok := value.(*types.ResolvedSymbol)
	if !ok {
		t.Fatalf("materialized value = %T, want *types.ResolvedSymbol", value)
	}
	if resolved.Name != "user/foo" {
		t.Fatalf("resolved symbol name = %q, want %q", resolved.Name, "user/foo")
	}
	if resolved.Resolution.Kind != types.GlobalSymbolResolution {
		t.Fatalf("resolution kind = %q, want %q", resolved.Resolution.Kind, types.GlobalSymbolResolution)
	}
}

func TestMaterializeMetadataReturnsRuntimeMap(t *testing.T) {
	metaPlan, err := expand.BuildMetadataLiteralPlan(sexpr.NewMap([]sexpr.MapEntry{
		{
			Key:   sexpr.NewKeyword("private", span.Unknown("<test>"), nil),
			Value: sexpr.NewBool(true, span.Unknown("<test>"), nil),
		},
	}, span.Unknown("<test>"), nil))
	if err != nil {
		t.Fatal(err)
	}

	meta, err := MaterializeMetadata(metaPlan)
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil {
		t.Fatal("expected runtime metadata map")
	}
	if got := meta.Get(atom.NewKeyword("private")); got != nil {
		if got.String() != "true" {
			t.Fatalf("metadata value = %s, want true", got)
		}
	}
}
