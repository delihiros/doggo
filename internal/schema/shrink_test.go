package schema

import (
	"errors"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestShrinkIntMovesTowardZero(t *testing.T) {
	sch, err := Normalize(atom.NewKeyword("int"))
	if err != nil {
		t.Fatal(err)
	}

	candidates, err := ShrinkSeq(sch, atom.NewNumberInt(10), 5)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := valuesString(candidates), "[0 5 9 8 7]"; got != want {
		t.Fatalf("unexpected shrink order: %s, want %s", got, want)
	}
	for _, candidate := range candidates {
		if !Validate(sch, candidate) {
			t.Fatalf("candidate %s no longer validates", candidate)
		}
	}
}

func TestShrinkStringIncludesEmptyString(t *testing.T) {
	sch, err := Normalize(atom.NewKeyword("string"))
	if err != nil {
		t.Fatal(err)
	}

	candidates, err := ShrinkSeq(sch, atom.NewString("abcd"), 4)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := valuesString(candidates), "[\"\" \"ab\" \"abc\" \"a\"]"; got != want {
		t.Fatalf("unexpected string shrink candidates: %s, want %s", got, want)
	}
	for _, candidate := range candidates {
		if candidate == nil || candidate.Type() != "string" {
			t.Fatalf("candidate %v is not a string", candidate)
		}
		if !Validate(sch, candidate) {
			t.Fatalf("candidate %s no longer validates", candidate)
		}
	}
}

func TestShrinkVectorPrefersRemovingElementsBeforeShrinkingValues(t *testing.T) {
	sch, err := Normalize(coll.NewVector(atom.NewKeyword("vector"), atom.NewKeyword("int")))
	if err != nil {
		t.Fatal(err)
	}

	value := coll.NewVector(atom.NewNumberInt(3), atom.NewNumberInt(2), atom.NewNumberInt(1))
	candidates, err := ShrinkSeq(sch, value, 9)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := valuesString(candidates), "[[2 1] [3 1] [3 2] [0 2 1] [1 2 1] [2 2 1] [3 0 1] [3 1 1] [3 2 0]]"; got != want {
		t.Fatalf("unexpected vector shrink order: %s, want %s", got, want)
	}
	for _, candidate := range candidates {
		if !Validate(sch, candidate) {
			t.Fatalf("candidate %s no longer validates", candidate)
		}
	}
}

func TestShrinkListPrefersRemovingElementsBeforeShrinkingValues(t *testing.T) {
	sch, err := Normalize(coll.NewVector(atom.NewKeyword("list"), atom.NewKeyword("int")))
	if err != nil {
		t.Fatal(err)
	}

	value := coll.NewList(atom.NewNumberInt(3), atom.NewNumberInt(2))
	candidates, err := ShrinkSeq(sch, value, 7)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := valuesString(candidates), "[(2) (3) (0 2) (1 2) (2 2) (3 0) (3 1)]"; got != want {
		t.Fatalf("unexpected list shrink order: %s, want %s", got, want)
	}
	for _, candidate := range candidates {
		if !Validate(sch, candidate) {
			t.Fatalf("candidate %s no longer validates", candidate)
		}
	}
}

func TestShrinkTupleExposesMultipleChildCandidates(t *testing.T) {
	sch, err := Normalize(coll.NewVector(atom.NewKeyword("tuple"), atom.NewKeyword("int")))
	if err != nil {
		t.Fatal(err)
	}

	value := coll.NewVector(atom.NewNumberInt(10))
	candidates, err := ShrinkSeq(sch, value, 3)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := valuesString(candidates), "[[0] [5] [9]]"; got != want {
		t.Fatalf("unexpected tuple shrink order: %s, want %s", got, want)
	}
	for _, candidate := range candidates {
		if !Validate(sch, candidate) {
			t.Fatalf("candidate %s no longer validates", candidate)
		}
	}
}

func TestShrinkSetExposesNestedCandidatesWithoutCollapsingSets(t *testing.T) {
	sch, err := Normalize(coll.NewVector(atom.NewKeyword("set"), atom.NewKeyword("int")))
	if err != nil {
		t.Fatal(err)
	}

	value := coll.NewSet(atom.NewNumberInt(3), atom.NewNumberInt(1))
	candidates, err := ShrinkSeq(sch, value, 10)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := valuesString(candidates), "[#{1} #{3} #{0 1} #{2 1} #{3 0}]"; got != want {
		t.Fatalf("unexpected set shrink order: %s, want %s", got, want)
	}
	for _, candidate := range candidates {
		if !Validate(sch, candidate) {
			t.Fatalf("candidate %s no longer validates", candidate)
		}
		if candidate.(*coll.Set).Count() != 1 && candidate.(*coll.Set).Count() != 2 {
			t.Fatalf("candidate %s has unexpected cardinality", candidate)
		}
	}
}

func TestShrinkMapKeepsRequiredKeysAndShrinksOptionalValues(t *testing.T) {
	sch, err := Normalize(coll.NewVector(
		atom.NewKeyword("map"),
		coll.NewVector(atom.NewKeyword("name"), atom.NewKeyword("string")),
		coll.NewVector(atom.NewKeyword("age"), coll.NewVector(atom.NewKeyword("maybe"), atom.NewKeyword("int"))),
	))
	if err != nil {
		t.Fatal(err)
	}

	value := coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("name"), atom.NewString("Ada"),
		atom.NewKeyword("age"), atom.NewNumberInt(7),
	})
	candidates, err := ShrinkSeq(sch, value, 4)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := valuesString(candidates), "[{:name \"Ada\", :age nil} {:name \"\", :age 7} {:name \"A\", :age 7} {:name \"Ad\", :age 7}]"; got != want {
		t.Fatalf("unexpected map shrink order: %s, want %s", got, want)
	}
	for _, candidate := range candidates {
		if !Validate(sch, candidate) {
			t.Fatalf("candidate %s no longer validates", candidate)
		}
		m := candidate.(*coll.Map)
		if m.Find(atom.NewKeyword("name")) == nil {
			t.Fatalf("required key :name was removed from %s", candidate)
		}
	}
}

