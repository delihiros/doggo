package emit

import (
	"fmt"

	"doggo/internal/compiler/hir"
)

type state[T any] struct {
	backend Backend[T]
}

func newState[T any](cfg Config[T]) state[T] {
	return state[T]{
		backend: cfg.Backend,
	}
}

func (s state[T]) applyScope(namespace string, aliases map[string]string) error {
	if s.backend == nil {
		return fmt.Errorf("emit backend config missing Backend")
	}
	return s.backend.ApplyScope(namespace, aliases)
}

func (s state[T]) emitNode(node hir.Node) error {
	if s.backend == nil {
		return fmt.Errorf("emit backend config missing Backend")
	}
	return s.backend.EmitNode(node)
}

func (s state[T]) finish() (T, error) {
	var zero T
	if s.backend == nil {
		return zero, fmt.Errorf("emit backend config missing Backend")
	}
	return s.backend.Finish()
}
