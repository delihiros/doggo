package compiler

import (
	"sync"

	"doggo/internal/compiler/debugmeta"
	"doggo/internal/compiler/emit"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
)

type ResolvedSpecialFormCompiler func(c *Compiler, list *sexpr.List) error

var (
	resolvedSpecialForms     map[string]ResolvedSpecialFormCompiler
	resolvedSpecialFormsOnce sync.Once
)

func initResolvedSpecialForms() {
	resolvedSpecialForms = map[string]ResolvedSpecialFormCompiler{
		"quote":            (*Compiler).compileQuoteForm,
		"quasiquote":       (*Compiler).compileQuasiquoteForm,
		"unquote":          compileFormUnquoteError,
		"unquote-splicing": compileFormUnquoteError,
		"splice-unquote":   compileFormUnquoteError,
		"if":               (*Compiler).compileIfForm,
		"do":               (*Compiler).compileDoForm,
		"let":              (*Compiler).compileLetForm,
		"letfn":            (*Compiler).compileLetfnForm,
		"loop":             (*Compiler).compileLoopForm,
		"recur":            (*Compiler).compileRecurForm,
		"def":              (*Compiler).compileDefForm,
		"defmacro":         (*Compiler).compileDefMacroForm,
		"fn":               (*Compiler).compileFnForm,
		"+":                compileAddForm,
		"-":                (*Compiler).compileSubtractForm,
		"*":                compileMulForm,
		"/":                compileDivForm,
		"mod":              compileModForm,
		"=":                compileEqForm,
		"not=":             compileNotEqForm,
		">":                compileGTForm,
		">=":               compileGTEForm,
		"<":                compileLTForm,
		"<=":               compileLTEForm,
		"not":              (*Compiler).compileNotForm,
		"and":              (*Compiler).compileAndForm,
		"or":               (*Compiler).compileOrForm,
		"throw":            (*Compiler).compileThrowForm,
		"try":              (*Compiler).compileTryForm,
		"ns":               (*Compiler).compileNsForm,
		"in-ns":            (*Compiler).compileInNsForm,
		"import":           (*Compiler).compileImportForm,
		".":                (*Compiler).compileDotForm,
		"new":              (*Compiler).compileNewForm,
		"reify":            (*Compiler).compileReifyForm,
		"go":               (*Compiler).compileGoForm,
		"dosync":           (*Compiler).compileDosyncForm,
		"defmulti":         (*Compiler).compileDefmultiForm,
		"defmethod":        (*Compiler).compileDefmethodForm,
		"defrecord":        (*Compiler).compileDefrecordForm,
		"deftype":          (*Compiler).compileDeftypeForm,
		"var":              (*Compiler).compileVarForm,
	}
}

func getResolvedSpecialFormCompiler(name string) (ResolvedSpecialFormCompiler, bool) {
	resolvedSpecialFormsOnce.Do(initResolvedSpecialForms)
	compiler, ok := resolvedSpecialForms[name]
	return compiler, ok
}

func compileFormUnquoteError(c *Compiler, list *sexpr.List) error {
	sym, _ := list.Items[0].(sexpr.Symbol)
	return c.newCompileError("%s not in quasiquote", sym.Name)
}

func (c *Compiler) CompileForm(form sexpr.Form) error {
	return emit.CompileForm(c, form)
}

func (c *Compiler) compileResolvedFormSymbol(sym sexpr.ResolvedSymbol) error {
	switch sym.Resolution.Kind {
	case sexpr.BuiltinSymbolResolution:
		c.loadSymbol(Symbol{Name: sym.Name, Scope: BuiltinScope, Index: sym.Resolution.Index})
		return nil
	case sexpr.GlobalSymbolResolution:
		c.loadSymbol(Symbol{Name: sym.Name, Scope: GlobalScope, Index: sym.Resolution.Index})
		return nil
	default:
		symbol, ok := c.symbolTable.Resolve(sym.Name)
		if !ok {
			return c.newCompileError("undefined variable: %s", sym.Name)
		}
		c.loadSymbol(symbol)
		return nil
	}
}

func (c *Compiler) setSourceSpan(sp span.Span) {
	c.debugMeta.SetCurrentSpan(debugmeta.Span{
		File:        sp.File,
		Line:        sp.Start.Line,
		Column:      sp.Start.Column,
		StartOffset: sp.StartOffset,
		EndOffset:   sp.EndOffset,
	})
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
