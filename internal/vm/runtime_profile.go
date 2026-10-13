package vm

import (
	"doggo/internal/types"
	"doggo/internal/vm/engine"
)

type RuntimeProfile struct {
	name            string
	builtinCatalog  *BuiltinCatalog
	bindingCatalog  runnerRuntimeBindingCatalog
	features        []RuntimeFeature
	modules         []RunnerModule
	bootstrapPolicy RuntimeBootstrapPolicy
}

// NewRuntimeProfile creates an exact runtime composition profile.
// It starts with the standard runtime service set so exported default modules
// and bootstrap policies remain self-contained, but it installs no builtins,
// modules, or bootstrap steps until the caller opts into them.
// Callers that want the full standard runtime should start from DefaultRuntimeProfile().Clone().
func NewRuntimeProfile(name string) *RuntimeProfile {
	return &RuntimeProfile{
		name:           name,
		bindingCatalog: defaultRunnerRuntimeBindingCatalog(),
	}
}

func newRuntimeProfile(
	name string,
	builtinCatalog *BuiltinCatalog,
	bindingCatalog runnerRuntimeBindingCatalog,
	features []RuntimeFeature,
	modules []RunnerModule,
	bootstrapPolicy RuntimeBootstrapPolicy,
) *RuntimeProfile {
	return &RuntimeProfile{
		name:            name,
		builtinCatalog:  builtinCatalog,
		bindingCatalog:  bindingCatalog.clone(),
		features:        copyRuntimeFeatures(features),
		modules:         copyRunnerModules(modules),
		bootstrapPolicy: bootstrapPolicy,
	}
}

func DefaultRuntimeProfile() *RuntimeProfile {
	return newRuntimeProfile(
		"default",
		DefaultBuiltinCatalog(),
		defaultRunnerRuntimeBindingCatalog(),
		DefaultRuntimeFeatures(),
		nil,
		DefaultRuntimeBootstrapPolicy(),
	)
}

func (profile *RuntimeProfile) BuiltinCatalog() *BuiltinCatalog {
	if profile == nil {
		return nil
	}
	return profile.builtinCatalog
}

func (profile *RuntimeProfile) Modules() []RunnerModule {
	if profile == nil {
		return nil
	}
	return copyRunnerModules(profile.modules)
}

func (profile *RuntimeProfile) Features() []RuntimeFeature {
	if profile == nil {
		return nil
	}
	return copyRuntimeFeatures(profile.features)
}

func (profile *RuntimeProfile) BootstrapPolicy() RuntimeBootstrapPolicy {
	if profile == nil {
		return nil
	}
	return profile.bootstrapPolicy
}

func (profile *RuntimeProfile) Clone() *RuntimeProfile {
	return profile.clone()
}

func (profile *RuntimeProfile) SetBuiltinCatalog(catalog *BuiltinCatalog) *RuntimeProfile {
	if profile == nil {
		profile = NewRuntimeProfile("")
	}
	profile.builtinCatalog = catalog
	return profile
}

func (profile *RuntimeProfile) SetModules(modules []RunnerModule) *RuntimeProfile {
	if profile == nil {
		profile = NewRuntimeProfile("")
	}
	profile.modules = copyRunnerModules(modules)
	return profile
}

func (profile *RuntimeProfile) SetFeatures(features []RuntimeFeature) *RuntimeProfile {
	if profile == nil {
		profile = NewRuntimeProfile("")
	}
	profile.features = copyRuntimeFeatures(features)
	return profile
}

func (profile *RuntimeProfile) SetBootstrapPolicy(policy RuntimeBootstrapPolicy) *RuntimeProfile {
	if profile == nil {
		profile = NewRuntimeProfile("")
	}
	profile.bootstrapPolicy = policy
	return profile
}

func (profile *RuntimeProfile) setBindingCatalog(catalog runnerRuntimeBindingCatalog) *RuntimeProfile {
	if profile == nil {
		profile = NewRuntimeProfile("")
	}
	profile.bindingCatalog = catalog.clone()
	return profile
}

func (profile *RuntimeProfile) clone() *RuntimeProfile {
	if profile == nil {
		return nil
	}
	return &RuntimeProfile{
		name:            profile.name,
		builtinCatalog:  profile.builtinCatalog,
		bindingCatalog:  profile.bindingCatalog.clone(),
		features:        copyRuntimeFeatures(profile.features),
		modules:         copyRunnerModules(profile.modules),
		bootstrapPolicy: profile.bootstrapPolicy,
	}
}

func WrapEngineBootstrapPolicyWithCoreContract(policy engine.RuntimeBootstrapPolicy) RuntimeBootstrapPolicy {
	return engineBootstrapPolicyWithCoreContract{policy: policy}
}

type engineBootstrapPolicyWithCoreContract struct {
	policy engine.RuntimeBootstrapPolicy
}

func (a engineBootstrapPolicyWithCoreContract) Name() string {
	return a.policy.Name()
}

func (a engineBootstrapPolicyWithCoreContract) StepNames() []string {
	names := []string{"install-core-runtime-contract"}
	return append(names, a.policy.StepNames()...)
}

func (a engineBootstrapPolicyWithCoreContract) Bootstrap(r *Runner) error {
	if err := r.installRuntimeBootstrapPhase(runtimeBootstrapPhaseCoreContract); err != nil {
		return err
	}
	return a.policy.Bootstrap(vmRuntimeBootstrapper{runner: r})
}

type vmRuntimeBootstrapper struct {
	runner *Runner
}

func (b vmRuntimeBootstrapper) VMRunner() *Runner {
	return b.runner
}

func (b vmRuntimeBootstrapper) Run(source string) (types.Value, error) {
	if b.runner == nil {
		return nil, nil
	}
	return b.runner.runWithOriginTrusted(source, engine.SourceOrigin{File: "<runtime-bootstrap>"})
}
