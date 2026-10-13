package compiler

import (
	"errors"
	"strings"
	"testing"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/frontend"
	"doggo/internal/syntax/span"
	"doggo/internal/types"
	"doggo/internal/types/coll"
)

func TestLowerFileBuildsCompilerFile(t *testing.T) {
	src := "(ns demo.core) (def x 1)"
	parsed := frontend.Parse("demo.dg", src)
	if len(parsed.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", parsed.Diagnostics)
	}

	prog, err := sexpr.LowerFile(parsed.Forms)
	if err != nil {
		t.Fatal(err)
	}
	if prog.Namespace != "demo.core" {
		t.Fatalf("namespace mismatch: %q", prog.Namespace)
	}
}

func TestCompileFormPreservesContextualInvalidLiteralError(t *testing.T) {
	comp := New()
	comp.SetSourceFile("demo.dg")

	err := comp.CompileForm(sexpr.NewCharacter("bad-char", span.New("demo.dg", 2, 3, 2, 11), nil))
	if err == nil {
		t.Fatal("expected compile error")
	}

	var compileErr *CompileError
	if !errors.As(err, &compileErr) {
		t.Fatalf("expected CompileError, got %T", err)
	}
	if compileErr.File != "demo.dg" || compileErr.Line != 2 || compileErr.Column != 3 {
		t.Fatalf("unexpected compile error context: %+v", compileErr)
	}
	if !strings.Contains(compileErr.Message, "invalid character literal") {
		t.Fatalf("unexpected compile error message: %+v", compileErr)
	}
}

func TestCompileFilePreservesContextualInvalidLiteralError(t *testing.T) {
	session := NewSession(SessionConfig{
		SourceLineProvider: func(file string, line int) (string, bool) {
			if file == "demo.dg" && line == 3 {
				return `\bad-char`, true
			}
			return "", false
		},
	})
	file := &sexpr.File{
		Path: "demo.dg",
		Forms: []sexpr.Form{
			sexpr.NewCharacter("bad-char", span.New("demo.dg", 3, 2, 3, 10), nil),
		},
	}

	_, err := session.CompileFile(file)
	if err == nil {
		t.Fatal("expected compile error")
	}

	var compileErr *CompileError
	if !errors.As(err, &compileErr) {
		t.Fatalf("expected CompileError, got %T", err)
	}
	if compileErr.File != "demo.dg" || compileErr.Line != 3 || compileErr.Column != 2 {
		t.Fatalf("unexpected compile error context: %+v", compileErr)
	}
	if !strings.Contains(compileErr.Message, "invalid character literal") {
		t.Fatalf("unexpected compile error message: %+v", compileErr)
	}
}

func TestCompileMacroExpansionPreservesExpansionAwareCompileErrorContext(t *testing.T) {
	session := NewSession(SessionConfig{
		SourceLineProvider: func(file string, line int) (string, bool) {
			if file != "demo.dg" {
				return "", false
			}
			switch line {
			case 1:
				return "(defmacro fail [] nil)", true
			case 2:
				return "(fail)", true
			case 9:
				return "(inner missing)", true
			default:
				return "", false
			}
		},
		MacroEvaluator: func(fn *Closure, args []types.Value) (types.Value, error) {
			return coll.NewListWithPos(9, 6, types.Symbol("missing")), nil
		},
	})

	_, err := session.CompileSource("demo.dg", "(defmacro fail [] nil)\n(fail)")
	if err == nil {
		t.Fatal("expected compile error")
	}

	var compileErr *CompileError
	if !errors.As(err, &compileErr) {
		t.Fatalf("expected CompileError, got %T", err)
	}
	if compileErr.File != "demo.dg" || compileErr.Line != 2 || compileErr.Column != 1 {
		t.Fatalf("unexpected compile error context: %+v", compileErr)
	}
	if compileErr.SnippetLine != "(fail)" {
		t.Fatalf("unexpected snippet line: %+v", compileErr)
	}
	if !strings.Contains(compileErr.Message, "undefined variable: missing") {
		t.Fatalf("unexpected compile error message: %+v", compileErr)
	}
}
