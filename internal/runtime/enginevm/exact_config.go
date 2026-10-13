package enginevm

import (
	"doggo/internal/runtime/composition"
	"doggo/internal/runtime/profile"
)

func ResolveExactConfig(cfg Config) (ExactConfig, error) {
	runtimeProfile := cfg.Profile
	if runtimeProfile == nil {
		runtimeProfile = profile.Default()
	}

	engineCfg := composition.StandardEngineConfig(composition.StandardConfig{
		ProjectRoot:       cfg.ProjectRoot,
		LoadPaths:         cfg.LoadPaths,
		GenerationSources: cfg.GenerationSources,
		Lock:              cfg.Lock,
		BootstrapCore:     cfg.BootstrapCore,
		Profile:           runtimeProfile,
	})
	exactProfile, err := resolveExactRuntimeProfile(engineCfg, runtimeProfile)
	if err != nil {
		return ExactConfig{}, err
	}
	return ExactConfig{
		Engine:       engineCfg,
		ExactProfile: exactProfile,
	}, nil
}
