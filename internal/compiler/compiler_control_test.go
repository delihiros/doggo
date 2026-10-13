package compiler

import (
	"testing"

	"doggo/internal/opcode"
)

func TestConditionals(t *testing.T) {
	tests := []struct {
		source               string
		expectedConstants    []interface{}
		expectedInstructions []opcode.Instructions
	}{
		{
			source:            "(if true 10 20)",
			expectedConstants: []interface{}{10, 20},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpTrue),              // 0000
				opcode.EncodeInstruction(opcode.OpJumpNotTruthy, 10), // 0001
				opcode.EncodeInstruction(opcode.OpConst, 0),          // 0004
				opcode.EncodeInstruction(opcode.OpJump, 13),          // 0007
				opcode.EncodeInstruction(opcode.OpConst, 1),          // 0010
			},
		},
		{
			source:            "(if true 10)",
			expectedConstants: []interface{}{10},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpTrue),              // 0000
				opcode.EncodeInstruction(opcode.OpJumpNotTruthy, 10), // 0001
				opcode.EncodeInstruction(opcode.OpConst, 0),          // 0004
				opcode.EncodeInstruction(opcode.OpJump, 11),          // 0007
				opcode.EncodeInstruction(opcode.OpNil),               // 0010
			},
		},
	}

	for _, tt := range tests {
		runCompilerTest(t, tt.source, tt.expectedConstants, tt.expectedInstructions)
	}
}

func TestDoExpression(t *testing.T) {
	tests := []struct {
		source               string
		expectedConstants    []interface{}
		expectedInstructions []opcode.Instructions
	}{
		{
			source:            "(do 1 2 3)",
			expectedConstants: []interface{}{1, 2, 3},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
				opcode.EncodeInstruction(opcode.OpPop),
				opcode.EncodeInstruction(opcode.OpConst, 1),
				opcode.EncodeInstruction(opcode.OpPop),
				opcode.EncodeInstruction(opcode.OpConst, 2),
			},
		},
	}

	for _, tt := range tests {
		runCompilerTest(t, tt.source, tt.expectedConstants, tt.expectedInstructions)
	}
}
