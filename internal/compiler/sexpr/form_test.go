package sexpr

import (
	"testing"

	"doggo/internal/syntax/frontend"
)

func TestLowerFilePreservesMetadataAndSpans(t *testing.T) {
	src := "^{:private true} (def x [1 2 3])"
	parsed := frontend.Parse("test.dg", src)
	if len(parsed.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", parsed.Diagnostics)
	}

	file, err := LowerFile(parsed.Forms)
	if err != nil {
		t.Fatal(err)
	}
	if got := file.Forms[0].Meta().Get("private"); !got.Truthy() {
		t.Fatalf("metadata not preserved: %#v", file.Forms[0].Meta())
	}
	if file.Forms[0].Span().Start.Line != 1 || file.Forms[0].Span().Start.Column != 1 {
		t.Fatalf("unexpected span: %#v", file.Forms[0].Span())
	}
}
