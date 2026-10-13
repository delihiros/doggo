package schema

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestNormalizeSchemaBuildsCanonicalVectorAndMapSchemas(t *testing.T) {
	vectorSchema, err := Normalize(coll.NewVector(
		atom.NewKeyword("vector"),
		atom.NewKeyword("string"),
	))
	if err != nil {
		t.Fatalf("Normalize(vector schema) returned error: %v", err)
	}
	if got, want := vectorSchema.Kind(), KindVector; got != want {
		t.Fatalf("vector schema kind = %v, want %v", got, want)
	}
	if got, want := vectorSchema.Element().Kind(), KindString; got != want {
		t.Fatalf("vector schema element kind = %v, want %v", got, want)
	}

	mapSchema, err := Normalize(coll.NewVector(
		atom.NewKeyword("map"),
		coll.NewVector(atom.NewKeyword("name"), atom.NewKeyword("string")),
		coll.NewVector(atom.NewKeyword("age"), atom.NewKeyword("int")),
	))
	if err != nil {
		t.Fatalf("Normalize(map schema) returned error: %v", err)
	}
	if got, want := mapSchema.Kind(), KindMap; got != want {
		t.Fatalf("map schema kind = %v, want %v", got, want)
	}
	if got, want := len(mapSchema.Entries()), 2; got != want {
		t.Fatalf("map schema entry count = %d, want %d", got, want)
	}
	if got, want := mapSchema.Entries()[0].Key.String(), ":name"; got != want {
		t.Fatalf("map schema first key = %q, want %q", got, want)
	}
	if got, want := mapSchema.Entries()[0].Schema.Kind(), KindString; got != want {
		t.Fatalf("map schema first value kind = %v, want %v", got, want)
	}
	if got, want := mapSchema.Entries()[1].Schema.Kind(), KindInt; got != want {
		t.Fatalf("map schema second value kind = %v, want %v", got, want)
	}
}

func TestPredicateSchemaUsesCallableBoundary(t *testing.T) {
	if _, err := Normalize(coll.NewVector(atom.NewKeyword("predicate"), atom.NewString("not-callable"))); err == nil {
		t.Fatal("Normalize(predicate schema with string) = nil error, want callable boundary failure")
	}

	schema, err := Normalize(coll.NewVector(atom.NewKeyword("predicate"), testCallable{}))
	if err != nil {
		t.Fatalf("Normalize(predicate schema with callable) returned error: %v", err)
	}
	if got, want := schema.Kind(), KindPredicate; got != want {
		t.Fatalf("predicate schema kind = %v, want %v", got, want)
	}
}

func TestNormalizeSupportsRemainingFirstSliceDslForms(t *testing.T) {
	tests := []struct {
		name   string
		schema types.Value
		kind   Kind
	}{
		{
			name:   "maybe",
			schema: coll.NewVector(atom.NewKeyword("maybe"), atom.NewKeyword("int")),
			kind:   KindMaybe,
		},
		{
			name:   "set",
			schema: coll.NewVector(atom.NewKeyword("set"), atom.NewKeyword("keyword")),
			kind:   KindSet,
		},
		{
			name:   "list",
			schema: coll.NewVector(atom.NewKeyword("list"), atom.NewKeyword("string")),
			kind:   KindList,
		},
		{
			name:   "tuple",
			schema: coll.NewVector(atom.NewKeyword("tuple"), atom.NewKeyword("string"), atom.NewKeyword("int")),
			kind:   KindTuple,
		},
		{
			name:   "map-of",
			schema: coll.NewVector(atom.NewKeyword("map-of"), atom.NewKeyword("keyword"), atom.NewKeyword("int")),
			kind:   KindMapOf,
		},
		{
			name:   "or",
			schema: coll.NewVector(atom.NewKeyword("or"), atom.NewKeyword("string"), atom.NewKeyword("int")),
			kind:   KindOr,
		},
		{
			name:   "and",
			schema: coll.NewVector(atom.NewKeyword("and"), atom.NewKeyword("number"), atom.NewKeyword("int")),
			kind:   KindAnd,
		},
		{
			name:   "enum",
			schema: coll.NewVector(atom.NewKeyword("enum"), atom.NewKeyword("dev"), atom.NewKeyword("prod")),
			kind:   KindEnum,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := Normalize(tt.schema)
			if err != nil {
				t.Fatalf("Normalize(%s) returned error: %v", tt.name, err)
			}
			if got, want := schema.Kind(), tt.kind; got != want {
				t.Fatalf("schema kind = %v, want %v", got, want)
			}
		})
	}
}

func TestNormalizeRejectsEmptyOrAndSchemas(t *testing.T) {
	tests := []types.Value{
		coll.NewVector(atom.NewKeyword("or")),
		coll.NewVector(atom.NewKeyword("and")),
	}

	for _, schema := range tests {
		if _, err := Normalize(schema); err == nil {
			t.Fatalf("Normalize(%s) = nil error, want malformed combinator failure", schema.String())
		}
	}
}

func TestNormalizeValueRejectsNonSchemaInput(t *testing.T) {
	if _, err := Normalize(coll.NewEmptyMap()); err == nil {
		t.Fatal("Normalize(map literal) = nil error, want schema normalization failure")
	}
}

func TestNormalizeErrorProjectsCanonicalExceptionData(t *testing.T) {
	_, err := Normalize(coll.NewVector(atom.NewKeyword("predicate"), atom.NewNumberInt(42)))
	if err == nil {
		t.Fatal("Normalize(predicate schema with non-callable) = nil error, want failure")
	}

	normalizeErr, ok := err.(*NormalizeError)
	if !ok {
		t.Fatalf("Normalize returned %T, want *NormalizeError", err)
	}

	data := normalizeErr.ExceptionData()
	if data == nil {
		t.Fatal("NormalizeError.ExceptionData() = nil")
	}
	if got := data.Type; got != "SchemaNormalizeError" {
		t.Fatalf("ExceptionData type = %q, want %q", got, "SchemaNormalizeError")
	}
	if got := data.Data["expected"]; got != "callable" {
		t.Fatalf("ExceptionData expected = %#v, want %#v", got, "callable")
	}
	if got := data.Data["actual_type"]; got != "number" {
		t.Fatalf("ExceptionData actual_type = %#v, want %#v", got, "number")
	}
}
