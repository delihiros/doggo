package schema

import (
	"context"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type failingPredicateCallable struct{}

func (failingPredicateCallable) Invoke(context.Context, ...types.Value) (types.Value, error) {
	return atom.False, nil
}

func (failingPredicateCallable) String() string                { return "#<failing-predicate>" }
func (failingPredicateCallable) Type() string                  { return "failing-predicate" }
func (failingPredicateCallable) Hash() uint32                  { return 2 }
func (failingPredicateCallable) Equals(other types.Value) bool { _, ok := other.(failingPredicateCallable); return ok }
func (failingPredicateCallable) IsSelfEvaluating() bool        { return true }

func TestExplainIncludesPathExpectedAndActual(t *testing.T) {
	schema, err := Normalize(coll.NewVector(
		atom.NewKeyword("map"),
		coll.NewVector(atom.NewKeyword("name"), atom.NewKeyword("string")),
		coll.NewVector(atom.NewKeyword("age"), atom.NewKeyword("int")),
	))
	if err != nil {
		t.Fatalf("Normalize(map schema) returned error: %v", err)
	}

	value := coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("name"), atom.NewNumberInt(42),
		atom.NewKeyword("age"), atom.NewString("old"),
	})

	explain := Explain(schema, value)
	if explain.Valid {
		t.Fatal("Explain(...) valid = true, want false")
	}
	if explain.Schema == nil {
		t.Fatal("Explain(...) schema is nil, want canonical schema")
	}
	if explain.Value == nil {
		t.Fatal("Explain(...) value is nil, want original value")
	}
	if got, want := len(explain.Problems), 1; got != want {
		t.Fatalf("Explain(...) problems = %d, want %d", got, want)
	}

	problem := explain.Problems[0]
	if got, want := problem.Path.String(), "[:name]"; got != want {
		t.Fatalf("problem path = %q, want %q", got, want)
	}
	if got, want := problem.Expected.String(), ":string"; got != want {
		t.Fatalf("problem expected = %q, want %q", got, want)
	}
	if got, want := problem.Actual.Type(), "number"; got != want {
		t.Fatalf("problem actual type = %q, want %q", got, want)
	}
}

func TestExplainTreatsNilMapEntryAsPresentValue(t *testing.T) {
	schema, err := Normalize(coll.NewVector(
		atom.NewKeyword("map"),
		coll.NewVector(atom.NewKeyword("age"), coll.NewVector(atom.NewKeyword("maybe"), atom.NewKeyword("int"))),
	))
	if err != nil {
		t.Fatalf("Normalize(map schema) returned error: %v", err)
	}

	value := coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("age"), nil,
	})

	explain := Explain(schema, value)
	if !explain.Valid {
		t.Fatalf("Explain(...) valid = false, want true with nil-valued maybe entry: %#v", explain.Problems)
	}
	if len(explain.Problems) != 0 {
		t.Fatalf("Explain(...) problems = %d, want 0", len(explain.Problems))
	}
}

func TestExplainOrIncludesBranchAccurateProblems(t *testing.T) {
	schema, err := Normalize(coll.NewVector(
		atom.NewKeyword("or"),
		atom.NewKeyword("string"),
		atom.NewKeyword("int"),
	))
	if err != nil {
		t.Fatalf("Normalize(or schema) returned error: %v", err)
	}

	explain := Explain(schema, atom.True)
	if explain.Valid {
		t.Fatal("Explain(or schema, true) valid = true, want false")
	}
	if got, want := len(explain.Problems), 2; got != want {
		t.Fatalf("Explain(or schema, true) problems = %d, want %d", got, want)
	}
	if explain.Problems[0].Branch == nil || *explain.Problems[0].Branch != 0 {
		t.Fatalf("first problem branch = %#v, want 0", explain.Problems[0].Branch)
	}
	if explain.Problems[1].Branch == nil || *explain.Problems[1].Branch != 1 {
		t.Fatalf("second problem branch = %#v, want 1", explain.Problems[1].Branch)
	}
}

