package vm

import (
	"context"
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

type runtimeEnvContextKey struct{}

func TestVMSnapshotExecutionEnvRoundTripsVMState(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	profiler := NewProfiler()
	profiler.Enable()
	runner.SetProfiler(profiler)
	runner.runtimeServices.sources.register("<runtime-env>", "(+ 1 2)")

	machine, err := runner.runtimeServices.execution.newMachine(
		&compiler.Bytecode{SourceFile: "<runtime-env>", Constants: []types.Value{atom.NewNumberInt(1)}},
		context.Background(),
		false,
	)
	if err != nil {
		t.Fatalf("newMachine: %v", err)
	}

	sentinelImport := func(string) (map[string]types.Value, error) {
		return map[string]types.Value{"sentinel": atom.NewNumberInt(7)}, nil
	}
	machine.constants = []types.Value{atom.NewNumberInt(42)}
	machine.globals = []types.Value{atom.NewNumberInt(99)}
	machine.importCallback = sentinelImport

	snapshot := machine.snapshotExecutionEnv()
	child := &VM{}
	ctx := context.WithValue(context.Background(), runtimeEnvContextKey{}, "runtime-env")
	snapshot.applyToVM(child, ctx)

	if child.ctx != ctx {
		t.Fatal("child context should match apply context")
	}
	if len(child.constants) != 1 || child.constants[0].String() != "42" {
		t.Fatalf("child constants = %#v", child.constants)
	}
	if len(child.globals) != 1 || child.globals[0].String() != "99" {
		t.Fatalf("child globals = %#v", child.globals)
	}
	if child.builtinCatalog != machine.builtinCatalog {
		t.Fatal("child builtin catalog mismatch")
	}
	if len(child.builtins) != len(machine.builtins) {
		t.Fatalf("child builtins length = %d, want %d", len(child.builtins), len(machine.builtins))
	}
	if child.importCallback == nil {
		t.Fatal("child import callback should be copied")
	}
	if child.stmStore != machine.stmStore {
		t.Fatal("child STM store mismatch")
	}
	if child.profiler != machine.profiler {
		t.Fatal("child profiler mismatch")
	}
	if child.sourceLineProvider == nil {
		t.Fatal("child source line provider should be copied")
	}
	if child.observability != machine.observability {
		t.Fatal("child observability mismatch")
	}
}
