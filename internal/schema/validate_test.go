package schema

import (
	"context"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type schemaContextKey struct{}

type contextCheckingPredicate struct{}

func (contextCheckingPredicate) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	return atom.NewBoolean(ctx != nil && ctx.Value(schemaContextKey{}) == "present"), nil
}

func (contextCheckingPredicate) String() string { return "#<context-checking-predicate>" }
func (contextCheckingPredicate) Type() string   { return "context-checking-predicate" }
func (contextCheckingPredicate) Hash() uint32   { return 2 }
func (contextCheckingPredicate) Equals(other types.Value) bool {
	_, ok := other.(contextCheckingPredicate)
	return ok
}
func (contextCheckingPredicate) IsSelfEvaluating() bool { return true }

func TestValidateAcceptsMatchingScalarValues(t *testing.T) {
	schema, err := Normalize(atom.NewKeyword("int"))
	if err != nil {
		t.Fatalf("Normalize(:int) returned error: %v", err)
	}
	if ok := Validate(schema, atom.NewNumberInt(7)); !ok {
		t.Fatal("Validate(:int, 7) = false, want true")
	}
}

func TestValidateRejectsMismatchedScalarValues(t *testing.T) {
	schema, err := Normalize(atom.NewKeyword("string"))
	if err != nil {
		t.Fatalf("Normalize(:string) returned error: %v", err)
	}
	if ok := Validate(schema, atom.NewNumberInt(7)); ok {
		t.Fatal("Validate(:string, 7) = true, want false")
	}
}

func TestValidateSupportsVectorElementChecks(t *testing.T) {
	schema, err := Normalize(coll.NewVector(atom.NewKeyword("vector"), atom.NewKeyword("int")))
	if err != nil {
		t.Fatalf("Normalize(vector schema) returned error: %v", err)
	}
	if ok := Validate(schema, coll.NewVector(atom.NewNumberInt(1), atom.NewNumberInt(2))); !ok {
		t.Fatal("Validate(vector schema, matching vector) = false, want true")
	}
}

func TestValidateSupportsRemainingFirstSliceDslForms(t *testing.T) {
	tests := []struct {
		name   string
		schema types.Value
		value  types.Value
		valid  bool
	}{
		{
			name:   "maybe_nil",
			schema: coll.NewVector(atom.NewKeyword("maybe"), atom.NewKeyword("int")),
			value:  nil,
			valid:  true,
		},
		{
			name:   "map_entry_maybe_nil",
			schema: coll.NewVector(atom.NewKeyword("map"), coll.NewVector(atom.NewKeyword("age"), coll.NewVector(atom.NewKeyword("maybe"), atom.NewKeyword("int")))),
			value:  coll.NewMapFromPairs([]types.Value{atom.NewKeyword("age"), nil}),
			valid:  true,
		},
		{
			name:   "set",
			schema: coll.NewVector(atom.NewKeyword("set"), atom.NewKeyword("keyword")),
			value:  coll.NewSet(atom.NewKeyword("a"), atom.NewKeyword("b")),
			valid:  true,
		},
		{
			name:   "list",
			schema: coll.NewVector(atom.NewKeyword("list"), atom.NewKeyword("string")),
			value:  coll.NewList(atom.NewString("a"), atom.NewString("b")),
			valid:  true,
		},
		{
			name:   "tuple",
			schema: coll.NewVector(atom.NewKeyword("tuple"), atom.NewKeyword("string"), atom.NewKeyword("int")),
			value:  coll.NewVector(atom.NewString("age"), atom.NewNumberInt(3)),
			valid:  true,
		},
		{
			name:   "map_of",
			schema: coll.NewVector(atom.NewKeyword("map-of"), atom.NewKeyword("keyword"), atom.NewKeyword("int")),
			value:  coll.NewMapFromPairs([]types.Value{atom.NewKeyword("a"), atom.NewNumberInt(1), atom.NewKeyword("b"), atom.NewNumberInt(2)}),
			valid:  true,
		},
		{
			name:   "map_of_empty",
			schema: coll.NewVector(atom.NewKeyword("map-of"), atom.NewKeyword("keyword"), atom.NewKeyword("int")),
			value:  coll.NewMapFromPairs(nil),
			valid:  true,
		},
		{
			name:   "map_of_false_key",
			schema: coll.NewVector(atom.NewKeyword("map-of"), atom.NewKeyword("bool"), atom.NewKeyword("int")),
			value:  coll.NewMapFromPairs([]types.Value{atom.False, atom.NewNumberInt(10)}),
			valid:  true,
		},
		{
			name:   "or",
			schema: coll.NewVector(atom.NewKeyword("or"), atom.NewKeyword("string"), atom.NewKeyword("int")),
			value:  atom.NewNumberInt(7),
			valid:  true,
		},
		{
			name:   "and",
			schema: coll.NewVector(atom.NewKeyword("and"), atom.NewKeyword("number"), atom.NewKeyword("int")),
			value:  atom.NewNumberInt(7),
			valid:  true,
		},
		{
			name:   "enum",
			schema: coll.NewVector(atom.NewKeyword("enum"), atom.NewKeyword("dev"), atom.NewKeyword("prod")),
			value:  atom.NewKeyword("prod"),
			valid:  true,
		},
		{
			name:   "map_of_invalid_value",
			schema: coll.NewVector(atom.NewKeyword("map-of"), atom.NewKeyword("keyword"), atom.NewKeyword("int")),
			value:  coll.NewMapFromPairs([]types.Value{atom.NewKeyword("a"), atom.NewString("x")}),
			valid:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := Normalize(tt.schema)
			if err != nil {
				t.Fatalf("Normalize(%s) returned error: %v", tt.name, err)
			}
			if got := Validate(schema, tt.value); got != tt.valid {
				t.Fatalf("Validate(%s) = %v, want %v", tt.name, got, tt.valid)
			}
		})
	}
}

