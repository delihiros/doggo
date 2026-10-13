package services

import (
	"doggo/internal/types"
	"doggo/internal/vm/engine"
)

type BasisServices interface {
	Current() (types.Value, error)
	Initial() (types.Value, error)
}

type basisServices struct {
	engine *engine.BasisServices
}

func NewBasisServicesFromEngine(basis *engine.BasisServices) BasisServices {
	return &basisServices{engine: basis}
}

func (s *basisServices) Current() (types.Value, error) {
	if s == nil || s.engine == nil {
		return nil, nil
	}
	return s.engine.Current()
}

func (s *basisServices) Initial() (types.Value, error) {
	if s == nil || s.engine == nil {
		return nil, nil
	}
	return s.engine.Initial()
}
