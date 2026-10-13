package compiler

import (
	"doggo/internal/compiler/emit"
	"doggo/internal/compiler/expand"
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
)

func (c *Compiler) compileQuoteForm(list *sexpr.List) error {
	if len(list.Items) != 2 {
		return c.newCompileError("quote requires exactly 1 argument")
	}
	plan, err := expand.BuildQuotedLiteralPlan(list.Items[1])
	if err != nil {
		return c.newCompileError("quote literal conversion failed: %v", err)
	}
	quoted, err := emit.MaterializeQuotedLiteral(plan)
	if err != nil {
		return c.newCompileError("quote literal materialization failed: %v", err)
	}
	c.emit(opcode.OpConst, c.addConstant(quoted))
	return nil
}

func (c *Compiler) compileIfForm(list *sexpr.List) error {
	if len(list.Items) < 3 || len(list.Items) > 4 {
		return c.newCompileError("if requires 2 or 3 arguments")
	}

	if err := c.CompileForm(list.Items[1]); err != nil {
		return err
	}

	jumpNotTruthyPos := c.emit(opcode.OpJumpNotTruthy, 9999)

	if err := c.CompileForm(list.Items[2]); err != nil {
		return err
	}

	jumpPos := c.emit(opcode.OpJump, 9999)

	afterConsequence := len(c.currentInstructions())
	c.changeOperand(jumpNotTruthyPos, afterConsequence)

	if len(list.Items) == 4 {
		if err := c.CompileForm(list.Items[3]); err != nil {
			return err
		}
	} else {
		c.emit(opcode.OpNil)
	}

	afterAlternative := len(c.currentInstructions())
	c.changeOperand(jumpPos, afterAlternative)

	return nil
}

func (c *Compiler) compileDoForm(list *sexpr.List) error {
	if len(list.Items) == 1 {
		c.emit(opcode.OpNil)
		return nil
	}

	for i := 1; i < len(list.Items); i++ {
		if err := c.CompileForm(list.Items[i]); err != nil {
			return err
		}
		if i < len(list.Items)-1 {
			c.emit(opcode.OpPop)
		}
	}

	return nil
}
