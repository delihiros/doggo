package frontend

import (
	"fmt"
	"strings"

	doggoerr "doggo/internal/errors"
	"doggo/internal/lexer"
	"doggo/internal/parser"
	"doggo/internal/syntax/form"
	"doggo/internal/syntax/lower"
	"doggo/internal/types"
)

// Reader is the canonical Doggo source reader shared by runtime and tooling.
// It owns lexing and parser state so callers do not orchestrate lexer/parser directly.
type Reader struct {
	path   string
	source string
	tokens []lexer.Token
	parser *parser.Parser
}

func NewReader(path, source string) (*Reader, error) {
	tokens, err := tokenizeSource(source)
	if err != nil {
		return nil, err
	}
	return &Reader{
		path:   path,
		source: source,
		tokens: tokens,
		parser: parser.New(tokens, source),
	}, nil
}

func (r *Reader) Tokens() []lexer.Token {
	return r.tokens
}

// Tokenize returns legacy lexer tokens through the frontend boundary.
// Doggo source consumers should prefer this over wiring lexer directly.
func Tokenize(source string) ([]lexer.Token, error) {
	tokens, err := tokenizeSource(source)
	if err != nil {
		return nil, err
	}
	out := make([]lexer.Token, len(tokens))
	copy(out, tokens)
	return out, nil
}

// IsSingleIdentifierToken reports whether source lexes to exactly one IDENT token.
// This is a lexical predicate and intentionally permits qualified/package-like names.
func IsSingleIdentifierToken(source string) bool {
	if strings.TrimSpace(source) == "" {
		return false
	}
	tokens, err := Tokenize(source)
	if err != nil || len(tokens) < 2 {
		return false
	}
	if tokens[0].Type != lexer.IDENT || tokens[0].Literal != source {
		return false
	}
	return tokens[1].Type == lexer.EOF
}

// IsSimpleSymbol reports whether source matches a Clojure-style simple symbol:
// one identifier token with no namespace/package slash.
func IsSimpleSymbol(source string) bool {
	return IsSingleIdentifierToken(source) && !strings.Contains(source, "/")
}

func (r *Reader) ReadSyntaxNext() (*form.Node, error) {
	node, err := r.parser.ParseNext()
	if node != nil {
		form.SetFileRecursive(r.path, node)
	}
	return node, err
}

func (r *Reader) ReadSyntaxAll() ([]*form.Node, error) {
	nodes, err := r.parser.ParseAll()
	if err != nil {
		return nil, err
	}
	form.SetFilesRecursive(r.path, nodes)
	return nodes, nil
}

func (r *Reader) ReadNext() (types.Value, error) {
	node, err := r.ReadSyntaxNext()
	if err != nil || node == nil {
		return nil, err
	}
	return lower.ToValue(node)
}

func (r *Reader) ReadAll() ([]types.Value, error) {
	nodes, err := r.ReadSyntaxAll()
	if err != nil {
		return nil, err
	}
	return lower.ToValues(nodes)
}

// ReadFirst returns the first readable form, skipping top-level discards.
// If no readable form remains, it reports EOF like clojure.core/read-string.
func (r *Reader) ReadFirst() (types.Value, error) {
	formValue, err := r.ReadNext()
	if err != nil {
		return nil, err
	}
	if formValue != nil {
		return formValue, nil
	}
	return nil, r.eofError()
}

func ReadForms(path, source string) ([]types.Value, error) {
	reader, err := NewReader(path, source)
	if err != nil {
		return nil, err
	}
	return reader.ReadAll()
}

// ReadFirstForm returns the first readable form and ignores trailing forms.
// This matches clojure.core/read-string behavior.
func ReadFirstForm(path, source string) (types.Value, error) {
	reader, err := NewReader(path, source)
	if err != nil {
		return nil, err
	}
	return reader.ReadFirst()
}

// ReadSingleForm requires the entire source to contain exactly one top-level form.
func ReadSingleForm(path, source string) (types.Value, error) {
	forms, err := ReadForms(path, source)
	if err != nil {
		return nil, err
	}
	if len(forms) == 0 {
		return nil, fmt.Errorf("%s: empty file", path)
	}
	if len(forms) > 1 {
		return nil, fmt.Errorf("%s: expected a single root form", path)
	}
	return forms[0], nil
}

func (r *Reader) eofError() error {
	line, column := 1, 1
	if len(r.tokens) > 0 {
		eof := r.tokens[len(r.tokens)-1]
		if eof.Line > 0 {
			line = eof.Line
		}
		if eof.Column > 0 {
			column = eof.Column
		}
	}
	return doggoerr.ParserError("unexpected EOF", line, column)
}

func tokenizeSource(source string) ([]lexer.Token, error) {
	return lexer.New(source).Tokenize()
}
