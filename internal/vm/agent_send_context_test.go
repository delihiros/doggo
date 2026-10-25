package vm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"doggo/internal/agent"
	"doggo/internal/types"
)

type agentSendWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (ctx *agentSendWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.waiting) })
	return ctx.Context.Done()
}

func TestAgentRunnerSendFullQueueHonorsContext(t *testing.T) {
	runner := MustNewRunner()
	value, err := runner.Run(`(def context-agent (agent 0)) context-agent`)
	if err != nil {
		t.Fatal(err)
	}
	a := value.(*agent.Agent)
	started, release, workerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		a.Close()
		close(release)
		select {
		case <-workerDone:
		case <-time.After(time.Second):
			t.Error("native action did not stop during cleanup")
		}
	})
	// An independent native gate isolates queue submission from Doggo channels.
	if err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
		defer close(workerDone)
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
	// NewAgent's queue has 1024 slots; the gated worker cannot consume them.
	for i := 0; i < 1024; i++ {
		if err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
			return state, nil
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &agentSendWaitContext{Context: base, waiting: make(chan struct{})}
	runner.SetContext(ctx)
	done := make(chan error, 1)
	go func() {
		_, runErr := runner.Run(`(send context-agent identity)`)
		done <- runErr
	}()
	joined := false
	t.Cleanup(func() {
		if !joined {
			a.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("Runner did not stop during cleanup")
			}
		}
	})
	select {
	case <-ctx.waiting:
	case <-time.After(time.Second):
		t.Error("send never observed the Runner context's Done channel")
	}
	cancel()
	select {
	case err := <-done:
		joined = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Runner send = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Runner send stayed blocked after its context was canceled")
	}
	if a.IsClosed() || a.CurrentError() != nil || a.State().String() != "0" {
		t.Fatal("canceled Runner send changed the agent lifecycle or state")
	}
}
