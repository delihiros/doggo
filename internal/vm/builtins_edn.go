package vm

import (
	"context"
	stderrors "errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	doggoerr "doggo/internal/errors"
	"doggo/internal/lexer"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type ednReader struct {
	ctx      context.Context
	tokens   []lexer.Token
	position int
	current  lexer.Token
	options  ednReadOptions
}

var builtinsEDN = []BuiltinDefinition{
	{
		Name:       "edn-read-string",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("edn-read-string", args, 1, 2); err != nil {
				return nil, err
			}

			input, ok := args[0].(atom.String)
			if !ok {
				return nil, ednInvalidArg("doggo.edn/read-string", "argument must be a string, got %s", args[0].Type())
			}

			var optionsArg types.Value = coll.NewMap(nil)
			if len(args) == 2 {
				optionsArg = args[1]
			}
			options, err := ednParseReadOptions("doggo.edn/read-string", optionsArg, true)
			if err != nil {
				return nil, err
			}

			value, err := readEDNString(ctx, string(input), options)
			if err != nil {
				if options.location {
					return nil, ednReadFailure("doggo.edn/read-string", err)
				}
				return nil, ednInvalidArg("doggo.edn/read-string", "%v", err)
			}
			return value, nil
		}},
	},
	{
		Name:       "edn-read-string-all*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("doggo.edn/read-string-all", args, 1, 2); err != nil {
				return nil, err
			}

			input, ok := args[0].(atom.String)
			if !ok {
				return nil, ednInvalidArg("doggo.edn/read-string-all", "argument must be a string, got %s", args[0].Type())
			}

			var optionsArg types.Value = coll.NewMap(nil)
			if len(args) == 2 {
				optionsArg = args[1]
			}
			options, err := ednParseReadOptions("doggo.edn/read-string-all", optionsArg, false)
			if err != nil {
				return nil, err
			}

			values, err := readAllEDNString(ctx, string(input), options)
			if err != nil {
				if options.location {
					return nil, ednReadFailure("doggo.edn/read-string-all", err)
				}
				return nil, ednInvalidArg("doggo.edn/read-string-all", "%v", err)
			}
			return coll.NewVector(values...), nil
		}},
	},
	{
		Name:       "edn-write-string",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("edn-write-string", args, 1, 2); err != nil {
				return nil, err
			}

			var optionsArg types.Value = coll.NewMap(nil)
			if len(args) == 2 {
				optionsArg = args[1]
			}
			options, err := ednParseWriteOptions("doggo.edn/write-string", optionsArg)
			if err != nil {
				return nil, err
			}

			encoded, err := ednWriteStringWithOptions(args[0], options)
			if err != nil {
				return nil, ednInvalidArg("doggo.edn/write-string", "%v", err)
			}
			return atom.NewString(encoded), nil
		}},
	},
}

func ednInvalidArg(label, format string, args ...any) error {
	return invalidArgumentVMError(label, fmt.Sprintf("%s: %s", label, fmt.Sprintf(format, args...)))
}

type ednReadDiagnosticError struct {
	label   string
	err     error
	at      doggoerr.Position
	context map[string]any
}

func (e *ednReadDiagnosticError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *ednReadDiagnosticError) ExceptionData() *doggoerr.ExceptionData {
	if e == nil {
		return nil
	}

	data := ednCanonicalReadErrorData(e.err)
	if data == nil {
		data = &doggoerr.ExceptionData{
			Type:    "ReadError",
			Kind:    "read error",
			Message: e.Error(),
		}
	}

	data = data.Clone()
	if data.Type == "" {
		data.Type = "ReadError"
	}
	if data.Kind == "" {
		data.Kind = "read error"
	}
	if data.Message == "" {
		data.Message = e.Error()
	}
	if e.label != "" || len(e.context) > 0 {
		if data.Context == nil {
			data.Context = map[string]any{}
		}
		if e.label != "" {
			data.Context["context"] = e.label
		}
		for key, value := range e.context {
			data.Context[key] = value
		}
	}
	data.Phase = "read"
	if data.Data == nil {
		data.Data = map[string]any{}
	}
	data.Data["phase"] = "read"
	if len(e.context) > 0 {
		publicContext := make(map[string]any, len(e.context))
		for key, value := range e.context {
			publicContext[key] = value
		}
		data.Data["context"] = publicContext
	}
	if !e.at.IsZero() {
		data.At = e.at
		if len(data.Trace) == 0 {
			data.Trace = []doggoerr.ExceptionFrame{{
				File:   e.at.File,
				Line:   e.at.Line,
				Column: e.at.Column,
			}}
		}
	}
	return data
}

