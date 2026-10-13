package vm

import (
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/compiler/macroconvert"
	"doggo/internal/types"
)

func compileRuntimeValue(tb testing.TB, c *compiler.Compiler, value types.Value) error {
	tb.Helper()
	if value == nil {
		return nil
	}
	form, err := macroconvert.ValueToForm(value)
	if err != nil {
		return err
	}
	return c.CompileForm(form)
}
