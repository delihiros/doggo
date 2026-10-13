package compiler

import (
	"fmt"
	"strconv"
	"strings"

	doggoerr "doggo/internal/errors"
)

// CompileError はコンパイル時のエラーを表します。
type CompileError struct {
	File        string // エラーが発生したファイル名（空は不明）
	Message     string // エラーメッセージ
	Line        int    // エラーが発生した行番号（0は不明）
	Column      int    // エラーが発生した列番号（0は不明）
	SnippetLine string // 該当行のコード断片（空は不明）
	SnippetCol  int    // スニペット上のキャレット列（0は Column を使用）
}

// Error はエラーメッセージを返します。
func (e *CompileError) Error() string {
	file := e.File
	if file == "" {
		file = "<unknown>"
	}

	if e.Line > 0 {
		column := e.Column
		if column <= 0 {
			column = 1
		}

		head := fmt.Sprintf("%s:%d:%d: %s", file, e.Line, column, e.Message)
		if e.SnippetLine == "" {
			return head
		}

		caretCol := e.SnippetCol
		if caretCol <= 0 {
			caretCol = column
		}
		return head + "\n" + formatSnippet(e.Line, e.SnippetLine, caretCol)
	}

	return fmt.Sprintf("%s: %s", file, e.Message)
}

// SetSnippet はスニペット情報を設定したエラーを返します。
func (e *CompileError) SetSnippet(line string, col int) *CompileError {
	e.SnippetLine = line
	e.SnippetCol = col
	return e
}

func newCompileErrorWithContext(file string, line, column int, format string, args ...interface{}) *CompileError {
	return &CompileError{
		File:    file,
		Message: fmt.Sprintf(format, args...),
		Line:    line,
		Column:  column,
	}
}

func (c *Compiler) newCompileError(format string, args ...interface{}) *CompileError {
	if c == nil || c.diagnosticOwner == nil {
		return newCompileErrorWithContext("", 0, 0, format, args...)
	}
	return c.diagnosticOwner.Errorf(format, args...)
}

// SetSourceFile はコンパイル対象のソースファイル名を設定します。
func (c *Compiler) SetSourceFile(file string) {
	if c == nil || c.debugMeta == nil {
		return
	}
	c.debugMeta.SetCurrent(file, 0, 0)
}

// SetSourceLineProvider は file/line からコード断片を取得するコールバックを設定します。
func (c *Compiler) SetSourceLineProvider(provider func(file string, line int) (string, bool)) {
	if c != nil && c.diagnosticOwner != nil {
		c.diagnosticOwner.SetSourceLineProvider(provider)
	}
}

// SetSourceContentProvider は file から完全なソース本文を取得するコールバックを設定します。
func (c *Compiler) SetSourceContentProvider(provider func(file string) (string, bool)) {
	c.sourceContentProvider = provider
}

func formatSnippet(lineNumber int, lineText string, caretCol int) string {
	runes := []rune(lineText)
	if caretCol <= 0 {
		caretCol = 1
	}
	if len(runes) == 0 {
		caretCol = 1
	} else if caretCol > len(runes) {
		caretCol = len(runes)
	}

	lineNum := strconv.Itoa(lineNumber)
	var sb strings.Builder
	sb.WriteString(lineNum)
	sb.WriteString(" | ")
	sb.WriteString(lineText)
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat(" ", len(lineNum)))
	sb.WriteString(" | ")
	sb.WriteString(strings.Repeat(" ", caretCol-1))
	sb.WriteString("^")
	return sb.String()
}

// ExceptionData projects compile diagnostics into the canonical exception-data model.
func (e *CompileError) ExceptionData() *doggoerr.ExceptionData {
	if e == nil {
		return nil
	}

	context := map[string]any(nil)
	if e.SnippetLine != "" {
		context = map[string]any{
			"snippet":           e.SnippetLine,
			"snippet_column":    e.SnippetCol,
			"formatted_snippet": formatSnippet(e.Line, e.SnippetLine, maxSnippetCol(e.Column, e.SnippetCol)),
		}
	}

	return &doggoerr.ExceptionData{
		Type:    "CompileError",
		Kind:    "compile error",
		Phase:   "compile",
		Message: e.Message,
		Context: context,
		At: doggoerr.Position{
			File:   e.File,
			Line:   e.Line,
			Column: e.Column,
		},
	}
}

func maxSnippetCol(column, snippetCol int) int {
	if snippetCol > 0 {
		return snippetCol
	}
	if column > 0 {
		return column
	}
	return 1
}
