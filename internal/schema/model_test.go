package schema

import (
	"context"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

type testCallable struct{}

func (testCallable) Invoke(context.Context, ...types.Value) (types.Value, error) {
	return atom.True, nil
}

func (testCallable) String() string                { return "#<test-callable>" }
func (testCallable) Type() string                  { return "test-callable" }
func (testCallable) Hash() uint32                  { return 1 }
func (testCallable) Equals(other types.Value) bool { _, ok := other.(testCallable); return ok }
func (testCallable) IsSelfEvaluating() bool        { return true }

func TestNormalizedModelExposesCanonicalKind(t *testing.T) {
	schema, err := Normalize(atom.NewKeyword("string"))
	if err != nil {
		t.Fatalf("Normalize(:string) returned error: %v", err)
	}
	if got, want := schema.Kind(), KindString; got != want {
		t.Fatalf("schema kind = %v, want %v", got, want)
	}
}
