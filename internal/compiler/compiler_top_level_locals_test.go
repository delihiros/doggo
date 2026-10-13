package compiler

import (
	"fmt"
	"strings"
	"testing"

	"doggo/internal/types"
)

func firstCompiledFunction(t *testing.T, constants []types.Value) *CompiledFunction {
	t.Helper()
	for _, constant := range constants {
		if fn, ok := constant.(*CompiledFunction); ok {
			return fn
		}
	}
	t.Fatal("expected compiled function in constants")
	return nil
}

func TestTopLevelBytecodeNumLocalsIgnoresGlobalDefs(t *testing.T) {
	c := New()

	defBytecode, err := compileTestSource(t, c, "(def one 1)")
	if err != nil {
		t.Fatalf("compile def failed: %v", err)
	}
	if got := defBytecode.NumLocals; got != 0 {
		t.Fatalf("expected def to use 0 top-level locals, got %d", got)
	}

	letBytecode, err := compileTestSource(t, c, "(let [x 5] x)")
	if err != nil {
		t.Fatalf("compile let failed: %v", err)
	}
	if got := letBytecode.NumLocals; got != 1 {
		t.Fatalf("expected def + let script to reserve 1 top-level local, got %d", got)
	}
}

func TestTopLevelBytecodeNumLocalsReusesSlotsAcrossForms(t *testing.T) {
	c := New()

	bytecode, err := compileTestSource(t, c, "(let [x 1] x) (let [y 2] y)")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	if got := bytecode.NumLocals; got != 1 {
		t.Fatalf("expected top-level forms to reuse 1 local slot, got %d", got)
	}
}

func TestTopLevelBytecodeNumLocalsReusesSlotsAcrossSiblingBlocks(t *testing.T) {
	c := New()

	bytecode, err := compileTestSource(t, c, "(do (let [x 1] x) (let [y 2] y))")
	if err != nil {
		t.Fatalf("compile do failed: %v", err)
	}

	if got := bytecode.NumLocals; got != 1 {
		t.Fatalf("expected sibling top-level blocks to reuse 1 local slot, got %d", got)
	}
}

func TestCompiledFunctionNumLocalsReusesSlotsAcrossSiblingBlocks(t *testing.T) {
	c := New()

	bytecode, err := compileTestSource(t, c, "(fn [] (do (let [x 1] x) (let [y 2] y)))")
	if err != nil {
		t.Fatalf("compile fn failed: %v", err)
	}

	fn := firstCompiledFunction(t, bytecode.Constants)
	if got := fn.NumLocals; got != 1 {
		t.Fatalf("expected sibling function blocks to reuse 1 local slot, got %d", got)
	}
}

func TestNewWithStateResetsTopLevelLocalsBetweenCompiles(t *testing.T) {
	st := NewSymbolTable()
	constants := NewConstantStore(nil)

	comp1 := NewWithState(st, constants)
	if err := compileTestLoweredSource(t, comp1, "(let [x 5] x)"); err != nil {
		t.Fatalf("first compile failed: %v", err)
	}
	if got := comp1.Bytecode().NumLocals; got != 1 {
		t.Fatalf("expected first compile to reserve 1 top-level local, got %d", got)
	}

	comp2 := NewWithState(st, constants)
	if err := compileTestLoweredSource(t, comp2, "42"); err != nil {
		t.Fatalf("second compile failed: %v", err)
	}
	if got := comp2.Bytecode().NumLocals; got != 0 {
		t.Fatalf("expected second compile to reset top-level locals to 0, got %d", got)
	}
}

func TestCompileRejectsTooManyLocalBindings(t *testing.T) {
	c := New()

	var builder strings.Builder
	builder.WriteString("(let [")
	for i := 0; i <= MaxLocalSlots; i++ {
		if i > 0 {
			builder.WriteByte(' ')
		}
		fmt.Fprintf(&builder, "x%d %d", i, i)
	}
	builder.WriteString("] x0)")

	_, err := compileTestSource(t, c, builder.String())
	if err == nil {
		t.Fatalf("expected compile error for too many locals")
	}
	if !strings.Contains(err.Error(), "too many local bindings") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileAllowsManySequentialSingleSlotBlocks(t *testing.T) {
	c := New()

	var builder strings.Builder
	builder.WriteString("(do")
	for i := 0; i <= MaxLocalSlots; i++ {
		fmt.Fprintf(&builder, " (let [x%d %d] %d)", i, i, i)
	}
	builder.WriteByte(')')

	bytecode, err := compileTestSource(t, c, builder.String())
	if err != nil {
		t.Fatalf("expected sequential single-slot blocks to compile, got error: %v", err)
	}

	if got := bytecode.NumLocals; got != 1 {
		t.Fatalf("expected sequential single-slot blocks to reserve 1 local slot, got %d", got)
	}
}

func TestTopLevelBytecodeNumLocalsReusesSlotsAcrossSiblingTryFinallyBlocks(t *testing.T) {
	c := New()

	bytecode, err := compileTestSource(t, c, "(do (try 1 (finally 10)) (try 2 (finally 20)))")
	if err != nil {
		t.Fatalf("compile sibling try/finally failed: %v", err)
	}

	if got := bytecode.NumLocals; got != 1 {
		t.Fatalf("expected sibling try/finally blocks to reuse 1 local slot, got %d", got)
	}
}

func TestCompileRejectsMultipleTypedCatchClauses(t *testing.T) {
	c := New()
	_, err := compileTestSource(t, c, "(try 1 (catch Type0 e0 0) (catch Type1 e1 1))")
	if err == nil || !strings.Contains(err.Error(), "try supports exactly one catch clause") {
		t.Fatalf("expected canonical catch grammar error, got %v", err)
	}
}
