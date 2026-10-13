package engine

import (
	"doggo/internal/project"
	"doggo/internal/types"
)

type NamespaceLoader interface {
	LoadFile(path string) (types.Value, error)
	Require(ns string) error
	ResolveResourcePath(name string) (string, bool)
	GetLoadPaths() []string
	ProjectRoot() string
}

type NamespaceState interface {
	CurrentNamespace() string
	SetCurrentNamespace(ns string)
}

type NamespaceConfig struct {
	ProjectRoot       string
	LoadPaths         []string
	GenerationSources []*project.GenerationSource
	Runtime           RuntimeConfig
	Loader            NamespaceLoader
	State             NamespaceState
}

type NamespaceServices struct {
	loader NamespaceLoader
	state  NamespaceState
}

func NewNamespaceServices(cfg NamespaceConfig) *NamespaceServices {
	loader := cfg.Loader
	if loader == nil {
		loader = &inertNamespaceLoader{
			projectRoot: cfg.ProjectRoot,
			loadPaths:   append([]string{}, cfg.LoadPaths...),
		}
	}
	state := cfg.State
	if state == nil {
		state = &inertNamespaceState{}
	}
	return &NamespaceServices{
		loader: loader,
		state:  state,
	}
}

func (s *NamespaceServices) Loader() NamespaceLoader {
	if s == nil {
		return nil
	}
	return s.loader
}

func (s *NamespaceServices) LoadFile(path string) (types.Value, error) {
	if s == nil || s.loader == nil {
		return nil, nil
	}
	return s.loader.LoadFile(path)
}

func (s *NamespaceServices) Require(ns string) error {
	if s == nil || s.loader == nil {
		return nil
	}
	return s.loader.Require(ns)
}

func (s *NamespaceServices) ResolveResourcePath(name string) (string, bool) {
	if s == nil || s.loader == nil {
		return "", false
	}
	return s.loader.ResolveResourcePath(name)
}

func (s *NamespaceServices) CurrentNamespace() string {
	if s == nil || s.state == nil {
		return ""
	}
	return s.state.CurrentNamespace()
}

func (s *NamespaceServices) SetCurrentNamespace(ns string) {
	if s == nil || s.state == nil {
		return
	}
	s.state.SetCurrentNamespace(ns)
}

type inertNamespaceLoader struct {
	projectRoot string
	loadPaths   []string
}

func (l *inertNamespaceLoader) LoadFile(string) (types.Value, error) { return nil, nil }
func (l *inertNamespaceLoader) Require(string) error                 { return nil }
func (l *inertNamespaceLoader) ResolveResourcePath(string) (string, bool) {
	return "", false
}
func (l *inertNamespaceLoader) GetLoadPaths() []string {
	if l == nil {
		return nil
	}
	return append([]string{}, l.loadPaths...)
}
func (l *inertNamespaceLoader) ProjectRoot() string {
	if l == nil {
		return ""
	}
	return l.projectRoot
}

type inertNamespaceState struct {
	current string
}

func (s *inertNamespaceState) CurrentNamespace() string {
	if s == nil {
		return ""
	}
	return s.current
}

func (s *inertNamespaceState) SetCurrentNamespace(ns string) {
	if s == nil {
		return
	}
	s.current = ns
}
