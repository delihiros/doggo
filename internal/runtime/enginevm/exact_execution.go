package enginevm

import (
	"fmt"

	"doggo/internal/vm"
	"doggo/internal/vm/engine"
)

type ExactExecution struct {
	Runner         *vm.Runner
	Initialization engine.RuntimeInitialization
	Namespaces     *engine.NamespaceServices
	Sources        *engine.SourceRegistry
	Basis          *engine.BasisServices
}

type InitializedExactExecution struct {
	Execution *RuntimeExecution
	Runner    *vm.Runner
}

func NewInitializedExactExecution(graph GraphHandles, exact ExactConfig) (InitializedExactExecution, error) {
	exec, err := newExactRuntimeExecution(graph, exact)
	if err != nil {
		return InitializedExactExecution{}, err
	}
	if err := InitializeRuntimeExecution(exec); err != nil {
		return InitializedExactExecution{}, err
	}
	runner, err := RequireVMRunner(exec)
	if err != nil {
		return InitializedExactExecution{}, err
	}
	return InitializedExactExecution{
		Execution: exec,
		Runner:    runner,
	}, nil
}

func AsVMRunner(value any) (*vm.Runner, bool) {
	switch candidate := value.(type) {
	case *vm.Runner:
		return candidate, candidate != nil
	case interface{ VMRunner() any }:
		return AsVMRunner(candidate.VMRunner())
	default:
		return nil, false
	}
}

func RequireVMRunner(value any) (*vm.Runner, error) {
	runner, ok := AsVMRunner(value)
	if !ok {
		return nil, fmt.Errorf("runtime runner did not expose a vm runner")
	}
	return runner, nil
}
