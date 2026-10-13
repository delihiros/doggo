package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type collidingWatchKey string

func (k collidingWatchKey) String() string         { return string(k) }
func (k collidingWatchKey) Type() string           { return "watch-key" }
func (k collidingWatchKey) Hash() uint32           { return 1 }
func (k collidingWatchKey) IsSelfEvaluating() bool { return true }
func (k collidingWatchKey) Equals(other types.Value) bool {
	otherKey, ok := other.(collidingWatchKey)
	return ok && k == otherKey
}

// TestNewAgent は新しいAgentの作成をテストする
func TestNewAgent(t *testing.T) {
	val := atom.NewNumberInt(42)
	a := NewAgent(val)
	defer a.Close()

	if a.State() != val {
		t.Errorf("expected %v, got %v", val, a.State())
	}

	if a.Type() != "agent" {
		t.Errorf("expected type 'agent', got '%s'", a.Type())
	}
}

func TestAgentNotifiesWithoutComparingUnboundedStates(t *testing.T) {
	newUnboundedState := func() types.Value {
		return coll.NewConsSeq(coll.RangeInt(-1), coll.NewInfiniteRangeLazySeq())
	}
	a := NewAgent(newUnboundedState())
	called := make(chan struct{}, 1)
	a.AddWatch(atom.NewKeyword("watch"), func(_ context.Context, _ types.Value, _ *Agent, _, _ types.Value) error {
		called <- struct{}{}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := a.SendAndWait(ctx, func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return newUnboundedState(), nil
	}, nil)
	if err != nil {
		t.Fatalf("SendAndWait failed: %v", err)
	}
	defer a.Close()

	select {
	case <-called:
	default:
		t.Fatal("watcher was not called")
	}
}

// TestAgentSend はsendによる状態更新をテストする
func TestAgentSend(t *testing.T) {
	val := atom.NewNumberInt(10)
	a := NewAgent(val)
	defer a.Close()

	// 状態を2倍にするアクション
	err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
		num := state.(*atom.Number)
		return atom.NewNumberInt(int64(num.ToFloat64() * 2)), nil
	}, nil)

	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	// アクションの完了を待つ
	err = AwaitFor(time.Second, a)
	if err != nil {
		t.Fatalf("await failed: %v", err)
	}

	// 状態が更新されていることを確認
	result := a.State().(*atom.Number)
	if result.ToFloat64() != 20 {
		t.Errorf("expected 20, got %v", result.ToFloat64())
	}
}

func TestAgentSendAllowsDifferentConcreteValueType(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(1))
	defer a.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := a.SendAndWait(ctx, func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return atom.NewString("changed"), nil
	}, nil)
	if err != nil {
		t.Fatalf("SendAndWait failed: %v", err)
	}
	if got := a.State(); !got.Equals(atom.NewString("changed")) {
		t.Fatalf("agent state = %v, want changed", got)
	}
}

// TestAgentSerialExecution はアクションの直列実行をテストする
func TestAgentSerialExecution(t *testing.T) {
	val := atom.NewNumberInt(0)
	a := NewAgent(val)
	defer a.Close()

	// 複数のアクションを送信
	for i := 0; i < 100; i++ {
		err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
			num := state.(*atom.Number)
			return atom.NewNumberInt(int64(num.ToFloat64() + 1)), nil
		}, nil)
		if err != nil {
			t.Fatalf("send failed: %v", err)
		}
	}

	// すべてのアクションの完了を待つ
	err := AwaitFor(5*time.Second, a)
	if err != nil {
		t.Fatalf("await failed: %v", err)
	}

	// 最終的な状態を確認
	result := a.State().(*atom.Number)
	if result.ToFloat64() != 100 {
		t.Errorf("expected 100, got %v", result.ToFloat64())
	}
}

