// Package errors は doggo の構造化エラーハンドリングを提供します。
package errors

import (
	"errors"
	"fmt"
	"strings"
)

// Kind はエラーの種類を表します。
type Kind int

const (
	KindUnknown   Kind = iota
	KindLexer          // 字句解析エラー
	KindParser         // 構文解析エラー
	KindEvaluator      // 評価エラー
	KindType           // 型エラー
	KindArgument       // 引数エラー
	KindNotFound       // 見つからないエラー
	KindRuntime        // ランタイムエラー
)

func (k Kind) String() string {
	switch k {
	case KindLexer:
		return "lexer error"
	case KindParser:
		return "parser error"
	case KindEvaluator:
		return "evaluator error"
	case KindType:
		return "type error"
	case KindArgument:
		return "argument error"
	case KindNotFound:
		return "not found"
	case KindRuntime:
		return "runtime error"
	default:
		return "error"
	}
}

// Position はソースコード内の位置情報を保持します。
type Position struct {
	File   string // ファイル名（オプション）
	Line   int    // 行番号（1から開始）
	Column int    // 列番号（1から開始）
}

func (p Position) String() string {
	if p.Line == 0 && p.Column == 0 {
		return ""
	}
	if p.File != "" {
		return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Column)
	}
	return fmt.Sprintf("line %d, column %d", p.Line, p.Column)
}

// IsZero は位置情報が未設定かどうかを返します。
func (p Position) IsZero() bool {
	return p.Line == 0 && p.Column == 0 && p.File == ""
}

// DoggoError は doggo のエラーを表します。
type DoggoError struct {
	Kind     Kind     // エラーの種類
	Message  string   // エラーメッセージ
	Position Position // 位置情報
	Cause    error    // 元のエラー
	Context  string   // コンテキスト情報（関数名など）
}

// Error はerrorインターフェースを実装します。
func (e *DoggoError) Error() string {
	var b strings.Builder

	// Kind prefix
	if e.Kind != KindUnknown {
		b.WriteString(e.Kind.String())
		b.WriteString(": ")
	}

	// Context
	if e.Context != "" {
		b.WriteString(e.Context)
		b.WriteString(": ")
	}

	// Message
	b.WriteString(e.Message)

	// Position
	if !e.Position.IsZero() {
		b.WriteString(" at ")
		b.WriteString(e.Position.String())
	}

	return b.String()
}

// Unwrap はラップされたエラーを返します。
func (e *DoggoError) Unwrap() error {
	return e.Cause
}

// New は新しいDoggoErrorを作成します。
func New(kind Kind, message string) *DoggoError {
	return &DoggoError{
		Kind:    kind,
		Message: message,
	}
}

// Newf はフォーマット付きで新しいDoggoErrorを作成します。
func Newf(kind Kind, format string, args ...any) *DoggoError {
	return &DoggoError{
		Kind:    kind,
		Message: fmt.Sprintf(format, args...),
	}
}

// SetPosition は位置情報を設定したエラーを返します。
func (e *DoggoError) SetPosition(line, column int) *DoggoError {
	e.Position.Line = line
	e.Position.Column = column
	return e
}

// SetFile はファイル名を設定したエラーを返します。
func (e *DoggoError) SetFile(file string) *DoggoError {
	e.Position.File = file
	return e
}

// SetContext はコンテキスト情報を設定したエラーを返します。
func (e *DoggoError) SetContext(context string) *DoggoError {
	e.Context = context
	return e
}

// SetCause は元のエラーを設定したエラーを返します。
func (e *DoggoError) SetCause(cause error) *DoggoError {
	e.Cause = cause
	return e
}

// Wrap は既存のエラーをラップしたDoggoErrorを作成します。
func Wrap(kind Kind, err error, message string) *DoggoError {
	return &DoggoError{
		Kind:    kind,
		Message: message,
		Cause:   err,
	}
}

// Wrapf は既存のエラーをフォーマット付きでラップしたDoggoErrorを作成します。
func Wrapf(kind Kind, err error, format string, args ...any) *DoggoError {
	return &DoggoError{
		Kind:    kind,
		Message: fmt.Sprintf(format, args...),
		Cause:   err,
	}
}

