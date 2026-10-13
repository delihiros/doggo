package vm

import "fmt"

// RunnerModule installs a cohesive runtime feature set into a Runner.
// Runner construction composes modules instead of hard-coding feature wiring.
type RunnerModule interface {
	Name() string
	Install(*Runner) error
}

type runnerModule struct {
	name    string
	install func(*Runner) error
}

func (m runnerModule) Name() string {
	return m.name
}

func (m runnerModule) Install(r *Runner) error {
	if m.install == nil {
		return nil
	}
	return m.install(r)
}

// NewRunnerModule creates a named RunnerModule for explicit runtime composition.
func NewRunnerModule(name string, install func(*Runner) error) RunnerModule {
	return runnerModule{name: name, install: install}
}

func copyRunnerModules(modules []RunnerModule) []RunnerModule {
	if modules == nil {
		return nil
	}
	out := make([]RunnerModule, len(modules))
	copy(out, modules)
	return out
}

func installRunnerModules(r *Runner, modules []RunnerModule) error {
	for i, module := range modules {
		if module == nil {
			return fmt.Errorf("runner module[%d]: nil module", i)
		}
		name := module.Name()
		if name == "" {
			name = fmt.Sprintf("module[%d]", i)
		}
		if err := module.Install(r); err != nil {
			return fmt.Errorf("runner module %q: %w", name, err)
		}
	}
	return nil
}
