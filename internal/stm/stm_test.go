package stm

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"doggo/internal/types"
	"doggo/internal/types/atom"
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

// TestNewRef はRefの作成をテストする
func TestNewRef(t *testing.T) {
	val := atom.NewNumberInt(42)
	ref := NewRef(val)

	if ref == nil {
		t.Fatal("NewRef returned nil")
	}

	if ref.CurrentValue() != val {
		t.Errorf("expected %v, got %v", val, ref.CurrentValue())
	}

	if ref.Type() != "ref" {
		t.Errorf("expected type 'ref', got %s", ref.Type())
	}
}

// TestRefWithValidator はバリデータ付きRefをテストする
func TestRefWithValidator(t *testing.T) {
	// 正の数のみを許可するバリデータ
	validator := func(v types.Value) bool {
		if num, ok := v.(*atom.Number); ok {
			return num.ToFloat64() > 0
		}
		return false
	}

	// 有効な初期値
	ref, err := NewRefWithValidator(atom.NewNumberInt(10), validator)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref == nil {
		t.Fatal("expected ref, got nil")
	}

	// 無効な初期値
	_, err = NewRefWithValidator(atom.NewNumberInt(-5), validator)
	if err == nil {
		t.Error("expected error for invalid initial value")
	}
	if !errors.Is(err, ErrValidatorRejected) {
		t.Errorf("expected ErrValidatorRejected, got %v", err)
	}
}

// TestTransactionBasic は基本的なトランザクション操作をテストする
func TestTransactionBasic(t *testing.T) {
	store := NewStore()
	ref := NewRef(atom.NewNumberInt(10))
	ctx := context.Background()

	result, err := store.RunInTransaction(ctx, func(txCtx context.Context) (types.Value, error) {
		tx := GetTransaction(txCtx)
		if tx == nil {
			t.Fatal("transaction not found in context")
		}

		// 値を読み取り
		val, err := tx.Deref(ref)
		if err != nil {
			return nil, err
		}

		// 新しい値を設定
		num := val.(*atom.Number)
		newVal := atom.NewNumberInt(int64(num.ToFloat64()) + 5)
		if err := tx.Set(ref, newVal); err != nil {
			return nil, err
		}

		return newVal, nil
	})

	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// 結果を確認
	if result.(*atom.Number).ToFloat64() != 15 {
		t.Errorf("expected 15, got %v", result)
	}

	// Refの値を確認
	if ref.CurrentValue().(*atom.Number).ToFloat64() != 15 {
		t.Errorf("ref value expected 15, got %v", ref.CurrentValue())
	}
}

// TestTransactionIsolation はトランザクションの分離性をテストする
func TestTransactionIsolation(t *testing.T) {
	store := NewStore()
	ref := NewRef(atom.NewNumberInt(0))
	ctx := context.Background()

	var wg sync.WaitGroup
	var successCount int64
	iterations := 100
	goroutines := 10

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_, err := store.RunInTransaction(ctx, func(txCtx context.Context) (types.Value, error) {
					tx := GetTransaction(txCtx)

					val, err := tx.Deref(ref)
					if err != nil {
						return nil, err
					}

					num := val.(*atom.Number)
					newVal := atom.NewNumberInt(int64(num.ToFloat64()) + 1)
					if err := tx.Set(ref, newVal); err != nil {
						return nil, err
					}

					return newVal, nil
				})

				if err != nil {
					t.Errorf("transaction failed: %v", err)
					return
				}
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	// 全てのトランザクションが成功したか確認
	expectedTxCount := int64(goroutines * iterations)
	if successCount != expectedTxCount {
		t.Errorf("expected %d successful transactions, got %d", expectedTxCount, successCount)
	}

	// 全てのインクリメントが反映されているか確認
	actual := int64(ref.CurrentValue().(*atom.Number).ToFloat64())
	if actual != expectedTxCount {
		t.Errorf("expected %d, got %d", expectedTxCount, actual)
	}
}

