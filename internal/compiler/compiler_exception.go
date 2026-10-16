package compiler

import (
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
	"doggo/internal/types"
)

func (c *Compiler) compileThrowForm(list *sexpr.List) error {
	if len(list.Items) != 2 {
		return c.newCompileError("throw requires exactly 1 argument")
	}
	if err := c.CompileForm(list.Items[1]); err != nil {
		return err
	}
	c.emit(opcode.OpThrow)
	return nil
}

type catchClauseFormInfo struct {
	clause    *sexpr.List
	varSymbol types.Symbol
	bodyStart int
}

func (c *Compiler) compileTryForm(list *sexpr.List) error {
	if len(list.Items) < 2 {
		return c.newCompileError("try requires body")
	}

	var catchClauseForms []*sexpr.List
	var finallyClause *sexpr.List
	bodyEndIdx := len(list.Items)

	for i := len(list.Items) - 1; i >= 1; i-- {
		item := list.Items[i]
		itemList, ok := item.(*sexpr.List)
		if !ok || len(itemList.Items) == 0 {
			break
		}
		symName, ok := formSymbolName(itemList.Items[0])
		if !ok {
			break
		}
		switch symName {
		case "catch":
			catchClauseForms = append(catchClauseForms, itemList)
			bodyEndIdx = i
		case "finally":
			if finallyClause != nil {
				return c.newCompileError("multiple finally clauses not supported")
			}
			finallyClause = itemList
			bodyEndIdx = i
		default:
			goto endLoop
		}
	}
endLoop:

	if len(catchClauseForms) == 0 && finallyClause == nil {
		return c.newCompileError("try requires catch or finally clause")
	}
	if len(catchClauseForms) > 1 {
		return c.newCompileError("try supports exactly one catch clause")
	}
	var catchClauses []catchClauseFormInfo
	if len(catchClauseForms) == 1 {
		info, err := c.parseCatchClauseForm(catchClauseForms[0])
		if err != nil {
			return err
		}
		catchClauses = append(catchClauses, info)
	}

	body := list.Items[1:bodyEndIdx]
	if len(body) == 0 {
		return c.newCompileError("try requires body")
	}

	tryPos := c.emit(opcode.OpTry, 9999)
	for i, expr := range body {
		if err := c.CompileForm(expr); err != nil {
			return err
		}
		if i < len(body)-1 {
			c.emit(opcode.OpPop)
		}
	}

	c.emit(opcode.OpEndTry)
	if finallyClause != nil {
		if err := c.compileFinallyPreservingStackResultForm("try_result", finallyClause); err != nil {
			return err
		}
	}

	jumpToEndPos := c.emit(opcode.OpJump, 9999)
	handlerStartPos := len(c.currentInstructions())
	c.changeOperand(tryPos, handlerStartPos)

	if len(catchClauses) > 0 {
		if err := c.compileCatchClausesForm(catchClauses, finallyClause); err != nil {
			return err
		}
	} else {
		if err := c.withBlockScope(func() error {
			excSym, err := c.defineHiddenTemp("exception")
			if err != nil {
				return err
			}
			c.emitSetSymbol(excSym)
			return c.compileRethrowWithFinallyForm(excSym, finallyClause)
		}); err != nil {
			return err
		}
	}

	afterAllPos := len(c.currentInstructions())
	c.changeOperand(jumpToEndPos, afterAllPos)
	return nil
}

func (c *Compiler) parseCatchClauseForm(clause *sexpr.List) (catchClauseFormInfo, error) {
	if len(clause.Items) < 3 {
		return catchClauseFormInfo{}, c.newCompileError("catch requires a binding vector and body")
	}

	binding, ok := clause.Items[1].(*sexpr.Vector)
	if !ok || len(binding.Items) != 1 {
		return catchClauseFormInfo{}, c.newCompileError("catch requires exactly one symbol in its binding vector")
	}
	symName, ok := formSymbolName(binding.Items[0])
	if !ok {
		return catchClauseFormInfo{}, c.newCompileError("catch binding must be a symbol")
	}

	return catchClauseFormInfo{
		clause:    clause,
		varSymbol: types.Symbol(symName),
		bodyStart: 2,
	}, nil
}

