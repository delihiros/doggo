package vm

import (
	"context"
	"fmt"

	"doggo/internal/compiler"
	"doggo/internal/types"
)

type exactCallableRuntime struct {
	env *vmExecutionEnv
}

type boundCallableRuntime struct {
	machines  exactCallableRuntime
	bindingVM *VM
}

type runnerExactLazySeqRuntime struct {
	runner *Runner
}

func (env vmExecutionEnv) exactCallableRuntime() exactCallableRuntime {
	runtime := exactCallableRuntime{env: &env}
	env.callableRuntime = runtime.callableRuntime()
	runtime.env = &env
	return runtime
}

func (runtime exactCallableRuntime) callableRuntime() boundCallableRuntime {
	return boundCallableRuntime{machines: runtime}
}

func (runtime boundCallableRuntime) Apply(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	return runtime.machines.applyCallableWithBinding(ctx, runtime.bindingVM, fn, args)
}

func (runtime boundCallableRuntime) Bind(vm *VM) CallableRuntime {
	runtime.bindingVM = vm
	return runtime
}

func (runtime boundCallableRuntime) ApplyFast(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	return runtime.machines.applyCallableFastWithBinding(ctx, runtime.bindingVM, fn, args)
}

func (runtime boundCallableRuntime) InvokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error) {
	return runtime.machines.applyCallableWithBinding(ctx, runtime.bindingVM, cl, args)
}

func (runtime exactCallableRuntime) applyCallableWithBinding(ctx context.Context, bindingVM *VM, fn types.Value, args []types.Value) (types.Value, error) {
	machine := runtime.newExecutionMachine()
	if machine == nil {
		return nil, fmt.Errorf("callable execution machine is not initialized")
	}
	return machine.applyCallableWithBinding(ctx, bindingVM, fn, args)
}

func (runtime exactCallableRuntime) applyCallableFastWithBinding(ctx context.Context, bindingVM *VM, fn types.Value, args []types.Value) (types.Value, error) {
	machine := runtime.newExecutionMachine()
	if machine == nil {
		return nil, fmt.Errorf("callable execution machine is not initialized")
	}
	return machine.applyCallableFastWithBinding(ctx, bindingVM, fn, args)
}

func newRunnerExactLazySeqRuntime(r *Runner) LazySeqRuntime {
	return runnerExactLazySeqRuntime{runner: r}
}

func (runtime runnerExactLazySeqRuntime) Apply(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	if runtime.runner == nil {
		return nil, fmt.Errorf("runner lazy-seq runtime is not initialized")
	}
	return runtime.runner.SnapshotExactCallableRuntime().Apply(ctx, nil, fn, args)
}

func (runtime runnerExactLazySeqRuntime) ApplyFast(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	if runtime.runner == nil {
		return nil, fmt.Errorf("runner lazy-seq runtime is not initialized")
	}
	return runtime.runner.SnapshotExactCallableRuntime().ApplyFast(ctx, nil, fn, args)
}

func (runtime runnerExactLazySeqRuntime) InvokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error) {
	if runtime.runner == nil {
		return nil, fmt.Errorf("runner lazy-seq runtime is not initialized")
	}
	return runtime.runner.SnapshotExactCallableRuntime().InvokeClosure(ctx, cl, args)
}

func (runtime runnerExactLazySeqRuntime) NewLazySeq(ctx context.Context, thunk types.Value) *VMLazySeq {
	if runtime.runner == nil {
		return nil
	}
	return NewVMLazySeq(thunk, func(thunkValue types.Value) (types.Value, error) {
		if fn, ok := thunkValue.(types.Fn); ok {
			return fn.Invoke(ctx)
		}

		closure, ok := thunkValue.(*compiler.Closure)
		if !ok {
			return nil, &RuntimeError{Message: "lazy-seq: thunk must be a function"}
		}
		if closure.Fn.NumParameters != 0 {
			return nil, &RuntimeError{Message: "lazy-seq: thunk must take no arguments"}
		}
		return runtime.InvokeClosure(ctx, closure, nil)
	})
}

func (r *Runner) SetLazySeqRuntime(runtime LazySeqRuntime) {
	if r == nil || r.runtimeServices == nil {
		return
	}
	r.runtimeServices.lazySeq = runtime
}

func (r *Runner) resolveLazySeqRuntime() LazySeqRuntime {
	if r == nil || r.runtimeServices == nil {
		return nil
	}
	if r.runtimeServices.lazySeq != nil {
		return r.runtimeServices.lazySeq
	}
	return newRunnerExactLazySeqRuntime(r)
}

func (r *Runner) NewLazySeq(thunk types.Value) *VMLazySeq {
	runtime := r.resolveLazySeqRuntime()
	if runtime == nil {
		return nil
	}
	return runtime.NewLazySeq(forkBindingContext(r.contextForExecution()), thunk)
}
