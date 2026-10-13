package engine

import "fmt"

type RuntimeInitialization struct {
	InstallFeatures     func() error
	InstallModules      func() error
	CaptureInitialBasis func()
	BootstrapCore       bool
	BootstrapCoreFunc   func() error
}

func InitializeRuntime(cfg RuntimeInitialization) error {
	if cfg.InstallFeatures != nil {
		if err := cfg.InstallFeatures(); err != nil {
			return err
		}
	}
	if cfg.InstallModules != nil {
		if err := cfg.InstallModules(); err != nil {
			return err
		}
	}
	if cfg.CaptureInitialBasis != nil {
		cfg.CaptureInitialBasis()
	}
	if !cfg.BootstrapCore {
		return nil
	}
	if cfg.BootstrapCoreFunc == nil {
		return fmt.Errorf("runtime initialization: core bootstrap requested without bootstrap function")
	}
	return cfg.BootstrapCoreFunc()
}
