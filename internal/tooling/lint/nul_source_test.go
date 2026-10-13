package lint

import (
	"strings"
	"testing"
)

func TestLintRejectsRawNULBeforeInvalidTail(t *testing.T) {
	issues := AnalyzeWithOptions("nul.dog", "42\n\x00(\n", AnalyzeOptions{})
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "NUL") || issues[0].Line != 2 || issues[0].Column != 1 {
		t.Fatalf("diagnostics = %v, want NUL at 2:1", issues)
	}
}
