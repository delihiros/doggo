package enginevm

import (
	"context"
	"fmt"

	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/vm"
	"doggo/internal/vm/engine"
)

type RuntimeExecution struct {
	runner         *vm.Runner
	services       *callableLazySeqRuntime
	initialization engine.RuntimeInitialization
	namespaces     *engine.NamespaceServices
	sources        *engine.SourceRegistry
	basis          *engine.BasisServices
}

func (s *RuntimeExecution) EvalString(src string) (types.Value, error) {
	if s == nil || s.runner == nil {
		return nil, fmt.Errorf("runtime execution is not initialized")
	}
	return s.runner.Run(src)
}

func (s *RuntimeExecution) VMRunner() any {
	if s == nil {
		return nil
	}
	return s.runner
}

func (s *RuntimeExecution) RuntimeInitialization() engine.RuntimeInitialization {
	if s == nil {
		return engine.RuntimeInitialization{}
	}
	return s.initialization
}

func (s *RuntimeExecution) Namespaces() *engine.NamespaceServices {
	if s == nil {
		return nil
	}
	return s.namespaces
}

func (s *RuntimeExecution) Sources() *engine.SourceRegistry {
	if s == nil {
		return nil
	}
	return s.sources
}

func (s *RuntimeExecution) Basis() *engine.BasisServices {
	if s == nil {
		return nil
	}
	return s.basis
}

func (s *RuntimeExecution) NewLazySeq(thunk types.Value) *vm.VMLazySeq {
	if s == nil || s.services == nil || s.runner == nil {
		return nil
	}
	return s.services.NewLazySeq(s.runner.ContextForExecution(), thunk)
}

func (s *RuntimeExecution) Apply(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	if s == nil || s.services == nil {
		return nil, fmt.Errorf("callable runtime is not initialized")
	}
	return s.services.Apply(ctx, fn, args)
}

func (s *RuntimeExecution) ApplyFast(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	if s == nil || s.services == nil {
		return nil, fmt.Errorf("callable runtime is not initialized")
	}
	return s.services.ApplyFast(ctx, fn, args)
}

func (s *RuntimeExecution) InvokeClosure(ctx context.Context, cl *compiler.Closure, args []types.Value) (types.Value, error) {
	if s == nil || s.services == nil {
		return nil, fmt.Errorf("callable runtime is not initialized")
	}
	return s.services.InvokeClosure(ctx, cl, args)
}
