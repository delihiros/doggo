package parser

import (
	"strconv"
	"strings"
	"unicode/utf8"

	doggoerr "doggo/internal/errors"
	"doggo/internal/lexer"
	"doggo/internal/syntax/form"
	"doggo/internal/syntax/span"
	"doggo/internal/types/atom"
)

// parseHandler maps a token type to its parser implementation.
type parseHandler func(p *Parser) (*form.Node, error)

// Parser builds syntax forms from lexer tokens.
// Its canonical output is syntax/form.Node, not runtime values.
type Parser struct {
	source      string
	lineOffsets []int
	tokens      []lexer.Token
	position    int
	current     lexer.Token
	handlers    map[lexer.TokenType]parseHandler
}

// New creates a parser from tokenized source.
// Passing the original source enables exact span reconstruction.
func New(tokens []lexer.Token, source ...string) *Parser {
	src := ""
	if len(source) > 0 {
		src = source[0]
	}
	p := &Parser{
		source:      src,
		lineOffsets: buildLineOffsets(src),
		tokens:      tokens,
	}
	if len(tokens) > 0 {
		p.current = tokens[0]
	}
	p.initHandlers()
	return p
}

func (p *Parser) initHandlers() {
	p.handlers = map[lexer.TokenType]parseHandler{
		lexer.LPAREN:   (*Parser).parseList,
		lexer.LBRACKET: (*Parser).parseVector,
		lexer.LBRACE:   (*Parser).parseMap,
		lexer.HASHSET:  (*Parser).parseSet,

		lexer.QUOTE:      (*Parser).parseQuote,
		lexer.QUASIQUOTE: (*Parser).parseQuasiquote,
		lexer.UNQUOTE:    (*Parser).parseUnquote,
		lexer.SPLICE:     (*Parser).parseSplice,

		lexer.VAR_QUOTE: (*Parser).parseVarQuote,
		lexer.METADATA:  (*Parser).parseMetadata,
		lexer.DEREF:     (*Parser).parseDeref,
		lexer.HASHFN:    (*Parser).parseHashFn,

		lexer.IDENT:   (*Parser).parseIdent,
		lexer.DOT:     (*Parser).parseDot,
		lexer.NUMBER:  (*Parser).parseNumber,
		lexer.STRING:  (*Parser).parseString,
		lexer.KEYWORD: (*Parser).parseKeyword,
		lexer.REGEX:   (*Parser).parseRegex,
		lexer.CHAR:    (*Parser).parseChar,
	}
}

func (p *Parser) nextToken() {
	p.position++
	if p.position < len(p.tokens) {
		p.current = p.tokens[p.position]
	} else {
		p.current = lexer.Token{Type: lexer.EOF}
	}
}

// Parse parses a single form.
func (p *Parser) Parse() (*form.Node, error) {
	if len(p.tokens) == 0 || (len(p.tokens) == 1 && p.tokens[0].Type == lexer.EOF) {
		return nil, doggoerr.New(doggoerr.KindParser, "empty input")
	}
	node, err := p.ParseNext()
	if err != nil {
		return nil, err
	}
	if node == nil {
		line, column := p.current.Line, p.current.Column
		if line <= 0 {
			line = 1
		}
		if column <= 0 {
			column = 1
		}
		return nil, doggoerr.ParserError("unexpected EOF", line, column)
	}
	return node, nil
}

// ParseNext parses the next top-level form.
// It returns nil, nil at EOF.
func (p *Parser) ParseNext() (*form.Node, error) {
	if err := p.skipTopLevelDiscards(); err != nil {
		return nil, err
	}
	if p.current.Type == lexer.EOF {
		return nil, nil
	}
	return p.parseExpression()
}

// ParseAll parses all top-level forms.
func (p *Parser) ParseAll() ([]*form.Node, error) {
	var forms []*form.Node
	for {
		node, err := p.ParseNext()
		if err != nil {
			return nil, err
		}
		if node == nil {
			break
		}
		forms = append(forms, node)
	}
	return forms, nil
}

