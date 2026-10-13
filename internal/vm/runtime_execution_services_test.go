package vm

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/opcode"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestRunnerSourceRegistryServicesRegisterAndLookupContent(t *testing.T) {
	projectRoot := t.TempDir()
	projectFile := filepath.Join(projectRoot, "app", "sample.dog")
	if err := os.MkdirAll(filepath.Dir(projectFile), 0755); err != nil {
		t.Fatalf("mkdir project file parent: %v", err)
	}
	if err := os.WriteFile(projectFile, []byte("(+ 1 2)\n42\n"), 0644); err != nil {
		t.Fatalf("write project file: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	runner.runtimeServices.sources.register("<memory>", "alpha\nbeta")

	line, ok := runner.runtimeServices.sources.lookupLine("<memory>", 2)
	if !ok || line != "beta" {
		t.Fatalf("lookupLine(<memory>, 2) = %q, %v", line, ok)
	}

	content, ok := runner.runtimeServices.sources.lookupContent("<memory>")
	if !ok || content != "alpha\nbeta" {
		t.Fatalf("lookupContent(<memory>) = %q, %v", content, ok)
	}

	content, ok = runner.runtimeServices.sources.lookupContent("app/sample.dog")
	if !ok || content != "(+ 1 2)\n42\n" {
		t.Fatalf("lookupContent(project relative) = %q, %v", content, ok)
	}
}

func TestRunnerVMExecutionServicesNewMachineAppliesRuntimeState(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	session := runner.getOrCreateExecutionSession()
	sessionCallableRuntimeBefore := session.callableRuntime

	profiler := NewProfiler()
	profiler.Enable()
	runner.SetProfiler(profiler)
	runner.runtimeServices.sources.register("<vm-service>", "(+ 1 2)")

	machine, err := runner.runtimeServices.execution.newMachine(
		&compiler.Bytecode{SourceFile: "<vm-service>", Constants: []types.Value{atom.NewNumberInt(1)}},
		context.Background(),
		true,
	)
	if err != nil {
		t.Fatalf("newMachine: %v", err)
	}

	if machine.ctx == nil {
		t.Fatal("machine context should be set")
	}
	if machine.stmStore != session.stmStore {
		t.Fatal("machine STM store mismatch")
	}
	if machine.profiler != profiler {
		t.Fatal("machine profiler mismatch")
	}
	if machine.observability != session.getOrCreateObservabilityState() {
		t.Fatal("machine observability mismatch")
	}
	if machine.session != session {
		t.Fatal("machine session mismatch")
	}
	if session.callableRuntime != sessionCallableRuntimeBefore {
		t.Fatal("new machine should not take ownership of session callable runtime")
	}
	if !machine.collectStats {
		t.Fatal("machine should collect stats")
	}
	if machine.sourceLineProvider == nil {
		t.Fatal("machine sourceLineProvider should be set")
	}
	if line, ok := machine.sourceLineProvider("<vm-service>", 1); !ok || line != "(+ 1 2)" {
		t.Fatalf("machine source line = %q, %v", line, ok)
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
		t.Fatal("machine callable runtime snapshot should be available")
	}
	runtime, ok := snapshot.(*exactCallableSnapshotRuntime)
	if !ok {
		t.Fatalf("machine callable runtime snapshot = %T, want *exactCallableSnapshotRuntime", snapshot)
	}
	value, err := runtime.Apply(context.Background(), closure, nil)
	if err != nil {
		t.Fatalf("snapshot runtime Apply failed: %v", err)
	}
	if got := value.String(); got != "1" {
		t.Fatalf("snapshot runtime Apply result = %s, want 1", got)
	}
	value, err = machine.callableRuntime.Apply(context.Background(), closure, nil)
	if err != nil {
		t.Fatalf("machine callable runtime Apply failed: %v", err)
	}
	if got := value.String(); got != "1" {
		t.Fatalf("machine callable runtime Apply result = %s, want 1", got)
	}
}

func TestRunnerVMExecutionServicesBoundCallableRuntimeUsesMachineConstants(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	machine, err := runner.runtimeServices.execution.newMachine(
		&compiler.Bytecode{SourceFile: "<vm-service>", Constants: []types.Value{atom.NewNumberInt(41)}},
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

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("bound callable runtime should not panic on machine-only constants: %v", recovered)
		}
	}()

	value, err := machine.callableRuntime.InvokeClosure(context.Background(), closure, nil)
	if err != nil {
		t.Fatalf("InvokeClosure failed: %v", err)
	}
	if got := value.String(); got != "41" {
		t.Fatalf("InvokeClosure result = %s, want 41", got)
	}
}

