package schema

import (
	"errors"
	"math/rand"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestGenerateIsDeterministicWithSeed(t *testing.T) {
	sch, err := Normalize(coll.NewVector(atom.NewKeyword("vector"), atom.NewKeyword("int")))
	if err != nil {
		t.Fatal(err)
	}

	opts := GenerateOptions{Seed: 42, Size: 4}
	first, err := Generate(sch, opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(sch, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Equals(second) {
		t.Fatalf("expected deterministic output, got %s and %s", first, second)
	}
}

func TestGenerateProducesValuesThatValidate(t *testing.T) {
	tests := []struct {
		name   string
		schema types.Value
	}{
		{name: "string", schema: atom.NewKeyword("string")},
		{name: "vector", schema: coll.NewVector(atom.NewKeyword("vector"), atom.NewKeyword("int"))},
		{name: "set", schema: coll.NewVector(atom.NewKeyword("set"), atom.NewKeyword("keyword"))},
		{name: "list", schema: coll.NewVector(atom.NewKeyword("list"), atom.NewKeyword("string"))},
		{name: "tuple", schema: coll.NewVector(atom.NewKeyword("tuple"), atom.NewKeyword("string"), atom.NewKeyword("int"))},
		{name: "map", schema: coll.NewVector(atom.NewKeyword("map"), coll.NewVector(atom.NewKeyword("name"), atom.NewKeyword("string")), coll.NewVector(atom.NewKeyword("age"), atom.NewKeyword("int")))},
		{name: "map-of", schema: coll.NewVector(atom.NewKeyword("map-of"), atom.NewKeyword("keyword"), atom.NewKeyword("int"))},
		{name: "or", schema: coll.NewVector(atom.NewKeyword("or"), atom.NewKeyword("string"), atom.NewKeyword("int"))},
		{name: "enum", schema: coll.NewVector(atom.NewKeyword("enum"), atom.NewKeyword("dev"), atom.NewKeyword("prod"))},
		{name: "maybe", schema: coll.NewVector(atom.NewKeyword("maybe"), atom.NewKeyword("int"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sch, err := Normalize(tt.schema)
			if err != nil {
				t.Fatal(err)
			}
			value, err := Generate(sch, GenerateOptions{Seed: 7, Size: 3})
			if err != nil {
				t.Fatal(err)
			}
			if !Validate(sch, value) {
				t.Fatalf("generated value %s does not validate against %s", value, tt.schema)
			}
		})
	}
}

func TestGeneratePredicateAndAndFailWithoutOverride(t *testing.T) {
	tests := []struct {
		name         string
		schema       types.Value
		expectedKind string
	}{
		{name: "predicate", schema: coll.NewVector(atom.NewKeyword("predicate"), testCallable{}), expectedKind: "predicate"},
		{name: "and", schema: coll.NewVector(atom.NewKeyword("and"), atom.NewKeyword("number"), atom.NewKeyword("int")), expectedKind: "and"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sch, err := Normalize(tt.schema)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Generate(sch, GenerateOptions{Seed: 9, Size: 2})
			if err == nil {
				t.Fatalf("Generate(%s) = nil error, want unsupported generation failure", tt.name)
			}

			var genErr *GenerateError
			if !errors.As(err, &genErr) {
				t.Fatalf("Generate(%s) returned %T, want *GenerateError", tt.name, err)
			}
			data := genErr.ExceptionData()
			if data == nil {
				t.Fatalf("Generate(%s) ExceptionData() = nil", tt.name)
			}
			if got, want := data.Type, "SchemaGenerateError"; got != want {
				t.Fatalf("Generate(%s) exception type = %q, want %q", tt.name, got, want)
			}
			if got, want := data.Kind, "schema generation error"; got != want {
				t.Fatalf("Generate(%s) exception kind = %q, want %q", tt.name, got, want)
			}
			if got, want := data.Phase, "runtime"; got != want {
				t.Fatalf("Generate(%s) exception phase = %q, want %q", tt.name, got, want)
			}
			if got, want := data.Data["schema_kind"], tt.expectedKind; got != want {
				t.Fatalf("Generate(%s) schema_kind = %#v, want %#v", tt.name, got, want)
			}
		})
	}
}

func TestGeneratePredicateOverrideSucceeds(t *testing.T) {
	sch, err := Normalize(coll.NewVector(atom.NewKeyword("predicate"), testCallable{}))
	if err != nil {
		t.Fatal(err)
	}

	opts := GenerateOptions{
		Seed: 11,
		Size: 2,
		GeneratorOverrides: map[Kind]any{
			KindPredicate: GeneratorFunc(func(_ *Schema, rng *rand.Rand, _ GenerateOptions) (types.Value, error) {
				return atom.NewNumberInt(rng.Int63n(10)), nil
			}),
		},
	}

	value, err := Generate(sch, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !Validate(sch, value) {
		t.Fatalf("generated predicate override value %s does not validate", value)
	}
}

func TestGenerateMalformedOverrideConfigReturnsCanonicalError(t *testing.T) {
	sch, err := Normalize(coll.NewVector(atom.NewKeyword("predicate"), testCallable{}))
	if err != nil {
		t.Fatal(err)
	}

	_, err = Generate(sch, GenerateOptions{
		GeneratorOverrides: map[Kind]any{
			KindPredicate: "not-a-generator",
		},
	})
	if err == nil {
		t.Fatal("Generate(predicate, malformed override) = nil error, want failure")
	}

	var genErr *GenerateError
	if !errors.As(err, &genErr) {
		t.Fatalf("Generate(predicate, malformed override) returned %T, want *GenerateError", err)
	}
	data := genErr.ExceptionData()
	if data == nil {
		t.Fatal("GenerateError.ExceptionData() = nil")
	}
	if got, want := data.Type, "SchemaGenerateError"; got != want {
		t.Fatalf("ExceptionData type = %q, want %q", got, want)
	}
	if got := data.Data["option_path"]; got == nil {
		t.Fatal("ExceptionData option_path = nil, want structured generator option path")
	}
	if got := data.Data["option_value"]; got != "not-a-generator" {
		t.Fatalf("ExceptionData option_value = %#v, want %#v", got, "not-a-generator")
	}
}

func TestGenerateOverrideResultMustValidate(t *testing.T) {
	sch, err := Normalize(coll.NewVector(atom.NewKeyword("and"), atom.NewKeyword("number"), atom.NewKeyword("int")))
	if err != nil {
		t.Fatal(err)
	}

	_, err = Generate(sch, GenerateOptions{
		GeneratorOverrides: map[Kind]any{
			KindAnd: atom.NewString("not-an-int"),
		},
	})
	if err == nil {
		t.Fatal("Generate(and, invalid override) = nil error, want failure")
	}

	var genErr *GenerateError
	if !errors.As(err, &genErr) {
		t.Fatalf("Generate(and, invalid override) returned %T, want *GenerateError", err)
	}
	if got, want := genErr.Reason, "invalid generator override result"; got != want {
		t.Fatalf("GenerateError reason = %q, want %q", got, want)
	}
}

func TestGenerateSetAndMapOfPreferUniqueEntries(t *testing.T) {
	t.Run("set-keyword", func(t *testing.T) {
		sch, err := Normalize(coll.NewVector(atom.NewKeyword("set"), atom.NewKeyword("keyword")))
		if err != nil {
			t.Fatal(err)
		}
		value, err := Generate(sch, GenerateOptions{Seed: 21, Size: 4})
		if err != nil {
			t.Fatal(err)
		}
		setValue, ok := value.(*coll.Set)
		if !ok {
			t.Fatalf("Generate(set) returned %T, want *coll.Set", value)
		}
		if got, want := setValue.Count(), 4; got != want {
			t.Fatalf("set count = %d, want %d", got, want)
		}
	})

	t.Run("set-bool-best-effort", func(t *testing.T) {
		sch, err := Normalize(coll.NewVector(atom.NewKeyword("set"), atom.NewKeyword("bool")))
		if err != nil {
			t.Fatal(err)
		}
		value, err := Generate(sch, GenerateOptions{Seed: 1, Size: 4})
		if err != nil {
			t.Fatal(err)
		}
		setValue := value.(*coll.Set)
		if got := setValue.Count(); got < 1 || got > 2 {
			t.Fatalf("bool set count = %d, want best-effort count within domain", got)
		}
	})

	t.Run("map-of-keyword", func(t *testing.T) {
		sch, err := Normalize(coll.NewVector(atom.NewKeyword("map-of"), atom.NewKeyword("keyword"), atom.NewKeyword("int")))
		if err != nil {
			t.Fatal(err)
		}
		value, err := Generate(sch, GenerateOptions{Seed: 33, Size: 4})
		if err != nil {
			t.Fatal(err)
		}
		mapValue, ok := value.(*coll.Map)
		if !ok {
			t.Fatalf("Generate(map-of) returned %T, want *coll.Map", value)
		}
		if got, want := mapValue.Count(), 4; got != want {
			t.Fatalf("map-of count = %d, want %d", got, want)
		}
	})

	t.Run("map-of-bool-best-effort", func(t *testing.T) {
		sch, err := Normalize(coll.NewVector(atom.NewKeyword("map-of"), atom.NewKeyword("bool"), atom.NewKeyword("int")))
		if err != nil {
			t.Fatal(err)
		}
		value, err := Generate(sch, GenerateOptions{Seed: 2, Size: 4})
		if err != nil {
			t.Fatal(err)
		}
		mapValue := value.(*coll.Map)
		if got := mapValue.Count(); got < 1 || got > 2 {
			t.Fatalf("bool map-of count = %d, want best-effort count within domain", got)
		}
	})
}

func TestSampleReturnsRequestedCount(t *testing.T) {
	sch, err := Normalize(coll.NewVector(atom.NewKeyword("enum"), atom.NewKeyword("dev"), atom.NewKeyword("prod")))
	if err != nil {
		t.Fatal(err)
	}

	values, err := Sample(sch, 3, GenerateOptions{Seed: 5, Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(values), 3; got != want {
		t.Fatalf("Sample count = %d, want %d", got, want)
	}
	for i, value := range values {
		if !Validate(sch, value) {
			t.Fatalf("Sample value %d (%s) does not validate", i, value)
		}
	}
}
