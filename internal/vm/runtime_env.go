package vm

import (
	"context"

	"doggo/internal/compiler"
	"doggo/internal/stm"
	"doggo/internal/types"
)

type CallableRuntime interface {
	Bind(*VM) CallableRuntime
	Apply(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error)
	ApplyFast(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error)
	InvokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error)
}

type vmExecutionEnv struct {
	session            *executionSession
	constants          []types.Value
	globals            []types.Value
	builtinCatalog     *BuiltinCatalog
	builtins           []*Builtin
	importCallback     ImportCallback
	stmStore           *stm.Store
	profiler           *Profiler
	sourceLineProvider func(file string, line int) (string, bool)
	observability      *ObservabilityState
	callableRuntime    CallableRuntime
}

func (vm *VM) snapshotExecutionEnv() vmExecutionEnv {
	return vmExecutionEnv{
		session:            vm.session,
		constants:          vm.constants,
		globals:            vm.globals,
		builtinCatalog:     vm.builtinCatalog,
		builtins:           vm.builtins,
		importCallback:     vm.importCallback,
		stmStore:           vm.stmStore,
		profiler:           vm.profiler,
		sourceLineProvider: vm.sourceLineProvider,
		observability:      vm.observability,
		callableRuntime:    vm.callableRuntime,
	}
}

func (env vmExecutionEnv) applyToVM(vm *VM, ctx context.Context) {
	vm.session = env.session
	vm.constants = env.constants
	vm.globals = env.globals
	vm.ctx = ctx
	vm.builtinCatalog = env.builtinCatalog
	vm.builtins = env.builtins
	vm.importCallback = env.importCallback
	vm.stmStore = env.stmStore
	vm.profiler = env.profiler
	vm.sourceLineProvider = env.sourceLineProvider
	vm.observability = env.observability
	vm.installCallableRuntime(env.callableRuntime)
}

func bindCallableRuntime(runtime CallableRuntime, vm *VM) CallableRuntime {
	if runtime == nil {
		return nil
	}
	return runtime.Bind(vm)
}