func (p *Parser) skipTopLevelDiscards() error {
	for p.current.Type == lexer.DISCARD {
		p.nextToken()
		if err := p.discardReadableForm(map[lexer.TokenType]struct{}{lexer.EOF: {}}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Parser) discardReadableForm(stopTokens map[lexer.TokenType]struct{}) error {
	if p.current.Type == lexer.EOF {
		return doggoerr.ParserError("unexpected EOF", p.current.Line, p.current.Column)
	}
	if stopTokens != nil {
		if _, blocked := stopTokens[p.current.Type]; blocked {
			return doggoerr.ParserErrorf(p.current.Line, p.current.Column, "unexpected token '%s'", p.current.Literal)
		}
	}
	if p.current.Type == lexer.DISCARD {
		p.nextToken()
		if err := p.discardReadableForm(stopTokens); err != nil {
			return err
		}
		return p.discardReadableForm(stopTokens)
	}
	_, err := p.parseExpression()
	return err
}

func (p *Parser) parseExpression() (*form.Node, error) {
	if p.current.Type == lexer.EOF {
		return nil, doggoerr.ParserError("unexpected EOF", p.current.Line, p.current.Column)
	}
	if handler, ok := p.handlers[p.current.Type]; ok {
		return handler(p)
	}
	return nil, doggoerr.ParserErrorf(p.current.Line, p.current.Column, "unexpected token '%s'", p.current.Literal)
}

func (p *Parser) parseIdent() (*form.Node, error) {
	tok := p.current
	sp := p.spanForToken(tok)
	p.nextToken()
	switch tok.Literal {
	case "true":
		return form.NewBoolean(true, sp, tok.Offset, tok.EndOffset), nil
	case "false":
		return form.NewBoolean(false, sp, tok.Offset, tok.EndOffset), nil
	case "nil":
		return form.NewNil(sp, tok.Offset, tok.EndOffset), nil
	default:
		return form.NewSymbol(tok.Literal, sp, tok.Offset, tok.EndOffset), nil
	}
}

func (p *Parser) parseDot() (*form.Node, error) {
	tok := p.current
	sp := p.spanForToken(tok)
	p.nextToken()
	return form.NewSymbol(".", sp, tok.Offset, tok.EndOffset), nil
}

func (p *Parser) parseNumber() (*form.Node, error) {
	tok := p.current
	sp := p.spanForToken(tok)
	p.nextToken()

	num := form.NumberLiteral{Literal: tok.Literal}
	if strings.ContainsRune(tok.Literal, '.') || strings.ContainsAny(tok.Literal, "eE") {
		val, err := strconv.ParseFloat(tok.Literal, 64)
		if err != nil {
			return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "invalid float number '%s'", tok.Literal)
		}
		num.IsFloat = true
		num.Float64 = val
		return form.NewNumber(num, sp, tok.Offset, tok.EndOffset), nil
	}

	val, err := strconv.ParseInt(tok.Literal, 10, 64)
	if err != nil {
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "invalid integer '%s'", tok.Literal)
	}
	num.Int64 = val
	return form.NewNumber(num, sp, tok.Offset, tok.EndOffset), nil
}

func (p *Parser) parseString() (*form.Node, error) {
	tok := p.current
	sp := p.spanForToken(tok)
	p.nextToken()
	return form.NewString(tok.Literal, sp, tok.Offset, tok.EndOffset), nil
}

func (p *Parser) parseKeyword() (*form.Node, error) {
	tok := p.current
	sp := p.spanForToken(tok)
	p.nextToken()
	return form.NewKeyword(tok.Literal, sp, tok.Offset, tok.EndOffset), nil
}

func (p *Parser) parseRegex() (*form.Node, error) {
	tok := p.current
	sp := p.spanForToken(tok)
	if _, err := atom.NewRegex(tok.Literal); err != nil {
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "invalid regex pattern: %v", err)
	}
	p.nextToken()
	return form.NewRegex(tok.Literal, sp, tok.Offset, tok.EndOffset), nil
}

func (p *Parser) parseChar() (*form.Node, error) {
	tok := p.current
	sp := p.spanForToken(tok)
	ch, ok := atom.NewCharacterFromLiteral(tok.Literal)
	if !ok {
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "invalid character literal: \\%s", tok.Literal)
	}
	p.nextToken()
	return form.NewCharacter(
		form.CharacterLiteral{Literal: tok.Literal, Value: rune(ch)},
		sp,
		tok.Offset,
		tok.EndOffset,
	), nil
}