// TestTransactionEnsure はensureをテストする
func TestTransactionEnsure(t *testing.T) {
	store := NewStore()
	ref1 := NewRef(atom.NewNumberInt(10))
	ref2 := NewRef(atom.NewNumberInt(20))
	ctx := context.Background()

	result, err := store.RunInTransaction(ctx, func(txCtx context.Context) (types.Value, error) {
		tx := GetTransaction(txCtx)

		// ref2をensure
		if err := tx.Ensure(ref2); err != nil {
			return nil, err
		}

		// ref1の値を読み取ってref2の値を基に更新
		val1, err := tx.Deref(ref1)
		if err != nil {
			return nil, err
		}

		val2, err := tx.Deref(ref2)
		if err != nil {
			return nil, err
		}

		num1 := val1.(*atom.Number).ToFloat64()
		num2 := val2.(*atom.Number).ToFloat64()
		newVal := atom.NewNumberInt(int64(num1 + num2))
		if err := tx.Set(ref1, newVal); err != nil {
			return nil, err
		}

		return newVal, nil
	})

	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	if result.(*atom.Number).ToFloat64() != 30 {
		t.Errorf("expected 30, got %v", result)
	}
}

// TestTransactionRetry はリトライをテストする
func TestTransactionRetry(t *testing.T) {
	store := NewStore()
	ref := NewRef(atom.NewNumberInt(0))
	ctx := context.Background()

	var wg sync.WaitGroup

	// 2つの並行トランザクションを実行
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.RunInTransaction(ctx, func(txCtx context.Context) (types.Value, error) {
				tx := GetTransaction(txCtx)

				val, err := tx.Deref(ref)
				if err != nil {
					return nil, err
				}

				// 意図的に競合を発生させやすくする
				num := val.(*atom.Number)
				newVal := atom.NewNumberInt(int64(num.ToFloat64()) + 1)
				if err := tx.Set(ref, newVal); err != nil {
					return nil, err
				}

				return newVal, nil
			})

			if err != nil {
				t.Errorf("transaction failed: %v", err)
			}
		}()
	}

	wg.Wait()

	// 結果は2であるべき
	actual := int64(ref.CurrentValue().(*atom.Number).ToFloat64())
	if actual != 2 {
		t.Errorf("expected 2, got %d", actual)
	}
}

// TestRefHash はRef.Hashをテストする
func TestRefHash(t *testing.T) {
	ref1 := NewRef(atom.NewNumberInt(42))
	ref2 := NewRef(atom.NewNumberInt(42))

	// 異なるRefは異なるハッシュを持つべき（同じ値でも）
	if ref1.Hash() == ref2.Hash() {
		t.Error("different refs should have different hashes")
	}
}

// TestRefEquals はRef.Equalsをテストする
func TestRefEquals(t *testing.T) {
	ref1 := NewRef(atom.NewNumberInt(42))
	ref2 := NewRef(atom.NewNumberInt(42))

	// 同じ参照は等しい
	if !ref1.Equals(ref1) {
		t.Error("ref should be equal to itself")
	}

	// 異なる参照は等しくない（値が同じでも）
	if ref1.Equals(ref2) {
		t.Error("different refs should not be equal")
	}

	// nilとの比較
	if ref1.Equals(nil) {
		t.Error("ref should not equal nil")
	}
}

// TestSetValidator はSetValidatorをテストする
func TestSetValidator(t *testing.T) {
	ref := NewRef(atom.NewNumberInt(10))

	// 正の数のみを許可するバリデータを設定
	validator := func(v types.Value) bool {
		if num, ok := v.(*atom.Number); ok {
			return num.ToFloat64() > 0
		}
		return false
	}

	err := ref.SetValidator(validator)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 負の数を持つRefにバリデータを設定しようとする
	negRef := NewRef(atom.NewNumberInt(-5))
	err = negRef.SetValidator(validator)
	if err == nil {
		t.Error("expected error when setting validator on ref with invalid value")
	}
}

// TestTransactionWithValidatorReject はバリデータによる拒否をテストする
func TestTransactionWithValidatorReject(t *testing.T) {
	store := NewStore()
	validator := func(v types.Value) bool {
		if num, ok := v.(*atom.Number); ok {
			return num.ToFloat64() >= 0
		}
		return false
	}

	ref, err := NewRefWithValidator(atom.NewNumberInt(10), validator)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()

	// 無効な値を設定しようとする
	_, err = store.RunInTransaction(ctx, func(txCtx context.Context) (types.Value, error) {
		tx := GetTransaction(txCtx)
		newVal := atom.NewNumberInt(-5)
		if err := tx.Set(ref, newVal); err != nil {
			return nil, err
		}
		return newVal, nil
	})

	if err == nil {
		t.Error("expected error when setting invalid value")
	}
	if !errors.Is(err, ErrValidatorRejected) {
		t.Errorf("expected ErrValidatorRejected, got %v", err)
	}
}

