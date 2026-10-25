package vm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type channelContextRunResult struct {
	value types.Value
	err   error
}

// Done signals that the operation has reached its cancellation-aware wait.
type channelWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (ctx *channelWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.waiting) })
	return ctx.Context.Done()
}

func TestChannelRunnerWaitHonorsContext(t *testing.T) {
	operations := []struct {
		name, source string
		send         bool
	}{
		{"receive", `(<! context-channel)`, false},
		{"send", `(>! context-channel 41)`, true},
		{"alts_receive", `(alts! [context-channel])`, false},
		{"alts_send", `(alts! [[context-channel 41]])`, true},
		{"alts_priority", `(alts! [context-channel] {:priority true})`, false},
		{"alts_timeout", `(alts! [context-channel] {:timeout 10000})`, false},
	}
	for _, operation := range operations {
		for _, deadline := range []bool{false, true} {
			name := operation.name + "/cancel"
			if deadline {
				name = operation.name + "/deadline"
			}
			t.Run(name, func(t *testing.T) {
				runner := MustNewRunnerWithoutCore()
				value, err := runner.Run(`(def context-channel (chan)) context-channel`)
				if err != nil {
					t.Fatal(err)
				}
				ch := value.(*types.Channel)
				defer ch.Close()
				var base context.Context
				var cancel context.CancelFunc
				wantError := context.Canceled
				if deadline {
					base, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
					wantError = context.DeadlineExceeded
				} else {
					base, cancel = context.WithCancel(context.Background())
				}
				defer cancel()
				ctx := &channelWaitContext{Context: base, waiting: make(chan struct{})}
				runner.SetContext(ctx)
				done := make(chan channelContextRunResult, 1)
				go func() {
					result, runErr := runner.Run(operation.source)
					done <- channelContextRunResult{result, runErr}
				}()

				if !deadline {
					select {
					case <-ctx.waiting:
					case <-time.After(200 * time.Millisecond):
						t.Error("channel operation never observed the runner context")
					}
					cancel()
				}
				select {
				case result := <-done:
					if result.err == nil || !strings.Contains(result.err.Error(), wantError.Error()) {
						t.Errorf("Run(%s) = %v, %v; want %v", operation.source, result.value, result.err, wantError)
					}
				case <-time.After(time.Second):
					t.Error("channel operation stayed blocked after context cancellation")
					// Release the operation and join it even when the regression fails.
					if operation.send {
						select {
						case <-ch.Value:
						case <-time.After(time.Second):
							t.Fatal("send cleanup timed out")
						}
					} else {
						select {
						case ch.Value <- atom.NewNumberInt(42):
						case <-time.After(time.Second):
							t.Fatal("receive cleanup timed out")
						}
					}
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Fatal("runner cleanup timed out")
					}
				}

				// A returned canceled operation must not leave a waiter that can
				// communicate later when a peer becomes available.
				timer := time.NewTimer(10 * time.Millisecond)
				defer timer.Stop()
				if operation.send {
					select {
					case value := <-ch.Value:
						t.Errorf("canceled send communicated later: %v", value)
					case <-timer.C:
					}
				} else {
					select {
					case ch.Value <- atom.NewNumberInt(42):
						t.Error("canceled receive communicated later")
					case <-timer.C:
					}
				}
			})
		}
	}
}

func TestChannelCanceledContextDoesNotUseReadyPort(t *testing.T) {
	for _, name := range []string{"<!", ">!", "alts_receive", "alts_send", "alts_priority_receive", "alts_priority_send", "alts_default", "alts_timeout", "closed_receive", "closed_send"} {
		t.Run(name, func(t *testing.T) {
			ch, err := types.NewChannel(1)
			if err != nil {
				t.Fatal(err)
			}
			defer ch.Close()
			sending := name == ">!" || name == "alts_send" || name == "alts_priority_send" || name == "closed_send"
			closed := strings.HasPrefix(name, "closed_")
			if closed {
				if err := ch.Close(); err != nil {
					t.Fatal(err)
				}
			} else if !sending {
				ch.Value <- atom.NewNumberInt(7)
			}
			builtinName := "alts!"
			var args []types.Value
			switch name {
			case "<!", "closed_receive":
				builtinName, args = "<!", []types.Value{ch}
			case ">!", "closed_send":
				builtinName, args = ">!", []types.Value{ch, atom.NewNumberInt(41)}
			default:
				var port types.Value = ch
				if sending {
					port = coll.NewVector(ch, atom.NewNumberInt(41))
				}
				args = []types.Value{coll.NewVector(port)}
				if strings.Contains(name, "priority") {
					args = append(args, coll.NewMap(map[types.Value]types.Value{atom.Keyword("priority"): atom.True}))
				} else if name == "alts_default" {
					args = append(args, coll.NewMap(map[types.Value]types.Value{atom.Keyword("default"): atom.NilValue}))
				} else if name == "alts_timeout" {
					args = append(args, coll.NewMap(map[types.Value]types.Value{atom.Keyword("timeout"): atom.NewNumberInt(0)}))
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var builtin *Builtin
			for _, definition := range builtinsChannel {
				if definition.Name == builtinName {
					builtin = definition.Builtin
					break
				}
			}
			_, err = builtin.Invoke(ctx, args...)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("Invoke() error = %v, want context.Canceled", err)
			}
			if !closed {
				wantLength := 1
				if sending {
					wantLength = 0
				}
				if got := len(ch.Value); got != wantLength {
					t.Errorf("channel buffer length = %d, want %d; canceled operation communicated", got, wantLength)
				}
			}
		})
	}
}

func TestChannelContextPreservesNilValuesAndArgumentErrors(t *testing.T) {
	for _, test := range []struct {
		name, source, want, wantError string
	}{
		{"nil_value", `(let [ch (chan 1)] [(>! ch nil) (<! ch)])`, "[nil nil]", ""},
		{"alts_nil_receive", `(let [ch (chan 1)] (>! ch nil) (get (alts! [ch]) 0))`, "nil", ""},
		{"alts_nil_send", `(let [ch (chan 1)] [(get (alts! [[ch nil]]) 0) (<! ch)])`, "[true nil]", ""},
		{"closed_receive", `(let [ch (chan)] (close! ch) (<! ch))`, "nil", ""},
		{"send_arity", `(>! (chan))`, "", ">!: wrong number of arguments"},
		{"receive_arity", `(<!)`, "", "<!: wrong number of arguments"},
		{"send_type", `(>! 1 2)`, "", ">!: first argument must be a channel"},
		{"receive_type", `(<! 1)`, "", "<!: argument must be a channel"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := MustNewRunnerWithoutCore()
			runner.SetContext(context.Background())
			value, err := runner.Run(test.source)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Run() error = %v, want %q", err, test.wantError)
				}
			} else if err != nil || value == nil || value.String() != test.want {
				t.Fatalf("Run() = %v, %v; want %s", value, err, test.want)
			}
		})
	}
}
