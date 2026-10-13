package vm

import (
	"fmt"

	"doggo/internal/project"
	"doggo/internal/types"
	"doggo/internal/vm/engine"
)

type runnerServicesBundle struct {
	namespaces *runnerNamespaceServices
	sources    *runnerSourceRegistryServices
	compile    *runnerCompileServices
	execution  *runnerExecutionServices
	lazySeq    LazySeqRuntime
	core       *runnerCoreBootstrapper
	basis      *engine.BasisServices
	bindings   runnerRuntimeBindingCatalog
}

func newRunnerServices(runner *Runner, cfg RunnerConfig) *runnerServicesBundle {
	profile := cfg.RuntimeProfile
	if profile == nil {
		profile = DefaultRuntimeProfile()
	}
	namespaces := newRunnerNamespaceServices(runner, cfg)
	sources := newRunnerSourceRegistryServices(namespaces.Loader())
	bindings := profile.bindingCatalog.clone()
	services := &runnerServicesBundle{
		namespaces: namespaces,
		sources:    sources,
		bindings:   bindings,
	}
	services.compile = newRunnerCompileServices(runner, sources)
	services.execution = newRunnerExecutionServices(runner, sources)
	services.lazySeq = newRunnerExactLazySeqRuntime(runner)
	services.core = &runnerCoreBootstrapper{
		runner: runner,
		policy: profile.BootstrapPolicy(),
	}
	services.basis = engine.NewBasisServices(namespaces.Loader(), cfg.Lock)
	return services
}

func newExactEngineRunner(
	exactProfile *RuntimeProfile,
	bootstrapCore bool,
	namespaces *engine.NamespaceServices,
	generationSources []*project.GenerationSource,
	lock *project.LockFile,
) (ExactEngineExecution, error) {
	if exactProfile == nil {
		return ExactEngineExecution{}, fmt.Errorf("exact execution requires ExactProfile")
	}
	if namespaces == nil || namespaces.Loader() == nil {
		return ExactEngineExecution{}, fmt.Errorf("exact execution requires namespace services")
	}

	exactProfile = exactProfile.clone()
	projectRoot := namespaces.Loader().ProjectRoot()
	loadPaths := namespaces.Loader().GetLoadPaths()
	runner := newRunnerBase(runnerBaseConfig{
		ProjectRoot:       projectRoot,
		LoadPaths:         loadPaths,
		GenerationSources: append([]*project.GenerationSource(nil), generationSources...),
		Lock:              lock,
		BootstrapCore:     bootstrapCore,
		Profile:           exactProfile,
	})

	runner.macroExpander.SetClosureExecutor(runner.runtimeServices.execution.macroExecutor())
	return ExactEngineExecution{
		Runner:  runner,
		Sources: runner.runtimeServices.sources.registry,
	}, nil
}

func (services *runnerServicesBundle) captureInitialBasis() {
	if services == nil || services.basis == nil {
		return
	}
	services.basis.CaptureInitial()
}

func (r *Runner) CaptureInitialBasis() {
	if r == nil || r.runtimeServices == nil {
		return
	}
	r.runtimeServices.captureInitialBasis()
}

func (r *Runner) initializeRuntimeProfile(profile *RuntimeProfile, bootstrapCore bool) error {
	if r == nil {
		return nil
	}
	return engine.InitializeRuntime(engine.RuntimeInitialization{
		InstallFeatures: func() error {
			if profile == nil {
				return nil
			}
			return r.InstallRuntimeFeatures(profile.Features())
		},
		InstallModules: func() error {
			if profile == nil {
				return nil
			}
			return r.InstallRuntimeModules(profile.Modules())
		},
		CaptureInitialBasis: func() {
			r.CaptureInitialBasis()
		},
		BootstrapCore:     bootstrapCore,
		BootstrapCoreFunc: r.BootstrapCore,
	})
}

func (services *runnerServicesBundle) currentBasisValue() (types.Value, error) {
	if services == nil || services.basis == nil {
		return buildBasisValue(basisRuntimeConfig{})
	}
	return services.basis.Current()
}

func (services *runnerServicesBundle) initialBasisValue() (types.Value, error) {
	if services == nil || services.basis == nil {
		return buildBasisValue(basisRuntimeConfig{})
	}
	return services.basis.Initial()
}

type runnerCoreBootstrapper struct {
	runner       *Runner
	policy       RuntimeBootstrapPolicy
	bootstrapped bool
}

func (bootstrapper *runnerCoreBootstrapper) BootstrapOnce() error {
	if bootstrapper == nil || bootstrapper.runner == nil {
		return nil
	}
	if bootstrapper.bootstrapped {
		return nil
	}
	if bootstrapper.policy == nil {
		return fmt.Errorf("runtime core bootstrap: missing bootstrap policy")
	}
	if err := bootstrapper.policy.Bootstrap(bootstrapper.runner); err != nil {
		return err
	}
	bootstrapper.bootstrapped = true
	return nil
}

func (r *Runner) InstallRuntimeBindingGroup(group string) error {
	return r.installRuntimeBindingGroup(runtimeBindingGroup(group))
}