func ednCanonicalReadErrorData(err error) *doggoerr.ExceptionData {
	if err == nil {
		return nil
	}

	var diagnostic *ednReadDiagnosticError
	if stderrors.As(err, &diagnostic) {
		return diagnostic.ExceptionData()
	}

	var vmErr *VMError
	if stderrors.As(err, &vmErr) {
		if exc, ok := vmErr.Value.(*atom.Exception); ok {
			return exc.ExceptionData()
		}
		return doggoerr.NewThrownValueData(vmErr.Value.Type(), vmErr.Value.String(), vmErr.Value)
	}

	return doggoerr.NormalizeExceptionData(err)
}

func ednReadFailure(label string, err error) error {
	return &VMError{Value: exceptionFromCanonicalData((&ednReadDiagnosticError{
		label: label,
		err:   err,
	}).ExceptionData())}
}

func ednReadErrorAtWithContext(err error, line, column int, context map[string]any) error {
	if err == nil {
		return nil
	}
	if line <= 0 || column <= 0 {
		if len(context) > 0 {
			return &ednReadDiagnosticError{
				err:     err,
				context: context,
			}
		}
		return err
	}
	return &ednReadDiagnosticError{
		err:     err,
		context: context,
		at: doggoerr.Position{
			Line:   line,
			Column: column,
		},
	}
}

func readEDNString(ctx context.Context, input string, options ednReadOptions) (types.Value, error) {
	l := lexer.New(input)
	tokens, err := l.Tokenize()
	if err != nil {
		return nil, err
	}

	reader := newEDNReader(ctx, tokens, options)
	return reader.readSingle()
}

func readAllEDNString(ctx context.Context, input string, options ednReadOptions) ([]types.Value, error) {
	l := lexer.New(input)
	tokens, err := l.Tokenize()
	if err != nil {
		return nil, err
	}

	reader := newEDNReader(ctx, tokens, options)
	return reader.readAllForms()
}

func newEDNReader(ctx context.Context, tokens []lexer.Token, options ednReadOptions) *ednReader {
	reader := &ednReader{
		ctx:     ctx,
		tokens:  tokens,
		options: options,
	}
	if len(tokens) > 0 {
		reader.current = tokens[0]
	} else {
		reader.current = lexer.Token{Type: lexer.EOF}
	}
	return reader
}

func (r *ednReader) nextToken() {
	r.position++
	if r.position < len(r.tokens) {
		r.current = r.tokens[r.position]
		return
	}
	r.current = lexer.Token{Type: lexer.EOF}
}

func (r *ednReader) readSingle() (types.Value, error) {
	forms, err := r.readAllForms()
	if err != nil {
		return nil, err
	}
	if len(forms) == 0 {
		if r.options.hasEOF {
			return r.options.eofValue, nil
		}
		return atom.NilValue, nil
	}
	if len(forms) != 1 {
		return nil, fmt.Errorf("expected a single EDN value")
	}
	return forms[0], nil
}

func (r *ednReader) readAllForms() ([]types.Value, error) {
	var forms []types.Value
	for r.current.Type != lexer.EOF {
		if r.current.Type == lexer.DISCARD {
			if err := r.discardNextForm(); err != nil {
				return nil, err
			}
			continue
		}
		form, err := r.parseExpression()
		if err != nil {
			return nil, err
		}
		forms = append(forms, form)
	}
	return forms, nil
}

func (r *ednReader) discardNextForm() error {
	r.nextToken()
	if r.current.Type == lexer.EOF {
		return doggoerr.ParserErrorf(r.current.Line, r.current.Column, "#_ requires a form to discard")
	}
	if r.current.Type == lexer.DISCARD {
		return r.discardNextForm()
	}
	_, err := r.parseExpression()
	return err
}

