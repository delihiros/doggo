package schema

import (
	"context"
	"errors"
	"iter"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestCheckShrinkingPreservesPredicateContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), schemaContextKey{}, "present")
	predicate := coll.NewVector(atom.NewKeyword("predicate"), contextCheckingPredicate{})
	value := atom.NewNumberInt(5)
	for _, tt := range []struct {
		name  string
		form  types.Value
		value types.Value
	}{
		{"predicate", predicate, value},
		{"maybe", coll.NewVector(atom.NewKeyword("maybe"), predicate), value},
		{"vector", coll.NewVector(atom.NewKeyword("vector"), predicate), coll.NewVector(value)},
		{"list", coll.NewVector(atom.NewKeyword("list"), predicate), coll.NewList(value)},
		{"set", coll.NewVector(atom.NewKeyword("set"), predicate), coll.NewSet(value)},
		{"tuple", coll.NewVector(atom.NewKeyword("tuple"), predicate), coll.NewVector(value)},
		{"map", coll.NewVector(atom.NewKeyword("map"), coll.NewVector(atom.NewKeyword("x"), predicate)), coll.NewMapFromPairs([]types.Value{atom.NewKeyword("x"), value})},
		{"map-of-value", coll.NewVector(atom.NewKeyword("map-of"), atom.NewKeyword("int"), predicate), coll.NewMapFromPairs([]types.Value{value, value})},
		{"map-of-key", coll.NewVector(atom.NewKeyword("map-of"), predicate, atom.NewKeyword("int")), coll.NewMapFromPairs([]types.Value{value, value})},
		{"or", coll.NewVector(atom.NewKeyword("or"), predicate, atom.NewKeyword("int")), value},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sch := mustSchema(t, tt.form)
			report, err := Check(ctx, checkTestFn{invoke: func(callCtx context.Context, _ ...types.Value) (types.Value, error) {
				if callCtx != ctx {
					t.Error("property lost caller context")
				}
				return atom.False, nil
			}}, sch, CheckOptions{Seed: 1, SeedSet: true, Iterations: 1, IterationsSet: true,
				GeneratorOverrides: map[Kind]any{sch.Kind(): tt.value}})
			if err != nil {
				t.Fatalf("check should report unsupported shrinking as an ordinary property failure: %v", err)
			}
			if report.Pass || report.Failure == nil || report.Failure.Kind != atom.NewKeyword("return-false") {
				t.Fatalf("expected ordinary property failure, got %#v", report)
			}
			if !types.Equal(report.FailingInput, tt.value) || !types.Equal(report.ShrunkInput, tt.value) || len(report.ShrinkSteps) != 0 {
				t.Fatalf("unsupported shrinking changed counterexample: %#v", report)
			}
		})
	}
}

func TestShrinkWithContextPreservesRecursiveValidation(t *testing.T) {
	ctx := context.WithValue(context.Background(), schemaContextKey{}, "present")
	// The rejecting predicate keeps the int branch shrinkable while observing
	// the context used by initial, recursive, branch, and candidate validation.
	guardedInt := coll.NewVector(atom.NewKeyword("or"),
		coll.NewVector(atom.NewKeyword("predicate"), checkTestFn{invoke: func(callCtx context.Context, _ ...types.Value) (types.Value, error) {
			if callCtx != ctx {
				t.Error("shrink validation lost caller context")
			}
			return atom.False, nil
		}}), atom.NewKeyword("int"))
	three, one := atom.NewNumberInt(3), atom.NewNumberInt(1)
	for _, tt := range []struct {
		name        string
		form, value types.Value
		want        string
	}{
		{"or", guardedInt, three, "[0 1 2]"},
		{"maybe", coll.NewVector(atom.NewKeyword("maybe"), guardedInt), three, "[nil 0 1 2]"},
		{"vector", coll.NewVector(atom.NewKeyword("vector"), guardedInt), coll.NewVector(three, one), "[[1] [3] [0 1] [1 1] [2 1] [3 0]]"},
		{"list", coll.NewVector(atom.NewKeyword("list"), guardedInt), coll.NewList(three, one), "[(1) (3) (0 1) (1 1) (2 1) (3 0)]"},
		{"set", coll.NewVector(atom.NewKeyword("set"), guardedInt), coll.NewSet(three, one), "[#{1} #{3} #{0 1} #{2 1} #{3 0}]"},
		{"tuple", coll.NewVector(atom.NewKeyword("tuple"), guardedInt, guardedInt), coll.NewVector(three, one), "[[0 1] [1 1] [2 1] [3 0]]"},
		{"map", coll.NewVector(atom.NewKeyword("map"), coll.NewVector(atom.NewKeyword("x"), coll.NewVector(atom.NewKeyword("maybe"), guardedInt)), coll.NewVector(atom.NewKeyword("y"), guardedInt)), coll.NewMapFromPairs([]types.Value{atom.NewKeyword("x"), three, atom.NewKeyword("y"), one}), "[{:x nil, :y 1} {:x 0, :y 1} {:x 1, :y 1} {:x 2, :y 1} {:x 3, :y 0}]"},
		{"map-of", coll.NewVector(atom.NewKeyword("map-of"), guardedInt, guardedInt), coll.NewMapFromPairs([]types.Value{three, one}), "[{} {3 0} {0 1} {1 1} {2 1}]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sch := mustSchema(t, tt.form)
			stream, err := ShrinkWithContext(ctx, sch, tt.value)
			if err != nil {
				t.Fatal(err)
			}
			var candidates []types.Value
			for candidate := range stream {
				candidates = append(candidates, candidate)
			}
			if got := valuesString(candidates); got != tt.want {
				t.Fatalf("shrink candidates = %s, want %s", got, tt.want)
			}
			first, err := ShrinkSeqWithContext(ctx, sch, tt.value, 2)
			if err != nil || valuesString(first) != valuesString(candidates[:2]) {
				t.Fatalf("shrink-seq prefix = %v, %v; want %v", first, err, candidates[:2])
			}
		})
	}
}

