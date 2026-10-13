package vm

import (
	"context"
	"strings"
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

type stubMacroClosureExecutor struct {
	called bool
	ctx    context.Context
	fn     *compiler.Closure
	args   []types.Value
	result types.Value
	err    error
}

func (s *stubMacroClosureExecutor) InvokeClosure(ctx context.Context, fn *compiler.Closure, args []types.Value) (types.Value, error) {
	s.called = true
	s.ctx = ctx
	s.fn = fn
	s.args = append([]types.Value{}, args...)
	return s.result, s.err
}

func TestMacroExpanderEvaluateMacroUsesInjectedExecutor(t *testing.T) {
	expander := NewMacroExpander()
	executor := &stubMacroClosureExecutor{result: atom.NewKeyword("expanded")}
	expander.SetClosureExecutor(executor)

	macro := &compiler.Closure{Fn: &compiler.CompiledFunction{Name: "macro-fn"}}
	args := []types.Value{atom.NewNumberInt(1), atom.NewString("x")}

	result, err := expander.EvaluateMacro(macro, args)
	if err != nil {
		t.Fatalf("EvaluateMacro failed: %v", err)
	}
	if !executor.called {
		t.Fatal("expected injected executor to be called")
	}
	if executor.ctx == nil {
		t.Fatal("expected executor context to be set")
	}
	if executor.fn != macro {
		t.Fatal("executor received wrong closure")
	}
	if len(executor.args) != len(args) {
		t.Fatalf("executor received %d args, want %d", len(executor.args), len(args))
	}
	if got := result.String(); got != ":expanded" {
		t.Fatalf("expected :expanded, got %s", got)
	}
}

func TestMacroExpanderEvaluateMacroRequiresExecutor(t *testing.T) {
	expander := NewMacroExpander()
	macro := &compiler.Closure{Fn: &compiler.CompiledFunction{Name: "macro-fn"}}

	_, err := expander.EvaluateMacro(macro, nil)
	if err == nil {
		t.Fatal("expected missing executor error")
	}
	if !strings.Contains(err.Error(), "macro executor") {
		t.Fatalf("unexpected error: %v", err)
	}
}
