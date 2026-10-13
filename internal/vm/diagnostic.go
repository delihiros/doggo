package vm

import (
	"fmt"
	"strconv"
	"strings"
)

func clampCaretColumn(lineText string, col int) int {
	if col <= 0 {
		col = 1
	}
	runes := []rune(lineText)
	if len(runes) == 0 {
		return 1
	}
	if col > len(runes) {
		return len(runes)
	}
	return col
}

func renderSnippet(lineNumber int, lineText string, caretCol int) string {
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

func formatDiagnostic(file string, line, column int, message, snippet string, snippetCol int) string {
	if file == "" {
		file = "<unknown>"
	}
	if line <= 0 {
		return fmt.Sprintf("%s: %s", file, message)
	}
	if column <= 0 {
		column = 1
	}

	head := fmt.Sprintf("%s:%d:%d: %s", file, line, column, message)
	if snippet == "" {
		return head
	}

	if snippetCol <= 0 {
		snippetCol = column
	}
	snippetCol = clampCaretColumn(snippet, snippetCol)
	return head + "\n" + renderSnippet(line, snippet, snippetCol)
}

// SourceDiagnosticError は parser/compile/runtime 以外の入力エラーを
// file:line:column + snippet 形式で表示するためのラッパーです。
type SourceDiagnosticError struct {
	File       string
	Line       int
	Column     int
	Message    string
	Snippet    string
	SnippetCol int
}

func (e *SourceDiagnosticError) Error() string {
	return formatDiagnostic(e.File, e.Line, e.Column, e.Message, e.Snippet, e.SnippetCol)
}
