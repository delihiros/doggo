package emit

import (
	"strconv"

	"doggo/internal/compiler/debugmeta"
	"doggo/internal/compiler/hir"
	"doggo/internal/compiler/opt"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

type Backend[T any] interface {
	ApplyScope(namespace string, aliases map[string]string) error
	EmitNode(node hir.Node) error
	Finish() (T, error)
}

type Config[T any] struct {
	Backend Backend[T]
}

type Emitter[T any] struct {
	state state[T]
}

func NewEmitter[T any](cfg Config[T]) *Emitter[T] {
	return &Emitter[T]{state: newState(cfg)}
}

type SourceHooks interface {
	SetFormSourceSpan(span.Span)
	EffectiveSourceFile() string
	PushExpansionSource(debugmeta.Span)
	PopExpansionSource()
}

type DiagnosticHooks interface {
	NewCompileError(format string, args ...interface{}) error
}

type FormBackend interface {
	opt.Resolver
	WithCompileEntry(func() error) error
	SourceHooks
	DiagnosticHooks
	EmitConstant(types.Value)
	EmitBool(bool)
	EmitNil()
	CompileSymbolReference(name string) error
	CompileResolvedFormSymbol(sym sexpr.ResolvedSymbol) error
	CompileVectorFormLiteral(node *sexpr.Vector) error
	CompileMapFormLiteral(node *sexpr.Map) error
	CompileSetFormLiteral(node *sexpr.Set) error
	ExpandMacroForm(list *sexpr.List) (sexpr.Form, error)
	CompileResolvedSpecialForm(name string, list *sexpr.List) (bool, error)
	OptimizationConfig() *opt.Config
	EmitCall(argc int)
}

func CompileForm(backend FormBackend, form sexpr.Form) error {
	if backend == nil || form == nil {
		return nil
	}
	return backend.WithCompileEntry(func() error {
		return compileFormNode(backend, form)
	})
}

func compileFormNode(backend FormBackend, form sexpr.Form) error {
	if form == nil {
		return nil
	}
	backend.SetFormSourceSpan(form.Span())

	switch current := form.(type) {
	case sexpr.Number:
		number, err := runtimeNumberFromLiteral(current.Literal)
		if err != nil {
			return backend.NewCompileError("%s", err)
		}
		backend.EmitConstant(number)
	case sexpr.String:
		backend.EmitConstant(atom.NewString(current.Value))
	case sexpr.Regex:
		regex, err := atom.NewRegex(current.Pattern)
		if err != nil {
			return backend.NewCompileError("%s", err)
		}
		backend.EmitConstant(regex)
	case sexpr.Character:
		character, ok := atom.NewCharacterFromLiteral(current.Literal)
		if !ok {
			return backend.NewCompileError("invalid character literal: %s", current.Literal)
		}
		backend.EmitConstant(character)
	case sexpr.Bool:
		backend.EmitBool(current.Value)
	case sexpr.Nil:
		backend.EmitNil()
	case sexpr.Keyword:
		backend.EmitConstant(atom.NewKeyword(current.Name))
	case sexpr.Symbol:
		return backend.CompileSymbolReference(current.Name)
	case sexpr.ResolvedSymbol:
		return backend.CompileResolvedFormSymbol(current)
	case *sexpr.List:
		return compileFormList(backend, current)
	case *sexpr.Vector:
		return backend.CompileVectorFormLiteral(current)
	case *sexpr.Map:
		return backend.CompileMapFormLiteral(current)
	case *sexpr.Set:
		return backend.CompileSetFormLiteral(current)
	default:
		return backend.NewCompileError("unknown form type: %T", form)
	}

	return nil
}

func compileFormList(backend FormBackend, list *sexpr.List) error {
	if list == nil {
		return nil
	}
	if len(list.Items) == 0 {
		backend.EmitNil()
		return nil
	}

	if symName, ok := formSymbolName(list.Items[0]); ok {
		if symName != "defmacro" {
			expanded, err := backend.ExpandMacroForm(list)
			if err != nil {
				return err
			}
			if expanded != nil {
				listSpan := list.Span()
				backend.PushExpansionSource(debugmeta.Span{
					File:        backend.EffectiveSourceFile(),
					Line:        listSpan.Start.Line,
					Column:      listSpan.Start.Column,
					StartOffset: listSpan.StartOffset,
					EndOffset:   listSpan.EndOffset,
				})
				defer backend.PopExpansionSource()
				return CompileForm(backend, expanded)
			}
		}

		if handled, err := backend.CompileResolvedSpecialForm(symName, list); handled || err != nil {
			return err
		}
	}

	if optimized, ok := opt.TryOptimizeFormList(list, backend.OptimizationConfig(), backend); ok {
		return CompileForm(backend, optimized)
	}

	return compileFormFunctionCall(backend, list)
}

func compileFormFunctionCall(backend FormBackend, list *sexpr.List) error {
	if err := CompileForm(backend, list.Items[0]); err != nil {
		return err
	}
	for i := 1; i < len(list.Items); i++ {
		if err := CompileForm(backend, list.Items[i]); err != nil {
			return err
		}
	}
	backend.EmitCall(len(list.Items) - 1)
	return nil
}

func runtimeNumberFromLiteral(literal string) (*atom.Number, error) {
	if intValue, err := strconv.ParseInt(literal, 10, 64); err == nil {
		return atom.NewNumberInt(intValue), nil
	}
	floatValue, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		return nil, err
	}
	return atom.NewNumberFloat(floatValue), nil
}

func formSymbolName(form sexpr.Form) (string, bool) {
	switch current := form.(type) {
	case sexpr.Symbol:
		return current.Name, true
	case sexpr.ResolvedSymbol:
		return current.Name, true
	default:
		return "", false
	}
}
