package services

import (
	"doggo/internal/types"
	"doggo/internal/vm/engine"
)

type NamespaceServices interface {
	LoadFile(path string) (types.Value, error)
	Require(ns string) error
	ResolveResourcePath(name string) (string, bool)
	CurrentNamespace() string
	SetCurrentNamespace(ns string)
}

type namespaceServices struct {
	engine *engine.NamespaceServices
}

func NewNamespaceServicesFromEngine(ns *engine.NamespaceServices) NamespaceServices {
	return &namespaceServices{engine: ns}
}

func (s *namespaceServices) LoadFile(path string) (types.Value, error) {
	if s == nil || s.engine == nil {
		return nil, nil
	}
	return s.engine.LoadFile(path)
}

func (s *namespaceServices) Require(ns string) error {
	if s == nil || s.engine == nil {
		return nil
	}
	return s.engine.Require(ns)
}

func (s *namespaceServices) ResolveResourcePath(name string) (string, bool) {
	if s == nil || s.engine == nil {
		return "", false
	}
	return s.engine.ResolveResourcePath(name)
}

func (s *namespaceServices) CurrentNamespace() string {
	if s == nil || s.engine == nil {
		return ""
	}
	return s.engine.CurrentNamespace()
}

func (s *namespaceServices) SetCurrentNamespace(ns string) {
	if s == nil || s.engine == nil {
		return
	}
	s.engine.SetCurrentNamespace(ns)
}
