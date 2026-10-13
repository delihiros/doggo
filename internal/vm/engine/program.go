package engine

import (
	"doggo/internal/compiler"
	"doggo/internal/types"
)

// Program holds precompiled VM bytecode plus the original source text.
type Program struct {
	Bytecode   *compiler.Bytecode
	Constants  []types.Value
	Macros     map[string]*compiler.Closure
	SourceFile string
	Source     string
}
