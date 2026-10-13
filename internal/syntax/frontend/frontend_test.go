package frontend

import (
	"errors"
	"testing"

	"doggo/internal/lexer"
	"doggo/internal/syntax/form"
	"doggo/internal/types"
)

func TestReaderReadNextReadsFormsSequentially(t *testing.T) {
	reader, err := NewReader("seq.dog", "(def x 1)\n(+ x 2)\n")
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	first, err := reader.ReadNext()
	if err != nil {
		t.Fatalf("ReadNext first failed: %v", err)
	}
	if got := first.String(); got != "(def x 1)" {
		t.Fatalf("first form mismatch: %q", got)
	}

	second, err := reader.ReadNext()
	if err != nil {
		t.Fatalf("ReadNext second failed: %v", err)
	}
	if got := second.String(); got != "(+ x 2)" {
		t.Fatalf("second form mismatch: %q", got)
	}

	eof, err := reader.ReadNext()
	if err != nil {
		t.Fatalf("ReadNext eof failed: %v", err)
	}
	if eof != nil {
		t.Fatalf("expected nil at eof, got %v", eof)
	}
}

func TestReaderReadNextSkipsTopLevelDiscard(t *testing.T) {
	reader, err := NewReader("discard-seq.dog", "#_ 1\n2\n")
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	form, err := reader.ReadNext()
	if err != nil {
		t.Fatalf("ReadNext failed: %v", err)
	}
	if form == nil || form.String() != "2" {
		t.Fatalf("ReadNext = %v, want 2", form)
	}
}

func TestReadSingleFormRejectsEmptyAndMultipleForms(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if _, err := ReadSingleForm("empty.dog", ""); err == nil {
			t.Fatalf("expected empty input error")
		}
	})

	t.Run("multiple", func(t *testing.T) {
		if _, err := ReadSingleForm("many.dog", "(def x 1)\n(def y 2)"); err == nil {
			t.Fatalf("expected multiple form error")
		}
	})
}

func TestReadFirstFormMatchesClojureReadStringSemantics(t *testing.T) {
	form, err := ReadFirstForm("read-string.dog", "(+ 1 2) 3")
	if err != nil {
		t.Fatalf("ReadFirstForm failed: %v", err)
	}
	if got := form.String(); got != "(+ 1 2)" {
		t.Fatalf("first form mismatch: %q", got)
	}
}

func TestReadFirstFormSkipsTopLevelDiscardLikeClojure(t *testing.T) {
	form, err := ReadFirstForm("read-string-discard.dog", "#_ 1 2")
	if err != nil {
		t.Fatalf("ReadFirstForm failed: %v", err)
	}
	if got := form.String(); got != "2" {
		t.Fatalf("first form mismatch: %q", got)
	}
}

func TestReadFirstFormRequiresReadableFormAfterDiscard(t *testing.T) {
	if _, err := ReadFirstForm("read-string-discard-eof.dog", "#_ 1"); err == nil {
		t.Fatalf("expected eof error after discarded form")
	}
}

func TestReadFirstFormNestedDiscardMatchesClojure(t *testing.T) {
	if _, err := ReadFirstForm("read-string-nested-discard.dog", "#_ #_ 1 2"); err == nil {
		t.Fatalf("expected eof error after nested discard")
	}
}

func TestParsePreservesCommentTrivia(t *testing.T) {
	source := `; head
(def x 1) ; tail
`
	result := Parse("main.dog", source)
	if len(result.Diagnostics) > 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	if len(result.Tokens) == 0 {
		t.Fatalf("expected tokens")
	}

	foundHead := false
	foundTail := false
	for _, tok := range result.Tokens {
		for _, tr := range tok.LeadingTrivia {
			if tr.Kind == "comment" && tr.Text == "; head" {
				foundHead = true
			}
		}
		for _, tr := range tok.TrailingTrivia {
			if tr.Kind == "comment" && tr.Text == "; tail" {
				foundTail = true
			}
		}
	}
	if !foundHead {
		t.Fatalf("expected leading head comment in trivia")
	}
	if !foundTail {
		t.Fatalf("expected trailing tail comment in trivia")
	}
}

func TestParseBuildsFormsWithNonZeroSpans(t *testing.T) {
	source := `(ns app.main)
(defn fib [n] (if (<= n 1) n (+ (fib (dec n)) (fib (- n 2)))))
`
	result := Parse("fib.dog", source)
	if len(result.Diagnostics) > 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	if len(result.Forms) == 0 {
		t.Fatalf("expected forms")
	}
	for _, form := range result.Forms {
		assertNodeSpan(t, form)
	}
}

func TestParseSupportsTopLevelDiscard(t *testing.T) {
	source := `#_ (skip-this)
(def x 1)
`
	result := Parse("discard.dog", source)
	if len(result.Diagnostics) > 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	if len(result.LoweredForms) != 1 {
		t.Fatalf("expected 1 form after discard, got %d", len(result.LoweredForms))
	}
	if len(result.Forms) != 1 {
		t.Fatalf("expected 1 form after discard")
	}
}