func (r *ednReader) parseExpression() (types.Value, error) {
	tok := r.current
	switch tok.Type {
	case lexer.EOF:
		return nil, doggoerr.ParserError("unexpected EOF", tok.Line, tok.Column)
	case lexer.LPAREN:
		return r.parseList()
	case lexer.LBRACKET:
		return r.parseVector()
	case lexer.LBRACE:
		return r.parseMap()
	case lexer.HASHSET:
		return r.parseSet()
	case lexer.IDENT:
		return r.parseIdent()
	case lexer.NUMBER:
		return r.parseNumber()
	case lexer.STRING:
		value := atom.NewString(tok.Literal)
		r.nextToken()
		return value, nil
	case lexer.KEYWORD:
		value := atom.NewKeyword(tok.Literal)
		r.nextToken()
		return value, nil
	case lexer.CHAR:
		ch, ok := atom.NewCharacterFromLiteral(tok.Literal)
		if !ok {
			return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "invalid character literal: \\%s", tok.Literal)
		}
		r.nextToken()
		return ch, nil
	case lexer.QUOTE:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "quote reader syntax is not EDN")
	case lexer.QUASIQUOTE:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "quasiquote reader syntax is not EDN")
	case lexer.UNQUOTE:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "unquote reader syntax is not EDN")
	case lexer.SPLICE:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "splice reader syntax is not EDN")
	case lexer.DEREF:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "deref reader syntax is not EDN")
	case lexer.DOT:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "dot reader syntax is not EDN")
	case lexer.HASHFN:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "function reader syntax is not EDN")
	case lexer.VAR_QUOTE:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "var quote reader syntax is not EDN")
	case lexer.METADATA:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "metadata reader syntax is not EDN")
	case lexer.REGEX:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "regex literals are not EDN")
	case lexer.DISCARD:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "unexpected token '#_'")
	default:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "unexpected token '%s'", tok.Literal)
	}
}

func (r *ednReader) parseIdent() (types.Value, error) {
	tok := r.current
	switch tok.Literal {
	case "true":
		r.nextToken()
		return atom.True, nil
	case "false":
		r.nextToken()
		return atom.False, nil
	case "nil":
		r.nextToken()
		return atom.NilValue, nil
	}

	if strings.HasPrefix(tok.Literal, "##") {
		return r.parseSymbolicValue(tok)
	}
	if strings.HasPrefix(tok.Literal, "#") {
		return r.parseTaggedLiteral(tok)
	}

	r.nextToken()
	return types.NewSymbol(tok.Literal), nil
}

func (r *ednReader) parseSymbolicValue(tok lexer.Token) (types.Value, error) {
	r.nextToken()
	switch tok.Literal {
	case "##NaN":
		return atom.NewNumberFloat(math.NaN()), nil
	case "##Inf":
		return atom.NewNumberFloat(math.Inf(1)), nil
	case "##-Inf":
		return atom.NewNumberFloat(math.Inf(-1)), nil
	default:
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "unknown symbolic value: %s", tok.Literal)
	}
}

func (r *ednReader) parseTaggedLiteral(tok lexer.Token) (types.Value, error) {
	tagName := strings.TrimPrefix(tok.Literal, "#")
	tag := types.NewSymbol(tagName)

	r.nextToken()
	for r.current.Type == lexer.DISCARD {
		if err := r.discardNextForm(); err != nil {
			return nil, err
		}
	}
	if r.current.Type == lexer.EOF {
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "tagged literal #%s requires a value", tagName)
	}

	value, err := r.parseExpression()
	if err != nil {
		return nil, err
	}
	taggedValue := value
	value, err = r.applyTaggedLiteral(tag, value)
	if r.options.location {
		return value, ednReadErrorAtWithContext(err, tok.Line, tok.Column, map[string]any{
			"tag":   tag,
			"value": taggedValue,
		})
	}
	return value, err
}

