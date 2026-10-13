package atom

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"doggo/internal/types"
	"doggo/internal/types/coll"
	"doggo/internal/types/util"
)

// WatchFn は状態変更を監視する関数型。
type WatchFn func(ctx context.Context, key types.Value, atom *Atom, oldState, newState types.Value) error

// Atom は、同期的かつ独立した状態を管理する参照型です。
type Atom struct {
	id    int64
	state atomic.Pointer[atomState]

	// mu は以下のフィールドを保護する
	mu        sync.RWMutex
	validator func(types.Value) bool
	watchers  map[uint32][]watcherEntry
	watchGen  uint64
}

type atomState struct {
	value types.Value
}

type watcherEntry struct {
	key types.Value
	fn  WatchFn
}

// インターフェース実装の保証
var _ types.Value = (*Atom)(nil)

// ErrValidatorRejected はバリデーションエラー
var ErrValidatorRejected = fmt.Errorf("validator rejected")

// NewAtom は新しいAtomを作成する。
func NewAtom(initialState types.Value) *Atom {
	a := &Atom{
		id:       util.NextID(),
		watchers: make(map[uint32][]watcherEntry),
	}
	a.state.Store(&atomState{value: initialState})
	return a
}

// NewAtomWithValidator はバリデータ付きAtomを作成する。
func NewAtomWithValidator(initialState types.Value, validator func(types.Value) bool) (*Atom, error) {
	if validator != nil && !validator(initialState) {
		return nil, ErrValidatorRejected
	}
	a := NewAtom(initialState)
	a.validator = validator
	return a, nil
}

// String はAtomの文字列表現を返す。
func (a *Atom) String() string {
	return fmt.Sprintf("#<Atom@%d: ...>", a.id)
}

// Type は型名を返す。
func (a *Atom) Type() string { return "atom" }

// Hash はハッシュ値を返す。
func (a *Atom) Hash() uint32 {
	return util.HashString(fmt.Sprintf("atom-%d", a.id))
}

// Equals は参照等価性を確認する。
func (a *Atom) Equals(other types.Value) bool { return a == other }

// IsSelfEvaluating は常にfalseを返す。
func (a *Atom) IsSelfEvaluating() bool { return false }

// Deref は現在の状態を返す。
func (a *Atom) Deref() types.Value {
	return a.state.Load().value
}

// Reset は値を設定し、新しい値を返す。
func (a *Atom) Reset(newState types.Value) (types.Value, error) {
	return a.ResetContext(context.Background(), newState)
}

// ResetContext は呼び出し元の実行コンテキストをウォッチャーへ渡して値を設定する。
func (a *Atom) ResetContext(ctx context.Context, newState types.Value) (types.Value, error) {
	if err := a.validate(newState); err != nil {
		return nil, err
	}

	oldState := a.state.Swap(&atomState{value: newState}).value

	if err := a.notifyWatchers(ctx, oldState, newState); err != nil {
		return newState, err
	}

	return newState, nil
}

// ResetVals は値を設定し、[oldVal, newVal]を返す。
func (a *Atom) ResetVals(newState types.Value) (types.Value, types.Value, error) {
	return a.ResetValsContext(context.Background(), newState)
}

// ResetValsContext は呼び出し元の実行コンテキストをウォッチャーへ渡して値を設定する。
func (a *Atom) ResetValsContext(ctx context.Context, newState types.Value) (types.Value, types.Value, error) {
	if err := a.validate(newState); err != nil {
		return nil, nil, err
	}

	oldState := a.state.Swap(&atomState{value: newState}).value

	if err := a.notifyWatchers(ctx, oldState, newState); err != nil {
		return oldState, newState, err
	}

	return oldState, newState, nil
}

// Swap は関数を適用して値を更新する。
func (a *Atom) Swap(ctx context.Context, fn func(types.Value) (types.Value, error)) (types.Value, error) {
	for {
		old := a.state.Load()
		oldState := old.value
		newState, err := fn(oldState)
		if err != nil {
			return nil, err
		}

		if err := a.validate(newState); err != nil {
			return nil, err
		}

		if a.state.CompareAndSwap(old, &atomState{value: newState}) {
			if err := a.notifyWatchers(ctx, oldState, newState); err != nil {
				return newState, err
			}
			return newState, nil
		}
	}
}