// ---- 便利なファクトリ関数 ----

// LexerError は字句解析エラーを作成します。
func LexerError(message string, line, column int) *DoggoError {
	return New(KindLexer, message).SetPosition(line, column)
}

// LexerErrorf はフォーマット付きで字句解析エラーを作成します。
func LexerErrorf(line, column int, format string, args ...any) *DoggoError {
	return Newf(KindLexer, format, args...).SetPosition(line, column)
}

// ParserError は構文解析エラーを作成します。
func ParserError(message string, line, column int) *DoggoError {
	return New(KindParser, message).SetPosition(line, column)
}

// ParserErrorf はフォーマット付きで構文解析エラーを作成します。
func ParserErrorf(line, column int, format string, args ...any) *DoggoError {
	return Newf(KindParser, format, args...).SetPosition(line, column)
}

// TypeError は型エラーを作成します。
func TypeError(context, expected, got string) *DoggoError {
	return &DoggoError{
		Kind:    KindType,
		Context: context,
		Message: fmt.Sprintf("expected %s, got %s", expected, got),
	}
}

// ArgCountError は引数数エラーを作成します。
func ArgCountError(formName string, min, max, actual int) *DoggoError {
	var message string
	if min == max {
		message = fmt.Sprintf("expects %d argument(s), got %d", min, actual)
	} else if max == -1 {
		message = fmt.Sprintf("expects at least %d argument(s), got %d", min, actual)
	} else {
		message = fmt.Sprintf("expects %d to %d argument(s), got %d", min, max, actual)
	}
	return &DoggoError{
		Kind:    KindArgument,
		Context: formName,
		Message: message,
	}
}

// NotFoundError は見つからないエラーを作成します。
func NotFoundError(context, itemType, itemName string) *DoggoError {
	return &DoggoError{
		Kind:    KindNotFound,
		Context: context,
		Message: fmt.Sprintf("%s not found: %s", itemType, itemName),
	}
}

// EvalError は評価エラーを作成します。
func EvalError(context, message string) *DoggoError {
	return &DoggoError{
		Kind:    KindEvaluator,
		Context: context,
		Message: message,
	}
}

// EvalErrorf はフォーマット付きで評価エラーを作成します。
func EvalErrorf(context string, format string, args ...any) *DoggoError {
	return &DoggoError{
		Kind:    KindEvaluator,
		Context: context,
		Message: fmt.Sprintf(format, args...),
	}
}

// RuntimeError はランタイムエラーを作成します。
func RuntimeError(message string) *DoggoError {
	return New(KindRuntime, message)
}

// ---- エラー検査関数 ----

// IsDoggoError はエラーがDoggoErrorかどうかを判定します。
func IsDoggoError(err error) bool {
	var doggoErr *DoggoError
	return errors.As(err, &doggoErr)
}

// AsDoggoError searches an error chain for a DoggoError.
func AsDoggoError(err error) (*DoggoError, bool) {
	var doggoErr *DoggoError
	if errors.As(err, &doggoErr) {
		return doggoErr, true
	}
	return nil, false
}

// ExceptionData projects the historical DoggoError into the canonical
// exception-data model.
func (e *DoggoError) ExceptionData() *ExceptionData {
	if e == nil {
		return nil
	}

	context := map[string]any(nil)
	if e.Context != "" {
		context = map[string]any{"context": e.Context}
	}

	phase := ""
	switch e.Kind {
	case KindLexer, KindParser:
		phase = "read"
	case KindRuntime, KindEvaluator:
		phase = "runtime"
	case KindType, KindArgument, KindNotFound:
		phase = "analysis"
	}

	return &ExceptionData{
		Type:    "DoggoError",
		Kind:    e.Kind.String(),
		Phase:   phase,
		Message: e.Message,
		Context: context,
		At:      e.Position,
		Cause:   NormalizeExceptionData(e.Cause),
	}
}
