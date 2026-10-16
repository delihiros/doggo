package vm

import (
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/opcode"
	"doggo/internal/types/atom"
)

func TestDropBelow(t *testing.T) {
	tests := []struct {
		name   string
		before []int64
		keep   int
		drop   int
		want   []int64
	}{
		{name: "empty"},
		{name: "drop zero", before: []int64{1, 2}, keep: 2, want: []int64{1, 2}},
		{name: "keep zero", before: []int64{1, 2, 3}, drop: 2, want: []int64{1}},
		{name: "multiple values", before: []int64{1, 2, 3, 4, 5}, keep: 2, drop: 2, want: []int64{1, 4, 5}},
		{name: "overlapping slide", before: []int64{1, 2, 3, 4, 5}, keep: 3, drop: 1, want: []int64{1, 3, 4, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			machine, err := New(&compiler.Bytecode{
				Instructions: opcode.EncodeInstruction(opcode.OpDropBelow, tt.keep, tt.drop),
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range tt.before {
				if err := machine.push(atom.NewNumberInt(value)); err != nil {
					t.Fatal(err)
				}
			}
			if err := machine.Run(); err != nil {
				t.Fatal(err)
			}
			if machine.sp != len(tt.want) {
				t.Fatalf("stack length = %d, want %d", machine.sp, len(tt.want))
			}
			for i, value := range tt.want {
				if !machine.stack[i].Equals(atom.NewNumberInt(value)) {
					t.Errorf("stack[%d] = %s, want %d", i, machine.stack[i].String(), value)
				}
			}
		})
	}
}