// TestAgentValidator はバリデータをテストする
func TestAgentValidator(t *testing.T) {
	// 正の数のみを許可するバリデータ
	validator := func(v types.Value) bool {
		num, ok := v.(*atom.Number)
		if !ok {
			return false
		}
		return num.ToFloat64() > 0
	}

	// 正の初期値で作成
	a, err := NewAgentWithValidator(atom.NewNumberInt(10), validator)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer a.Close()

	// 負の値への更新を試みる
	err = a.Send(context.Background(), func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return atom.NewNumberInt(-5), nil
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	// アクションの完了を待つ
	time.Sleep(100 * time.Millisecond)

	// エラーが発生していることを確認
	if a.CurrentError() == nil {
		t.Error("expected validation error")
	}

	// 状態は変更されていないことを確認
	result := a.State().(*atom.Number)
	if result.ToFloat64() != 10 {
		t.Errorf("expected 10, got %v", result.ToFloat64())
	}
}

// TestAgentError はエラー状態をテストする
func TestAgentError(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()

	// エラーを発生させるアクション
	err := a.Send(context.Background(), func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return nil, ErrValidatorRejected
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	// アクションの完了を待つ
	time.Sleep(100 * time.Millisecond)

	// エラー状態になっていることを確認
	if a.CurrentError() == nil {
		t.Error("expected error state")
	}

	// エラー状態のAgentへのsendはエラーを返す
	err = a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
		return state, nil
	}, nil)
	if err == nil {
		t.Error("expected error when sending to failed agent")
	}
}

// TestAgentRestart はAgentのリスタートをテストする
func TestAgentRestart(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()

	// エラーを発生させる
	err := a.Send(context.Background(), func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return nil, ErrValidatorRejected
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// リスタート
	err = a.Restart(atom.NewNumberInt(100), false)
	if err != nil {
		t.Fatalf("restart failed: %v", err)
	}

	// エラーがクリアされていることを確認
	if a.CurrentError() != nil {
		t.Error("expected error to be cleared")
	}

	// 新しい状態になっていることを確認
	result := a.State().(*atom.Number)
	if result.ToFloat64() != 100 {
		t.Errorf("expected 100, got %v", result.ToFloat64())
	}

	// 再びsendできることを確認
	err = a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
		num := state.(*atom.Number)
		return atom.NewNumberInt(int64(num.ToFloat64() + 1)), nil
	}, nil)
	if err != nil {
		t.Fatalf("send after restart failed: %v", err)
	}

	err = AwaitFor(time.Second, a)
	if err != nil {
		t.Fatalf("await failed: %v", err)
	}

	result = a.State().(*atom.Number)
	if result.ToFloat64() != 101 {
		t.Errorf("expected 101, got %v", result.ToFloat64())
	}
}

