package compiler

import (
	"testing"

	"doggo/internal/types"
)

func TestCompilerFacadePropagatesSourceFileIntoNestedCompiledFunctions(t *testing.T) {
	bc, err := CompileSource("nested.dg", "(do (fn [] 1) (go 2) (dosync 3))")
	if err != nil {
		t.Fatal(err)
	}

	compiled := compiledFunctionConstants(bc.Constants)
	if len(compiled) != 3 {
		t.Fatalf("compiled function constants = %d, want 3", len(compiled))
	}
	for _, fn := range compiled {
		if fn.SourceFile != "nested.dg" {
			t.Fatalf("nested compiled function source file = %q, want %q", fn.SourceFile, "nested.dg")
		}
		if len(fn.Lines) == 0 {
			t.Fatal("expected nested compiled function line metadata")
		}
	}
}

func TestCompilerFacadePropagatesSourceFileIntoMacroCompiledFunctions(t *testing.T) {
	session := NewSession(SessionConfig{})
	_, err := session.CompileSource("macro.dg", "(defmacro twice [x] x)")
	if err != nil {
		t.Fatal(err)
	}

	macro := session.Macros()["user/twice"]
	if macro == nil || macro.Fn == nil {
		t.Fatal("expected compiled macro closure")
	}
	if macro.Fn.SourceFile != "macro.dg" {
		t.Fatalf("macro compiled function source file = %q, want %q", macro.Fn.SourceFile, "macro.dg")
	}
	if len(macro.Fn.Lines) == 0 {
		t.Fatal("expected macro compiled function line metadata")
	}
}

func TestCompileSourceBuildsLineColumnTables(t *testing.T) {
	bytecode, err := CompileSource("tables.dg", "(do\n  (+ 1 2)\n  (+ 3 4))")
	if err != nil {
		t.Fatal(err)
	}

	if len(bytecode.Lines) == 0 || len(bytecode.Columns) == 0 {
		t.Fatal("expected line/column tables")
	}
	if len(bytecode.Lines) != len(bytecode.Columns) {
		t.Fatalf("lines len = %d, columns len = %d", len(bytecode.Lines), len(bytecode.Columns))
	}
}

func compiledFunctionConstants(constants []types.Value) []*CompiledFunction {
	compiled := make([]*CompiledFunction, 0, len(constants))
	for _, constant := range constants {
		fn, ok := constant.(*CompiledFunction)
		if !ok {
			continue
		}
		compiled = append(compiled, fn)
	}
	return compiled
}
