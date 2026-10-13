package vm

import (
	"sync"

	"doggo/internal/vm/engine"
)

type runnerSourceRegistryServices struct {
	registry *engine.SourceRegistry
	originMu sync.RWMutex
	origins  map[string]engine.SourceOrigin
}

func newRunnerSourceRegistryServices(loader engine.NamespaceLoader) *runnerSourceRegistryServices {
	var reader engine.NamespaceLoader
	if loader != nil {
		reader = loader
	}
	return &runnerSourceRegistryServices{
		registry: engine.NewSourceRegistry(reader),
		origins:  make(map[string]engine.SourceOrigin),
	}
}

func (services *runnerSourceRegistryServices) register(file, source string) {
	services.registerOrigin(engine.SourceOrigin{File: file}, source)
}

func (services *runnerSourceRegistryServices) registerOrigin(origin engine.SourceOrigin, source string) {
	if services == nil {
		return
	}
	services.registry.Register(origin.File, source)
	if origin.GenerationSource != nil {
		services.originMu.Lock()
		services.origins[origin.File] = origin
		services.originMu.Unlock()
	}
}

func (services *runnerSourceRegistryServices) resolveOrigin(file string) (engine.SourceOrigin, bool) {
	if services == nil {
		return engine.SourceOrigin{}, false
	}
	services.originMu.RLock()
	origin, ok := services.origins[file]
	services.originMu.RUnlock()
	return origin, ok
}

func (services *runnerSourceRegistryServices) registerPrecompiled(code *engine.Program) {
	if services == nil {
		return
	}
	services.registry.RegisterPrecompiled(code)
}

func (services *runnerSourceRegistryServices) lookupLine(file string, line int) (string, bool) {
	if services == nil {
		return "", false
	}
	return services.registry.LookupLine(file, line)
}

func (services *runnerSourceRegistryServices) lookupContent(file string) (string, bool) {
	if services == nil {
		return "", false
	}
	return services.registry.LookupContent(file)
}
