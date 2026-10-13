package enginevm

import (
	"context"
	"fmt"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/vm"
)

type profilerSnapshotFn struct {
	want *vm.Profiler
}

func (f *profilerSnapshotFn) String() string                { return "<profiler-snapshot-fn>" }
func (f *profilerSnapshotFn) Type() string                  { return "fn" }
func (f *profilerSnapshotFn) Hash() uint32                  { return 0 }
func (f *profilerSnapshotFn) Equals(other types.Value) bool { return other == f }
func (f *profilerSnapshotFn) IsSelfEvaluating() bool        { return false }

func (f *profilerSnapshotFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	currentVM := vm.GetVM(ctx)
	if currentVM == nil {
		return nil, fmt.Errorf("vm not found in context")
	}
	if currentVM.GetProfiler() != f.want {
		return nil, fmt.Errorf("expected profiler snapshot %p, got %p", f.want, currentVM.GetProfiler())
	}
	return atom.NewNumberInt(1), nil
}

func TestExactCallableRuntimeAdapterCapturesRunnerProfilerSnapshot(t *testing.T) {
	runner := vm.MustNewRunner()
	original := vm.NewProfiler()
	runner.SetProfiler(original)

	runtime := newExactCallableRuntimeAdapter(runner)

	runner.SetProfiler(vm.NewProfiler())

	if _, err := runtime.Apply(context.Background(), &profilerSnapshotFn{want: original}, nil); err != nil {
		t.Fatalf("callable runtime should keep the captured profiler: %v", err)
	}
}
