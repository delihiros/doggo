package debugmeta

import (
	"testing"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
)

func TestOwnerUsesExpansionSourceFormFallbackForSourceText(t *testing.T) {
	owner := NewOwner()
	owner.SetCurrent("demo.dg", 4, 7)

	source := "(defconst answer 42)"
	macroCallSpan := span.New("demo.dg", 1, 1, 1, len(source))
	macroCallSpan.StartOffset = 0
	macroCallSpan.EndOffset = len(source)
	macroCall := sexpr.NewList(nil, macroCallSpan, nil)

	owner.PushExpansionSource(Span{File: "demo.dg", Line: 1, Column: 1}, macroCall)
	defer owner.PopExpansionSource()

	if got := owner.SourceFormText(nil, func(file string) (string, bool) {
		if file == "demo.dg" {
			return source, true
		}
		return "", false
	}); got != source {
		t.Fatalf("SourceFormText() = %q, want %q", got, source)
	}
}

func TestOwnerLeaveScopePreservesSourceFileAndInstructionTables(t *testing.T) {
	owner := NewOwner()
	owner.SetCurrent("nested.dg", 2, 3)

	owner.EnterScope()
	owner.RecordInstructions(3)

	table := owner.LeaveScope()
	if table.SourceFile != "nested.dg" {
		t.Fatalf("LeaveScope().SourceFile = %q, want %q", table.SourceFile, "nested.dg")
	}
	if len(table.Lines) != 3 || len(table.Columns) != 3 {
		t.Fatalf("LeaveScope() lengths = (%d,%d), want (3,3)", len(table.Lines), len(table.Columns))
	}
}
