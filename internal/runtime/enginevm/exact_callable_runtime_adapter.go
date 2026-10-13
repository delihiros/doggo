package enginevm

import (
	"context"

	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/vm"
)

type exactCallableRuntimeAdapter struct {
	snapshot vm.ExactCallableSnapshot
	vm       *vm.VM
}

func newExactCallableRuntimeAdapter(r *vm.Runner) exactCallableRuntimeAdapter {
	return exactCallableRuntimeAdapter{snapshot: r.SnapshotExactCallableRuntime()}
}

func (r exactCallableRuntimeAdapter) Bind(vmref *vm.VM) vm.CallableRuntime {
	r.vm = vmref
	return r
}

func (r exactCallableRuntimeAdapter) Apply(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	return r.snapshot.Apply(ctx, r.vm, fn, args)
}

func (r exactCallableRuntimeAdapter) ApplyFast(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	return r.snapshot.ApplyFast(ctx, r.vm, fn, args)
}

func (r exactCallableRuntimeAdapter) InvokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error) {
	return r.snapshot.InvokeClosure(ctx, cl, args)
}
