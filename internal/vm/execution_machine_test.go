package vm

import (
	"context"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func TestExactCallableRuntimeNestedClosureBuiltinRegression(t *testing.T) {
	runner := MustNewRunner()

	fn, err := runner.Run(`(fn [x] ((fn [y] (+ y 2)) x))`)
	if err != nil {
		t.Fatalf("build closure: %v", err)
	}

	result, err := runner.SnapshotExactCallableRuntime().Apply(
		context.Background(),
		nil,
		fn,
		[]types.Value{atom.NewNumberInt(40)},
	)
	if err != nil {
		t.Fatalf("snapshot Apply failed: %v", err)
	}
	if got := result.String(); got != "42" {
		t.Fatalf("snapshot Apply result = %s, want 42", got)
	}
}
