package composition

import (
	"fmt"

	"doggo/internal/project"
	"doggo/internal/runtime/bootstrap"
	"doggo/internal/runtime/builtins"
	"doggo/internal/runtime/profile"
	"doggo/internal/vm/engine"
)

type StandardConfig struct {
	ProjectRoot       string
	LoadPaths         []string
	GenerationSources []*project.GenerationSource
	Lock              *project.LockFile
	BootstrapCore     bool
	Profile           *profile.Profile
}

func StandardEngineConfig(cfg StandardConfig) engine.RuntimeConfig {
	if cfg.Profile == nil {
		cfg.Profile = profile.Default()
	}
	catalog := builtins.FromProfile(cfg.Profile)
	return engine.RuntimeConfig{
		ProjectRoot:       cfg.ProjectRoot,
		LoadPaths:         append([]string(nil), cfg.LoadPaths...),
		GenerationSources: append([]*project.GenerationSource(nil), cfg.GenerationSources...),
		Lock:              cfg.Lock,
		BootstrapCore:     cfg.BootstrapCore,
		BuiltinCatalog: engine.NewBuiltinCatalog(catalog.Names(), engine.BuiltinCatalogDescriptor{
			Mode: engine.BuiltinCatalogModeStandardRuntime,
		}),
		Features:        engine.DefaultRuntimeFeatures(),
		BootstrapPolicy: standardBootstrapPolicy{policy: bootstrap.DefaultPolicy()},
	}
}

type standardBootstrapPolicy struct {
	policy bootstrap.Policy
}

func (p standardBootstrapPolicy) Name() string {
	return "runtime-standard-bootstrap"
}

func (p standardBootstrapPolicy) StepNames() []string {
	return []string{"load-doggo-core"}
}

func (p standardBootstrapPolicy) Bootstrap(bootstrapper engine.RuntimeBootstrapper) error {
	for _, ns := range p.policy.Namespaces {
		if _, err := bootstrapper.Run(fmt.Sprintf("(require '[%s :refer :all])", ns)); err != nil {
			return err
		}
	}
	return nil
}
