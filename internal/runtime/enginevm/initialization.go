package enginevm

import (
	"fmt"

	"doggo/internal/vm"
	"doggo/internal/vm/engine"
)

type runtimeExecutionAdapter struct {
	runner        *vm.Runner
	profile       *vm.RuntimeProfile
	bootstrapCore bool
	basis         *engine.BasisServices
}

func newExactVMExecution(graph GraphHandles, exact ExactConfig) (ExactExecution, error) {
	if exact.ExactProfile == nil {
		return ExactExecution{}, fmt.Errorf("exact execution requires ExactProfile")
	}
	if graph.Namespaces == nil || graph.Namespaces.Loader() == nil {
		return ExactExecution{}, fmt.Errorf("exact execution requires namespace services")
	}
	clonedProfile := exact.ExactProfile.Clone()
	if clonedProfile == nil {
		return ExactExecution{}, fmt.Errorf("exact execution requires ExactProfile")
	}

	vmExec, err := vm.NewExactEngineRunner(
		clonedProfile,
		exact.Engine.BootstrapCore,
		graph.Namespaces,
		exact.Engine.GenerationSources,
		exact.Engine.Lock,
	)
	if err != nil {
		return ExactExecution{}, err
	}
	exactGraph := newExactGraphHandles(vmExec.Runner, graph, vmExec.Sources)

	return ExactExecution{
		Runner: vmExec.Runner,
		Initialization: runtimeExecutionAdapter{
			runner:        vmExec.Runner,
			profile:       clonedProfile,
			bootstrapCore: exact.Engine.BootstrapCore,
			basis:         exactGraph.Basis,
		}.RuntimeInitialization(),
		Namespaces: exactGraph.Namespaces,
		Sources:    exactGraph.Sources,
		Basis:      exactGraph.Basis,
	}, nil
}

func newExactRuntimeExecution(graph GraphHandles, exact ExactConfig) (*RuntimeExecution, error) {
	vmExec, err := newExactVMExecution(graph, exact)
	if err != nil {
		return nil, err
	}
	return newRuntimeExecutionFromExactExecution(vmExec), nil
}

func (s runtimeExecutionAdapter) VMRunner() any {
	if s.runner == nil {
		return nil
	}
	return s.runner
}

func (s runtimeExecutionAdapter) RuntimeInitialization() engine.RuntimeInitialization {
	return engine.RuntimeInitialization{
		InstallFeatures: func() error {
			if s.runner == nil || s.profile == nil {
				return nil
			}
			return s.runner.InstallRuntimeFeatures(s.profile.Features())
		},
		InstallModules: func() error {
			if s.runner == nil || s.profile == nil {
				return nil
			}
			return s.runner.InstallRuntimeModules(s.profile.Modules())
		},
		CaptureInitialBasis: func() {
			if s.runner != nil {
				s.runner.CaptureInitialBasis()
			}
			if s.basis != nil {
				s.basis.CaptureInitial()
			}
		},
		BootstrapCore:     s.bootstrapCore,
		BootstrapCoreFunc: s.runner.BootstrapCore,
	}
}

func InitializeRuntimeExecution(exec interface {
	RuntimeInitialization() engine.RuntimeInitialization
}) error {
	if exec == nil {
		return nil
	}
	return engine.InitializeRuntime(exec.RuntimeInitialization())
}
