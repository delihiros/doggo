package vm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"doggo/internal/agent"
	stateatom "doggo/internal/atom"
	"doggo/internal/stm"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

type derefContextResult struct {
	value types.Value
	err   error
}

// Each call has a fixture release and a bounded join, including assertion failures.
type derefContextCall struct {
	done   chan derefContextResult
	joined bool
}

func startDerefContextCall(t *testing.T, release func(), invoke func() (types.Value, error)) *derefContextCall {
	t.Helper()
	call := &derefContextCall{done: make(chan derefContextResult, 1)}
	t.Cleanup(func() {
		release()
		if !call.joined {
			select {
			case <-call.done:
				call.joined = true
			case <-time.After(time.Second):
				t.Error("deref fixture cleanup could not join the call")
			}
		}
	})
	go func() {
		value, err := invoke()
		call.done <- derefContextResult{value, err}
	}()
	return call
}

func (call *derefContextCall) result(t *testing.T) (derefContextResult, bool) {
	t.Helper()
	select {
	case result := <-call.done:
		call.joined = true
		return result, true
	case <-time.After(300 * time.Millisecond):
		t.Error("deref stayed blocked after its result was due")
		return derefContextResult{}, false
	}
}

func derefContextBuiltin(t *testing.T) *Builtin {
	t.Helper()
	builtin, ok := DefaultBuiltinCatalog().Lookup("deref")
	if !ok || builtin.CtxFn == nil {
		t.Fatal("registered deref has no context function")
	}
	return builtin
}

func derefContextChannel(t *testing.T, buffer int) *types.Channel {
	t.Helper()
	ch, err := types.NewChannel(buffer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	return ch
}

func awaitDerefContextEntry(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("registered deref was never entered")
	}
}

