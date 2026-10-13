package compiler

import "testing"

type stubDiagnosticLocationReader struct {
	file   string
	line   int
	column int
}

func (s stubDiagnosticLocationReader) EffectiveSourceLocation() (string, int, int) {
	return s.file, s.line, s.column
}

func TestDiagnosticOwnerBuildsCompileErrorFromLocationReader(t *testing.T) {
	owner := newDiagnosticOwner(stubDiagnosticLocationReader{
		file:   "demo.dg",
		line:   3,
		column: 7,
	}, func(file string, line int) (string, bool) {
		if file == "demo.dg" && line == 3 {
			return "(bad)", true
		}
		return "", false
	})

	err := owner.Errorf("boom")
	if err.File != "demo.dg" || err.Line != 3 || err.Column != 7 {
		t.Fatalf("unexpected compile error context: %+v", err)
	}
	if err.SnippetLine != "(bad)" {
		t.Fatalf("unexpected snippet line: %+v", err)
	}

	want := "demo.dg:3:7: boom\n3 | (bad)\n  |     ^"
	if got := err.Error(); got != want {
		t.Fatalf("unexpected error output:\nwant %q\ngot  %q", want, got)
	}
}
