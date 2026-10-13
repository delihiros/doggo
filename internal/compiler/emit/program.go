package emit

import "doggo/internal/compiler/hir"

func Program[T any](prog *hir.Program, cfg Config[T]) (T, error) {
	return NewEmitter(cfg).Program(prog)
}

func (e *Emitter[T]) Program(prog *hir.Program) (T, error) {
	var zero T
	if prog == nil {
		return e.state.finish()
	}

	scope := newScope(prog)
	if err := e.state.applyScope(scope.namespace, scope.aliases); err != nil {
		return zero, err
	}
	for _, node := range prog.Forms {
		if node == nil {
			continue
		}
		if err := e.state.emitNode(node); err != nil {
			return zero, err
		}
	}
	return e.state.finish()
}
