package frontend

import (
	"strings"
	"testing"
)

func TestParseRejectsRawNULInsteadOfReturningPrefixForms(t *testing.T) {
	parsed := Parse("nul.dog", "(def visible 1)\n\x00(def hidden 2)\n")
	if len(parsed.Diagnostics) != 1 || !strings.Contains(parsed.Diagnostics[0].Message, "NUL") {
		t.Fatalf("diagnostics = %v, want NUL error", parsed.Diagnostics)
	}
	if pos := parsed.Diagnostics[0].Span.Start; pos.Line != 2 || pos.Column != 1 {
		t.Fatalf("diagnostic position = %v, want 2:1", pos)
	}
	if len(parsed.Forms) != 0 || len(parsed.LoweredForms) != 0 {
		t.Fatalf("raw NUL returned executable prefix forms: %v", parsed.Forms)
	}
}