// TestGetTransaction はGetTransactionをテストする
func TestGetTransaction(t *testing.T) {
	store := NewStore()
	ctx := context.Background()

	// トランザクション外
	tx := GetTransaction(ctx)
	if tx != nil {
		t.Error("expected nil transaction outside of transaction")
	}

	// トランザクション内
	newTx := store.NewTransaction()
	txCtx := WithTransaction(ctx, newTx)
	tx = GetTransaction(txCtx)
	if tx != newTx {
		t.Error("expected to get the same transaction")
	}
}

// TestStoreWithOptions はStoreオプションをテストする
func TestStoreWithOptions(t *testing.T) {
	store := NewStoreWithOptions(
		WithMaxRetries(100),
		WithMaxBackoffBits(5),
	)

	if store.maxRetries != 100 {
		t.Errorf("expected maxRetries=100, got %d", store.maxRetries)
	}
	if store.maxBackoffBits != 5 {
		t.Errorf("expected maxBackoffBits=5, got %d", store.maxBackoffBits)
	}
}

// TestStoreMaxRetriesExceeded は最大リトライ回数超過をテストする
func TestStoreMaxRetriesExceeded(t *testing.T) {
	// リトライ回数を少なく設定
	store := NewStoreWithOptions(WithMaxRetries(1))
	ref := NewRef(atom.NewNumberInt(0))
	ctx := context.Background()

	// 常に失敗するトランザクション
	_, err := store.RunInTransaction(ctx, func(txCtx context.Context) (types.Value, error) {
		return nil, ErrRetry
	})

	if err == nil {
		t.Error("expected error after max retries exceeded")
	}
	if !errors.Is(err, ErrMaxRetriesExceeded) {
		t.Errorf("expected ErrMaxRetriesExceeded, got %v", err)
	}

	// refは変更されていないはず
	if ref.CurrentValue().(*atom.Number).ToFloat64() != 0 {
		t.Errorf("ref should not have been modified")
	}
}

