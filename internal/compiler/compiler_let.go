package compiler

import (
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
)

func (c *Compiler) compileLetForm(list *sexpr.List) error {
	if len(list.Items) < 2 {
		return c.newCompileError("let requires bindings vector")
	}

	bindings, ok := list.Items[1].(*sexpr.Vector)
	if !ok {
		return c.newCompileError("let bindings must be a vector")
	}
	if len(bindings.Items)%2 != 0 {
		return c.newCompileError("let bindings must have even number of forms")
	}

	return c.withBlockScope(func() error {
		for i := 0; i < len(bindings.Items); i += 2 {
			if err := c.CompileForm(bindings.Items[i+1]); err != nil {
				return err
			}
			if err := c.compileDestructuringBindForm(bindings.Items[i]); err != nil {
				return err
			}
		}
		return c.compileFormBody(list.Items[2:])
	})
}

func (c *Compiler) compileLetfnForm(list *sexpr.List) error {
	if len(list.Items) < 2 {
		return c.newCompileError("letfn requires function definitions vector")
	}

	fnDefs, ok := list.Items[1].(*sexpr.Vector)
	if !ok {
		return c.newCompileError("letfn function definitions must be a vector")
	}

	type fnInfo struct {
		name   string
		symbol Symbol
		def    *sexpr.List
	}

	return c.withBlockScope(func() error {
		fnInfos := make([]fnInfo, 0, len(fnDefs.Items))
		for i := 0; i < len(fnDefs.Items); i++ {
			fnDef, ok := fnDefs.Items[i].(*sexpr.List)
			if !ok {
				return c.newCompileError("letfn function definition must be a list")
			}
			if len(fnDef.Items) < 2 {
				return c.newCompileError("letfn function definition requires name and parameters")
			}

			fnName, ok := formSymbolName(fnDef.Items[0])
			if !ok {
				return c.newCompileError("letfn function name must be a symbol")
			}

			sym, err := c.symbolTable.DefineChecked(fnName)
			if err != nil {
				return c.newCompileError("%s", err)
			}
			fnInfos = append(fnInfos, fnInfo{name: fnName, symbol: sym, def: fnDef})
		}

		for _, info := range fnInfos {
			c.emit(opcode.OpNil)
			c.emit(opcode.OpBox)
			c.setSourceSpan(info.def.Items[0].Span())
			c.emitSetSymbol(info.symbol)
		}

		for _, info := range fnInfos {
			c.setSourceSpan(info.def.Span())
			if err := c.compileFnFormDecl(info.name, info.def.Items[1:]); err != nil {
				return err
			}
			c.setSourceSpan(info.def.Items[0].Span())
			c.emitSetSymbol(info.symbol)
		}

		return c.compileFormBody(list.Items[2:])
	})
}

func (c *Compiler) compileLoopForm(list *sexpr.List) error {
	if len(list.Items) < 2 {
		return c.newCompileError("loop requires bindings vector")
	}

	bindings, ok := list.Items[1].(*sexpr.Vector)
	if !ok {
		return c.newCompileError("loop bindings must be a vector")
	}
	if len(bindings.Items)%2 != 0 {
		return c.newCompileError("loop bindings must have even number of forms")
	}

	return c.withBlockScope(func() error {
		loopBindings := make([]Symbol, 0, len(bindings.Items)/2)
		for i := 0; i < len(bindings.Items); i += 2 {
			symName, ok := formSymbolName(bindings.Items[i])
			if !ok {
				return c.newCompileError("loop binding name must be a symbol")
			}

			if err := c.CompileForm(bindings.Items[i+1]); err != nil {
				return err
			}

			symbol, err := c.symbolTable.DefineChecked(symName)
			if err != nil {
				return c.newCompileError("%s", err)
			}
			loopBindings = append(loopBindings, symbol)
			c.setSourceSpan(bindings.Items[i].Span())
			c.emitSetSymbol(symbol)
		}

		loopStartPos := len(c.currentInstructions())
		c.loopStack = append(c.loopStack, LoopContext{
			StartPos: loopStartPos,
			Bindings: loopBindings,
		})
		defer func() {
			c.loopStack = c.loopStack[:len(c.loopStack)-1]
		}()

		return c.compileFormBody(list.Items[2:])
	})
}

func (c *Compiler) compileRecurForm(list *sexpr.List) error {
	if len(c.loopStack) == 0 {
		return c.newCompileError("recur must be inside a loop or fn")
	}
	if len(c.loopStack) <= c.scopes[c.scopeIndex].catchFinallyLoopDepth {
		return c.newCompileError("recur cannot exit a catch with finally")
	}

	loopCtx := c.loopStack[len(c.loopStack)-1]
	numArgs := len(list.Items) - 1
	if numArgs != len(loopCtx.Bindings) {
		return c.newCompileError("recur expects %d arguments, got %d", len(loopCtx.Bindings), numArgs)
	}

	for i := 1; i < len(list.Items); i++ {
		if err := c.CompileForm(list.Items[i]); err != nil {
			return err
		}
	}

	for i := len(loopCtx.Bindings) - 1; i >= 0; i-- {
		c.emitSetSymbol(loopCtx.Bindings[i])
	}

	c.emit(opcode.OpJump, loopCtx.StartPos)
	return nil
}

func (c *Compiler) compileFormBody(forms []sexpr.Form) error {
	if len(forms) == 0 {
		c.emit(opcode.OpNil)
		return nil
	}

	for i, form := range forms {
		if err := c.CompileForm(form); err != nil {
			return err
		}
		if i < len(forms)-1 {
			c.emit(opcode.OpPop)
		}
	}

	return nil
}
