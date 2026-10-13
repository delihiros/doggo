// Package vm はDoggo言語の仮想マシンを実装します。
package vm

import (
	"fmt"
	"strings"

	doggoerr "doggo/internal/errors"
	"doggo/internal/types"
)

// StackFrame はスタックトレースの1フレームを表します。
type StackFrame struct {
	File   string // ファイル名（空は不明）
	Line   int    // 行番号（0は不明）
	Column int    // 列番号（0は不明）
	Name   string // 関数名（ない場合は空文字）
}

// RuntimeError はVM実行時のランタイムエラーを表します。
type RuntimeError struct {
	Message       string       // エラーメッセージ
	Cause         error        // 元のエラー
	Value         types.Value  // 元の throw 値（あれば）
	File          string       // エラーが発生したファイル
	Line          int          // エラーが発生した行番号（0は不明）
	Column        int          // エラーが発生した列番号（0は不明）
	CorrelationID string       // 相関 ID（空は不明）
	Snippet       string       // 該当行のソースコード
	SnippetCol    int          // スニペット表示上の列
	StackTrace    []StackFrame // スタックトレース
}

// Unwrap は元のエラーを返します。
func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Error はエラーメッセージを返します。
func (e *RuntimeError) Error() string {
	var sb strings.Builder
	sb.WriteString(formatDiagnostic(e.File, e.Line, e.Column, e.Message, e.Snippet, e.SnippetCol))
	if e.CorrelationID != "" {
		sb.WriteString("\ncorrelation-id: ")
		sb.WriteString(e.CorrelationID)
	}

	// スタックトレースを追加
	if len(e.StackTrace) > 0 {
		sb.WriteString("\nStack trace:")
		for i, frame := range e.StackTrace {
			sb.WriteString("\n  [")
			sb.WriteString(fmt.Sprintf("%d", i))
			sb.WriteString("] ")
			sb.WriteString(formatStackFrame(frame))
		}
	}

	return sb.String()
}

// ExceptionData projects runtime diagnostics into the canonical exception-data model.
func (e *RuntimeError) ExceptionData() *doggoerr.ExceptionData {
	if e == nil {
		return nil
	}

	data := map[string]any(nil)
	if e.CorrelationID != "" || e.Snippet != "" || e.SnippetCol > 0 {
		data = map[string]any{}
		if e.CorrelationID != "" {
			data["correlation-id"] = e.CorrelationID
		}
		if e.Snippet != "" {
			data["snippet"] = e.Snippet
		}
		if e.SnippetCol > 0 {
			data["snippet_column"] = e.SnippetCol
		}
	}

	context := map[string]any(nil)
	if e.Snippet != "" || e.SnippetCol > 0 {
		context = map[string]any{
			"snippet":        e.Snippet,
			"snippet_column": e.SnippetCol,
		}
	}

	trace := make([]doggoerr.ExceptionFrame, 0, len(e.StackTrace))
	for _, frame := range e.StackTrace {
		trace = append(trace, doggoerr.ExceptionFrame{
			File:   frame.File,
			Line:   frame.Line,
			Column: frame.Column,
			Name:   frame.Name,
		})
	}

	return &doggoerr.ExceptionData{
		Type:          "RuntimeError",
		Kind:          "runtime error",
		Phase:         "runtime",
		Message:       e.Message,
		Original:      e.Value,
		Cause:         doggoerr.NormalizeExceptionData(e.Cause),
		CorrelationID: e.CorrelationID,
		Data:          data,
		Context:       context,
		At: doggoerr.Position{
			File:   e.File,
			Line:   e.Line,
			Column: e.Column,
		},
		Trace: trace,
	}
}

// VMError はVM実行中の例外を表します。
type VMError struct {
	Value types.Value
}

func (e *VMError) Error() string {
	return e.Value.String()
}

// newRuntimeError は現在の実行位置とスタックトレース付きのエラーを作成します。
func (vm *VM) newRuntimeError(format string, args ...interface{}) *RuntimeError {
	message := fmt.Sprintf(format, args...)

	// 現在の位置情報を取得
	currentFile := ""
	currentLine := 0
	currentColumn := 0
	if vm.frameIndex > 0 {
		frame := vm.currentFrame()
		currentFile, currentLine, currentColumn = frame.CurrentPosition()
	}

	// スタックトレースを構築
	trace := vm.captureStackTrace()

	snippet, snippetCol := vm.resolveSnippet(currentFile, currentLine, currentColumn)

	return &RuntimeError{
		Message:       message,
		File:          currentFile,
		Line:          currentLine,
		Column:        currentColumn,
		CorrelationID: correlationIDFromContext(vm.ctx),
		Snippet:       snippet,
		SnippetCol:    snippetCol,
		StackTrace:    trace,
	}
}