func TestShrinkMapOfExposesMultipleValueAndSafeKeyCandidates(t *testing.T) {
	sch, err := Normalize(coll.NewVector(atom.NewKeyword("map-of"), atom.NewKeyword("bool"), atom.NewKeyword("int")))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("values-before-keys", func(t *testing.T) {
		value := coll.NewMapFromPairs([]types.Value{
			atom.True, atom.NewNumberInt(3),
		})
		candidates, err := ShrinkSeq(sch, value, 5)
		if err != nil {
			t.Fatal(err)
		}

		if got, want := valuesString(candidates), "[{} {true 0} {true 1} {true 2} {false 3}]"; got != want {
			t.Fatalf("unexpected map-of shrink order: %s, want %s", got, want)
		}
		for _, candidate := range candidates {
			if !Validate(sch, candidate) {
				t.Fatalf("candidate %s no longer validates", candidate)
			}
		}
	})

	t.Run("internal-value-candidate-cap", func(t *testing.T) {
		value := coll.NewMapFromPairs([]types.Value{
			atom.True, atom.NewNumberInt(10),
		})
		candidates, err := ShrinkSeq(sch, value, 10)
		if err != nil {
			t.Fatal(err)
		}

		if got, want := valuesString(candidates), "[{} {true 0} {true 5} {true 9} {true 8} {true 7} {true 6} {true 4} {true 3} {false 10}]"; got != want {
			t.Fatalf("unexpected capped map-of shrink order: %s, want %s", got, want)
		}
		for _, candidate := range candidates {
			if !Validate(sch, candidate) {
				t.Fatalf("candidate %s no longer validates", candidate)
			}
		}
	})
}

