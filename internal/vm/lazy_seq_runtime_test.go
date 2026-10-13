package vm

import (
	"testing"

	"doggo/internal/compiler"
)

func TestRunnerVMExecutionServicesNewLazySeqInvokesThunk(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`(fn [] [1 2 3])`)
	if err != nil {
		t.Fatalf("build thunk: %v", err)
	}
	thunk, ok := value.(*compiler.Closure)
	if !ok {
		t.Fatalf("expected closure, got %T", value)
	}

	lazySeq := runner.NewLazySeq(thunk)
	if lazySeq == nil {
		t.Fatal("expected lazy seq")
	}
	if got := lazySeq.String(); got != "(1 2 3)" {
		t.Fatalf("expected (1 2 3), got %s", got)
	}
}
