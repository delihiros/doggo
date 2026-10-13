package enginevm

import (
	"fmt"
	"strings"

	runtime_builtins "doggo/internal/runtime/builtins"
	runtime_profile "doggo/internal/runtime/profile"
	"doggo/internal/vm"
	"doggo/internal/vm/engine"
)

func resolveExactRuntimeProfile(engineCfg engine.RuntimeConfig, runtimeProfile *runtime_profile.Profile) (*vm.RuntimeProfile, error) {
	if runtimeProfile == nil {
		runtimeProfile = runtime_profile.Default()
	}

	catalog, err := builtinCatalogForEngineConfig(engineCfg, runtimeProfile)
	if err != nil {
		return nil, err
	}

	exactProfile := vm.NewRuntimeProfile(runtimeProfile.Name()).
		SetBuiltinCatalog(catalog).
		SetFeatures(vmFeaturesFromEngine(engineCfg.Features)).
		SetModules(vmModulesFromEngine(engineCfg.Modules))
	if engineCfg.BootstrapPolicy != nil {
		if engineCfg.BuiltinCatalog != nil {
			if descriptor, ok := engineCfg.BuiltinCatalog.Data().(engine.BuiltinCatalogDescriptor); ok &&
				descriptor.Mode == engine.BuiltinCatalogModeStandardRuntime {
				exactProfile = exactProfile.SetBootstrapPolicy(vm.DefaultRuntimeBootstrapPolicy())
				return exactProfile, nil
			}
		}
		exactProfile = exactProfile.SetBootstrapPolicy(vm.WrapEngineBootstrapPolicyWithCoreContract(engineCfg.BootstrapPolicy))
	}
	return exactProfile, nil
}

func builtinCatalogForEngineConfig(engineCfg engine.RuntimeConfig, runtimeProfile *runtime_profile.Profile) (*vm.BuiltinCatalog, error) {
	if engineCfg.BuiltinCatalog != nil {
		if descriptor, ok := engineCfg.BuiltinCatalog.Data().(engine.BuiltinCatalogDescriptor); ok {
			switch descriptor.Mode {
			case engine.BuiltinCatalogModeStandardRuntime:
				return standardRuntimeBuiltinCatalog(runtimeProfile)
			case engine.BuiltinCatalogModeExact:
				// fall through to explicit data/name handling below
			}
		}
		if definitions, ok := engineCfg.BuiltinCatalog.Data().([]vm.BuiltinDefinition); ok {
			return vm.NewBuiltinCatalog(definitions)
		}
		return builtinCatalogForEngineNames(engineCfg.BuiltinCatalog.Names())
	}
	return builtinCatalogForRuntimeProfile(runtimeProfile)
}

func builtinCatalogForEngineNames(names []string) (*vm.BuiltinCatalog, error) {
	defsByName := make(map[string]vm.BuiltinDefinition)
	for _, def := range vm.DefaultBuiltinCatalog().Definitions() {
		defsByName[def.Name] = def
	}

	defs := make([]vm.BuiltinDefinition, 0, len(names))
	for _, name := range names {
		def, ok := defsByName[name]
		if !ok {
			return nil, fmt.Errorf("runtime builtin %q is not available in vm default catalog", name)
		}
		defs = append(defs, def)
	}

	return vm.NewBuiltinCatalog(defs)
}

func standardRuntimeBuiltinCatalog(runtimeProfile *runtime_profile.Profile) (*vm.BuiltinCatalog, error) {
	allowHostEffects := runtimeProfileHasFeature(runtimeProfile, runtime_profile.FeatureHostEffects)
	if allowHostEffects {
		return vm.DefaultBuiltinCatalog(), nil
	}

	publicCatalog, err := builtinCatalogForRuntimeProfile(runtimeProfile)
	if err != nil {
		return nil, err
	}
	publicDefs := publicCatalog.Definitions()
	allowed := make(map[string]struct{}, len(publicDefs))
	for _, def := range publicDefs {
		allowed[def.Name] = struct{}{}
	}

	privateBootstrapDefs := make([]vm.BuiltinDefinition, 0)
	for _, def := range vm.DefaultBuiltinCatalog().Definitions() {
		if _, ok := allowed[def.Name]; ok {
			continue
		}
		if !allowHostEffects && isEffectfulStandardExcludedBuiltin(def.Name) {
			continue
		}
		def.Visibility = vm.BuiltinPrivate
		privateBootstrapDefs = append(privateBootstrapDefs, def)
	}

	return vm.NewBuiltinCatalog(publicDefs, privateBootstrapDefs)
}

func runtimeProfileHasFeature(runtimeProfile *runtime_profile.Profile, want runtime_profile.Feature) bool {
	if runtimeProfile == nil {
		return false
	}
	for _, feature := range runtimeProfile.Features() {
		if feature == want {
			return true
		}
	}
	return false
}

func isEffectfulStandardExcludedBuiltin(name string) bool {
	switch {
	case name == "slurp", name == "spit":
		return true
	case name == "print", name == "println", name == "prn", name == "log!":
		return true
	case strings.HasPrefix(name, "io-"):
		return true
	case strings.HasPrefix(name, "process-"):
		return true
	case strings.HasPrefix(name, "os-"):
		return true
	case strings.HasPrefix(name, "time-"):
		return true
	default:
		return false
	}
}

func builtinCatalogForRuntimeProfile(runtimeProfile *runtime_profile.Profile) (*vm.BuiltinCatalog, error) {
	if runtimeProfile == nil {
		runtimeProfile = runtime_profile.Default()
	}
	if err := runtime_builtins.ValidateProfile(runtimeProfile); err != nil {
		return nil, err
	}

	defsByName := make(map[string]vm.BuiltinDefinition)
	for _, def := range vm.DefaultBuiltinCatalog().Definitions() {
		defsByName[def.Name] = def
	}

	names := runtime_builtins.NamesForProfile(runtimeProfile)

	defs := make([]vm.BuiltinDefinition, 0, len(names))
	for _, name := range names {
		def, ok := defsByName[name]
		if !ok {
			return nil, fmt.Errorf("runtime builtin %q is not available in vm default catalog", name)
		}
		def.Visibility = vm.BuiltinPublic
		defs = append(defs, def)
	}

	return vm.NewBuiltinCatalog(defs)
}

func vmFeaturesFromEngine(features []engine.RuntimeFeature) []vm.RuntimeFeature {
	if features == nil {
		return nil
	}
	out := make([]vm.RuntimeFeature, len(features))
	for i, feature := range features {
		out[i] = vm.RuntimeFeature(feature)
	}
	return out
}

func vmModulesFromEngine(modules []engine.RunnerModule) []vm.RunnerModule {
	if modules == nil {
		return nil
	}
	out := make([]vm.RunnerModule, 0, len(modules))
	for _, module := range modules {
		if module == nil {
			out = append(out, nil)
			continue
		}
		module := module
		out = append(out, vm.NewRunnerModule(module.Name(), func(r *vm.Runner) error {
			return module.Install(vmRuntimeModuleInstaller{runner: r})
		}))
	}
	return out
}

type vmRuntimeModuleInstaller struct {
	runner *vm.Runner
}

func (i vmRuntimeModuleInstaller) VMRunner() *vm.Runner {
	return i.runner
}

func (i vmRuntimeModuleInstaller) InstallRuntimeBindingGroup(group string) error {
	if i.runner == nil {
		return nil
	}
	return i.runner.InstallRuntimeBindingGroup(group)
}
