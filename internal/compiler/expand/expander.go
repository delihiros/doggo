package expand

import (
	"fmt"

	"doggo/internal/compiler/sexpr"
)

type Macro func([]sexpr.Form) (sexpr.Form, error)

type RuntimeMacroHandle any

type RuntimeMacroExpander interface {
	ExpandRuntimeMacro(handle RuntimeMacroHandle, args []sexpr.Form) (sexpr.Form, error)
}

type Expander struct {
	LookupMacro        func(sexpr.Symbol) (Macro, bool)
	LookupRuntimeMacro func(sexpr.Symbol) (RuntimeMacroHandle, bool)
	RuntimeMacros      RuntimeMacroExpander
}

func (e Expander) Macroexpand1(form sexpr.Form) (sexpr.Form, error) {
	list, ok := form.(*sexpr.List)
	if !ok || len(list.Items) == 0 {
		return form, nil
	}

	sym, ok := list.Items[0].(sexpr.Symbol)
	if !ok {
		return form, nil
	}

	if e.LookupRuntimeMacro != nil {
		handle, ok := e.LookupRuntimeMacro(sym)
		if ok {
			if e.RuntimeMacros == nil {
				return nil, fmt.Errorf("runtime macro expander not set")
			}
			return e.RuntimeMacros.ExpandRuntimeMacro(handle, list.Items[1:])
		}
	}

	if e.LookupMacro == nil {
		return form, nil
	}

	macro, ok := e.LookupMacro(sym)
	if !ok {
		return form, nil
	}

	return macro(list.Items[1:])
}