func TestExplainPreservesNestedBranchMetadata(t *testing.T) {
	schema, err := Normalize(coll.NewVector(
		atom.NewKeyword("or"),
		coll.NewVector(atom.NewKeyword("or"), atom.NewKeyword("string"), atom.NewKeyword("int")),
		atom.NewKeyword("keyword"),
	))
	if err != nil {
		t.Fatalf("Normalize(nested or schema) returned error: %v", err)
	}

	explain := Explain(schema, atom.True)
	if explain.Valid {
		t.Fatal("Explain(nested or schema, true) valid = true, want false")
	}
	if got, want := len(explain.Problems), 3; got != want {
		t.Fatalf("Explain(nested or schema, true) problems = %d, want %d", got, want)
	}
	if explain.Problems[0].Branch == nil || *explain.Problems[0].Branch != 0 {
		t.Fatalf("first nested branch = %#v, want 0", explain.Problems[0].Branch)
	}
	if explain.Problems[1].Branch == nil || *explain.Problems[1].Branch != 1 {
		t.Fatalf("second nested branch = %#v, want 1", explain.Problems[1].Branch)
	}
	if explain.Problems[2].Branch == nil || *explain.Problems[2].Branch != 1 {
		t.Fatalf("outer keyword branch = %#v, want 1", explain.Problems[2].Branch)
	}
}

func TestExplainAdditionalFailureShapes(t *testing.T) {
	tests := []struct {
		name           string
		schema         types.Value
		value          types.Value
		wantProblems   int
		wantPredicate  bool
		wantAnyBranch  bool
		wantFirstPath  string
	}{
		{
			name:          "and",
			schema:        coll.NewVector(atom.NewKeyword("and"), atom.NewKeyword("number"), atom.NewKeyword("int")),
			value:         atom.NewString("x"),
			wantProblems:  1,
			wantAnyBranch: true,
		},
		{
			name:          "enum",
			schema:        coll.NewVector(atom.NewKeyword("enum"), atom.NewKeyword("dev"), atom.NewKeyword("prod")),
			value:         atom.NewKeyword("staging"),
			wantProblems:  1,
		},
		{
			name:          "map-of",
			schema:        coll.NewVector(atom.NewKeyword("map-of"), atom.NewKeyword("keyword"), atom.NewKeyword("int")),
			value:         coll.NewMapFromPairs([]types.Value{atom.NewKeyword("a"), atom.NewString("x")}),
			wantProblems:  1,
			wantFirstPath: "[:a]",
		},
		{
			name:          "predicate",
			schema:        coll.NewVector(atom.NewKeyword("predicate"), failingPredicateCallable{}),
			value:         atom.False,
			wantProblems:  1,
			wantPredicate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := Normalize(tt.schema)
			if err != nil {
				t.Fatalf("Normalize(%s) returned error: %v", tt.name, err)
			}

			explain := Explain(schema, tt.value)
			if explain.Valid {
				t.Fatalf("Explain(%s) valid = true, want false", tt.name)
			}
			if got, want := len(explain.Problems), tt.wantProblems; got != want {
				t.Fatalf("Explain(%s) problems = %d, want %d", tt.name, got, want)
			}
			if tt.wantPredicate && explain.Problems[0].Predicate == nil {
				t.Fatalf("Explain(%s) predicate = nil, want non-nil", tt.name)
			}
			if tt.wantAnyBranch && explain.Problems[0].Branch == nil {
				t.Fatalf("Explain(%s) branch = nil, want branch index", tt.name)
			}
			if tt.wantFirstPath != "" {
				if got := explain.Problems[0].Path.String(); got != tt.wantFirstPath {
					t.Fatalf("Explain(%s) first path = %q, want %q", tt.name, got, tt.wantFirstPath)
				}
			}
		})
	}
}
