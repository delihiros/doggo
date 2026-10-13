package emit

import "doggo/internal/compiler/hir"

type scope struct {
	namespace string
	aliases   map[string]string
}

func newScope(prog *hir.Program) scope {
	if prog == nil {
		return scope{}
	}
	aliases := make(map[string]string, len(prog.Aliases))
	for alias, namespace := range prog.Aliases {
		aliases[alias] = namespace
	}
	return scope{
		namespace: prog.Namespace,
		aliases:   aliases,
	}
}
