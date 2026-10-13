package services

import (
	"doggo/internal/project"
	"doggo/internal/runtime/enginevm"
	"doggo/internal/vm/engine"
)

var newEngineVMGraphHandles = enginevm.NewGraphHandles

type Graph struct {
	engineCfg  engine.RuntimeConfig
	namespaces *engine.NamespaceServices
	sources    *engine.SourceRegistry
	basis      *engine.BasisServices
	serviceSet *ServiceSet
}

func NewGraph(cfg engine.RuntimeConfig) *Graph {
	handles := newEngineVMGraphHandles(cfg)
	engineCfg := handles.Engine
	engineCfg.LoadPaths = append([]string(nil), handles.Engine.LoadPaths...)
	engineCfg.GenerationSources = append([]*project.GenerationSource(nil), handles.Engine.GenerationSources...)

	graph := &Graph{
		engineCfg:  engineCfg,
		namespaces: handles.Namespaces,
		sources:    handles.Sources,
		basis:      handles.Basis,
	}
	graph.serviceSet = &ServiceSet{
		graph:      graph,
		Namespaces: NewNamespaceServicesFromEngine(handles.Namespaces),
		Sources:    NewSourceRegistryFromEngine(handles.Sources),
		Basis:      NewBasisServicesFromEngine(handles.Basis),
	}
	return graph
}

func (g *Graph) Services() *ServiceSet {
	if g == nil {
		return nil
	}
	return g.serviceSet
}

func (g *Graph) EngineConfig() engine.RuntimeConfig {
	if g == nil {
		return engine.RuntimeConfig{}
	}
	cfg := g.engineCfg
	cfg.LoadPaths = append([]string(nil), g.engineCfg.LoadPaths...)
	cfg.GenerationSources = append([]*project.GenerationSource(nil), g.engineCfg.GenerationSources...)
	return cfg
}

func (g *Graph) NamespaceEngine() *engine.NamespaceServices {
	if g == nil {
		return nil
	}
	return g.namespaces
}

func (g *Graph) SourceRegistryEngine() *engine.SourceRegistry {
	if g == nil {
		return nil
	}
	return g.sources
}

func (g *Graph) BasisEngine() *engine.BasisServices {
	if g == nil {
		return nil
	}
	return g.basis
}

func (g *Graph) BindEngineHandles(namespaces *engine.NamespaceServices, sources *engine.SourceRegistry, basis *engine.BasisServices) {
	if g == nil {
		return
	}
	if g.serviceSet == nil {
		g.serviceSet = &ServiceSet{graph: g}
	}
	g.serviceSet.graph = g
	if namespaces != nil {
		g.namespaces = namespaces
		g.serviceSet.Namespaces = NewNamespaceServicesFromEngine(namespaces)
	}
	if sources != nil {
		g.sources = sources
		g.serviceSet.Sources = NewSourceRegistryFromEngine(sources)
	}
	if basis != nil {
		g.basis = basis
		g.serviceSet.Basis = NewBasisServicesFromEngine(basis)
	}
}

func (g *Graph) Handles() enginevm.GraphHandles {
	if g == nil {
		return enginevm.GraphHandles{}
	}
	return enginevm.GraphHandles{
		Engine:     g.EngineConfig(),
		Namespaces: g.namespaces,
		Sources:    g.sources,
		Basis:      g.basis,
	}
}