// TestContextCancellation はコンテキストキャンセルをテストする
func TestContextCancellation(t *testing.T) {
	store := NewStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 即座にキャンセル

	_, err := store.RunInTransaction(ctx, func(txCtx context.Context) (types.Value, error) {
		return atom.NewNumberInt(1), nil
	})

	if err == nil {
		t.Error("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestRefAddWatch はRef.AddWatchをテストする
func TestRefAddWatch(t *testing.T) {
	store := NewStore()
	ref := NewRef(atom.NewNumberInt(10))
	ctx := context.Background()

	var watchCalled int32
	var watchOldVal, watchNewVal types.Value
	var watchRef *Ref
	var watchDone = make(chan struct{})

	// ウォッチャーを追加
	ref.AddWatch(atom.NewKeyword("test"), func(c context.Context, key types.Value, r *Ref, oldVal, newVal types.Value) error {
		atomic.AddInt32(&watchCalled, 1)
		watchOldVal = oldVal
		watchNewVal = newVal
		watchRef = r
		close(watchDone)
		return nil
	})

	// トランザクションで値を変更
	_, err := store.RunInTransaction(ctx, func(txCtx context.Context) (types.Value, error) {
		tx := GetTransaction(txCtx)
		return atom.NewNumberInt(20), tx.Set(ref, atom.NewNumberInt(20))
	})

	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// ウォッチャーが呼ばれるまで待機（非同期なので少し待つ）
	select {
	case <-watchDone:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("test timeout")
	}

	if atomic.LoadInt32(&watchCalled) != 1 {
		t.Errorf("expected watch to be called once, got %d", watchCalled)
	}
	if watchOldVal.(*atom.Number).ToFloat64() != 10 {
		t.Errorf("expected old value 10, got %v", watchOldVal)
	}
	if watchNewVal.(*atom.Number).ToFloat64() != 20 {
		t.Errorf("expected new value 20, got %v", watchNewVal)
	}
	if watchRef != ref {
		t.Error("expected ref to be the same")
	}
}

func TestRefWatcherFailureDoesNotChangeCommitOutcome(t *testing.T) {
	store := NewStore()
	ref := NewRef(atom.NewNumberInt(0))
	var called atomic.Bool
	ref.AddWatch(atom.NewKeyword("failing"), func(context.Context, types.Value, *Ref, types.Value, types.Value) error {
		called.Store(true)
		return errors.New("watch failed")
	})

	result, err := store.RunInTransaction(context.Background(), func(ctx context.Context) (types.Value, error) {
		value := atom.NewNumberInt(1)
		return value, GetTransaction(ctx).Set(ref, value)
	})
	if err != nil {
		t.Fatalf("committed transaction returned watcher failure: %v", err)
	}
	if got := result.String(); got != "1" {
		t.Fatalf("result = %s, want 1", got)
	}
	if got := ref.CurrentValue().String(); got != "1" {
		t.Fatalf("state = %s, want committed value 1", got)
	}
	if !called.Load() {
		t.Fatal("watcher was not called")
	}
}

// TestRefRemoveWatch はRef.RemoveWatchをテストする
func TestRefRemoveWatch(t *testing.T) {
	store := NewStore()
	ref := NewRef(atom.NewNumberInt(10))
	ctx := context.Background()

	var watchCalled int32

	key := atom.NewKeyword("test")
	// ウォッチャーを追加
	ref.AddWatch(key, func(c context.Context, k types.Value, r *Ref, oldVal, newVal types.Value) error {
		atomic.AddInt32(&watchCalled, 1)
		return nil
	})

	// ウォッチャーを削除
	ref.RemoveWatch(key)

	// トランザクションで値を変更
	_, err := store.RunInTransaction(ctx, func(txCtx context.Context) (types.Value, error) {
		tx := GetTransaction(txCtx)
		return atom.NewNumberInt(20), tx.Set(ref, atom.NewNumberInt(20))
	})

	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// ウォッチャーが呼ばれていないことを確認（少し待つ）
	// 非同期で実行されるため、短いスリープで待機
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&watchCalled) != 0 {
		t.Errorf("expected watch not to be called after removal, got %d", watchCalled)
	}
}

func TestRefWatchKeysUseHashAndEquality(t *testing.T) {
	store := NewStore()
	ref := NewRef(atom.NewNumberInt(0))
	var firstCalls, secondCalls, collisionCalls int32
	notifyDone := make(chan struct{}, 2)

	ref.AddWatch(atom.NewNumberInt(7), func(context.Context, types.Value, *Ref, types.Value, types.Value) error {
		atomic.AddInt32(&firstCalls, 1)
		return nil
	})
	ref.AddWatch(atom.NewNumberInt(7), func(context.Context, types.Value, *Ref, types.Value, types.Value) error {
		atomic.AddInt32(&secondCalls, 1)
		return nil
	})
	ref.AddWatch(collidingWatchKey("one"), func(context.Context, types.Value, *Ref, types.Value, types.Value) error {
		atomic.AddInt32(&collisionCalls, 1)
		return nil
	})
	ref.AddWatch(collidingWatchKey("two"), func(context.Context, types.Value, *Ref, types.Value, types.Value) error {
		atomic.AddInt32(&collisionCalls, 10)
		notifyDone <- struct{}{}
		return nil
	})

	setRef := func(value int64) {
		t.Helper()
		_, err := store.RunInTransaction(context.Background(), func(ctx context.Context) (types.Value, error) {
			newValue := atom.NewNumberInt(value)
			return newValue, GetTransaction(ctx).Set(ref, newValue)
		})
		if err != nil {
			t.Fatalf("transaction: %v", err)
		}
		select {
		case <-notifyDone:
		case <-time.After(time.Second):
			t.Fatal("watcher notification timed out")
		}
	}

	setRef(1)
	if got := atomic.LoadInt32(&firstCalls); got != 0 {
		t.Fatalf("replaced watcher called %d times, want 0", got)
	}
	if got := atomic.LoadInt32(&secondCalls); got != 1 {
		t.Fatalf("replacement watcher called %d times, want 1", got)
	}
	if got := atomic.LoadInt32(&collisionCalls); got != 11 {
		t.Fatalf("colliding watcher calls = %d, want 11", got)
	}

	ref.RemoveWatch(atom.NewNumberInt(7))
	ref.RemoveWatch(collidingWatchKey("one"))
	setRef(2)
	if got := atomic.LoadInt32(&secondCalls); got != 1 {
		t.Fatalf("equal-key removal left watcher active: %d", got)
	}
	if got := atomic.LoadInt32(&collisionCalls); got != 21 {
		t.Fatalf("collision removal calls = %d, want 21", got)
	}
}
