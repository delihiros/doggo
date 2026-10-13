package services

import "doggo/internal/vm/engine"

type SourceRegistry interface {
	Register(file, source string)
	RegisterPrecompiled(code *engine.Program)
	LookupLine(file string, line int) (string, bool)
	LookupContent(file string) (string, bool)
}

type sourceRegistry struct {
	engine *engine.SourceRegistry
}

func NewSourceRegistryFromEngine(reg *engine.SourceRegistry) SourceRegistry {
	return &sourceRegistry{engine: reg}
}

func (s *sourceRegistry) Register(file, source string) {
	if s == nil || s.engine == nil {
		return
	}
	s.engine.Register(file, source)
}

func (s *sourceRegistry) RegisterPrecompiled(code *engine.Program) {
	if s == nil || s.engine == nil {
		return
	}
	s.engine.RegisterPrecompiled(code)
}

func (s *sourceRegistry) LookupLine(file string, line int) (string, bool) {
	if s == nil || s.engine == nil {
		return "", false
	}
	return s.engine.LookupLine(file, line)
}

func (s *sourceRegistry) LookupContent(file string) (string, bool) {
	if s == nil || s.engine == nil {
		return "", false
	}
	return s.engine.LookupContent(file)
}
