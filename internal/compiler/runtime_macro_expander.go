package compiler

import (
	"fmt"

	"doggo/internal/compiler/expand"
	"doggo/internal/compiler/macroconvert"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/types"
)

type runtimeMacroExpander struct {
	evaluator MacroEvaluator
	lookup    func(sexpr.Symbol) (*Closure, bool)
}

func (e runtimeMacroExpander) ExpandRuntimeMacro(handle expand.RuntimeMacroHandle, args []sexpr.Form) (sexpr.Form, error) {
	closure, err := e.resolveHandle(handle)
	if err != nil {
		return nil, err
	}
	if e.evaluator == nil {
		return nil, fmt.Errorf("macro evaluator not set")
	}

	runtimeArgs, err := macroArgsToValues(args)
	if err != nil {
		return nil, err
	}

	expanded, err := e.evaluator(closure, runtimeArgs)
	if err != nil {
		return nil, err
	}

	return macroconvert.ValueToForm(expanded)
}

func (e runtimeMacroExpander) resolveHandle(handle expand.RuntimeMacroHandle) (*Closure, error) {
	switch current := handle.(type) {
	case *Closure:
		if current == nil {
			return nil, fmt.Errorf("runtime macro handle is nil")
		}
		return current, nil
	case sexpr.Symbol:
		if e.lookup == nil {
			return nil, fmt.Errorf("runtime macro lookup not set")
		}
		closure, ok := e.lookup(current)
		if !ok || closure == nil {
			return nil, fmt.Errorf("runtime macro not found: %s", current.Name)
		}
		return closure, nil
	default:
		return nil, fmt.Errorf("unsupported runtime macro handle %T", handle)
	}
}

func macroArgsToValues(args []sexpr.Form) ([]types.Value, error) {
	runtimeArgs := make([]types.Value, 0, len(args))
	for _, arg := range args {
		value, err := macroconvert.FormToValue(arg)
		if err != nil {
			return nil, err
		}
		runtimeArgs = append(runtimeArgs, value)
	}
	return runtimeArgs, nil
}
