package types_test

import (
	"context"
	"strings"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestMetadataSymbolsCompareByName(t *testing.T) {
	meta := coll.NewMapFromPairs([]types.Value{atom.NewKeyword("tag"), atom.NewString("meta")})
	symbol := types.NewSymbolWithMeta("user/name", meta)
	resolved := types.NewResolvedSymbolWithMeta("user/name", types.SymbolResolution{
		Kind:      types.GlobalSymbolResolution,
		Index:     7,
		Namespace: "user",
	}, meta)

	for _, other := range []types.Value{types.Symbol("user/name"), symbol, resolved} {
		if !symbol.Equals(other) {
			t.Fatalf("SymbolWithMeta should equal %T with same name", other)
		}
		if !resolved.Equals(other) {
			t.Fatalf("ResolvedSymbol should equal %T with same name", other)
		}
	}
}

func TestMetadataSymbolsInvokeAsMapKeys(t *testing.T) {
	values := coll.NewMapFromPairs([]types.Value{
		types.Symbol("user/name"), atom.NewString("Ada"),
	})
	symbol := types.NewSymbolWithMeta("user/name", nil)
	resolved := types.NewResolvedSymbol("user/name", types.SymbolResolution{
		Kind:      types.GlobalSymbolResolution,
		Index:     1,
		Namespace: "user",
	})

	for _, fn := range []types.Fn{symbol, resolved} {
		got, err := fn.Invoke(context.Background(), values)
		if err != nil {
			t.Fatalf("%T Invoke returned error: %v", fn, err)
		}
		if got.String() != `"Ada"` {
			t.Fatalf("%T Invoke returned %s", fn, got.String())
		}

		got, err = fn.Invoke(context.Background(), coll.NewEmptyMap(), atom.NewString("default"))
		if err != nil {
			t.Fatalf("%T Invoke with default returned error: %v", fn, err)
		}
		if got.String() != `"default"` {
			t.Fatalf("%T Invoke with default returned %s", fn, got.String())
		}
	}
}

func TestMetadataSymbolsRejectInvalidInvocation(t *testing.T) {
	symbol := types.NewSymbolWithMeta("user/name", nil)
	resolved := types.NewResolvedSymbol("user/name", types.SymbolResolution{})

	for _, fn := range []types.Fn{symbol, resolved} {
		if _, err := fn.Invoke(context.Background()); err == nil || !strings.Contains(err.Error(), "requires at least 1 argument") {
			t.Fatalf("%T Invoke without args error = %v", fn, err)
		}
		if _, err := fn.Invoke(context.Background(), atom.NewString("not map")); err == nil || !strings.Contains(err.Error(), "expects map") {
			t.Fatalf("%T Invoke with non-map error = %v", fn, err)
		}
	}
}
