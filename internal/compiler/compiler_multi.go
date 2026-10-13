package compiler

import (
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
	"doggo/internal/types"
)

func (c *Compiler) compileDefmultiForm(list *sexpr.List) error {
	if len(list.Items) != 3 {
		return c.newCompileError("defmulti requires name and dispatch function")
	}

	nameVal := list.Items[1]
	name, ok := formSymbolName(nameVal)
	if !ok {
		return c.newCompileError("defmulti name must be a symbol, got %T", nameVal)
	}
	if err := c.symbolTable.ValidateVarGlobalAllowed(name); err != nil {
		return c.newCompileError("%s", err)
	}

	meta := c.buildDefMetadata(nameVal, name, list)
	metaIndex := c.addConstant(meta) + 1

	symbol, err := c.symbolTable.DefineVarGlobalChecked(name)
	if err != nil {
		return c.newCompileError("%s", err)
	}
	c.updateGlobalPrivacy(name, meta)

	createMMIdx, ok := c.symbolTable.ResolveBuiltin("create-multimethod")
	if !ok {
		return c.newCompileError("builtin 'create-multimethod' not found")
	}
	c.emit(opcode.OpGetBuiltin, createMMIdx)
	nameSymIdx := c.addConstant(types.Symbol(name))
	c.emit(opcode.OpConst, nameSymIdx)
	if err := c.CompileForm(list.Items[2]); err != nil {
		return err
	}
	c.emit(opcode.OpCall, 2)
	c.emit(opcode.OpDefVar, symbol.Index, metaIndex)
	c.emit(opcode.OpNil)
	return nil
}

func (c *Compiler) compileDefmethodForm(list *sexpr.List) error {
	if len(list.Items) < 4 {
		return c.newCompileError("defmethod requires name, dispatch-value, params, and body")
	}

	nameVal := list.Items[1]
	name, ok := formSymbolName(nameVal)
	if !ok {
		return c.newCompileError("defmethod name must be a symbol, got %T", nameVal)
	}

	addMethodIdx, ok := c.symbolTable.ResolveBuiltin("add-method")
	if !ok {
		return c.newCompileError("builtin 'add-method' not found")
	}
	c.emit(opcode.OpGetBuiltin, addMethodIdx)

	symbol, ok := c.symbolTable.Resolve(name)
	if !ok {
		return c.newCompileError("defmethod: multimethod '%s' not defined", name)
	}
	c.loadSymbol(symbol)

	if err := c.CompileForm(list.Items[2]); err != nil {
		return err
	}

	fnItems := make([]sexpr.Form, 0, len(list.Items)-2)
	fnItems = append(fnItems, sexpr.NewSymbol("fn", list.Span(), nil))
	fnItems = append(fnItems, list.Items[3:]...)
	fnList := sexpr.NewList(fnItems, list.Span(), nil)
	if err := c.CompileForm(fnList); err != nil {
		return err
	}

	c.emit(opcode.OpCall, 3)
	c.emit(opcode.OpPop)
	c.emit(opcode.OpNil)
	return nil
}