// A private catalog wraps the actual registered implementation. Entry is visible
// even on the old implementation, which never calls the context's Done method.
func derefContextRunner(t *testing.T, entered chan struct{}) *Runner {
	t.Helper()
	definitions := DefaultBuiltinCatalog().Definitions()
	var once sync.Once
	for i := range definitions {
		if definitions[i].Name == "deref" {
			actual := definitions[i].Builtin.CtxFn
			definitions[i].Builtin.CtxFn = func(ctx context.Context, args ...types.Value) (types.Value, error) {
				once.Do(func() { close(entered) })
				return actual(ctx, args...)
			}
		}
	}
	profile := DefaultRuntimeProfile().Clone().SetBuiltinCatalog(MustBuiltinCatalog(definitions))
	runner, err := NewRunnerWithConfig(RunnerConfig{RuntimeProfile: profile})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestDerefChannelRunnerHonorsContext(t *testing.T) {
	for _, source := range []string{`(deref deref-channel)`, `@deref-channel`} {
		for _, mode := range []string{"cancel", "deadline"} {
			t.Run(source+"/"+mode, func(t *testing.T) {
				entered := make(chan struct{})
				runner := derefContextRunner(t, entered)
				value, err := runner.Run(`(def deref-channel (chan)) deref-channel`)
				if err != nil {
					t.Fatal(err)
				}
				ch := value.(*types.Channel)
				var ctx context.Context
				var cancel context.CancelFunc
				wantErr := context.Canceled
				if mode == "deadline" {
					ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
					wantErr = context.DeadlineExceeded
				} else {
					ctx, cancel = context.WithCancel(context.Background())
				}
				defer cancel()
				runner.SetContext(ctx)
				call := startDerefContextCall(t, func() { _ = ch.Close() }, func() (types.Value, error) {
					return runner.Run(source)
				})
				awaitDerefContextEntry(t, entered)
				if mode == "cancel" {
					cancel()
				}
				if result, ok := call.result(t); ok {
					// Runner exposes the VM's established wrapped error text.
					if result.err == nil || !strings.Contains(result.err.Error(), wantErr.Error()) {
						t.Errorf("Run() = %v, %v; want %v", result.value, result.err, wantErr)
					}
					select {
					case ch.Value <- atom.NewNumberInt(42):
						t.Error("canceled deref left a consumer for a later value")
					case <-time.After(20 * time.Millisecond):
					}
				}
			})
		}
	}
}

func TestDerefChannelCanceledContextPrecedesReadyValue(t *testing.T) {
	for _, state := range []string{"pending", "buffered", "closed"} {
		t.Run(state, func(t *testing.T) {
			ch := derefContextChannel(t, 1)
			if state == "buffered" {
				ch.Value <- atom.NewNumberInt(7)
			} else if state == "closed" {
				_ = ch.Close()
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			builtin := derefContextBuiltin(t)
			call := startDerefContextCall(t, func() { _ = ch.Close() }, func() (types.Value, error) {
				return builtin.CtxFn(ctx, ch)
			})
			if result, ok := call.result(t); ok && !errors.Is(result.err, context.Canceled) {
				t.Errorf("deref error = %v, want context.Canceled", result.err)
			}
			if state == "buffered" && len(ch.Value) != 1 {
				t.Error("already canceled deref consumed a buffered value")
			}
		})
	}
}

func TestDerefChannelNormalResultsAndNilContext(t *testing.T) {
	for _, source := range []string{`(deref deref-channel)`, `@deref-channel`} {
		t.Run(source+"/pending", func(t *testing.T) {
			entered := make(chan struct{})
			runner := derefContextRunner(t, entered)
			value, err := runner.Run(`(def deref-channel (chan)) deref-channel`)
			if err != nil {
				t.Fatal(err)
			}
			ch := value.(*types.Channel)
			call := startDerefContextCall(t, func() { _ = ch.Close() }, func() (types.Value, error) {
				return runner.Run(source)
			})
			awaitDerefContextEntry(t, entered)
			select {
			case ch.Value <- atom.NewNumberInt(42):
			case <-time.After(time.Second):
				t.Fatal("normal deref did not receive")
			}
			if result, ok := call.result(t); ok && (result.err != nil || result.value == nil || result.value.String() != "42") {
				t.Errorf("normal deref = %v, %v; want 42", result.value, result.err)
			}
		})
	}
	for _, state := range []string{"buffered", "nil-value", "closed"} {
		for _, nilContext := range []bool{false, true} {
			name := state + "/background"
			if nilContext {
				name = state + "/nil-context"
			}
			t.Run(name, func(t *testing.T) {
				ch := derefContextChannel(t, 1)
				want := "nil"
				if state == "closed" {
					_ = ch.Close()
				} else if state == "nil-value" {
					ch.Value <- atom.NilValue
				} else {
					ch.Value <- atom.NewNumberInt(42)
					want = "42"
				}
				var ctx context.Context = context.Background()
				if nilContext {
					ctx = nil
				}
				builtin := derefContextBuiltin(t)
				call := startDerefContextCall(t, func() { _ = ch.Close() }, func() (types.Value, error) {
					return builtin.CtxFn(ctx, ch)
				})
				if result, ok := call.result(t); ok && (result.err != nil || result.value == nil || result.value.String() != want) {
					t.Errorf("deref = %v, %v; want %s", result.value, result.err, want)
				}
			})
		}
	}
}

func TestDerefProcessExitRefHonorsContext(t *testing.T) {
	for _, mode := range []string{"already-canceled", "cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			ref := newProcessExitRef()
			var ctx context.Context
			var cancel context.CancelFunc
			wantErr := context.Canceled
			if mode == "deadline" {
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
				wantErr = context.DeadlineExceeded
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			if mode == "already-canceled" {
				cancel()
			}
			entered := make(chan struct{})
			builtin := derefContextBuiltin(t)
			call := startDerefContextCall(t, func() { ref.fulfill(atom.NewNumberInt(99)) }, func() (types.Value, error) {
				close(entered)
				return builtin.CtxFn(ctx, atom.NewGoObject(ref))
			})
			awaitDerefContextEntry(t, entered)
			if mode == "cancel" {
				cancel()
			}
			if result, ok := call.result(t); ok && !errors.Is(result.err, wantErr) {
				t.Errorf("deref error = %v, want %v", result.err, wantErr)
			}
			if ref.realized() {
				t.Error("canceling deref realized the process exit reference")
			}
			if value, ready := ref.valueIfRealized(); ready || value != atom.NilValue {
				t.Errorf("pending exit reference = %v, %v; want nil, false", value, ready)
			}
			// Cancellation of this waiter does not prevent later process completion.
			ref.fulfill(atom.NewNumberInt(42))
			completion := startDerefContextCall(t, func() {}, func() (types.Value, error) {
				return builtin.CtxFn(context.Background(), atom.NewGoObject(ref))
			})
			if result, ok := completion.result(t); ok && (result.err != nil || result.value == nil || result.value.String() != "42") {
				t.Errorf("later completion = %v, %v; want 42", result.value, result.err)
			}
		})
	}
}

func TestDerefProcessExitRefCompletionAndCanceledPrecedence(t *testing.T) {
	for _, value := range []types.Value{atom.NewNumberInt(0), nil, atom.NewNumberInt(17)} {
		name := "nil"
		if value != nil {
			name = value.String()
		}
		t.Run(name, func(t *testing.T) {
			ref := newProcessExitRef()
			ref.fulfill(value)
			want := value
			if want == nil {
				want = atom.NilValue
			}
			builtin := derefContextBuiltin(t)
			for _, ctx := range []context.Context{context.Background(), nil} {
				call := startDerefContextCall(t, func() {}, func() (types.Value, error) {
					return builtin.CtxFn(ctx, atom.NewGoObject(ref))
				})
				if result, ok := call.result(t); ok && (result.err != nil || result.value == nil || !result.value.Equals(want)) {
					t.Errorf("completed deref = %v, %v; want %v", result.value, result.err, want)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			call := startDerefContextCall(t, func() {}, func() (types.Value, error) {
				return builtin.CtxFn(ctx, atom.NewGoObject(ref))
			})
			if result, ok := call.result(t); ok && !errors.Is(result.err, context.Canceled) {
				t.Errorf("completed ref with canceled context error = %v, want context.Canceled", result.err)
			}
			legacy := startDerefContextCall(t, func() {}, func() (types.Value, error) { return ref.Deref(), nil })
			if result, ok := legacy.result(t); ok && (result.value == nil || !result.value.Equals(want)) {
				t.Errorf("context-free process completion = %v; want %v", result.value, want)
			}
		})
	}
}

func TestDerefOtherValuesKeepCanceledContextBehavior(t *testing.T) {
	value := atom.NewNumberInt(42)
	ready := &futureValue{done: make(chan struct{}), value: value}
	close(ready.done)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	builtin := derefContextBuiltin(t)
	ag := agent.NewAgent(value)
	t.Cleanup(ag.Close)
	for name, target := range map[string]types.Value{
		"atom": stateatom.NewAtom(value), "ref": stm.NewRef(value),
		"agent": ag, "var": atom.NewVar(types.Symbol("value"), value),
		"ready-future": ready,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := builtin.CtxFn(ctx, target)
			if err != nil || got == nil || !got.Equals(value) {
				t.Errorf("deref = %v, %v; want %v", got, err, value)
			}
		})
	}
	pending := &futureValue{done: make(chan struct{})}
	call := startDerefContextCall(t, func() { close(pending.done) }, func() (types.Value, error) {
		return builtin.CtxFn(ctx, pending)
	})
	if result, ok := call.result(t); ok && !errors.Is(result.err, context.Canceled) {
		t.Errorf("pending future error = %v, want context.Canceled", result.err)
	}
}

func TestDerefReflectedGoObjectRetainsLegacySupport(t *testing.T) {
	for _, test := range []struct {
		name, source string
	}{
		{"direct-atom-control", `(deref (atom 42))`},
		{"reflected-atom", `(do (import "reflect") (deref (. (reflect/ValueOf (atom 42)) Interface)))`},
		{"reflected-atom-reader", `(do (import "reflect") @(. (reflect/ValueOf (atom 42)) Interface))`},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := MustNewRunnerWithoutCore()
			value, err := runner.Run(test.source)
			if err != nil || value == nil || value.String() != "42" {
				t.Errorf("Run() = %v, %v; want 42", value, err)
			}
		})
	}
}