func TestShrinkOrUsesAcceptedBranchOnly(t *testing.T) {
	sch, err := Normalize(coll.NewVector(
		atom.NewKeyword("or"),
		atom.NewKeyword("string"),
		atom.NewKeyword("int"),
	))
	if err != nil {
		t.Fatal(err)
	}

	candidates, err := ShrinkSeq(sch, atom.NewNumberInt(7), 3)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := valuesString(candidates), "[0 3 6]"; got != want {
		t.Fatalf("unexpected or shrink order: %s, want %s", got, want)
	}
	for _, candidate := range candidates {
		if candidate.Type() != "number" {
			t.Fatalf("expected accepted int branch only, got %s", candidate.Type())
		}
		if !Validate(sch, candidate) {
			t.Fatalf("candidate %s no longer validates", candidate)
		}
	}
}

func TestShrinkRejectsPredicateAndAndCanonically(t *testing.T) {
	tests := []struct {
		name         string
		schema       types.Value
		expectedKind string
	}{
		{
			name:         "predicate",
			schema:       coll.NewVector(atom.NewKeyword("predicate"), testCallable{}),
			expectedKind: "predicate",
		},
		{
			name:         "and",
			schema:       coll.NewVector(atom.NewKeyword("and"), atom.NewKeyword("int"), atom.NewKeyword("number")),
			expectedKind: "and",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sch, err := Normalize(tt.schema)
			if err != nil {
				t.Fatal(err)
			}

			_, err = ShrinkSeq(sch, atom.NewNumberInt(7), 3)
			if err == nil {
				t.Fatal("ShrinkSeq returned nil error, want canonical failure")
			}

			var shrinkErr *ShrinkError
			if !errors.As(err, &shrinkErr) {
				t.Fatalf("ShrinkSeq returned %T, want *ShrinkError", err)
			}

			data := shrinkErr.ExceptionData()
			if data == nil {
				t.Fatal("ShrinkError.ExceptionData() = nil")
			}
			if got, want := data.Type, "SchemaShrinkError"; got != want {
				t.Fatalf("exception type = %q, want %q", got, want)
			}
			if got, want := data.Data["schema_kind"], tt.expectedKind; got != want {
				t.Fatalf("schema_kind = %#v, want %#v", got, want)
			}
			if got, want := data.Data["reason"], "unsupported shrink schema kind"; got != want {
				t.Fatalf("reason = %#v, want %#v", got, want)
			}
		})
	}
}

func TestShrinkRejectsInvalidInputCanonically(t *testing.T) {
	sch, err := Normalize(coll.NewVector(atom.NewKeyword("vector"), atom.NewKeyword("int")))
	if err != nil {
		t.Fatal(err)
	}

	_, err = ShrinkSeq(sch, coll.NewVector(atom.NewNumberInt(1), atom.NewString("x")), 2)
	if err == nil {
		t.Fatal("ShrinkSeq returned nil error, want canonical failure")
	}

	var shrinkErr *ShrinkError
	if !errors.As(err, &shrinkErr) {
		t.Fatalf("ShrinkSeq returned %T, want *ShrinkError", err)
	}
	data := shrinkErr.ExceptionData()
	if data == nil {
		t.Fatal("ShrinkError.ExceptionData() = nil")
	}
	if got, want := data.Data["reason"], "value does not satisfy schema for shrinking"; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
	if data.Data["value"] == nil {
		t.Fatal("expected invalid input value in canonical error data")
	}
}

func TestShrinkSeqCountHandling(t *testing.T) {
	sch, err := Normalize(atom.NewKeyword("int"))
	if err != nil {
		t.Fatal(err)
	}

	candidates, err := ShrinkSeq(sch, atom.NewNumberInt(10), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("ShrinkSeq(..., 0) = %s, want []", valuesString(candidates))
	}

	_, err = ShrinkSeq(sch, atom.NewNumberInt(10), -1)
	if err == nil {
		t.Fatal("ShrinkSeq(..., -1) = nil error, want canonical failure")
	}
	var shrinkErr *ShrinkError
	if !errors.As(err, &shrinkErr) {
		t.Fatalf("ShrinkSeq(..., -1) returned %T, want *ShrinkError", err)
	}
	if got, want := shrinkErr.ExceptionData().Data["reason"], "malformed shrink count"; got != want {
		t.Fatalf("reason = %#v, want %#v", got, want)
	}
}

func valuesString(values []types.Value) string {
	if len(values) == 0 {
		return "[]"
	}
	return coll.NewVector(values...).String()
}