// SwapVals は関数を適用して値を更新し、[oldVal, newVal]を返す。
func (a *Atom) SwapVals(ctx context.Context, fn func(types.Value) (types.Value, error)) (types.Value, types.Value, error) {
	for {
		old := a.state.Load()
		oldState := old.value
		newState, err := fn(oldState)
		if err != nil {
			return nil, nil, err
		}

		if err := a.validate(newState); err != nil {
			return nil, nil, err
		}

		if a.state.CompareAndSwap(old, &atomState{value: newState}) {
			if err := a.notifyWatchers(ctx, oldState, newState); err != nil {
				return oldState, newState, err
			}
			return oldState, newState, nil
		}
	}
}

// CompareAndSet は値を比較して更新する。
func (a *Atom) CompareAndSet(ctx context.Context, oldVal, newVal types.Value) (bool, error) {
	if err := a.validate(newVal); err != nil {
		return false, err
	}

	// atomic.ValueのCompareAndSwapはポインタ比較ではなく値比較を行うが、
	// types.Valueの実装によっては注意が必要。
	// ここではValueインターフェースの実装が適切であることを前提とする。
	// ただし、atomic.CompareAndSwapはinterface{}の比較において、
	// 型と値の両方が一致する必要がある。

	currentState := a.state.Load()
	current := currentState.value
	if !current.Equals(oldVal) {
		return false, nil
	}

	// CAS操作
	// 注意: atomic.Value.CompareAndSwap はGo 1.17+で利用可能
	swapped := a.state.CompareAndSwap(currentState, &atomState{value: newVal})

	if swapped {
		if err := a.notifyWatchers(ctx, current, newVal); err != nil {
			return true, err
		}
	}

	return swapped, nil
}

// SetValidator はバリデータを設定する。
func (a *Atom) SetValidator(validator func(types.Value) bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if validator != nil {
		current := a.Deref()
		if !validator(current) {
			return ErrValidatorRejected
		}
	}
	a.validator = validator
	return nil
}

// GetValidator は現在のバリデータを返す。
func (a *Atom) GetValidator() func(types.Value) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.validator
}

// AddWatch は監視関数を追加する。
func (a *Atom) AddWatch(key types.Value, fn WatchFn) error {
	hash, err := coll.ValueHashResult(key)
	if err != nil {
		return err
	}
	for {
		a.mu.RLock()
		generation := a.watchGen
		bucket := append([]watcherEntry(nil), a.watchers[hash]...)
		a.mu.RUnlock()

		match := -1
		for i := range bucket {
			equal, err := coll.ValueEqualsResult(bucket[i].key, key)
			if err != nil {
				return err
			}
			if equal {
				match = i
				break
			}
		}

		a.mu.Lock()
		if generation != a.watchGen {
			a.mu.Unlock()
			continue
		}
		if match >= 0 {
			bucket[match] = watcherEntry{key: key, fn: fn}
			a.watchers[hash] = bucket
		} else {
			a.watchers[hash] = append(bucket, watcherEntry{key: key, fn: fn})
		}
		a.watchGen++
		a.mu.Unlock()
		return nil
	}
}

// RemoveWatch は監視関数を削除する。
func (a *Atom) RemoveWatch(key types.Value) error {
	hash, err := coll.ValueHashResult(key)
	if err != nil {
		return err
	}
	for {
		a.mu.RLock()
		generation := a.watchGen
		bucket := append([]watcherEntry(nil), a.watchers[hash]...)
		a.mu.RUnlock()

		match := -1
		for i := range bucket {
			equal, err := coll.ValueEqualsResult(bucket[i].key, key)
			if err != nil {
				return err
			}
			if equal {
				match = i
				break
			}
		}
		if match < 0 {
			return nil
		}

		a.mu.Lock()
		if generation != a.watchGen {
			a.mu.Unlock()
			continue
		}
		bucket = append(bucket[:match], bucket[match+1:]...)
		if len(bucket) == 0 {
			delete(a.watchers, hash)
		} else {
			a.watchers[hash] = bucket
		}
		a.watchGen++
		a.mu.Unlock()
		return nil
	}
}

// ヘルパーメソッド

func (a *Atom) validate(val types.Value) error {
	a.mu.RLock()
	v := a.validator
	a.mu.RUnlock()

	if v != nil && !v(val) {
		return ErrValidatorRejected
	}
	return nil
}

func (a *Atom) notifyWatchers(ctx context.Context, oldState, newState types.Value) error {
	if types.Identical(oldState, newState) {
		return nil
	}
	a.mu.RLock()
	var watchers []watcherEntry
	for _, bucket := range a.watchers {
		watchers = append(watchers, bucket...)
	}
	a.mu.RUnlock()

	var firstErr error
	for _, w := range watchers {
		if err := util.InvokeWatcher(func() error {
			return w.fn(ctx, w.key, a, oldState, newState)
		}); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("watch %s failed: %w", w.key.String(), err)
		}
	}
	return firstErr
}
