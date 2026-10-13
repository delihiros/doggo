package vm

import (
	"strings"
	"testing"
)

func TestClosedChannelOperationsReturnErrors(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{name: "close twice", expr: `(let [ch (chan)] (close! ch) (close! ch))`, want: "close of closed channel"},
		{name: "send", expr: `(let [ch (chan 1)] (close! ch) (>! ch 1))`, want: "send on closed channel"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			if _, err := runner.Run(tt.expr); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestChannelConstructorRejectsInvalidArguments(t *testing.T) {
	for _, expr := range []string{
		`(chan 1 2)`,
		`(chan 9223372036854775807)`,
	} {
		runner := MustNewRunner()
		if _, err := runner.Run(expr); err == nil {
			t.Fatalf("Run(%q) succeeded, want error", expr)
		}
	}
}

func TestChannelBasicOperations(t *testing.T) {
	runner := MustNewRunner()

	// 基本的なチャネル送受信
	result, err := runner.Run(`
		(def ch (chan 1))
		(>! ch 42)
		(<! ch)
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.String() != "42" {
		t.Errorf("expected '42', got '%s'", result.String())
	}
}

func TestChannelWithGoroutine(t *testing.T) {
	runner := MustNewRunner()

	// goブロック内でのチャネル操作
	result, err := runner.Run(`
		(def ch (chan))
		(go (>! ch "from goroutine"))
		(<! ch)
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.String() != `"from goroutine"` {
		t.Errorf("expected '\"from goroutine\"', got '%s'", result.String())
	}
}

func TestGoBuiltinCapturesClosureRuntime(t *testing.T) {
	runner := MustNewRunner()

	if _, err := runner.Run(`
		(def ch (chan 1))
		(go (>! ch (count [1 2 3])))
	`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRunResult(t, runner, "(<! ch)", "3")
}
