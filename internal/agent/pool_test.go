package agent

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

// TestPoolRegisterUnregister は登録・解除をテストする
func TestPoolRegisterUnregister(t *testing.T) {
	p := NewPool()

	a1 := NewAgent(atom.NewNumberInt(1))
	defer a1.Close()
	a2 := NewAgent(atom.NewNumberInt(2))
	defer a2.Close()

	// 登録
	p.Register(a1)
	if p.Count() != 1 {
		t.Errorf("expected count 1, got %d", p.Count())
	}

	p.Register(a2)
	if p.Count() != 2 {
		t.Errorf("expected count 2, got %d", p.Count())
	}

	// 同じAgentを再登録しても数は増えない
	p.Register(a1)
	if p.Count() != 2 {
		t.Errorf("expected count 2 after re-register, got %d", p.Count())
	}

	// 解除
	p.Unregister(a1)
	if p.Count() != 1 {
		t.Errorf("expected count 1, got %d", p.Count())
	}

	p.Unregister(a2)
	if p.Count() != 0 {
		t.Errorf("expected count 0, got %d", p.Count())
	}

	// 存在しないAgentの解除は何も起きない
	p.Unregister(a1)
	if p.Count() != 0 {
		t.Errorf("expected count 0, got %d", p.Count())
	}
}

// TestPoolShutdown はShutdownをテストする
func TestPoolShutdown(t *testing.T) {
	p := NewPool()

	agents := make([]*Agent, 5)
	for i := 0; i < 5; i++ {
		agents[i] = NewAgent(atom.NewNumberInt(int64(i)))
		p.Register(agents[i])
	}

	if p.Count() != 5 {
		t.Errorf("expected count 5, got %d", p.Count())
	}

	p.Shutdown()

	// 全Agentがクローズされている
	for i, a := range agents {
		if !a.IsClosed() {
			t.Errorf("agent %d should be closed", i)
		}
	}
}

// TestPoolConcurrent は並行アクセスをテストする
func TestPoolConcurrent(t *testing.T) {
	p := NewPool()
	var wg sync.WaitGroup

	// 並行して登録・解除
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := NewAgent(atom.NewNumberInt(1))
			p.Register(a)
			p.Count()
			p.Unregister(a)
			a.Close()
		}()
	}

	wg.Wait()

	if p.Count() != 0 {
		t.Errorf("expected count 0, got %d", p.Count())
	}
}

// TestAwaitMultipleAgents は複数Agentの待機をテストする
func TestAwaitMultipleAgents(t *testing.T) {
	agents := make([]*Agent, 3)
	for i := 0; i < 3; i++ {
		agents[i] = NewAgent(atom.NewNumberInt(0))
		defer agents[i].Close()
	}

	// 各Agentにアクションを送信
	for _, a := range agents {
		err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
			time.Sleep(10 * time.Millisecond)
			num := state.(*atom.Number)
			return atom.NewNumberInt(int64(num.ToFloat64() + 1)), nil
		}, nil)
		if err != nil {
			t.Fatalf("send failed: %v", err)
		}
	}

	// 全Agentの完了を待つ
	err := Await(context.Background(), agents...)
	if err != nil {
		t.Fatalf("await failed: %v", err)
	}

	// 全Agentの状態が更新されている
	for i, a := range agents {
		result := a.State().(*atom.Number)
		if result.ToFloat64() != 1 {
			t.Errorf("agent %d: expected 1, got %v", i, result.ToFloat64())
		}
	}
}

// TestAwaitWithNilAgent はnil Agentを含む待機をテストする
func TestAwaitWithNilAgent(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(1))
	defer a.Close()

	// nilを含むスライス
	err := Await(context.Background(), nil, a, nil)
	if err != nil {
		t.Errorf("await with nil should not fail: %v", err)
	}
}

// TestAwaitWithClosedAgent はクローズ済みAgentを含む待機をテストする
func TestAwaitWithClosedAgent(t *testing.T) {
	a1 := NewAgent(atom.NewNumberInt(1))
	a2 := NewAgent(atom.NewNumberInt(2))
	defer a2.Close()

	// a1をクローズ
	a1.Close()

	// クローズ済みAgentを含む
	err := Await(context.Background(), a1, a2)
	if err != nil {
		t.Errorf("await with closed agent should not fail: %v", err)
	}
}

// TestAwaitContextCancel はコンテキストキャンセルをテストする
func TestAwaitContextCancel(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()

	// 長時間かかるアクションを送信
	err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
		time.Sleep(500 * time.Millisecond)
		return state, nil
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	// 短いタイムアウトでAwait
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = Await(ctx, a)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

// TestAwaitForTimeout はAwaitForのタイムアウトをテストする
func TestAwaitForTimeout(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()

	// 長時間かかるアクションを送信
	err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
		time.Sleep(500 * time.Millisecond)
		return state, nil
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	err = AwaitFor(50*time.Millisecond, a)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestAwaitForTimeoutDoesNotLeakWaiters(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	started := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		close(release)
		a.Close()
	}()

	err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
		close(started)
		<-release
		return state, nil
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	<-started
	baseline := runtime.NumGoroutine()

	for range 10 {
		if err := AwaitFor(time.Millisecond, a); err != context.DeadlineExceeded {
			t.Fatalf("AwaitFor() error = %v, want DeadlineExceeded", err)
		}
	}

	deadline := time.Now().Add(100 * time.Millisecond)
	for runtime.NumGoroutine() > baseline+2 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if got := runtime.NumGoroutine(); got > baseline+2 {
		t.Fatalf("goroutines after timed out awaits = %d, baseline = %d", got, baseline)
	}
}

// TestAwaitEmptyList は空リストの待機をテストする
func TestAwaitEmptyList(t *testing.T) {
	err := Await(context.Background())
	if err != nil {
		t.Errorf("await empty list should not fail: %v", err)
	}
}

// TestPoolShutdownIdempotent はShutdownの冪等性をテストする
func TestPoolShutdownIdempotent(t *testing.T) {
	p := NewPool()

	a := NewAgent(atom.NewNumberInt(1))
	p.Register(a)

	// 複数回Shutdownしてもパニックしない
	p.Shutdown()
	p.Shutdown()

	if !a.IsClosed() {
		t.Error("agent should be closed")
	}
}
