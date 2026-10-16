package compiler

import (
	"strings"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/interop"
	"doggo/internal/opcode"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func (c *Compiler) compileImportForm(list *sexpr.List) error {
	if len(list.Items) != 2 {
		return c.newCompileError("import requires exactly 1 argument (package name)")
	}
	pkgNameStr, ok := list.Items[1].(sexpr.String)
	if !ok {
		return c.newCompileError("import expects a string literal, got %T", list.Items[1])
	}
	pkgName := pkgNameStr.Value

	constIndex := c.addConstant(atom.String(pkgName))
	c.emit(opcode.OpImport, constIndex)

	exports, found := interop.LookupPackage(pkgName)
	if !found {
		return nil
	}

	shortPkgName := pkgName
	if idx := strings.LastIndex(pkgName, "/"); idx >= 0 {
		shortPkgName = pkgName[idx+1:]
	}

	for name := range exports {
		qualifiedName := shortPkgName + "/" + name
		symbol := c.symbolTable.DefineGlobalInNamespace("", qualifiedName)
		c.emit(opcode.OpDup)
		keyIdx := c.addConstant(types.NewSymbol(name))
		c.emit(opcode.OpConst, keyIdx)
		c.emit(opcode.OpIndex)
		c.emit(opcode.OpSetGlobal, symbol.Index)
	}

	return nil
}

func (c *Compiler) compileDotForm(list *sexpr.List) error {
	if len(list.Items) < 3 {
		return c.newCompileError(". requires at least 2 arguments (object and method/field name)")
	}
	if err := c.CompileForm(list.Items[1]); err != nil {
		return err
	}

	methodName, ok := formSymbolName(list.Items[2])
	if !ok {
		return c.newCompileError(". expects a symbol as method/field name, got %T", list.Items[2])
	}
	if len(methodName) > 1 && methodName[0] == '.' {
		methodName = methodName[1:]
	}

	numArgs := len(list.Items) - 3
	for i := 3; i < len(list.Items); i++ {
		if err := c.CompileForm(list.Items[i]); err != nil {
			return err
		}
	}

	methodNameIndex := c.addConstant(atom.String(methodName))
	c.emit(opcode.OpDot, methodNameIndex, numArgs)
	return nil
}

func (c *Compiler) compileNewForm(list *sexpr.List) error {
	if len(list.Items) < 2 {
		return c.newCompileError("new requires at least 1 argument (type name)")
	}

	var typeName atom.String
	switch t := list.Items[1].(type) {
	case sexpr.String:
		typeName = atom.String(t.Value)
	case sexpr.Symbol:
		typeName = atom.String(strings.ReplaceAll(t.Name, "/", "."))
	case sexpr.ResolvedSymbol:
		typeName = atom.String(strings.ReplaceAll(t.Name, "/", "."))
	default:
		return c.newCompileError("new expects a string literal or symbol as type name, got %T", list.Items[1])
	}

	numArgs := len(list.Items) - 2
	for i := 2; i < len(list.Items); i++ {
		if err := c.CompileForm(list.Items[i]); err != nil {
			return err
		}
	}

	typeNameIndex := c.addConstant(typeName)
	c.emit(opcode.OpNew, typeNameIndex, numArgs)
	return nil
}

func (c *Compiler) compileGoForm(list *sexpr.List) error {
	return c.compileScopedBodyOperation(list, "go", opcode.OpGo)
}

func (c *Compiler) compileDosyncForm(list *sexpr.List) error {
	return c.compileScopedBodyOperation(list, "dosync", opcode.OpDosync)
}

func (c *Compiler) compileScopedBodyOperation(list *sexpr.List, formName string, op opcode.Opcode) error {
	if len(list.Items) < 2 {
		return c.newCompileError("%s requires at least one expression", formName)
	}

	var body sexpr.Form
	if len(list.Items) == 2 {
		body = list.Items[1]
	} else {
		doItems := make([]sexpr.Form, 0, len(list.Items))
		doItems = append(doItems, sexpr.NewSymbol("do", list.Span(), nil))
		doItems = append(doItems, list.Items[1:]...)
		body = sexpr.NewList(doItems, list.Span(), nil)
	}

	c.enterScope()
	if err := c.CompileForm(body); err != nil {
		_, _, _ = c.leaveScope()
		return err
	}
	c.emit(opcode.OpReturnValue)

	freeSymbols := c.symbolTable.FreeSymbols
	numLocals := c.symbolTable.NumLocals()
	instructions, lines, columns := c.leaveScope()
	for _, s := range freeSymbols {
		c.captureSymbol(s)
	}

	compiledFn := newCompiledFunction(
		bytecodeMetadata{SourceFile: c.EffectiveSourceFile(), Lines: lines, Columns: columns},
		instructions,
		numLocals,
		0,
		false,
		c.symbolTable.CurrentNamespace(),
		"",
		nil,
	)
	fnIndex := c.addConstant(compiledFn)
	c.emit(opcode.OpClosure, fnIndex, len(freeSymbols))
	c.emit(op)
	return nil
}

func (c *Compiler) compileReifyForm(list *sexpr.List) error {
	if len(list.Items) != 3 {
		return c.newCompileError("reify requires exactly 2 arguments (interface target and implementation map)")
	}

	if _, ok := formSymbolName(list.Items[1]); !ok {
		return c.newCompileError("reify expects a symbol as first argument, got %T", list.Items[1])
	}
	if err := c.CompileForm(list.Items[1]); err != nil {
		return err
	}
	if err := c.CompileForm(list.Items[2]); err != nil {
		return err
	}
	c.emit(opcode.OpReify)
	return nil
}
