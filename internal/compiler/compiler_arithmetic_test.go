package compiler

import (
	"testing"

	"doggo/internal/opcode"
)

func TestIntegerArithmetic(t *testing.T) {
	tests := []struct {
		source               string
		expectedConstants    []interface{}
		expectedInstructions []opcode.Instructions
	}{
		{
			source:            "1",
			expectedConstants: []interface{}{1},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
			},
		},
		{
			source:            "2",
			expectedConstants: []interface{}{2},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
			},
		},
	}

	for _, tt := range tests {
		runCompilerTest(t, tt.source, tt.expectedConstants, tt.expectedInstructions)
	}
}

func TestBooleanExpressions(t *testing.T) {
	tests := []struct {
		source               string
		expectedConstants    []interface{}
		expectedInstructions []opcode.Instructions
	}{
		{
			source:            "true",
			expectedConstants: []interface{}{},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpTrue),
			},
		},
		{
			source:            "false",
			expectedConstants: []interface{}{},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpFalse),
			},
		},
	}

	for _, tt := range tests {
		runCompilerTest(t, tt.source, tt.expectedConstants, tt.expectedInstructions)
	}
}

func TestArithmeticOperations(t *testing.T) {
	tests := []struct {
		source               string
		expectedConstants    []interface{}
		expectedInstructions []opcode.Instructions
	}{
		{
			source:            "(+ 1 2)",
			expectedConstants: []interface{}{1, 2},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
				opcode.EncodeInstruction(opcode.OpConst, 1),
				opcode.EncodeInstruction(opcode.OpAdd),
			},
		},
		{
			source:            "(- 1 2)",
			expectedConstants: []interface{}{1, 2},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
				opcode.EncodeInstruction(opcode.OpConst, 1),
				opcode.EncodeInstruction(opcode.OpSub),
			},
		},
		{
			source:            "(* 1 2)",
			expectedConstants: []interface{}{1, 2},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
				opcode.EncodeInstruction(opcode.OpConst, 1),
				opcode.EncodeInstruction(opcode.OpMul),
			},
		},
		{
			source:            "(/ 2 1)",
			expectedConstants: []interface{}{2, 1},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
				opcode.EncodeInstruction(opcode.OpConst, 1),
				opcode.EncodeInstruction(opcode.OpDiv),
			},
		},
		{
			source:            "(- 1)",
			expectedConstants: []interface{}{1},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
				opcode.EncodeInstruction(opcode.OpNeg),
			},
		},
	}

	for _, tt := range tests {
		runCompilerTest(t, tt.source, tt.expectedConstants, tt.expectedInstructions)
	}
}

func TestVariadicArithmetic(t *testing.T) {
	tests := []struct {
		source               string
		expectedConstants    []interface{}
		expectedInstructions []opcode.Instructions
	}{
		{
			source:            "(+ 1 2 3)",
			expectedConstants: []interface{}{1, 2, 3},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpGetBuiltin, 0),
				opcode.EncodeInstruction(opcode.OpConst, 0),
				opcode.EncodeInstruction(opcode.OpConst, 1),
				opcode.EncodeInstruction(opcode.OpConst, 2),
				opcode.EncodeInstruction(opcode.OpCall, 3),
			},
		},
		{
			source:            "(* 2 3 4)",
			expectedConstants: []interface{}{2, 3, 4},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpGetBuiltin, 2),
				opcode.EncodeInstruction(opcode.OpConst, 0),
				opcode.EncodeInstruction(opcode.OpConst, 1),
				opcode.EncodeInstruction(opcode.OpConst, 2),
				opcode.EncodeInstruction(opcode.OpCall, 3),
			},
		},
		{
			source:            "(- 10 2 3)",
			expectedConstants: []interface{}{10, 2, 3},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpGetBuiltin, 1),
				opcode.EncodeInstruction(opcode.OpConst, 0),
				opcode.EncodeInstruction(opcode.OpConst, 1),
				opcode.EncodeInstruction(opcode.OpConst, 2),
				opcode.EncodeInstruction(opcode.OpCall, 3),
			},
		},
		{
			source:            "(/ 100 2 5)",
			expectedConstants: []interface{}{100, 2, 5},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpGetBuiltin, 3),
				opcode.EncodeInstruction(opcode.OpConst, 0),
				opcode.EncodeInstruction(opcode.OpConst, 1),
				opcode.EncodeInstruction(opcode.OpConst, 2),
				opcode.EncodeInstruction(opcode.OpCall, 3),
			},
		},
		{
			source:            "(+)",
			expectedConstants: []interface{}{0},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
			},
		},
		{
			source:            "(*)",
			expectedConstants: []interface{}{1},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
			},
		},
		{
			source:            "(+ 5)",
			expectedConstants: []interface{}{5},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
			},
		},
		{
			source:            "(* 7)",
			expectedConstants: []interface{}{7},
			expectedInstructions: []opcode.Instructions{
				opcode.EncodeInstruction(opcode.OpConst, 0),
			},
		},
	}

	for _, tt := range tests {
		runCompilerTest(t, tt.source, tt.expectedConstants, tt.expectedInstructions)
	}
}
