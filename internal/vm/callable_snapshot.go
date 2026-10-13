package vm

import (
	"context"
	"fmt"

	"doggo/internal/compiler"
	"doggo/internal/types"
)

// ExactCallableSnapshot captures the exact callable execution environment
// needed to execute closures, builtins, and Fn values after the source VM has
// been returned to the pool.
type ExactCallableSnapshot struct {
	runtime exactCallableRuntime
	err     error
}

type exactCallableSnapshotRuntime struct {
	snapshot  ExactCallableSnapshot
	bindingVM *VM
}

type runnerExactCallableRuntime struct {
	runner    *Runner
	bindingVM *VM
}

func (r *Runner) SnapshotExactCallableRuntime() ExactCallableSnapshot {
	exec := r.executionServices()
	if exec == nil {
		return ExactCallableSnapshot{err: fmt.Errorf("runner callable runtime is not initialized")}
	}
	return ExactCallableSnapshot{runtime: exec.exactCallableRuntime()}
}

func newExactCallableSnapshotRuntime(env vmExecutionEnv) *exactCallableSnapshotRuntime {
	runtime := &exactCallableSnapshotRuntime{}
	env.callableRuntime = runtime
	runtime.snapshot = ExactCallableSnapshot{runtime: env.exactCallableRuntime()}
	return runtime
}

func newRunnerExactCallableRuntime(r *Runner) CallableRuntime {
	return runnerExactCallableRuntime{runner: r}
}

func (runtime *exactCallableSnapshotRuntime) Bind(vm *VM) CallableRuntime {
	if runtime == nil {
		return runtime
	}
	clone := *runtime
	clone.bindingVM = vm
	return &clone
}

func (runtime *exactCallableSnapshotRuntime) Apply(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	return runtime.snapshot.Apply(ctx, runtime.bindingVM, fn, args)
}

func (runtime *exactCallableSnapshotRuntime) ApplyFast(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	return runtime.snapshot.ApplyFast(ctx, runtime.bindingVM, fn, args)
}

func (runtime *exactCallableSnapshotRuntime) InvokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error) {
	return runtime.snapshot.InvokeClosure(ctx, cl, args)
}

func (runtime *exactCallableSnapshotRuntime) SnapshotCallableRuntime() CallableRuntime {
	if runtime == nil {
		return runtime
	}
	clone := *runtime
	clone.bindingVM = nil
	return &clone
}

func (runtime runnerExactCallableRuntime) Bind(vm *VM) CallableRuntime {
	runtime.bindingVM = vm
	return runtime
}

func (runtime runnerExactCallableRuntime) SnapshotCallableRuntime() CallableRuntime {
	if runtime.runner == nil {
		return &exactCallableSnapshotRuntime{
			snapshot: ExactCallableSnapshot{err: fmt.Errorf("runner callable runtime is not initialized")},
		}
	}
	return &exactCallableSnapshotRuntime{snapshot: runtime.runner.SnapshotExactCallableRuntime()}
}

func (runtime runnerExactCallableRuntime) Apply(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	if runtime.runner == nil {
		return nil, fmt.Errorf("runner callable runtime is not initialized")
	}
	return runtime.runner.SnapshotExactCallableRuntime().Apply(ctx, runtime.bindingVM, fn, args)
}

func (runtime runnerExactCallableRuntime) ApplyFast(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	if runtime.runner == nil {
		return nil, fmt.Errorf("runner callable runtime is not initialized")
	}
	return runtime.runner.SnapshotExactCallableRuntime().ApplyFast(ctx, runtime.bindingVM, fn, args)
}

func (runtime runnerExactCallableRuntime) InvokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error) {
	if runtime.runner == nil {
		return nil, fmt.Errorf("runner callable runtime is not initialized")
	}
	return runtime.runner.SnapshotExactCallableRuntime().InvokeClosure(ctx, cl, args)
}

func (vm *VM) installCallableRuntime(runtime CallableRuntime) {
	vm.callableRuntime = bindCallableRuntime(runtime, vm)
}

func (vm *VM) SetCallableRuntime(runtime CallableRuntime) {
	vm.installCallableRuntime(runtime)
}

func (vm *VM) snapshotCallableRuntime() CallableRuntime {
	if vm.callableRuntime == nil {
		return nil
	}
	if snapshotter, ok := vm.callableRuntime.(interface{ SnapshotCallableRuntime() CallableRuntime }); ok {
		return bindCallableRuntime(snapshotter.SnapshotCallableRuntime(), nil)
	}
	return bindCallableRuntime(vm.callableRuntime, nil)
}

func requireCallableRuntimeSnapshot(vm *VM, owner string) (CallableRuntime, error) {
	runtime := vm.snapshotCallableRuntime()
	if runtime == nil {
		return nil, fmt.Errorf("%s: callable runtime not initialized", owner)
	}
	return runtime, nil
}

func (r *Runner) SetCallableRuntime(runtime CallableRuntime) {
	if r == nil {
		return
	}
	r.getOrCreateExecutionSession().setCallableRuntime(runtime)
}

func (snapshot ExactCallableSnapshot) Apply(ctx context.Context, bindingVM *VM, fn types.Value, args []types.Value) (types.Value, error) {
	if snapshot.err != nil {
		return nil, snapshot.err
	}
	return snapshot.runtime.applyCallableWithBinding(ctx, bindingVM, fn, args)
}

func (snapshot ExactCallableSnapshot) ApplyFast(ctx context.Context, bindingVM *VM, fn types.Value, args []types.Value) (types.Value, error) {
	if snapshot.err != nil {
		return nil, snapshot.err
	}
	return snapshot.runtime.applyCallableFastWithBinding(ctx, bindingVM, fn, args)
}

func (snapshot ExactCallableSnapshot) InvokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error) {
	if snapshot.err != nil {
		return nil, snapshot.err
	}
	return snapshot.runtime.applyCallableWithBinding(ctx, nil, cl, args)
}
