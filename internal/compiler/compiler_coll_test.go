package compiler

import (
	"testing"

	"doggo/internal/opcode"
)

func TestVectorLiterals(t *testing.T) {
	tests := []struct {
		source               string
		expectedConstants    []interface{}
		expectedInstructions []opcode.Instructions
	}{
		{
			source:            "[1 2 3]",
			expectedConstants: []interface{}{1, 2, 3},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
				opcode.EncodeInstruction(opcode.OpConst, 1),
				opcode.EncodeInstruction(opcode.OpConst, 2),
				opcode.EncodeInstruction(opcode.OpArray, 3),
			},
		},
	}

	for _, tt := range tests {
		runCompilerTest(t, tt.source, tt.expectedConstants, tt.expectedInstructions)
	}
}
