package vm

import (
	"context"

	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/vm/engine"
)

type closureExecutor interface {
	InvokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error)
}

type callableRuntime interface {
	Apply(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error)
	ApplyFast(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error)
}

type LazySeqRuntime interface {
	callableRuntime
	closureExecutor
	NewLazySeq(ctx context.Context, thunk types.Value) *VMLazySeq
}

type closureExecutorFunc func(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error)

func (f closureExecutorFunc) InvokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error) {
	return f(ctx, cl, args)
}

type runnerExecutionServices struct {
	runner  *Runner
	sources *runnerSourceRegistryServices
}

func newRunnerExecutionServices(runner *Runner, sources *runnerSourceRegistryServices) *runnerExecutionServices {
	return &runnerExecutionServices{
		runner:  runner,
		sources: sources,
	}
}

func (services *runnerExecutionServices) runtimeExecutionEnv() vmExecutionEnv {
	var sourceLineProvider func(string, int) (string, bool)
	if registry := services.sourceRegistry(); registry != nil {
		sourceLineProvider = registry.LookupLine
	}
	return services.runner.getOrCreateExecutionSession().runtimeExecutionEnv(sourceLineProvider)
}

func (services *runnerExecutionServices) exactCallableRuntime() exactCallableRuntime {
	return services.runtimeExecutionEnv().exactCallableRuntime()
}

func (services *runnerExecutionServices) compileTimeExecutionEnv() vmExecutionEnv {
	var sourceLineProvider func(string, int) (string, bool)
	if registry := services.sourceRegistry(); registry != nil {
		sourceLineProvider = registry.LookupLine
	}
	return services.runner.getOrCreateExecutionSession().compileTimeExecutionEnv(sourceLineProvider)
}

func (services *runnerExecutionServices) macroExecutor() closureExecutor {
	return closureExecutorFunc(func(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error) {
		return services.compileTimeExecutionEnv().exactCallableRuntime().invokeClosure(ctx, cl, args)
	})
}

func (services *runnerExecutionServices) newMachine(bytecode *compiler.Bytecode, execCtx context.Context, collectStats bool) (*VM, error) {
	if execCtx == nil {
		execCtx = services.runner.contextForExecution()
	}
	session := services.runner.getOrCreateExecutionSession()
	machine, err := NewWithGlobalsStoreAndBuiltins(bytecode, session.globals, services.runner.builtinCatalog)
	if err != nil {
		return nil, err
	}
	machine.session = session
	machine.SetContext(execCtx)
	machine.stmStore = session.stmStore
	machine.SetSourceLineProvider(services.lookupLine)
	machine.observability = session.getOrCreateObservabilityState()
	if collectStats {
		machine.ResetAndEnableExecStats()
	}
	if session.profiler != nil {
		machine.profiler = session.profiler
	}
	machine.installCallableRuntime(newExactCallableSnapshotRuntime(machine.snapshotExecutionEnv()))
	return machine, nil
}

func (services *runnerExecutionServices) lookupLine(file string, line int) (string, bool) {
	if services == nil || services.sources == nil {
		return "", false
	}
	return services.sources.lookupLine(file, line)
}

func (services *runnerExecutionServices) sourceRegistry() *engine.SourceRegistry {
	if services == nil || services.sources == nil {
		return nil
	}
	return services.sources.registry
}

func (r *Runner) ContextForExecution() context.Context {
	if r == nil {
		return context.Background()
	}
	return r.contextForExecution()
}
