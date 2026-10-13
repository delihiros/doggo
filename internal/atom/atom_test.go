package atom

import (
	"context"
	"errors"
	"testing"
	"time"

	"doggo/internal/types"
	typesatom "doggo/internal/types/atom"
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

func TestNewAtom(t *testing.T) {
	val := typesatom.NewString("initial")
	a := NewAtom(val)

	if a.Type() != "atom" {
		t.Errorf("Expected type atom, got %s", a.Type())
	}
	if !a.Deref().Equals(val) {
		t.Errorf("Expected value %s, got %s", val, a.Deref())
	}
}

func TestReset(t *testing.T) {
	a := NewAtom(typesatom.NewString("initial"))
	newVal := typesatom.NewString("new")

	ret, err := a.Reset(newVal)
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	if !ret.Equals(newVal) {
		t.Errorf("Reset returned %s, expected %s", ret, newVal)
	}
	if !a.Deref().Equals(newVal) {
		t.Errorf("Atom value is %s, expected %s", a.Deref(), newVal)
	}
}

func TestResetNotifiesWithoutComparingUnboundedStates(t *testing.T) {
	newUnboundedState := func() types.Value {
		return coll.NewConsSeq(coll.RangeInt(-1), coll.NewInfiniteRangeLazySeq())
	}
	a := NewAtom(newUnboundedState())
	called := make(chan struct{}, 1)
	a.AddWatch(typesatom.NewKeyword("watch"), func(_ context.Context, _ types.Value, _ *Atom, _, _ types.Value) error {
		called <- struct{}{}
		return nil
	})

	done := make(chan error, 1)
	go func() {
		_, err := a.Reset(newUnboundedState())
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Reset failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Reset compared unbounded states")
	}

	select {
	case <-called:
	default:
		t.Fatal("watcher was not called")
	}
}

func TestResetAllowsDifferentConcreteValueType(t *testing.T) {
	a := NewAtom(typesatom.NewNumberInt(1))
	want := typesatom.NewString("changed")

	got, err := a.Reset(want)
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}
	if !got.Equals(want) || !a.Deref().Equals(want) {
		t.Fatalf("Reset result/state = %v/%v, want %v", got, a.Deref(), want)
	}
}

func TestSwap(t *testing.T) {
	a := NewAtom(typesatom.NewNumberInt(1))

	// インクリメント関数
	inc := func(v types.Value) (types.Value, error) {
		n := v.(*typesatom.Number)
		return typesatom.NewNumberInt(int64(n.ToFloat64() + 1)), nil
	}

	ret, err := a.Swap(context.Background(), inc)
	if err != nil {
		t.Fatalf("Swap failed: %v", err)
	}

	expected := typesatom.NewNumberInt(2)
	if !ret.Equals(expected) {
		t.Errorf("Swap returned %s, expected %s", ret, expected)
	}
	if !a.Deref().Equals(expected) {
		t.Errorf("Atom value is %s, expected %s", a.Deref(), expected)
	}
}

func TestCompareAndSet(t *testing.T) {
	val1 := typesatom.NewString("val1")
	val2 := typesatom.NewString("val2")
	a := NewAtom(val1)

	// 失敗ケース（現在の値と異なる）
	success, err := a.CompareAndSet(context.Background(), val2, val2)
	if err != nil {
		t.Fatalf("CompareAndSet failed: %v", err)
	}
	if success {
		t.Error("CompareAndSet should fail when old value mismatch")
	}

	// 成功ケース
	success, err = a.CompareAndSet(context.Background(), val1, val2)
	if err != nil {
		t.Fatalf("CompareAndSet failed: %v", err)
	}
	if !success {
		t.Error("CompareAndSet should succeed")
	}
	if !a.Deref().Equals(val2) {
		t.Errorf("Atom value is %s, expected %s", a.Deref(), val2)
	}
}

func TestValidator(t *testing.T) {
	// 正の数のみ許可するバリデータ
	validator := func(v types.Value) bool {
		n, ok := v.(*typesatom.Number)
		if !ok {
			return false
		}
		return n.ToFloat64() > 0
	}

	a, err := NewAtomWithValidator(typesatom.NewNumberInt(1), validator)
	if err != nil {
		t.Fatalf("NewAtomWithValidator failed: %v", err)
	}

	// 成功ケース
	_, err = a.Reset(typesatom.NewNumberInt(2))
	if err != nil {
		t.Errorf("Valid reset failed: %v", err)
	}

	// 失敗ケース
	_, err = a.Reset(typesatom.NewNumberInt(0))
	if err != ErrValidatorRejected {
		t.Errorf("Expected ErrValidatorRejected, got %v", err)
	}

	// 値が変わっていないことを確認
	if !a.Deref().Equals(typesatom.NewNumberInt(2)) {
		t.Error("Value should not change on validation failure")
	}

	// SetValidator
	a2 := NewAtom(typesatom.NewNumberInt(1))
	err = a2.SetValidator(validator)
	if err != nil {
		t.Fatalf("SetValidator failed: %v", err)
	}

	// SetValidator fail
	a3 := NewAtom(typesatom.NewNumberInt(0))
	err = a3.SetValidator(validator)
	if err != ErrValidatorRejected {
		t.Errorf("SetValidator should fail for invalid current state")
	}
}

