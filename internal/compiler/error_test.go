package compiler

import "testing"

func TestCompileErrorExceptionData(t *testing.T) {
	err := (&CompileError{
		File:        "sample.dog",
		Message:     "unexpected token",
		Line:        7,
		Column:      5,
		SnippetLine: "(+ 1)",
		SnippetCol:  0,
	}).ExceptionData()

	if err == nil {
		t.Fatal("expected canonical exception data")
	}
	if err.Type != "CompileError" {
		t.Fatalf("unexpected type: %q", err.Type)
	}
	if err.Kind != "compile error" {
		t.Fatalf("unexpected kind: %q", err.Kind)
	}
	if err.Phase != "compile" {
		t.Fatalf("unexpected phase: %q", err.Phase)
	}
	if err.Message != "unexpected token" {
		t.Fatalf("unexpected message: %q", err.Message)
	}
	if err.At.File != "sample.dog" || err.At.Line != 7 || err.At.Column != 5 {
		t.Fatalf("unexpected position: %#v", err.At)
	}
	if got := err.Context["snippet"]; got != "(+ 1)" {
		t.Fatalf("unexpected context: %#v", err.Context)
	}
	if got := err.Context["snippet_column"]; got != 0 {
		t.Fatalf("unexpected snippet column: %#v", err.Context)
	}
	if got := err.Context["formatted_snippet"]; got != "7 | (+ 1)\n  |     ^" {
		t.Fatalf("unexpected formatted snippet: %q", got)
	}
}