func TestParseRetainsSyntaxArtifactsWhenLoweringFails(t *testing.T) {
	originalLowerForms := lowerForms
	lowerForms = func(nodes []*form.Node) ([]types.Value, error) {
		return nil, errors.New("synthetic lowering failure")
	}
	defer func() {
		lowerForms = originalLowerForms
	}()

	result := Parse("lower-fail.dog", "(def x 1)\n")
	if len(result.Diagnostics) != 1 {
		t.Fatalf("expected 1 lowering diagnostic, got %+v", result.Diagnostics)
	}
	if len(result.Forms) != 1 {
		t.Fatalf("expected forms even when lowering fails, got %d", len(result.Forms))
	}
	if len(result.LoweredForms) != 0 {
		t.Fatalf("expected no lowered forms on lowering failure, got %d", len(result.LoweredForms))
	}
}

func TestParseTokenLexemesAndSpanPositions(t *testing.T) {
	source := "(+ 1 2)\n"
	result := Parse("expr.dog", source)
	if len(result.Diagnostics) > 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	if len(result.Tokens) < 5 {
		t.Fatalf("expected at least 5 tokens, got %d", len(result.Tokens))
	}

	wantLexemes := []string{"(", "+", "1", "2", ")"}
	for i, want := range wantLexemes {
		if got := result.Tokens[i].Lexeme; got != want {
			t.Fatalf("token[%d] lexeme mismatch: want=%q got=%q", i, want, got)
		}
	}

	first := result.Tokens[0]
	if first.Span.Start.Line != 1 || first.Span.Start.Column != 1 {
		t.Fatalf("first token start mismatch: %+v", first.Span.Start)
	}
}

func TestParseStringTokenLexemePreservesQuotes(t *testing.T) {
	source := "(def s \"abc\")\n"
	result := Parse("str.dog", source)
	if len(result.Diagnostics) > 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	found := false
	for _, tok := range result.Tokens {
		if tok.Kind == "string" {
			found = true
			if tok.Lexeme != "\"abc\"" {
				t.Fatalf("string lexeme mismatch: %q", tok.Lexeme)
			}
		}
	}
	if !found {
		t.Fatalf("string token not found")
	}
}

func TestParseRegexTokenLexemePreservesPrefixAndQuotes(t *testing.T) {
	source := `(def r #"a+b")` + "\n"
	result := Parse("re.dog", source)
	if len(result.Diagnostics) > 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	found := false
	for _, tok := range result.Tokens {
		if tok.Kind == "regex" {
			found = true
			if tok.Lexeme != "#\"a+b\"" {
				t.Fatalf("regex lexeme mismatch: %q", tok.Lexeme)
			}
		}
	}
	if !found {
		t.Fatalf("regex token not found")
	}
}

func TestTokenizeReturnsLexerTokens(t *testing.T) {
	tokens, err := Tokenize("(import fmt)\n")
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}
	if len(tokens) < 5 {
		t.Fatalf("expected raw lexer tokens including EOF, got %d", len(tokens))
	}
	if tokens[0].Type != lexer.LPAREN {
		t.Fatalf("expected first token to be LPAREN, got %v", tokens[0].Type)
	}
	if tokens[1].Type != lexer.IDENT || tokens[1].Literal != "import" {
		t.Fatalf("expected import ident token, got %#v", tokens[1])
	}
	if tokens[2].Type != lexer.IDENT || tokens[2].Literal != "fmt" {
		t.Fatalf("expected fmt ident token, got %#v", tokens[2])
	}
	if tokens[len(tokens)-1].Type != lexer.EOF {
		t.Fatalf("expected trailing EOF token, got %v", tokens[len(tokens)-1].Type)
	}
}

func TestIsSingleIdentifierTokenMatchesLexerSingleTokenContract(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{input: "len", want: true},
		{input: "map?", want: true},
		{input: "foo/bar", want: true},
		{input: "github.com/hajimehoshi/ebiten/v2", want: true},
		{input: ":kw", want: false},
		{input: "two words", want: false},
		{input: "", want: false},
	}

	for _, tt := range tests {
		if got := IsSingleIdentifierToken(tt.input); got != tt.want {
			t.Fatalf("IsSingleIdentifierToken(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsSimpleSymbolMatchesClojureSimpleSymbolShape(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{input: "len", want: true},
		{input: "map?", want: true},
		{input: "+", want: true},
		{input: "foo/bar", want: false},
		{input: "net/http", want: false},
		{input: ":kw", want: false},
		{input: "", want: false},
	}

	for _, tt := range tests {
		if got := IsSimpleSymbol(tt.input); got != tt.want {
			t.Fatalf("IsSimpleSymbol(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func assertNodeSpan(t *testing.T, n *form.Node) {
	t.Helper()
	sl, sc := n.Span.Start.Line, n.Span.Start.Column
	el, ec := n.Span.End.Line, n.Span.End.Column
	if sl <= 0 || sc <= 0 || el <= 0 || ec <= 0 {
		t.Fatalf("invalid span: %d:%d - %d:%d", sl, sc, el, ec)
	}
	for _, child := range n.Children() {
		assertNodeSpan(t, child)
	}
}