func TestRunnerVMExecutionServicesMacroExecutorUsesCurrentConstantStore(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	session := runner.getOrCreateExecutionSession()

	constIndex := session.constantStore.Add(atom.NewNumberInt(41))
	if constIndex != 0 {
		t.Fatalf("expected first constant index 0, got %d", constIndex)
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

	result, err := runner.runtimeServices.execution.macroExecutor().InvokeClosure(context.Background(), closure, nil)
	if err != nil {
		t.Fatalf("macro executor failed: %v", err)
	}
	if got := result.String(); got != "41" {
		t.Fatalf("expected 41, got %s", got)
	}
}

func TestRunnerVMExecutionServicesCallableRuntimeUsesCurrentConstantStore(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	session := runner.getOrCreateExecutionSession()

	constIndex := session.constantStore.Add(atom.NewNumberInt(41))
	if constIndex != 0 {
		t.Fatalf("expected first constant index 0, got %d", constIndex)
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

	result, err := runner.SnapshotExactCallableRuntime().InvokeClosure(context.Background(), closure, nil)
	if err != nil {
		t.Fatalf("runtime callable execution failed: %v", err)
	}
	if got := result.String(); got != "41" {
		t.Fatalf("expected 41, got %s", got)
	}
}

func TestRunnerVMExecutionServicesMacroExecutorDoesNotShareRuntimeProfiler(t *testing.T) {
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

	session := runner.getOrCreateExecutionSession()
	constIndex := session.constantStore.Add(atom.NewNumberInt(7))
	if constIndex != 0 {
		t.Fatalf("expected first constant index 0, got %d", constIndex)
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

	if _, err := runner.runtimeServices.execution.macroExecutor().InvokeClosure(context.Background(), closure, nil); err != nil {
		t.Fatalf("macro executor failed: %v", err)
	}

	if got := profiler.Snapshot(); len(got) != 0 {
		t.Fatalf("compile-time macro execution should not record runtime profiler data: %#v", got)
	}
}

func TestRunnerVMExecutionServicesMacroExecutorDoesNotShareRuntimeObservability(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	var out bytes.Buffer
	runner.SetLogOutput(&out)

	value, err := runner.Run(`
(do
  (defmacro bump []
    (do
      (doggo.observability/log! :info "compile-log")
      (doggo.observability/metric-inc! "macro.calls")
      1))
  (bump)
  (doggo.observability/metrics-snapshot))
`)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if out.Len() != 0 {
		t.Fatalf("compile-time log should not reach runtime log sink: %q", out.String())
	}

	snapshot := mustMap(t, value)
	counters := mustMap(t, snapshot.Get(atom.NewKeyword("counters")))
	if counter := counters.Get(atom.NewString("macro.calls")); counter != nil {
		t.Fatalf("compile-time metric leaked into runtime snapshot: %s", counter.String())
	}
}

func TestRunnerVMExecutionServicesMacroExecutorDoesNotShareCompileTimeObservabilityBetweenInvocations(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	value, err := runner.Run(`
(do
  (defmacro bump []
    (do
      (doggo.observability/metric-inc! "macro.calls")
      1))
  (defmacro snap []
    (get (:counters (doggo.observability/metrics-snapshot)) "macro.calls"))
  [(bump) (snap)])
`)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	vector, ok := value.(*coll.Vector)
	if !ok {
		t.Fatalf("expected vector, got %T", value)
	}
	if got := vector.Items[0].String(); got != "1" {
		t.Fatalf("first macro expansion = %s, want 1", got)
	}
	if vector.Items[1] != atom.NilValue {
		t.Fatalf("compile-time observability leaked across macro invocations: %s", vector.Items[1].String())
	}
}

func TestRunnerVMExecutionServicesApplySupportsClosure(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	fn, err := runner.Run(`(fn [x] (+ x 2))`)
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
		t.Fatalf("Apply failed: %v", err)
	}
	if got := result.String(); got != "42" {
		t.Fatalf("expected 42, got %s", got)
	}
}

func TestRunnerVMExecutionServicesMachineRuntimeInvokesClosure(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	fn, err := runner.Run(`(fn [] 42)`)
	if err != nil {
		t.Fatalf("build closure: %v", err)
	}
	closure, ok := fn.(*compiler.Closure)
	if !ok {
		t.Fatalf("expected closure, got %T", fn)
	}

	result, err := runner.runtimeServices.execution.exactCallableRuntime().invokeClosure(
		context.Background(),
		closure,
		nil,
	)
	if err != nil {
		t.Fatalf("invokeClosure failed: %v", err)
	}
	if got := result.String(); got != "42" {
		t.Fatalf("expected 42, got %s", got)
	}
}

func TestRunnerVMExecutionServicesApplySupportsBuiltin(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	countBuiltin, ok := runner.builtinCatalog.Lookup("count")
	if !ok {
		t.Fatal("count builtin not found")
	}

	result, err := runner.SnapshotExactCallableRuntime().Apply(
		context.Background(),
		nil,
		countBuiltin,
		[]types.Value{coll.NewVector(atom.NewNumberInt(1), atom.NewNumberInt(2), atom.NewNumberInt(3))},
	)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if got := result.String(); got != "3" {
		t.Fatalf("expected 3, got %s", got)
	}
}

func TestRunnerVMExecutionServicesApplySupportsMultimethod(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	mm, err := runner.Run(`
(do
  (defmulti classify identity)
  (defmethod classify :ok [_] :done)
  classify)
`)
	if err != nil {
		t.Fatalf("build multimethod: %v", err)
	}

	result, err := runner.SnapshotExactCallableRuntime().Apply(
		context.Background(),
		nil,
		mm,
		[]types.Value{atom.NewKeyword("ok")},
	)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if got := result.String(); got != ":done" {
		t.Fatalf("expected :done, got %s", got)
	}
}

func TestRunnerVMExecutionServicesApplyFastSupportsDirectAndFallbackPaths(t *testing.T) {
	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{},
		BootstrapCore: false,
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	countBuiltin, ok := runner.builtinCatalog.Lookup("count")
	if !ok {
		t.Fatal("count builtin not found")
	}

	directResult, err := runner.SnapshotExactCallableRuntime().ApplyFast(
		context.Background(),
		nil,
		countBuiltin,
		[]types.Value{coll.NewVector(atom.NewNumberInt(1), atom.NewNumberInt(2))},
	)
	if err != nil {
		t.Fatalf("ApplyFast builtin failed: %v", err)
	}
	if got := directResult.String(); got != "2" {
		t.Fatalf("expected 2, got %s", got)
	}

	fn, err := runner.Run(`(fn [x] (+ x 5))`)
	if err != nil {
		t.Fatalf("build closure: %v", err)
	}

	fallbackResult, err := runner.SnapshotExactCallableRuntime().ApplyFast(
		context.Background(),
		nil,
		fn,
		[]types.Value{atom.NewNumberInt(37)},
	)
	if err != nil {
		t.Fatalf("ApplyFast closure failed: %v", err)
	}
	if got := fallbackResult.String(); got != "42" {
		t.Fatalf("expected 42, got %s", got)
	}
}

func TestRunnerVMExecutionServicesBoundApplyUsesVMContextForTypesFn(t *testing.T) {
	projectRoot := t.TempDir()
	sharedPath := filepath.Join(projectRoot, "lib", "shared", "value.dog")
	if err := os.MkdirAll(filepath.Dir(sharedPath), 0755); err != nil {
		t.Fatalf("mkdir shared dir: %v", err)
	}
	if err := os.WriteFile(sharedPath, []byte("42\n"), 0644); err != nil {
		t.Fatalf("write shared file: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: false})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	fn := &loadFileFn{namespaces: runner.runtimeNamespaces()}
	vm, err := New(&compiler.Bytecode{SourceFile: "lib/loader.dog"})
	if err != nil {
		t.Fatalf("new vm: %v", err)
	}

	value, err := runner.SnapshotExactCallableRuntime().Apply(
		context.Background(),
		vm,
		fn,
		[]types.Value{atom.NewString("shared/value.dog")},
	)
	if err != nil {
		t.Fatalf("bound Apply failed: %v", err)
	}
	if got := value.String(); got != "42" {
		t.Fatalf("unexpected loaded value: %s", got)
	}
}

func TestRunnerVMExecutionServicesBoundApplyFastUsesVMContextForTypesFn(t *testing.T) {
	projectRoot := t.TempDir()
	sharedPath := filepath.Join(projectRoot, "lib", "shared", "value.dog")
	if err := os.MkdirAll(filepath.Dir(sharedPath), 0755); err != nil {
		t.Fatalf("mkdir shared dir: %v", err)
	}
	if err := os.WriteFile(sharedPath, []byte("42\n"), 0644); err != nil {
		t.Fatalf("write shared file: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: false})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	fn := &loadFileFn{namespaces: runner.runtimeNamespaces()}
	vm, err := New(&compiler.Bytecode{SourceFile: "lib/loader.dog"})
	if err != nil {
		t.Fatalf("new vm: %v", err)
	}

	value, err := runner.SnapshotExactCallableRuntime().ApplyFast(
		context.Background(),
		vm,
		fn,
		[]types.Value{atom.NewString("shared/value.dog")},
	)
	if err != nil {
		t.Fatalf("bound ApplyFast failed: %v", err)
	}
	if got := value.String(); got != "42" {
		t.Fatalf("unexpected loaded value: %s", got)
	}
}
