package vm

import (
	"context"
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/opcode"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

type noopCallableRuntime struct{}

func (noopCallableRuntime) Bind(*VM) CallableRuntime { return noopCallableRuntime{} }

func (noopCallableRuntime) Apply(context.Context, types.Value, []types.Value) (types.Value, error) {
	return atom.NilValue, nil
}

func (noopCallableRuntime) ApplyFast(context.Context, types.Value, []types.Value) (types.Value, error) {
	return atom.NilValue, nil
}

func (noopCallableRuntime) InvokeClosure(context.Context, *compiler.Closure, []types.Value) (types.Value, error) {
	return atom.NilValue, nil
}

type recordingCallableRuntime struct {
	bound *VM
}

func (runtime recordingCallableRuntime) Bind(vm *VM) CallableRuntime {
	runtime.bound = vm
	return runtime
}

func (recordingCallableRuntime) Apply(context.Context, types.Value, []types.Value) (types.Value, error) {
	return atom.NilValue, nil
}

func (recordingCallableRuntime) ApplyFast(context.Context, types.Value, []types.Value) (types.Value, error) {
	return atom.NilValue, nil
}

func (recordingCallableRuntime) InvokeClosure(context.Context, *compiler.Closure, []types.Value) (types.Value, error) {
	return atom.NilValue, nil
}

func TestVMExecutionEnvAcquireVMCopiesAmbientRuntimeState(t *testing.T) {
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
	runner.runtimeServices.sources.register("<env>", "(+ 1 2)")

	machine, err := runner.runtimeServices.execution.newMachine(
		&compiler.Bytecode{SourceFile: "<env>", Constants: []types.Value{atom.NewNumberInt(1)}},
		context.Background(),
		false,
	)
	if err != nil {
		t.Fatalf("newMachine: %v", err)
	}
	if machine.session != runner.getOrCreateExecutionSession() {
		t.Fatal("machine session should be shared with the runner")
	}
	machine.installCallableRuntime(machine.snapshotCallableRuntime())

	err = machine.snapshotExecutionEnv().exactCallableRuntime().withBorrowedMachine(context.Background(), func(child *VM) error {
		if child.constants == nil {
			t.Fatal("child constants should be copied")
		}
		if child.globals == nil {
			t.Fatal("child globals should be copied")
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
		if child.callableRuntime == nil {
			t.Fatal("child callable runtime should be copied")
		}
		if child.session != machine.session {
			t.Fatal("child session should be copied")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withBorrowedMachine failed: %v", err)
	}
}

func TestVMSnapshotCallableRuntimeReturnsUnboundRuntime(t *testing.T) {
	vm := &VM{
		stack:  make([]types.Value, StackSize),
		frames: make([]*Frame, MaxFrames),
	}
	vm.SetCallableRuntime(recordingCallableRuntime{})

	bound, ok := vm.callableRuntime.(recordingCallableRuntime)
	if !ok {
		t.Fatalf("expected recording callable runtime, got %T", vm.callableRuntime)
	}
	if bound.bound != vm {
		t.Fatal("callableRuntime should bind the runtime to the current VM")
	}

	snapshot, ok := vm.snapshotCallableRuntime().(recordingCallableRuntime)
	if !ok {
		t.Fatalf("expected recording callable runtime snapshot, got %T", vm.snapshotCallableRuntime())
	}
	if snapshot.bound != nil {
		t.Fatal("snapshotCallableRuntime should return an unbound runtime")
	}
}

func TestVMSnapshotCallableRuntimeAppliesThroughBorrowedMachine(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	machine, err := runner.runtimeServices.execution.newMachine(
		&compiler.Bytecode{SourceFile: "<snapshot>", Constants: []types.Value{atom.NewNumberInt(41)}},
		context.Background(),
		false,
	)
	if err != nil {
		t.Fatalf("newMachine: %v", err)
	}

	closure := &compiler.Closure{
		Fn: &compiler.CompiledFunction{
			Instructions: []byte{
				byte(opcode.OpConst), 0, 0,
				byte(opcode.OpReturnValue),
			},
			NumLocals:     0,
			NumParameters: 0,
		},
	}

	snapshot := machine.snapshotCallableRuntime()
	if snapshot == nil {
		t.Fatal("snapshot callable runtime should be available")
	}

	value, err := snapshot.Apply(context.Background(), closure, nil)
	if err != nil {
		t.Fatalf("snapshot Apply failed: %v", err)
	}
	if got := value.String(); got != "41" {
		t.Fatalf("snapshot Apply result = %s, want 41", got)
	}
}

func TestVMRuntimeSettersInvalidateCallableRuntime(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*VM)
	}{
		{name: "context", mutate: func(vm *VM) { vm.SetContext(context.Background()) }},
		{name: "import callback", mutate: func(vm *VM) {
			vm.SetImportCallback(func(string) (map[string]types.Value, error) { return map[string]types.Value{}, nil })
		}},
		{name: "source line provider", mutate: func(vm *VM) { vm.SetSourceLineProvider(func(string, int) (string, bool) { return "", false }) }},
		{name: "profiler", mutate: func(vm *VM) { vm.SetProfiler(NewProfiler()) }},
		{name: "observability", mutate: func(vm *VM) { vm.SetObservability(NewObservabilityState()) }},
		{name: "stm store", mutate: func(vm *VM) { vm.SetSTMStore(nil) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := &VM{
				stack:  make([]types.Value, StackSize),
				frames: make([]*Frame, MaxFrames),
			}
			vm.installCallableRuntime(noopCallableRuntime{})
			if vm.callableRuntime == nil {
				t.Fatal("expected callable runtime to be installed")
			}

			tt.mutate(vm)

			if vm.callableRuntime != nil {
				t.Fatal("expected callable runtime to be invalidated")
			}
		})
	}
}

func TestVMResetForPoolClearsState(t *testing.T) {
	vm := &VM{
		stack:      make([]types.Value, StackSize),
		frames:     make([]*Frame, MaxFrames),
		sp:         1,
		frameIndex: 1,
		constants:  []types.Value{atom.NewNumberInt(2)},
		globals:    []types.Value{atom.NewNumberInt(3)},
	}
	vm.stack[0] = atom.NewNumberInt(1)
	vm.frames[0] = &Frame{}
	vm.installCallableRuntime(noopCallableRuntime{})

	vm.resetForPool()

	if vm.sp != 0 || vm.frameIndex != 0 {
		t.Fatalf("execution positions were not reset: sp=%d frameIndex=%d", vm.sp, vm.frameIndex)
	}
	if vm.stack[0] != nil || vm.frames[0] != nil {
		t.Fatal("stack or frame references were retained")
	}
	if vm.constants != nil || vm.globals != nil || vm.callableRuntime != nil {
		t.Fatal("shared execution state was retained")
	}
}

func TestVMResetClearsCallableRuntime(t *testing.T) {
	vm, err := New(&compiler.Bytecode{Constants: []types.Value{atom.NewNumberInt(1)}})
	if err != nil {
		t.Fatalf("new vm: %v", err)
	}
	vm.installCallableRuntime(noopCallableRuntime{})
	if vm.callableRuntime == nil {
		t.Fatal("expected callable runtime to be installed")
	}

	if err := vm.Reset(&compiler.Bytecode{Constants: []types.Value{atom.NewNumberInt(2)}}); err != nil {
		t.Fatalf("reset vm: %v", err)
	}
	if vm.callableRuntime != nil {
		t.Fatal("expected callable runtime to be cleared on reset")
	}
}