func TestResetVals(t *testing.T) {
	a := NewAtom(typesatom.NewString("old"))
	newVal := typesatom.NewString("new")

	oldRet, newRet, err := a.ResetVals(newVal)
	if err != nil {
		t.Fatalf("ResetVals failed: %v", err)
	}

	if !oldRet.Equals(typesatom.NewString("old")) {
		t.Errorf("Expected old value 'old', got %s", oldRet)
	}
	if !newRet.Equals(newVal) {
		t.Errorf("Expected new value 'new', got %s", newRet)
	}
	if !a.Deref().Equals(newVal) {
		t.Errorf("Atom value is %s, expected %s", a.Deref(), newVal)
	}
}

func TestSwapVals(t *testing.T) {
	a := NewAtom(typesatom.NewNumberInt(5))

	inc := func(v types.Value) (types.Value, error) {
		n := v.(*typesatom.Number)
		return typesatom.NewNumberInt(int64(n.ToFloat64() + 1)), nil
	}

	oldRet, newRet, err := a.SwapVals(context.Background(), inc)
	if err != nil {
		t.Fatalf("SwapVals failed: %v", err)
	}

	if !oldRet.Equals(typesatom.NewNumberInt(5)) {
		t.Errorf("Expected old value 5, got %s", oldRet)
	}
	if !newRet.Equals(typesatom.NewNumberInt(6)) {
		t.Errorf("Expected new value 6, got %s", newRet)
	}
}

func TestSwapConcurrency(t *testing.T) {
	a := NewAtom(typesatom.NewNumberInt(0))

	// 並行してインクリメント
	const goroutines = 10
	const increments = 100

	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < increments; j++ {
				a.Swap(context.Background(), func(v types.Value) (types.Value, error) {
					n := v.(*typesatom.Number)
					return typesatom.NewNumberInt(int64(n.ToFloat64() + 1)), nil
				})
			}
			done <- struct{}{}
		}()
	}

	// すべてのゴルーチンの完了を待機
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// 最終値を確認
	expected := typesatom.NewNumberInt(goroutines * increments)
	if !a.Deref().Equals(expected) {
		t.Errorf("Expected %s, got %s", expected, a.Deref())
	}
}

func TestWatcher(t *testing.T) {
	a := NewAtom(typesatom.NewNumberInt(0))

	ch := make(chan struct {
		key      types.Value
		oldState types.Value
		newState types.Value
	}, 1)

	watcher := func(ctx context.Context, key types.Value, atom *Atom, oldState, newState types.Value) error {
		ch <- struct{ key, oldState, newState types.Value }{key, oldState, newState}
		return nil
	}

	key := typesatom.NewKeyword("w1")
	a.AddWatch(key, watcher)

	// 更新
	newVal := typesatom.NewNumberInt(1)
	a.Reset(newVal)

	select {
	case event := <-ch:
		if !event.key.Equals(key) {
			t.Errorf("Watcher key mismatch")
		}
		if !event.oldState.Equals(typesatom.NewNumberInt(0)) {
			t.Errorf("Old state mismatch")
		}
		if !event.newState.Equals(newVal) {
			t.Errorf("New state mismatch")
		}
	case <-time.After(time.Second):
		t.Error("Watcher not called")
	}

	// RemoveWatch
	a.RemoveWatch(key)
	a.Reset(typesatom.NewNumberInt(2))

	select {
	case <-ch:
		t.Error("Watcher called after removal")
	case <-time.After(100 * time.Millisecond):
		// OK
	}
}

func TestWatcherFailuresAreReturnedAfterStateChange(t *testing.T) {
	tests := []struct {
		name    string
		watcher WatchFn
	}{
		{
			name: "error",
			watcher: func(context.Context, types.Value, *Atom, types.Value, types.Value) error {
				return errors.New("watch failed")
			},
		},
		{
			name: "panic",
			watcher: func(context.Context, types.Value, *Atom, types.Value, types.Value) error {
				panic("watch panicked")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAtom(typesatom.NewNumberInt(0))
			a.AddWatch(typesatom.NewKeyword("failing"), tt.watcher)

			_, err := a.Reset(typesatom.NewNumberInt(1))
			if err == nil {
				t.Fatal("Reset() discarded watcher failure")
			}
			if got := a.Deref().String(); got != "1" {
				t.Fatalf("state = %s, want committed value 1", got)
			}
		})
	}
}

func TestWatchKeysUseHashAndEquality(t *testing.T) {
	a := NewAtom(typesatom.NewNumberInt(0))
	var calls int

	a.AddWatch(collidingWatchKey("one"), func(context.Context, types.Value, *Atom, types.Value, types.Value) error {
		calls++
		return nil
	})
	a.AddWatch(collidingWatchKey("two"), func(context.Context, types.Value, *Atom, types.Value, types.Value) error {
		calls += 10
		return nil
	})
	if _, err := a.Reset(typesatom.NewNumberInt(1)); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if calls != 11 {
		t.Fatalf("colliding watcher calls = %d, want 11", calls)
	}

	a.RemoveWatch(collidingWatchKey("one"))
	if _, err := a.Reset(typesatom.NewNumberInt(2)); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if calls != 21 {
		t.Fatalf("watcher calls after equality removal = %d, want 21", calls)
	}
}
