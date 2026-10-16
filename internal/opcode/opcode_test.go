package opcode

import (
	"testing"
)

func TestMake(t *testing.T) {
	tests := []struct {
		op       Opcode
		operands []int
		expected []byte
	}{
		{OpConst, []int{65534}, []byte{byte(OpConst), 0xFF, 0xFE}},
		{OpAdd, []int{}, []byte{byte(OpAdd)}},
		{OpGetLocal, []int{255}, []byte{byte(OpGetLocal), 0xFF}},
		{OpInitLocal, []int{255}, []byte{byte(OpInitLocal), 0xFF}},
		{OpClosure, []int{65534, 255}, []byte{byte(OpClosure), 0xFF, 0xFE, 0xFF}},
		{OpCall, []int{3}, []byte{byte(OpCall), 0x03}},
	}

	for _, tt := range tests {
		instruction := EncodeInstruction(tt.op, tt.operands...)

		if len(instruction) != len(tt.expected) {
			t.Errorf("instruction has wrong length. want=%d, got=%d",
				len(tt.expected), len(instruction))
			continue
		}

		for i, b := range tt.expected {
			if instruction[i] != b {
				t.Errorf("wrong byte at pos %d. want=%d, got=%d",
					i, b, instruction[i])
			}
		}
	}
}

func TestReadOperands(t *testing.T) {
	tests := []struct {
		op        Opcode
		operands  []int
		bytesRead int
	}{
		{OpConst, []int{65535}, 2},
		{OpGetLocal, []int{255}, 1},
		{OpInitLocal, []int{255}, 1},
		{OpClosure, []int{65535, 255}, 3},
	}

	for _, tt := range tests {
		instruction := EncodeInstruction(tt.op, tt.operands...)

		def, err := Lookup(tt.op)
		if err != nil {
			t.Fatalf("definition not found: %q", err)
		}

		operandsRead, n := ReadOperands(def, instruction[1:])
		if n != tt.bytesRead {
			t.Fatalf("n wrong. want=%d, got=%d", tt.bytesRead, n)
		}

		for i, want := range tt.operands {
			if operandsRead[i] != want {
				t.Errorf("operand wrong. want=%d, got=%d", want, operandsRead[i])
			}
		}
	}
}

func TestInstructionsString(t *testing.T) {
	instructions := []Instructions{
		EncodeInstruction(OpConst, 1),
		EncodeInstruction(OpConst, 2),
		EncodeInstruction(OpAdd),
		EncodeInstruction(OpConst, 65535),
		EncodeInstruction(OpGetLocal, 0),
		EncodeInstruction(OpClosure, 65535, 255),
	}

	expected := `0000 OpConst 1
0003 OpConst 2
0006 OpAdd
0007 OpConst 65535
0010 OpGetLocal 0
0012 OpClosure 65535 255
`

	concatted := Instructions{}
	for _, ins := range instructions {
		concatted = append(concatted, ins...)
	}

	if concatted.String() != expected {
		t.Errorf("instructions wrongly formatted.\nwant=%q\ngot=%q",
			expected, concatted.String())
	}
}

func TestOpcodeString(t *testing.T) {
	tests := []struct {
		op       Opcode
		expected string
	}{
		{OpConst, "OpConst"},
		{OpAdd, "OpAdd"},
		{OpGetLocal, "OpGetLocal"},
		{OpInitLocal, "OpInitLocal"},
		{Opcode(0xFF), "UNKNOWN(0xFF)"},
	}

	for _, tt := range tests {
		if tt.op.String() != tt.expected {
			t.Errorf("wrong string. want=%s, got=%s", tt.expected, tt.op.String())
		}
	}
}

func TestLookup(t *testing.T) {
	tests := []struct {
		op          Opcode
		expectError bool
	}{
		{OpConst, false},
		{OpAdd, false},
		{Opcode(0xFF), true},
	}

	for _, tt := range tests {
		def, err := Lookup(tt.op)
		if tt.expectError {
			if err == nil {
				t.Errorf("expected error for opcode %d", tt.op)
			}
		} else {
			if err != nil {
				t.Errorf("unexpected error: %s", err)
			}
			if def == nil {
				t.Error("expected definition, got nil")
			}
		}
	}
}
