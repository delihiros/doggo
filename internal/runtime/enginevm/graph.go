package enginevm

import (
	"doggo/internal/project"
	"doggo/internal/vm/engine"
)

func NewGraphHandles(cfg engine.RuntimeConfig) GraphHandles {
	cfg.LoadPaths = append([]string(nil), cfg.LoadPaths...)
	cfg.GenerationSources = append([]*project.GenerationSource(nil), cfg.GenerationSources...)
	namespaces := engine.NewNamespaceServices(engine.NamespaceConfig{
		ProjectRoot:       cfg.ProjectRoot,
		LoadPaths:         append([]string(nil), cfg.LoadPaths...),
		GenerationSources: append([]*project.GenerationSource(nil), cfg.GenerationSources...),
		Runtime:           cfg,
	})
	sources := engine.NewSourceRegistry(namespaces.Loader())
	basis := engine.NewBasisServices(namespaces.Loader(), cfg.Lock)
	basis.CaptureInitial()

	return GraphHandles{
		Engine:     cfg,
		Namespaces: namespaces,
		Sources:    sources,
		Basis:      basis,
	}
}