func TestValidateSupportsApprovedScalarSchemas(t *testing.T) {
	tests := []struct {
		name   string
		schema types.Value
		value  types.Value
		valid  bool
	}{
		{"any", atom.NewKeyword("any"), atom.NewString("x"), true},
		{"nil", atom.NewKeyword("nil"), nil, true},
		{"keyword", atom.NewKeyword("keyword"), atom.NewKeyword("x"), true},
		{"symbol", atom.NewKeyword("symbol"), types.NewSymbol("x"), true},
		{"bool", atom.NewKeyword("bool"), atom.True, true},
		{"float", atom.NewKeyword("float"), atom.NewNumberFloat(1.5), true},
		{"number", atom.NewKeyword("number"), atom.NewNumberInt(2), true},
		{"map", atom.NewKeyword("map"), coll.NewMapFromPairs([]types.Value{atom.NewKeyword("a"), atom.NewNumberInt(1)}), true},
		{"vector", atom.NewKeyword("vector"), coll.NewVector(atom.NewNumberInt(1)), true},
		{"set", atom.NewKeyword("set"), coll.NewSet(atom.NewKeyword("a")), true},
		{"list", atom.NewKeyword("list"), coll.NewList(atom.NewString("x")), true},
		{"seq", atom.NewKeyword("seq"), coll.NewList(atom.NewString("x")), true},
		{"bool_invalid", atom.NewKeyword("bool"), atom.NewNumberInt(1), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := Normalize(tt.schema)
			if err != nil {
				t.Fatalf("Normalize(%s) returned error: %v", tt.name, err)
			}
			if got := Validate(schema, tt.value); got != tt.valid {
				t.Fatalf("Validate(%s) = %v, want %v", tt.name, got, tt.valid)
			}
		})
	}
}

func TestValidatePredicateUsesCallerContext(t *testing.T) {
	schema, err := Normalize(coll.NewVector(atom.NewKeyword("predicate"), contextCheckingPredicate{}))
	if err != nil {
		t.Fatalf("Normalize(predicate schema) returned error: %v", err)
	}

	ctx := context.WithValue(context.Background(), schemaContextKey{}, "present")
	if got := ValidateWithContext(ctx, schema, atom.NewNumberInt(1)); !got {
		t.Fatal("ValidateWithContext(predicate schema) = false, want true when caller context is preserved")
	}
}
