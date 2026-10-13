package token

import "doggo/internal/syntax/span"

// TriviaKind はトークン前後の非意味文字列の種別です。
type TriviaKind string

const (
	TriviaWhitespace TriviaKind = "whitespace"
	TriviaComment    TriviaKind = "comment"
)

// Trivia はコメントや空白などの付帯文字列です。
type Trivia struct {
	Kind TriviaKind
	Text string
	Span span.Span
}

// Token は tooling 向けのトークンです。
type Token struct {
	Kind           string
	Lexeme         string
	Span           span.Span
	LeadingTrivia  []Trivia
	TrailingTrivia []Trivia
}