// TestAgentWatcher はウォッチャーをテストする
func TestAgentWatcher(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()

	var watchCount int32
	var lastOld, lastNew types.Value

	// ウォッチャーを追加
	a.AddWatch(atom.NewKeyword("test"), func(_ context.Context, _ types.Value, _ *Agent, old, new types.Value) error {
		atomic.AddInt32(&watchCount, 1)
		lastOld = old
		lastNew = new
		return nil
	})

	// 状態を更新
	err := a.Send(context.Background(), func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return atom.NewNumberInt(42), nil
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	err = AwaitFor(time.Second, a)
	if err != nil {
		t.Fatalf("await failed: %v", err)
	}

	// ウォッチャーが呼ばれたことを確認
	if atomic.LoadInt32(&watchCount) != 1 {
		t.Errorf("expected watch count 1, got %d", watchCount)
	}

	oldNum := lastOld.(*atom.Number)
	newNum := lastNew.(*atom.Number)
	if oldNum.ToFloat64() != 0 || newNum.ToFloat64() != 42 {
		t.Errorf("watcher received wrong values: old=%v, new=%v", lastOld, lastNew)
	}

	// ウォッチャーを削除
	a.RemoveWatch(atom.NewKeyword("test"))

	// 再度状態を更新
	err = a.Send(context.Background(), func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return atom.NewNumberInt(100), nil
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	err = AwaitFor(time.Second, a)
	if err != nil {
		t.Fatalf("await failed: %v", err)
	}

	// ウォッチャーは呼ばれないはず
	if atomic.LoadInt32(&watchCount) != 1 {
		t.Errorf("expected watch count still 1, got %d", watchCount)
	}
}

func TestAgentWatchKeysUseHashAndEquality(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()
	var calls int32

	a.AddWatch(collidingWatchKey("one"), func(context.Context, types.Value, *Agent, types.Value, types.Value) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	a.AddWatch(collidingWatchKey("two"), func(context.Context, types.Value, *Agent, types.Value, types.Value) error {
		atomic.AddInt32(&calls, 10)
		return nil
	})
	sendIncrement := func() {
		t.Helper()
		if err := a.Send(context.Background(), func(context.Context, types.Value, []types.Value) (types.Value, error) {
			return atom.NewNumberInt(1), nil
		}, nil); err != nil {
			t.Fatalf("Send: %v", err)
		}
		if err := AwaitFor(time.Second, a); err != nil {
			t.Fatalf("AwaitFor: %v", err)
		}
	}

	sendIncrement()
	if got := atomic.LoadInt32(&calls); got != 11 {
		t.Fatalf("colliding watcher calls = %d, want 11", got)
	}
	a.RemoveWatch(collidingWatchKey("one"))
	if err := a.Send(context.Background(), func(context.Context, types.Value, []types.Value) (types.Value, error) {
		return atom.NewNumberInt(2), nil
	}, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := AwaitFor(time.Second, a); err != nil {
		t.Fatalf("AwaitFor: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 21 {
		t.Fatalf("watcher calls after equality removal = %d, want 21", got)
	}
}

// TestAwait は複数Agentのawaitをテストする
func TestAwait(t *testing.T) {
	agents := make([]*Agent, 3)
	for i := range agents {
		agents[i] = NewAgent(atom.NewNumberInt(0))
		defer agents[i].Close()
	}

	// 各Agentに複数のアクションを送信
	for _, a := range agents {
		for j := 0; j < 10; j++ {
			err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
				num := state.(*atom.Number)
				return atom.NewNumberInt(int64(num.ToFloat64() + 1)), nil
			}, nil)
			if err != nil {
				t.Fatalf("send failed: %v", err)
			}
		}
	}

	// すべてのAgentの完了を待つ
	err := AwaitFor(5*time.Second, agents...)
	if err != nil {
		t.Fatalf("await failed: %v", err)
	}

	// すべてのAgentが期待される状態になっていることを確認
	for i, a := range agents {
		result := a.State().(*atom.Number)
		if result.ToFloat64() != 10 {
			t.Errorf("agent %d: expected 10, got %v", i, result.ToFloat64())
		}
	}
}

// TestAgentEquality はAgentの等価性をテストする
func TestAgentEquality(t *testing.T) {
	a1 := NewAgent(atom.NewNumberInt(42))
	a2 := NewAgent(atom.NewNumberInt(42))
	defer a1.Close()
	defer a2.Close()

	// 同じ内容でも異なるAgent
	if a1.Equals(a2) {
		t.Error("different agents should not be equal")
	}

	// 同じAgentは等しい
	if !a1.Equals(a1) {
		t.Error("same agent should be equal")
	}
}

// TestNewAgentWithValidatorError は無効な初期値でのAgent作成をテストする
func TestNewAgentWithValidatorError(t *testing.T) {
	validator := func(v types.Value) bool {
		num, ok := v.(*atom.Number)
		if !ok {
			return false
		}
		return num.ToFloat64() > 0
	}

	// 負の値で作成を試みる
	_, err := NewAgentWithValidator(atom.NewNumberInt(-5), validator)
	if err == nil {
		t.Error("expected error for invalid initial value")
	}
}

func TestAgentConcurrentSendsAreSerialized(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()

	const numGoroutines = 5
	const numIncrements = 20

	// 複数のゴルーチンから同時にインクリメント
	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIncrements; j++ {
				err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
					num := state.(*atom.Number)
					return atom.NewNumberInt(int64(num.ToFloat64() + 1)), nil
				}, nil)
				if err != nil {
					t.Errorf("send failed: %v", err)
					return
				}
			}
		}()
	}

	// すべてのゴルーチンが完了するのを待つ
	wg.Wait()

	// すべてのアクションが完了するのを待つ
	err := AwaitFor(10*time.Second, a)
	if err != nil {
		t.Fatalf("await failed: %v", err)
	}

	expected := float64(numGoroutines * numIncrements)
	result := a.State().(*atom.Number)
	if result.ToFloat64() != expected {
		t.Errorf("expected %v, got %v", expected, result.ToFloat64())
	}
}

