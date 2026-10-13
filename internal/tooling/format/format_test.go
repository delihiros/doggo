package format

import (
	"strings"
	"testing"
)

func TestFormatPreservesComments(t *testing.T) {
	source := `(defn fib [n] ; inline
 (if (<= n 1)
 n
 (+ (fib (dec n)) ; nested
    (fib (- n 2)))))
; tail
`
	got, _, err := Format(source, Options{TabSize: 2, InsertSpaces: true})
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	if !strings.Contains(got, "; inline") || !strings.Contains(got, "; nested") || !strings.Contains(got, "; tail") {
		t.Fatalf("comments must be preserved:\n%s", got)
	}
}

func TestFormatIdempotent(t *testing.T) {
	source := `(+
1
2)
`
	first, _, err := Format(source, Options{TabSize: 2, InsertSpaces: true})
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	second, _, err := Format(first, Options{TabSize: 2, InsertSpaces: true})
	if err != nil {
		t.Fatalf("Format second failed: %v", err)
	}
	if first != second {
		t.Fatalf("format should be idempotent\nfirst:\n%q\nsecond:\n%q", first, second)
	}
}

func TestFormatStructuralIndent(t *testing.T) {
	source := "(+\n1\n2)\n"
	got, _, err := Format(source, Options{TabSize: 2, InsertSpaces: true})
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	want := "(+\n  1\n  2)\n"
	if got != want {
		t.Fatalf("unexpected formatted output\nwant=%q\ngot=%q", want, got)
	}
}

func TestFormatPreservesMultilineStringBody(t *testing.T) {
	source := `(def s "a
    b")
`
	got, _, err := Format(source, Options{TabSize: 2, InsertSpaces: true})
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	if got != source {
		t.Fatalf("multiline string content must be preserved\nwant=%q\ngot=%q", source, got)
	}
}

func TestFormatTrimsTrailingWhitespaceAfterMultilineStringClosed(t *testing.T) {
	source := "(def s \"a\nb\"   )   \n"
	got, _, err := Format(source, Options{TabSize: 2, InsertSpaces: true})
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	want := "(def s \"a\nb\"   )\n"
	if got != want {
		t.Fatalf("unexpected formatted output\nwant=%q\ngot=%q", want, got)
	}

	second, _, err := Format(got, Options{TabSize: 2, InsertSpaces: true})
	if err != nil {
		t.Fatalf("Format second failed: %v", err)
	}
	if second != got {
		t.Fatalf("format should converge for multiline strings\ngot=%q\nsecond=%q", got, second)
	}
}

func TestFormatDedentsAfterClosingLine(t *testing.T) {
	source := "(+\n  1\n)\n(def x 1)\n"
	got, _, err := Format(source, Options{TabSize: 2, InsertSpaces: true})
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	if got != source {
		t.Fatalf("closing paren line must not keep next line indented\nwant=%q\ngot=%q", source, got)
	}
}
