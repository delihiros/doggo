package runner

import (
	"fmt"

	"doggo/internal/runtime/enginevm"
	"doggo/internal/runtime/profile"
	"doggo/internal/runtime/services"
)

func newWithGraph(prof *profile.Profile, graph *services.Graph) (*Runner, error) {
	if prof == nil {
		prof = profile.Default()
	}
	if graph == nil {
		return nil, fmt.Errorf("graph is required")
	}
	cfg := graph.EngineConfig()
	exact, err := enginevm.ResolveExactConfig(enginevm.Config{
		ProjectRoot:       cfg.ProjectRoot,
		LoadPaths:         cfg.LoadPaths,
		GenerationSources: cfg.GenerationSources,
		Lock:              cfg.Lock,
		BootstrapCore:     cfg.BootstrapCore,
		Profile:           prof,
	})
	if err != nil {
		return nil, err
	}
	initialized, err := enginevm.NewInitializedExactExecution(graph.Handles(), exact)
	if err != nil {
		return nil, err
	}
	exec := initialized.Execution
	graph.BindEngineHandles(exec.Namespaces(), exec.Sources(), exec.Basis())
	svcs := graph.Services()
	svcs.Execution = exec
	return &Runner{
		profile:  prof,
		services: svcs,
		vmRunner: initialized.Runner,
	}, nil
}