func (r *ednReader) parseNumber() (types.Value, error) {
	tok := r.current
	r.nextToken()

	if strings.ContainsRune(tok.Literal, '.') || strings.ContainsAny(tok.Literal, "eE") {
		value, err := strconv.ParseFloat(tok.Literal, 64)
		if err != nil {
			return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "invalid float number '%s'", tok.Literal)
		}
		return atom.NewNumberFloat(value), nil
	}

	value, err := strconv.ParseInt(tok.Literal, 10, 64)
	if err != nil {
		return nil, doggoerr.ParserErrorf(tok.Line, tok.Column, "invalid integer '%s'", tok.Literal)
	}
	return atom.NewNumberInt(value), nil
}

func (r *ednReader) parseList() (types.Value, error) {
	line := r.current.Line
	column := r.current.Column
	r.nextToken()
	items, err := r.parseCollectionItems(lexer.RPAREN, "list")
	if err != nil {
		return nil, err
	}
	r.nextToken()
	return coll.NewListWithPos(line, column, items...), nil
}

func (r *ednReader) parseVector() (types.Value, error) {
	r.nextToken()
	items, err := r.parseCollectionItems(lexer.RBRACKET, "vector")
	if err != nil {
		return nil, err
	}
	r.nextToken()
	return coll.NewVector(items...), nil
}

func (r *ednReader) parseMap() (types.Value, error) {
	start := r.current
	r.nextToken()
	items, err := r.parseCollectionItems(lexer.RBRACE, "map")
	if err != nil {
		return nil, err
	}
	if len(items)%2 != 0 {
		return nil, doggoerr.ParserErrorf(start.Line, start.Column, "map must have even number of elements (key-value pairs)")
	}
	r.nextToken()
	return coll.NewMapFromPairs(items), nil
}

func (r *ednReader) parseSet() (types.Value, error) {
	r.nextToken()
	items, err := r.parseCollectionItems(lexer.RBRACE, "set")
	if err != nil {
		return nil, err
	}
	r.nextToken()
	return coll.NewSet(items...), nil
}

func (r *ednReader) parseCollectionItems(closeToken lexer.TokenType, collectionType string) ([]types.Value, error) {
	var items []types.Value

	for r.current.Type != closeToken {
		if r.current.Type == lexer.EOF {
			return nil, doggoerr.ParserErrorf(r.current.Line, r.current.Column, "unexpected EOF while parsing %s", collectionType)
		}
		if r.current.Type == lexer.DISCARD {
			if err := r.discardNextForm(); err != nil {
				return nil, err
			}
			continue
		}
		item, err := r.parseExpression()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, nil
}

func (r *ednReader) applyTaggedLiteral(tag types.Symbol, value types.Value) (types.Value, error) {
	if readerFn, ok := r.options.readers[tag.String()]; ok {
		return callEDNReaderFn(r.ctx, readerFn, []types.Value{value})
	}
	if builtinReader := builtinEDNTaggedReader(tag.String()); builtinReader != nil {
		return builtinReader(value)
	}
	if r.options.defaultFn != nil && r.options.defaultFn != atom.NilValue {
		return callEDNReaderFn(r.ctx, r.options.defaultFn, []types.Value{tag, value})
	}
	return nil, fmt.Errorf("no reader function for tag %s", tag.String())
}

func builtinEDNTaggedReader(tag string) func(types.Value) (types.Value, error) {
	switch tag {
	case "inst":
		return readEDNInstantValue
	case "uuid":
		return readEDNUUIDValue
	default:
		return nil
	}
}

func readEDNInstantValue(value types.Value) (types.Value, error) {
	text, ok := value.(atom.String)
	if !ok {
		return nil, fmt.Errorf("#inst data reader expected string")
	}
	parsed, _, err := parseTimestampValue(string(text))
	if err != nil {
		return nil, &VMError{
			Value: atom.NewException("IllegalArgumentException", err.Error(), nil),
		}
	}
	return atom.NewInstantFromTime(parsed), nil
}

func readEDNUUIDValue(value types.Value) (types.Value, error) {
	text, ok := value.(atom.String)
	if !ok {
		return nil, fmt.Errorf("#uuid data reader expected string")
	}
	return atom.NewUUID(string(text))
}

func callEDNReaderFn(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	vm := GetVM(ctx)
	if vm == nil {
		return nil, fmt.Errorf("VM not available in context")
	}
	return applyWithVM(ctx, vm, fn, args)
}
