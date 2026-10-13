package compiler

import (
	"testing"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/frontend"
)

func compileTestSource(t *testing.T, comp *Compiler, source string) (*Bytecode, error) {
	t.Helper()
	session := NewSession(SessionConfig{
		SymbolTable:          comp.symbolTable,
		ConstantStore:        comp.constants,
		Macros:               comp.Macros(),
		MacroMetadata:        comp.MacroMetadata(),
		MacroEvaluator:       comp.macroEvaluator,
		OptimizationConfig:   comp.OptimizationConfig(),
		AllowPrivateBuiltins: comp.allowPrivateBuiltins,
	})
	bytecode, err := session.CompileSource("<test>", source)
	if err != nil {
		return nil, err
	}
	comp.macros = session.Macros()
	comp.macroMetadata = session.MacroMetadata()
	return bytecode, nil
}

func compileTestForm(t *testing.T, c *Compiler, form sexpr.Form) error {
	t.Helper()
	return c.withCompileEntry(func() error {
		return c.CompileForm(form)
	})
}

func compileTestLoweredSource(t *testing.T, c *Compiler, source string) error {
	t.Helper()
	file := lowerTestSource(t, source)
	for _, form := range file.Forms {
		if err := compileTestForm(t, c, form); err != nil {
			return err
		}
	}
	return nil
}

func compileTestSourceForm(t *testing.T, source string) sexpr.Form {
	t.Helper()
	file := lowerTestSource(t, source)
	if len(file.Forms) != 1 {
		t.Fatalf("expected one lowered form, got %d", len(file.Forms))
	}
	return file.Forms[0]
}

func lowerTestSource(t *testing.T, source string) *sexpr.File {
	t.Helper()
	parsed := frontend.Parse("<test>", source)
	if len(parsed.Diagnostics) > 0 {
		t.Fatalf("parse diagnostics: %s", parsed.Diagnostics[0].Message)
	}
	file, err := sexpr.LowerFile(parsed.Forms)
	if err != nil {
		t.Fatalf("lower source failed: %v", err)
	}
	return file
}
