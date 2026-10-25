package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func newSendContextAgent(t *testing.T) *Agent {
	t.Helper()
	a := NewAgent(atom.NewNumberInt(0))
	t.Cleanup(func() {
		a.Close()
		done := make(chan struct{})
		go func() { a.wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("agent worker did not stop")
		}
	})
	return a
}

func newGatedSendContextAgent(t *testing.T) *Agent {
	t.Helper()
	a := newSendContextAgent(t)
	started, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { a.Close(); close(release) })
	// The worker is independent of the submission context and Doggo channels.
	if err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
		close(started)
		<-release
		return state, nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("native action did not start")
	}
	return a
}

func sendContextNoop(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
	return state, nil
}

func TestAgentSendFullQueueHonorsContext(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			a := newGatedSendContextAgent(t)
			for i := 0; i < cap(a.actionCh); i++ {
				if err := a.Send(context.Background(), sendContextNoop, nil); err != nil {
					t.Fatal(err)
				}
			}
			if len(a.actionCh) != cap(a.actionCh) {
				t.Fatal("queue is not full while the native worker is gated")
			}
			ctx, cancel := context.WithCancel(context.Background())
			wantError := context.Canceled
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
				wantError = context.DeadlineExceeded
			}
			defer cancel()
			calling, done := make(chan struct{}), make(chan error, 1)
			go func() {
				close(calling)
				done <- a.Send(ctx, sendContextNoop, nil)
			}()
			joined := false
			t.Cleanup(func() {
				if !joined {
					a.Close()
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("pending Send did not stop during cleanup")
					}
				}
			})
			<-calling
			if !deadline {
				cancel()
			}
			select {
			case err := <-done:
				joined = true
				if !errors.Is(err, wantError) {
					t.Fatalf("Send = %v, want %v", err, wantError)
				}
			case <-time.After(time.Second):
				t.Fatal("Send stayed blocked after submission context cancellation")
			}
			if len(a.actionCh) != cap(a.actionCh) || a.CurrentError() != nil || a.State().String() != "0" {
				t.Fatal("canceled submission changed the queue or agent state")
			}
		})
	}
}

func TestAgentSendAlreadyCanceledDoesNotEnqueue(t *testing.T) {
	a := newGatedSendContextAgent(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Send(ctx, sendContextNoop, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Send = %v, want context.Canceled", err)
	}
	if got := len(a.actionCh); got != 0 {
		t.Errorf("already canceled submission enqueued %d actions", got)
	}
}

func TestAgentSendPreservesExecutionContext(t *testing.T) {
	a := newSendContextAgent(t)
	type contextKey struct{}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), contextKey{}, "value"), time.Second)
	defer cancel()
	observed, canceled := make(chan context.Context, 1), make(chan error, 1)
	if err := a.Send(ctx, func(actionCtx context.Context, _ types.Value, args []types.Value) (types.Value, error) {
		observed <- actionCtx
		<-actionCtx.Done()
		canceled <- actionCtx.Err()
		return args[0], nil
	}, []types.Value{atom.NewNumberInt(42)}); err != nil {
		t.Fatal(err)
	}
	select {
	case actionCtx := <-observed:
		gotDeadline, _ := actionCtx.Deadline()
		wantDeadline, _ := ctx.Deadline()
		if actionCtx.Value(contextKey{}) != "value" || actionCtx.Done() != ctx.Done() || gotDeadline != wantDeadline || !isActionContext(actionCtx) {
			t.Error("action context lost its values, cancellation, deadline, or action marker")
		}
	case <-time.After(time.Second):
		t.Fatal("Send did not dispatch with available queue capacity")
	}
	cancel()
	select {
	case err := <-canceled:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("action context = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("action did not observe submission context cancellation")
	}
	if err := AwaitFor(time.Second, a); err != nil {
		t.Fatal(err)
	}
	if a.State().String() != "42" || a.CurrentError() != nil {
		t.Fatal("normal action did not update state without an agent error")
	}
}

func TestAgentSendNilContext(t *testing.T) {
	a := newSendContextAgent(t)
	observed := make(chan context.Context, 1)
	if err := a.Send(nil, func(ctx context.Context, state types.Value, _ []types.Value) (types.Value, error) {
		observed <- ctx
		return state, nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case ctx := <-observed:
		if ctx == nil || ctx.Err() != nil || ctx.Done() != nil || !isActionContext(ctx) {
			t.Fatal("nil submission context did not use a background action context")
		}
	case <-time.After(time.Second):
		t.Fatal("nil-context action did not run")
	}
}

func TestAgentSendCancellationPreservesAgentErrors(t *testing.T) {
	for _, closed := range []bool{false, true} {
		name := "failed"
		if closed {
			name = "closed"
		}
		t.Run(name, func(t *testing.T) {
			a := newSendContextAgent(t)
			wantError := ErrAgentFailed
			if closed {
				a.Close()
				wantError = ErrAgentClosed
			} else if err := a.SendAndWait(context.Background(), func(context.Context, types.Value, []types.Value) (types.Value, error) {
				return nil, ErrValidatorRejected
			}, nil); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := a.Send(ctx, sendContextNoop, nil); !errors.Is(err, wantError) {
				t.Fatalf("Send = %v, want %v", err, wantError)
			}
		})
	}
}