func (p *Parser) parseCollectionItems(closeToken lexer.TokenType, collectionType string) ([]*form.Node, error) {
	var items []*form.Node

	for p.current.Type != closeToken {
		if p.current.Type == lexer.EOF {
			return nil, doggoerr.ParserErrorf(p.current.Line, p.current.Column, "unexpected EOF while parsing %s", collectionType)
		}
		if p.current.Type == lexer.DISCARD {
			p.nextToken()
			if err := p.discardReadableForm(map[lexer.TokenType]struct{}{closeToken: {}}); err != nil {
				return nil, err
			}
			continue
		}
		item, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, nil
}

func (p *Parser) parseCollection(
	closeToken lexer.TokenType,
	collectionType string,
	create func(span.Span, int, int, ...*form.Node) *form.Node,
) (*form.Node, error) {
	startTok := p.current
	p.nextToken()
	items, err := p.parseCollectionItems(closeToken, collectionType)
	if err != nil {
		return nil, err
	}
	closeTok := p.current
	node := create(p.spanFromOffsets(startTok.Offset, closeTok.EndOffset), startTok.Offset, closeTok.EndOffset, items...)
	p.nextToken()
	return node, nil
}

func (p *Parser) parseList() (*form.Node, error) {
	return p.parseCollection(lexer.RPAREN, "list", form.NewList)
}

func (p *Parser) parseVector() (*form.Node, error) {
	return p.parseCollection(lexer.RBRACKET, "vector", form.NewVector)
}

func (p *Parser) parseMap() (*form.Node, error) {
	startTok := p.current
	p.nextToken()
	items, err := p.parseCollectionItems(lexer.RBRACE, "map")
	if err != nil {
		return nil, err
	}
	if len(items)%2 != 0 {
		return nil, doggoerr.ParserError("map must have even number of elements (key-value pairs)", p.current.Line, p.current.Column)
	}

	entries := make([]form.MapEntry, 0, len(items)/2)
	for i := 0; i < len(items); i += 2 {
		entries = append(entries, form.MapEntry{Key: items[i], Value: items[i+1]})
	}

	closeTok := p.current
	node := form.NewMap(p.spanFromOffsets(startTok.Offset, closeTok.EndOffset), startTok.Offset, closeTok.EndOffset, entries...)
	p.nextToken()
	return node, nil
}

func (p *Parser) parseSet() (*form.Node, error) {
	return p.parseCollection(lexer.RBRACE, "set", form.NewSet)
}

func (p *Parser) parseQuoteForm(symbolName string) (*form.Node, error) {
	prefixTok := p.current
	sym := form.NewSymbol(symbolName, p.spanForToken(prefixTok), prefixTok.Offset, prefixTok.EndOffset)
	p.nextToken()
	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	return form.NewList(
		p.spanFromOffsets(prefixTok.Offset, expr.EndOffset),
		prefixTok.Offset,
		expr.EndOffset,
		sym,
		expr,
	), nil
}

func (p *Parser) parseQuote() (*form.Node, error) {
	return p.parseQuoteForm("quote")
}

func (p *Parser) parseQuasiquote() (*form.Node, error) {
	return p.parseQuoteForm("quasiquote")
}

func (p *Parser) parseUnquote() (*form.Node, error) {
	return p.parseQuoteForm("unquote")
}

func (p *Parser) parseSplice() (*form.Node, error) {
	return p.parseQuoteForm("splice-unquote")
}

func (p *Parser) parseDeref() (*form.Node, error) {
	return p.parseQuoteForm("deref")
}

func (p *Parser) parseVarQuote() (*form.Node, error) {
	return p.parseQuoteForm("var")
}

func (p *Parser) parseMetadata() (*form.Node, error) {
	p.nextToken()

	var metadata *form.Node
	switch p.current.Type {
	case lexer.LBRACE:
		metaExpr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if metaExpr.Kind != form.KindMap {
			return nil, doggoerr.ParserErrorf(p.current.Line, p.current.Column, "metadata must be a map, got %s", metaExpr.Kind)
		}
		metadata = metaExpr
	case lexer.KEYWORD:
		tok := p.current
		kw := form.NewKeyword(tok.Literal, p.spanForToken(tok), tok.Offset, tok.EndOffset)
		truthy := form.NewBoolean(true, p.spanForToken(tok), tok.Offset, tok.EndOffset)
		metadata = form.NewMap(
			p.spanForToken(tok),
			tok.Offset,
			tok.EndOffset,
			form.MapEntry{Key: kw, Value: truthy},
		)
		p.nextToken()
	default:
		return nil, doggoerr.ParserErrorf(p.current.Line, p.current.Column, "metadata must be a map or keyword, got %v", p.current.Type)
	}

	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	return p.applyMetadata(expr, metadata)
}

func (p *Parser) applyMetadata(expr, metadata *form.Node) (*form.Node, error) {
	if expr == nil {
		return nil, nil
	}
	if !expr.SupportsMetadata() {
		return expr, nil
	}
	expr.Metadata = metadata
	return expr, nil
}

// maxHashFnArgs matches compiler.MaxLocalSlots; a rest argument uses one slot.
const maxHashFnArgs = 256

func (p *Parser) parseHashFn() (*form.Node, error) {
	startTok := p.current
	p.nextToken()

	items, err := p.parseCollectionItems(lexer.RPAREN, "anonymous function")
	if err != nil {
		return nil, err
	}
	closeTok := p.current
	p.nextToken()

	body := form.NewList(p.spanFromOffsets(startTok.Offset, closeTok.EndOffset), startTok.Offset, closeTok.EndOffset, items...)

	maxArg := 0
	hasRestArg := false
	if err := p.findArgs(body, &maxArg, &hasRestArg); err != nil {
		return nil, err
	}
	body = p.replacePercentArgs(body)

	params := make([]*form.Node, 0, maxArg+2)
	for i := 1; i <= maxArg; i++ {
		params = append(params, form.NewSymbol("%"+strconv.Itoa(i), p.spanForToken(startTok), startTok.Offset, startTok.EndOffset))
	}
	if hasRestArg {
		params = append(params,
			form.NewSymbol("&", p.spanForToken(startTok), startTok.Offset, startTok.EndOffset),
			form.NewSymbol("%&", p.spanForToken(startTok), startTok.Offset, startTok.EndOffset),
		)
	}

	paramVector := form.NewVector(p.spanForToken(startTok), startTok.Offset, startTok.EndOffset, params...)
	fnSymbol := form.NewSymbol("fn", p.spanForToken(startTok), startTok.Offset, startTok.EndOffset)
	return form.NewList(
		p.spanFromOffsets(startTok.Offset, closeTok.EndOffset),
		startTok.Offset,
		closeTok.EndOffset,
		fnSymbol,
		paramVector,
		body,
	), nil
}

func (p *Parser) findArgs(expr *form.Node, maxArg *int, hasRestArg *bool) error {
	if expr == nil {
		return nil
	}
	switch expr.Kind {
	case form.KindSymbol:
		name := expr.Text
		if !strings.HasPrefix(name, "%") {
			return nil
		}
		line, column := expr.Span.Start.Line, expr.Span.Start.Column
		if p.source == "" {
			for _, tok := range p.tokens {
				if tok.Offset == expr.StartOffset {
					line, column = tok.Line, tok.Column
					break
				}
			}
		}
		switch name {
		case "%":
			if *maxArg < 1 {
				*maxArg = 1
			}
		case "%&":
			*hasRestArg = true
		default:
			n, err := strconv.ParseUint(name[1:], 10, 64)
			if err != nil || n < 1 || n > maxHashFnArgs {
				return doggoerr.ParserErrorf(line, column, "invalid anonymous function argument %q: expected %% or %%& or %%1 through %%%d", name, maxHashFnArgs)
			}
			if int(n) > *maxArg {
				*maxArg = int(n)
			}
		}
		if *hasRestArg && *maxArg == maxHashFnArgs {
			return doggoerr.ParserErrorf(line, column, "anonymous function has too many parameters at %q: maximum is %d including the rest argument", name, maxHashFnArgs)
		}
	case form.KindList, form.KindVector, form.KindSet:
		for _, item := range expr.Items {
			if err := p.findArgs(item, maxArg, hasRestArg); err != nil {
				return err
			}
		}
	case form.KindMap:
		for _, entry := range expr.Entries {
			if err := p.findArgs(entry.Key, maxArg, hasRestArg); err != nil {
				return err
			}
			if err := p.findArgs(entry.Value, maxArg, hasRestArg); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Parser) replacePercentArgs(expr *form.Node) *form.Node {
	if expr == nil {
		return nil
	}

	switch expr.Kind {
	case form.KindSymbol:
		if expr.Text == "%" {
			expr.Text = "%1"
		}
	case form.KindList, form.KindVector, form.KindSet:
		for i := range expr.Items {
			expr.Items[i] = p.replacePercentArgs(expr.Items[i])
		}
	case form.KindMap:
		for i := range expr.Entries {
			expr.Entries[i].Key = p.replacePercentArgs(expr.Entries[i].Key)
			expr.Entries[i].Value = p.replacePercentArgs(expr.Entries[i].Value)
		}
	}

	return expr
}

func (p *Parser) spanForToken(tok lexer.Token) span.Span {
	return p.spanFromOffsets(tok.Offset, tok.EndOffset)
}

func (p *Parser) spanFromOffsets(startOffset, endOffset int) span.Span {
	startOffset = clamp(startOffset, 0, len(p.source))
	endOffset = clamp(endOffset, startOffset, len(p.source))
	startLine, startCol := offsetToLineCol(p.lineOffsets, p.source, startOffset)
	endLine, endCol := offsetToLineCol(p.lineOffsets, p.source, endOffset)
	return span.New("", startLine, startCol, endLine, endCol)
}

func buildLineOffsets(source string) []int {
	offsets := []int{0}
	for i := 0; i < len(source); i++ {
		if source[i] == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

func offsetToLineCol(lineOffsets []int, source string, offset int) (int, int) {
	offset = clamp(offset, 0, len(source))
	lineIdx := 0
	for i := 0; i < len(lineOffsets); i++ {
		if lineOffsets[i] > offset {
			break
		}
		lineIdx = i
	}
	lineStart := lineOffsets[lineIdx]
	col := utf8.RuneCountInString(source[lineStart:offset]) + 1
	return lineIdx + 1, col
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