func (c *Compiler) compileCatchClausesForm(catches []catchClauseFormInfo, finallyClause *sexpr.List) error {
	return c.withBlockScope(func() error {
		excSym, err := c.defineHiddenTemp("exception")
		if err != nil {
			return err
		}
		c.emitSetSymbol(excSym)
		return c.compileCatchHandlerForm(catches[0], excSym, finallyClause)
	})
}

func (c *Compiler) compileCatchHandlerForm(info catchClauseFormInfo, excSym Symbol, finallyClause *sexpr.List) error {
	return c.withBlockScope(func() error {
		var resultSym Symbol
		var catchTryPos int
		if finallyClause != nil {
			var err error
			resultSym, err = c.defineHiddenTemp("catch_result")
			if err != nil {
				return err
			}
			// The original try handler has already been removed on entry to catch.
			catchTryPos = c.emit(opcode.OpTry, 9999)
		}

		if err := c.withBlockScope(func() error {
			if finallyClause != nil {
				scopeIndex := c.scopeIndex
				previousDepth := c.scopes[scopeIndex].catchFinallyLoopDepth
				c.scopes[scopeIndex].catchFinallyLoopDepth = len(c.loopStack)
				defer func() {
					c.scopes[scopeIndex].catchFinallyLoopDepth = previousDepth
				}()
			}
			symbol, err := c.symbolTable.DefineChecked(string(info.varSymbol))
			if err != nil {
				return c.newCompileError("%s", err)
			}
			c.loadSymbol(excSym)
			c.emitSetSymbol(symbol)
			return c.compileCatchBodyForm(info, resultSym)
		}); err != nil {
			return err
		}

		if finallyClause == nil {
			return nil
		}

		// Finally must run outside this guard so its own exception propagates once.
		c.emit(opcode.OpEndTry)
		if err := c.compileFinallyBodyForm(finallyClause); err != nil {
			return err
		}
		c.emit(opcode.OpPop)
		c.loadSymbol(resultSym)
		jumpToEndPos := c.emit(opcode.OpJump, 9999)

		c.changeOperand(catchTryPos, len(c.currentInstructions()))
		c.emitSetSymbol(excSym)
		if err := c.compileRethrowWithFinallyForm(excSym, finallyClause); err != nil {
			return err
		}
		c.changeOperand(jumpToEndPos, len(c.currentInstructions()))
		return nil
	})
}

func (c *Compiler) compileCatchBodyForm(info catchClauseFormInfo, resultSym Symbol) error {
	for i := info.bodyStart; i < len(info.clause.Items); i++ {
		err := c.CompileForm(info.clause.Items[i])
		if err != nil {
			return err
		}
		if i < len(info.clause.Items)-1 {
			c.emit(opcode.OpPop)
		}
	}

	if resultSym.Scope != "" {
		c.emitSetSymbol(resultSym)
	}

	return nil
}

func (c *Compiler) compileRethrowWithFinallyForm(excSym Symbol, finallyClause *sexpr.List) error {
	if finallyClause != nil {
		if err := c.compileFinallyBodyForm(finallyClause); err != nil {
			return err
		}
		c.emit(opcode.OpPop)
	}

	c.loadSymbol(excSym)
	c.emit(opcode.OpThrow)
	return nil
}

func (c *Compiler) compileFinallyBodyForm(finallyClause *sexpr.List) error {
	if len(finallyClause.Items) < 2 {
		c.emit(opcode.OpNil)
		return nil
	}

	for i := 1; i < len(finallyClause.Items); i++ {
		err := c.CompileForm(finallyClause.Items[i])
		if err != nil {
			return err
		}
		if i < len(finallyClause.Items)-1 {
			c.emit(opcode.OpPop)
		}
	}
	return nil
}

func (c *Compiler) compileFinallyPreservingStackResultForm(prefix string, finallyClause *sexpr.List) error {
	return c.withBlockScope(func() error {
		resultSym, err := c.defineHiddenTemp(prefix)
		if err != nil {
			return err
		}
		c.emitSetSymbol(resultSym)
		if err := c.compileFinallyBodyForm(finallyClause); err != nil {
			return err
		}
		c.emit(opcode.OpPop)
		c.loadSymbol(resultSym)
		return nil
	})
}

func (c *Compiler) defineHiddenTemp(prefix string) (Symbol, error) {
	symbol, err := c.symbolTable.DefineTempChecked(gensym(prefix))
	if err != nil {
		return Symbol{}, c.newCompileError("%s", err)
	}
	return symbol, nil
}