// TestWatcherFailuresFailAgentAfterNotifyingRemainingWatchers はウォッチャー失敗の可視性をテストする
func TestWatcherFailuresFailAgentAfterNotifyingRemainingWatchers(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()

	var goodWatcherCalled int32

	// パニックを起こす「悪い」ウォッチャー
	a.AddWatch(atom.NewKeyword("bad"), func(_ context.Context, _ types.Value, _ *Agent, _, _ types.Value) error {
		panic("watcher panic!")
	})

	// エラーを返す「悪い」ウォッチャー
	a.AddWatch(atom.NewKeyword("error"), func(_ context.Context, _ types.Value, _ *Agent, _, _ types.Value) error {
		return ErrValidatorRejected
	})

	// 正常に動作する「良い」ウォッチャー
	a.AddWatch(atom.NewKeyword("good"), func(_ context.Context, _ types.Value, _ *Agent, _, _ types.Value) error {
		atomic.AddInt32(&goodWatcherCalled, 1)
		return nil
	})

	// 状態を更新
	err := a.Send(context.Background(), func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return atom.NewNumberInt(42), nil
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	err = AwaitFor(time.Second, a)
	if err != nil {
		t.Fatalf("await failed: %v", err)
	}

	// 状態が正しく更新されていることを確認
	result := a.State().(*atom.Number)
	if result.ToFloat64() != 42 {
		t.Errorf("expected 42, got %v", result.ToFloat64())
	}

	// 良いウォッチャーが呼ばれたことを確認
	if atomic.LoadInt32(&goodWatcherCalled) != 1 {
		t.Errorf("expected good watcher to be called once, got %d", goodWatcherCalled)
	}

	// 状態変更後のウォッチャー失敗がAgentのエラーとして観測できることを確認
	if a.CurrentError() == nil {
		t.Error("agent discarded watcher failure")
	}
}

// TestMultipleWatchers は複数のウォッチャーが正しく動作することをテストする
func TestMultipleWatchers(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()

	var watcher1Called, watcher2Called, watcher3Called int32

	a.AddWatch(atom.NewKeyword("w1"), func(_ context.Context, _ types.Value, _ *Agent, _, _ types.Value) error {
		atomic.AddInt32(&watcher1Called, 1)
		return nil
	})
	a.AddWatch(atom.NewKeyword("w2"), func(_ context.Context, _ types.Value, _ *Agent, _, _ types.Value) error {
		atomic.AddInt32(&watcher2Called, 1)
		return nil
	})
	a.AddWatch(atom.NewKeyword("w3"), func(_ context.Context, _ types.Value, _ *Agent, _, _ types.Value) error {
		atomic.AddInt32(&watcher3Called, 1)
		return nil
	})

	// 状態を更新
	err := a.Send(context.Background(), func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return atom.NewNumberInt(1), nil
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	err = AwaitFor(time.Second, a)
	if err != nil {
		t.Fatalf("await failed: %v", err)
	}

	// すべてのウォッチャーが呼ばれたことを確認
	if atomic.LoadInt32(&watcher1Called) != 1 {
		t.Errorf("watcher1 should be called once, got %d", watcher1Called)
	}
	if atomic.LoadInt32(&watcher2Called) != 1 {
		t.Errorf("watcher2 should be called once, got %d", watcher2Called)
	}
	if atomic.LoadInt32(&watcher3Called) != 1 {
		t.Errorf("watcher3 should be called once, got %d", watcher3Called)
	}
}

// TestAgentClose はAgentのクローズをテストする
func TestAgentClose(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))

	// いくつかのアクションを送信
	for i := 0; i < 5; i++ {
		_ = a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
			num := state.(*atom.Number)
			return atom.NewNumberInt(int64(num.ToFloat64() + 1)), nil
		}, nil)
	}

	// クローズ
	a.Close()

	// クローズ後のsendはエラーを返すべき
	err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
		return state, nil
	}, nil)
	if err != ErrAgentClosed {
		t.Errorf("expected ErrAgentClosed, got %v", err)
	}

	// IsClosedがtrueを返すことを確認
	if !a.IsClosed() {
		t.Error("agent should be closed")
	}
}

