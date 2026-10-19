package schema

import (
	"context"
	"math/rand"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestGenerateWithContextPreservesPredicateContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), schemaContextKey{}, "present")
	predicate := coll.NewVector(atom.NewKeyword("predicate"), contextCheckingPredicate{})
	for _, kind := range []string{"predicate", "vector", "set", "list", "seq", "tuple", "map", "map-of", "or", "maybe"} {
		t.Run(kind, func(t *testing.T) {
			var form types.Value = predicate
			switch kind {
			case "predicate":
			case "map":
				form = coll.NewVector(atom.NewKeyword(kind), coll.NewVector(atom.NewKeyword("value"), predicate))
			case "map-of":
				form = coll.NewVector(atom.NewKeyword(kind), atom.NewKeyword("int"), predicate)
			default:
				form = coll.NewVector(atom.NewKeyword(kind), predicate)
			}
			sch, err := Normalize(form)
			if err != nil {
				t.Fatal(err)
			}
			opts := GenerateOptions{Seed: 1, Size: 2, GeneratorOverrides: map[Kind]any{KindPredicate: atom.NewNumberInt(5)}}
			value, err := GenerateWithContext(ctx, sch, opts)
			if err != nil || !ValidateWithContext(ctx, sch, value) {
				t.Fatalf("generate %s with context = %v, %v", kind, value, err)
			}
			values, err := SampleWithContext(ctx, sch, 3, opts)
			if err != nil || len(values) != 3 {
				t.Fatalf("sample %s with context = %v, %v", kind, values, err)
			}
			for _, item := range values {
				if !ValidateWithContext(ctx, sch, item) {
					t.Fatalf("sample %s produced invalid %v", kind, item)
				}
			}
		})
	}
}

func TestGenerateWithContextValidatesCallableOverrides(t *testing.T) {
	ctx := context.WithValue(context.Background(), schemaContextKey{}, "present")
	sch, err := Normalize(coll.NewVector(atom.NewKeyword("predicate"), contextCheckingPredicate{}))
	if err != nil {
		t.Fatal(err)
	}
	generate := func(*Schema, *rand.Rand, GenerateOptions) (types.Value, error) { return atom.NewNumberInt(5), nil }
	for _, override := range []any{GeneratorFunc(generate), generate} {
		value, err := GenerateWithContext(ctx, sch, GenerateOptions{GeneratorOverrides: map[Kind]any{KindPredicate: override}})
		if err != nil || !ValidateWithContext(ctx, sch, value) {
			t.Fatalf("callable override with context = %v, %v", value, err)
		}
	}
}
