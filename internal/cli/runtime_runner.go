package cli

import (
	"fmt"

	"doggo/internal/project"
	"doggo/internal/runtime/enginevm"
	"doggo/internal/runtime/profile"
	runtimeRunner "doggo/internal/runtime/runner"
	"doggo/internal/vm"
)

func newStandardVMRunner(cfg project.RuntimeConfig, bootstrapCore bool) (*vm.Runner, error) {
	runner, err := runtimeRunner.New(runtimeRunner.Config{
		ProjectRoot:       cfg.ProjectRoot,
		LoadPaths:         append([]string(nil), cfg.LoadPaths...),
		GenerationSources: append([]*project.GenerationSource(nil), cfg.GenerationSources...),
		Lock:              cfg.Lock,
		BootstrapCore:     bootstrapCore,
		Profile:           profile.Host(),
	})
	if err != nil {
		return nil, err
	}
	vmRunner, ok := enginevm.AsVMRunner(runner)
	if !ok || vmRunner == nil {
		return nil, fmt.Errorf("runtime runner did not expose a vm runner")
	}
	return vmRunner, nil
}