// handleError はエラーを処理し、例外ハンドラがあればそこへ制御を移します。
func (vm *VM) handleError(err error) error {
	// ハンドラ検索前にスタックトレースを保存（フレームがポップされる前に）
	savedTrace := vm.captureStackTrace()
	exception := vm.normalizeException(err, savedTrace)

	// 現在のフレームから順にハンドラを探す
	for vm.frameIndex > 0 {
		frame := vm.currentFrame()
		if len(frame.catchStack) > 0 {
			// ハンドラが見つかった
			// ハンドラを取得してスタックから削除
			handler := frame.catchStack[len(frame.catchStack)-1]
			frame.catchStack = frame.catchStack[:len(frame.catchStack)-1]

			// スタックを try 開始時点に復元（オペランドを保持）
			vm.sp = handler.SavedSP

			// 例外オブジェクトをプッシュ
			if pushErr := vm.push(exception); pushErr != nil {
				return pushErr
			}

			// 制御をハンドラに移す
			frame.ip = handler.HandlerIP - 1 // 次のループでインクリメントされるため

			return nil // 実行継続
		}
		// このフレームにはハンドラがないのでポップして親フレームへ
		vm.popFrame()
	}

	// ハンドラが見つからなかった場合、スタックトレース付きエラーを返す
	if vmErr, ok := err.(*VMError); ok {
		// VMError をスタックトレース付きの RuntimeError に変換
		currentFile := ""
		currentLine := 0
		currentColumn := 0
		if len(savedTrace) > 0 {
			currentFile = savedTrace[0].File
			currentLine = savedTrace[0].Line
			currentColumn = savedTrace[0].Column
		}
		// Displayable を実装している場合は Display() を使用（人間が読みやすい形式）
		message := vmErr.Value.String()
		if d, ok := vmErr.Value.(types.Displayable); ok {
			message = d.Display()
		}
		snippet, snippetCol := vm.resolveSnippet(currentFile, currentLine, currentColumn)
		return &RuntimeError{
			Message:       message,
			Value:         vmErr.Value,
			File:          currentFile,
			Line:          currentLine,
			Column:        currentColumn,
			CorrelationID: correlationIDFromContext(vm.ctx),
			Snippet:       snippet,
			SnippetCol:    snippetCol,
			StackTrace:    savedTrace,
		}
	}

	if runtimeErr, ok := err.(*RuntimeError); ok {
		if runtimeErr.CorrelationID == "" {
			runtimeErr.CorrelationID = correlationIDFromContext(vm.ctx)
		}
		if len(runtimeErr.StackTrace) == 0 {
			runtimeErr.StackTrace = savedTrace
		}
		return runtimeErr
	}

	currentFile := ""
	currentLine := 0
	currentColumn := 0
	if len(savedTrace) > 0 {
		currentFile = savedTrace[0].File
		currentLine = savedTrace[0].Line
		currentColumn = savedTrace[0].Column
	}
	snippet, snippetCol := vm.resolveSnippet(currentFile, currentLine, currentColumn)
	return &RuntimeError{
		Message:       err.Error(),
		Cause:         err,
		File:          currentFile,
		Line:          currentLine,
		Column:        currentColumn,
		CorrelationID: correlationIDFromContext(vm.ctx),
		Snippet:       snippet,
		SnippetCol:    snippetCol,
		StackTrace:    savedTrace,
	}
}

func formatExceptionStackTrace(trace []StackFrame) []string {
	if len(trace) == 0 {
		return nil
	}
	formatted := make([]string, len(trace))
	for i, frame := range trace {
		formatted[i] = formatStackFrame(frame)
	}
	return formatted
}

// captureStackTrace は現在のスタックトレースを取得します。
func (vm *VM) captureStackTrace() []StackFrame {
	var trace []StackFrame
	for i := vm.frameIndex - 1; i >= 0; i-- {
		frame := vm.frames[i]
		if frame == nil {
			continue
		}
		fnName := ""
		if frame.cl != nil && frame.cl.Fn != nil {
			fnName = frame.cl.Fn.Name
		}
		file, line, column := frame.CurrentPosition()
		trace = append(trace, StackFrame{
			File:   file,
			Line:   line,
			Column: column,
			Name:   fnName,
		})
	}
	return trace
}

func (vm *VM) resolveSnippet(file string, line, column int) (string, int) {
	if vm.sourceLineProvider == nil || line <= 0 {
		return "", 0
	}
	if file == "" {
		file = "<unknown>"
	}
	sourceLine, ok := vm.sourceLineProvider(file, line)
	if !ok {
		return "", 0
	}
	if column <= 0 {
		column = 1
	}
	return sourceLine, column
}

func formatStackFrame(frame StackFrame) string {
	file := frame.File
	if file == "" {
		file = "<unknown>"
	}
	if frame.Line > 0 {
		col := frame.Column
		if col <= 0 {
			col = 1
		}
		if frame.Name != "" {
			return fmt.Sprintf("%s:%d:%d in %s", file, frame.Line, col, frame.Name)
		}
		return fmt.Sprintf("%s:%d:%d", file, frame.Line, col)
	}
	if frame.Name != "" {
		return fmt.Sprintf("%s in %s", file, frame.Name)
	}
	return file
}
