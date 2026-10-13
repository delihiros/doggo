// compiler_ops.go は算術・比較・論理演算子のコンパイルを担当します。
package compiler

import (
	"doggo/internal/compiler/sexpr"
	"doggo/internal/opcode"
	"doggo/internal/types/atom"
)

// ----------------------------------------------------------------------------
// 算術演算子
// ----------------------------------------------------------------------------

func compileAddForm(c *Compiler, list *sexpr.List) error {
	return c.compileArithmeticOpForm(list, opcode.OpAdd, 0)
}

func compileMulForm(c *Compiler, list *sexpr.List) error {
	return c.compileArithmeticOpForm(list, opcode.OpMul, 1)
}

func compileDivForm(c *Compiler, list *sexpr.List) error {
	return c.compileArithmeticOpForm(list, opcode.OpDiv, 1)
}

func compileModForm(c *Compiler, list *sexpr.List) error {
	return c.compileBinaryOpForm(list, opcode.OpMod)
}

func (c *Compiler) compileArithmeticOpForm(list *sexpr.List, op opcode.Opcode, identity int64) error {
	numArgs := len(list.Items) - 1
	if numArgs > 2 {
		return c.compileArithmeticCallForm(list)
	}

	switch numArgs {
	case 0:
		c.emit(opcode.OpConst, c.addConstant(atom.NewNumberInt(identity)))
		return nil
	case 1:
		if op == opcode.OpDiv {
			c.emit(opcode.OpConst, c.addConstant(atom.NewNumberInt(1)))
			if err := c.CompileForm(list.Items[1]); err != nil {
				return err
			}
			c.emit(opcode.OpDiv)
			return nil
		}
		return c.CompileForm(list.Items[1])
	default:
		if err := c.CompileForm(list.Items[1]); err != nil {
			return err
		}
		if err := c.CompileForm(list.Items[2]); err != nil {
			return err
		}
		c.emit(op)

		return nil
	}
}

func (c *Compiler) compileSubtractForm(list *sexpr.List) error {
	numArgs := len(list.Items) - 1
	if numArgs > 2 {
		return c.compileArithmeticCallForm(list)
	}

	switch numArgs {
	case 0:
		return c.newCompileError("-: wrong number of arguments (0)")
	case 1:
		if err := c.CompileForm(list.Items[1]); err != nil {
			return err
		}
		c.emit(opcode.OpNeg)
		return nil
	default:
		if err := c.CompileForm(list.Items[1]); err != nil {
			return err
		}
		if err := c.CompileForm(list.Items[2]); err != nil {
			return err
		}
		c.emit(opcode.OpSub)

		return nil
	}
}

func (c *Compiler) compileArithmeticCallForm(list *sexpr.List) error {
	for _, item := range list.Items {
		if err := c.CompileForm(item); err != nil {
			return err
		}
	}
	c.emit(opcode.OpCall, len(list.Items)-1)
	return nil
}

// ----------------------------------------------------------------------------
// 比較演算子
// ----------------------------------------------------------------------------

func compileEqForm(c *Compiler, list *sexpr.List) error {
	return c.compileBinaryOpForm(list, opcode.OpEq)
}

func compileNotEqForm(c *Compiler, list *sexpr.List) error {
	return c.compileBinaryOpForm(list, opcode.OpNotEq)
}

func compileGTForm(c *Compiler, list *sexpr.List) error {
	return c.compileBinaryOpForm(list, opcode.OpGT)
}

func compileGTEForm(c *Compiler, list *sexpr.List) error {
	return c.compileBinaryOpForm(list, opcode.OpGTE)
}

func compileLTForm(c *Compiler, list *sexpr.List) error {
	return c.compileBinaryOpForm(list, opcode.OpLT)
}

func compileLTEForm(c *Compiler, list *sexpr.List) error {
	return c.compileBinaryOpForm(list, opcode.OpLTE)
}

func (c *Compiler) compileBinaryOpForm(list *sexpr.List, op opcode.Opcode) error {
	if len(list.Items) != 3 {
		return c.compileArithmeticCallForm(list)
	}

	if err := c.CompileForm(list.Items[1]); err != nil {
		return err
	}
	if err := c.CompileForm(list.Items[2]); err != nil {
		return err
	}

	c.emit(op)
	return nil
}

// ----------------------------------------------------------------------------
// 論理演算子
// ----------------------------------------------------------------------------

func (c *Compiler) compileNotForm(list *sexpr.List) error {
	if len(list.Items) != 2 {
		return c.newCompileError("not requires exactly 1 argument")
	}

	if err := c.CompileForm(list.Items[1]); err != nil {
		return err
	}
	c.emit(opcode.OpNot)
	return nil
}

func (c *Compiler) compileAndForm(list *sexpr.List) error {
	return c.compileShortCircuitForm(list, opcode.OpTrue, opcode.OpJumpNotTruthy)
}

func (c *Compiler) compileOrForm(list *sexpr.List) error {
	return c.compileShortCircuitForm(list, opcode.OpNil, opcode.OpJumpTruthy)
}

func (c *Compiler) compileShortCircuitForm(list *sexpr.List, emptyOp, jumpOp opcode.Opcode) error {
	if len(list.Items) == 1 {
		c.emit(emptyOp)
		return nil
	}

	if len(list.Items) == 2 {
		return c.CompileForm(list.Items[1])
	}

	var jumpPositions []int
	for i := 1; i < len(list.Items)-1; i++ {
		if err := c.CompileForm(list.Items[i]); err != nil {
			return err
		}
		c.emit(opcode.OpDup)
		jumpPos := c.emit(jumpOp, 9999)
		jumpPositions = append(jumpPositions, jumpPos)
		c.emit(opcode.OpPop)
	}

	if err := c.CompileForm(list.Items[len(list.Items)-1]); err != nil {
		return err
	}

	afterPos := len(c.currentInstructions())
	for _, jumpPos := range jumpPositions {
		c.changeOperand(jumpPos, afterPos)
	}

	return nil
}
