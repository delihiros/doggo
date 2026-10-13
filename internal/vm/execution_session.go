package vm

import (
	"context"
	"io"

	"doggo/internal/compiler"
	"doggo/internal/stm"
	"doggo/internal/types"
)

type executionSession struct {
	runner          *Runner
	ctx             context.Context
	globals         []types.Value
	constants       []types.Value
	constantStore   *compiler.ConstantStore
	stmStore        *stm.Store
	profiler        *Profiler
	observability   *ObservabilityState
	callableRuntime CallableRuntime
}

func newExecutionSession() *executionSession {
	return &executionSession{
		ctx: withBindingState(context.Background()),
	}
}

func newExecutionSessionFromVM(vm *VM) *executionSession {
	session := newExecutionSession()
	if vm == nil {
		return session
	}

	session.ctx = vm.ctx
	if session.ctx == nil {
		session.ctx = context.Background()
	}
	session.constants = vm.constants
	session.globals = vm.globals
	session.stmStore = vm.stmStore
	session.profiler = vm.profiler
	session.observability = vm.observability
	session.callableRuntime = vm.snapshotCallableRuntime()
	return session
}

func (r *Runner) getOrCreateExecutionSession() *executionSession {
	if r == nil {
		return nil
	}
	if r.session == nil {
		r.session = newExecutionSession()
	}
	if r.session.runner == nil {
		r.session.runner = r
	}
	return r.session
}

func (vm *VM) getOrCreateExecutionSession() *executionSession {
	if vm == nil {
		return nil
	}
	if vm.session == nil {
		vm.session = newExecutionSessionFromVM(vm)
	}
	return vm.session
}

func (s *executionSession) getOrCreateObservabilityState() *ObservabilityState {
	if s == nil {
		return nil
	}
	if s.observability == nil {
		s.observability = NewObservabilityState()
	}
	return s.observability
}

func (s *executionSession) contextForExecution() context.Context {
	if s == nil {
		return withBindingState(context.Background())
	}
	ctx := withBindingState(s.ctx)
	if correlationIDFromContext(ctx) != "" {
		return ctx
	}
	return withCorrelationID(ctx, s.getOrCreateObservabilityState().NewCorrelationID())
}

func (s *executionSession) setCallableRuntime(runtime CallableRuntime) {
	if s == nil {
		return
	}
	s.callableRuntime = runtime
}

func (s *executionSession) runtimeExecutionEnv(sourceLineProvider func(string, int) (string, bool)) vmExecutionEnv {
	var builtinCatalog *BuiltinCatalog
	var constants []types.Value
	var globals []types.Value
	var stmStore *stm.Store
	var profiler *Profiler
	var observability *ObservabilityState
	if s != nil && s.runner != nil {
		builtinCatalog = s.runner.builtinCatalog
	}
	if s != nil {
		if s.constantStore != nil {
			constants = s.constantStore.Values()
		} else {
			constants = s.constants
		}
		globals = s.globals
		stmStore = s.stmStore
		profiler = s.profiler
		observability = s.getOrCreateObservabilityState()
	}
	env := vmExecutionEnv{
		session:            s,
		constants:          constants,
		globals:            globals,
		builtinCatalog:     builtinCatalog,
		builtins:           resolveCatalogBuiltins(builtinCatalog),
		importCallback:     nil,
		stmStore:           stmStore,
		profiler:           profiler,
		sourceLineProvider: sourceLineProvider,
		observability:      observability,
	}
	env.callableRuntime = newExactCallableSnapshotRuntime(env)
	return env
}

func (s *executionSession) compileTimeExecutionEnv(sourceLineProvider func(string, int) (string, bool)) vmExecutionEnv {
	observability := NewObservabilityState()
	observability.SetLogWriter(io.Discard)
	var builtinCatalog *BuiltinCatalog
	var constants []types.Value
	var globals []types.Value
	var stmStore *stm.Store
	if s != nil && s.runner != nil {
		builtinCatalog = s.runner.builtinCatalog
	}
	if s != nil {
		if s.constantStore != nil {
			constants = s.constantStore.Values()
		} else {
			constants = s.constants
		}
		globals = s.globals
		stmStore = s.stmStore
	}
	env := vmExecutionEnv{
		session:            s,
		constants:          constants,
		globals:            globals,
		builtinCatalog:     builtinCatalog,
		builtins:           resolveCatalogBuiltins(builtinCatalog),
		stmStore:           stmStore,
		sourceLineProvider: sourceLineProvider,
		observability:      observability,
	}
	env.callableRuntime = newExactCallableSnapshotRuntime(env)
	return env
}