// TestErrorModeContine はエラーモード:continueをテストする
func TestErrorModeContinue(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()

	// エラーモードをcontinueに設定
	a.SetErrorMode(ErrorModeContinue)

	// エラーを発生させるアクション
	err := a.Send(context.Background(), func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return nil, ErrValidatorRejected
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// エラー状態になっていることを確認
	if a.CurrentError() == nil {
		t.Error("expected error state")
	}

	// ErrorModeContinueなので、次のsendも実行できるべき
	err = a.Send(context.Background(), func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return atom.NewNumberInt(100), nil
	}, nil)
	if err != nil {
		t.Fatalf("send should succeed in continue mode: %v", err)
	}

	err = AwaitFor(time.Second, a)
	if err != nil {
		t.Fatalf("await failed: %v", err)
	}

	// 状態が更新されていることを確認
	result := a.State().(*atom.Number)
	if result.ToFloat64() != 100 {
		t.Errorf("expected 100, got %v", result.ToFloat64())
	}
}

// TestErrorHandler はエラーハンドラーをテストする
func TestErrorHandler(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()

	var handlerCalled int32
	var receivedErr atomic.Value // レース条件を防ぐためatomic.Valueを使用

	// エラーハンドラーを設定
	a.SetErrorHandler(func(agent *Agent, err error) {
		atomic.AddInt32(&handlerCalled, 1)
		receivedErr.Store(err)
	})

	// エラーを発生させるアクション
	err := a.Send(context.Background(), func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return nil, ErrValidatorRejected
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// エラーハンドラーが呼ばれたことを確認
	if atomic.LoadInt32(&handlerCalled) != 1 {
		t.Errorf("expected handler to be called once, got %d", handlerCalled)
	}

	// 正しいエラーが渡されたことを確認
	storedErr := receivedErr.Load()
	if storedErr != ErrValidatorRejected {
		t.Errorf("expected ErrValidatorRejected, got %v", storedErr)
	}
}

// TestAwaitTimeout はawaitのタイムアウトをテストする
func TestAwaitTimeout(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()

	// 長時間かかるアクションを送信
	err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
		time.Sleep(500 * time.Millisecond)
		num := state.(*atom.Number)
		return atom.NewNumberInt(int64(num.ToFloat64() + 1)), nil
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	// 短いタイムアウトでawait
	err = AwaitFor(50*time.Millisecond, a)
	if err == nil {
		t.Error("expected timeout error")
	}
}

// TestAwaitWithContext はcontextキャンセルでのawaitをテストする
func TestAwaitWithContext(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(0))
	defer a.Close()

	// 長時間かかるアクションを送信
	err := a.Send(context.Background(), func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
		time.Sleep(500 * time.Millisecond)
		num := state.(*atom.Number)
		return atom.NewNumberInt(int64(num.ToFloat64() + 1)), nil
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	// キャンセル可能なコンテキストでawait
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = Await(ctx, a)
	if err == nil {
		t.Error("expected context deadline exceeded error")
	}
}

// TestSetValidatorAfterCreation は作成後のバリデータ設定をテストする
func TestSetValidatorAfterCreation(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(10))
	defer a.Close()

	// 正の数のみを許可するバリデータを後から設定
	validator := func(v types.Value) bool {
		num, ok := v.(*atom.Number)
		if !ok {
			return false
		}
		return num.ToFloat64() > 0
	}

	err := a.SetValidator(validator)
	if err != nil {
		t.Fatalf("failed to set validator: %v", err)
	}

	// 負の値への更新を試みる
	err = a.Send(context.Background(), func(_ context.Context, _ types.Value, _ []types.Value) (types.Value, error) {
		return atom.NewNumberInt(-5), nil
	}, nil)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// エラーが発生していることを確認
	if a.CurrentError() == nil {
		t.Error("expected validation error")
	}

	// 状態は変更されていないことを確認
	result := a.State().(*atom.Number)
	if result.ToFloat64() != 10 {
		t.Errorf("expected 10, got %v", result.ToFloat64())
	}
}

// TestSetValidatorRejectsCurrentState は現在の状態が無効な場合のバリデータ設定をテストする
func TestSetValidatorRejectsCurrentState(t *testing.T) {
	a := NewAgent(atom.NewNumberInt(-10))
	defer a.Close()

	// 正の数のみを許可するバリデータ（現在の状態は-10なので拒否されるべき）
	validator := func(v types.Value) bool {
		num, ok := v.(*atom.Number)
		if !ok {
			return false
		}
		return num.ToFloat64() > 0
	}

	err := a.SetValidator(validator)
	if err != ErrValidatorRejected {
		t.Errorf("expected ErrValidatorRejected, got %v", err)
	}
}