func TestShrinkWithContextClassifiesPredicateAndInvalidValues(t *testing.T) {
	ctx := context.WithValue(context.Background(), schemaContextKey{}, "present")
	predicate := coll.NewVector(atom.NewKeyword("predicate"), contextCheckingPredicate{})
	sch := mustSchema(t, coll.NewVector(atom.NewKeyword("tuple"), predicate))
	value := coll.NewVector(atom.NewNumberInt(5))
	for _, tt := range []struct {
		name                 string
		ctx                  context.Context
		wantReason, wantPath string
	}{
		{"with caller context", ctx, "unsupported shrink schema kind", "[0]"},
		{"without caller context", context.Background(), "value does not satisfy schema for shrinking", "[]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ShrinkWithContext(tt.ctx, sch, value)
			var shrinkErr *ShrinkError
			if !errors.As(err, &shrinkErr) || shrinkErr.Reason != tt.wantReason || valuesString(shrinkErr.Path) != tt.wantPath {
				t.Fatalf("shrink error = %#v, want %q at %s", err, tt.wantReason, tt.wantPath)
			}
			_, err = ShrinkSeqWithContext(tt.ctx, sch, value, 2)
			if !errors.As(err, &shrinkErr) || shrinkErr.Reason != tt.wantReason || valuesString(shrinkErr.Path) != tt.wantPath {
				t.Fatalf("shrink-seq error = %#v, want %q at %s", err, tt.wantReason, tt.wantPath)
			}
		})
	}
}

func TestShrinkContextAPIsPreserveCompatibility(t *testing.T) {
	ctx := context.WithValue(context.Background(), schemaContextKey{}, "present")
	intSchema := mustSchema(t, atom.NewKeyword("int"))
	for _, tt := range []struct {
		name      string
		shrink    func(*Schema, types.Value) (iter.Seq[types.Value], error)
		shrinkSeq func(*Schema, types.Value, int) ([]types.Value, error)
	}{
		{"legacy", Shrink, ShrinkSeq},
		{"context", func(s *Schema, v types.Value) (iter.Seq[types.Value], error) { return ShrinkWithContext(ctx, s, v) }, func(s *Schema, v types.Value, n int) ([]types.Value, error) {
			return ShrinkSeqWithContext(ctx, s, v, n)
		}},
		{"nil context", func(s *Schema, v types.Value) (iter.Seq[types.Value], error) { return ShrinkWithContext(nil, s, v) }, func(s *Schema, v types.Value, n int) ([]types.Value, error) {
			return ShrinkSeqWithContext(nil, s, v, n)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			values, err := tt.shrinkSeq(intSchema, atom.NewNumberInt(10), 3)
			if err != nil || valuesString(values) != "[0 5 9]" {
				t.Fatalf("supported shrink = %v, %v", values, err)
			}
			stream, err := tt.shrink(intSchema, atom.NewNumberInt(10))
			if err != nil {
				t.Fatal(err)
			}
			var first types.Value
			for candidate := range stream {
				first = candidate
				break
			}
			if !types.Equal(first, atom.NewNumberInt(0)) {
				t.Fatalf("stream first = %v, want 0", first)
			}
			values, err = tt.shrinkSeq(nil, atom.NewString("invalid"), 0)
			if err != nil || values == nil || len(values) != 0 {
				t.Fatalf("zero count = %v, %v, want empty slice without validation", values, err)
			}
			_, err = tt.shrinkSeq(nil, atom.NewString("invalid"), -1)
			var shrinkErr *ShrinkError
			if !errors.As(err, &shrinkErr) || shrinkErr.Reason != "malformed shrink count" || shrinkErr.Count != -1 {
				t.Fatalf("negative count error = %#v", err)
			}
			_, err = tt.shrinkSeq(intSchema, atom.NewString("invalid"), 1)
			if !errors.As(err, &shrinkErr) || shrinkErr.Reason != "value does not satisfy schema for shrinking" {
				t.Fatalf("invalid value error = %#v", err)
			}
			_, err = tt.shrink(nil, atom.NewNumberInt(1))
			if !errors.As(err, &shrinkErr) || shrinkErr.Reason != "nil schema" {
				t.Fatalf("nil schema error = %#v", err)
			}
		})
	}
}
