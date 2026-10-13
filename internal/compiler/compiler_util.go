package compiler

import (
	"doggo/internal/opcode"
	"doggo/internal/types"
)

// emit は命令を発行します。
func (c *Compiler) emit(op opcode.Opcode, operands ...int) int {
	ins := opcode.EncodeInstruction(op, operands...)
	pos := c.addInstruction(ins)

	// 各バイトに対して行番号を記録
	c.debugMeta.RecordInstructions(len(ins))

	c.setLastInstruction(op, pos)

	return pos
}

// addInstruction は命令列に命令を追加します。
func (c *Compiler) addInstruction(ins []byte) int {
	posNewInstruction := len(c.currentInstructions())
	updatedInstructions := append(c.currentInstructions(), ins...)
	c.scopes[c.scopeIndex].instructions = updatedInstructions
	return posNewInstruction
}

// setLastInstruction は最後に発行した命令を記録します。
func (c *Compiler) setLastInstruction(op opcode.Opcode, pos int) {
	previous := c.scopes[c.scopeIndex].lastInstruction
	last := EmittedInstruction{Opcode: op, Position: pos}
	c.scopes[c.scopeIndex].previousInstruction = previous
	c.scopes[c.scopeIndex].lastInstruction = last
}

// lastInstructionIs は最後の命令が指定されたOpcodeかどうかを確認します。
func (c *Compiler) lastInstructionIs(op opcode.Opcode) bool {
	if len(c.currentInstructions()) == 0 {
		return false
	}
	return c.scopes[c.scopeIndex].lastInstruction.Opcode == op
}

// replaceLastPopWithReturn は最後のPopをReturnValueに置き換えます。
func (c *Compiler) replaceLastPopWithReturn() {
	lastPos := c.scopes[c.scopeIndex].lastInstruction.Position
	c.replaceInstruction(lastPos, opcode.EncodeInstruction(opcode.OpReturnValue))
	c.scopes[c.scopeIndex].lastInstruction.Opcode = opcode.OpReturnValue
}

// replaceInstruction は指定位置の命令を置き換えます。
func (c *Compiler) replaceInstruction(pos int, newInstruction []byte) {
	ins := c.currentInstructions()
	for i := 0; i < len(newInstruction); i++ {
		ins[pos+i] = newInstruction[i]
	}
}

// changeOperand は指定位置の命令のオペランドを変更します。
func (c *Compiler) changeOperand(opPos int, operand int) {
	op := opcode.Opcode(c.currentInstructions()[opPos])
	newInstruction := opcode.EncodeInstruction(op, operand)
	c.replaceInstruction(opPos, newInstruction)
}

// addConstant は定数プールに値を追加し、インデックスを返します。
func (c *Compiler) addConstant(obj types.Value) int {
	return c.constants.Add(obj)
}

// currentInstructions は現在のスコープの命令列を返します。
func (c *Compiler) currentInstructions() opcode.Instructions {
	return c.scopes[c.scopeIndex].instructions
}

// enterScope は新しいスコープに入ります。
func (c *Compiler) enterScope() {
	c.debugMeta.EnterScope()
	scope := CompilationScope{
		instructions:        opcode.Instructions{},
		lastInstruction:     EmittedInstruction{},
		previousInstruction: EmittedInstruction{},
	}
	c.scopes = append(c.scopes, scope)
	c.scopeIndex++
	c.symbolTable = NewEnclosedSymbolTable(c.symbolTable)
}

// leaveScope は現在のスコープを抜けて命令列と位置情報を返します。
func (c *Compiler) leaveScope() (opcode.Instructions, []int, []int) {
	instructions := c.currentInstructions()
	table := c.debugMeta.LeaveScope()
	c.scopes = c.scopes[:len(c.scopes)-1]
	c.scopeIndex--
	c.symbolTable = c.symbolTable.Outer
	return instructions, table.Lines, table.Columns
}

// withBlockScope executes fn in a block scope that shares the surrounding
// frame's locals without converting outer locals into free variables.
func (c *Compiler) withBlockScope(fn func() error) error {
	outer := c.symbolTable
	c.symbolTable = NewBlockSymbolTable(outer)
	defer func() {
		c.symbolTable.LeaveBlockScope()
		c.symbolTable = outer
	}()
	return fn()
}

// loadSymbol はシンボルをスタックにロードします。
func (c *Compiler) loadSymbol(s Symbol) {
	switch s.Scope {
	case GlobalScope, ProtectedGlobalScope:
		c.emit(opcode.OpGetGlobal, s.Index)
	case LocalScope:
		c.emit(opcode.OpGetLocal, s.Index)
	case FreeScope:
		c.emit(opcode.OpGetFree, s.Index)
	case BuiltinScope:
		c.emit(opcode.OpGetBuiltin, s.Index)
	}
}
