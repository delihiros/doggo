package stm

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"doggo/internal/types"
	"doggo/internal/types/coll"
	"doggo/internal/types/util"
)

// entry はリビジョン付きの値を保持する。
type entry struct {
	val      types.Value
	revision int64
}

// RefWatchFn はRefのウォッチャー関数の型。
type RefWatchFn func(ctx context.Context, key types.Value, ref *Ref, oldVal, newVal types.Value) error

// Ref はSTM（ソフトウェアトランザクションメモリ）の参照を表す。
type Ref struct {
	id      int64
	current atomic.Value // *entry

	// mu は以下のフィールドを保護する
	mu        sync.RWMutex
	validator func(types.Value) bool
	watchers  map[uint32][]refWatcherEntry
	watchGen  uint64
}

type refWatcherEntry struct {
	key types.Value
	fn  RefWatchFn
}

// インターフェース実装の保証
var _ types.Value = (*Ref)(nil)

// NewRef は新しいRefを作成する。
func NewRef(val types.Value) *Ref {
	r := &Ref{id: util.NextID()}
	r.current.Store(&entry{val: val, revision: 0})
	return r
}

// NewRefWithValidator はバリデータ付きRefを作成する。
func NewRefWithValidator(val types.Value, validator func(types.Value) bool) (*Ref, error) {
	if validator != nil && !validator(val) {
		return nil, ErrValidatorRejected
	}
	r := &Ref{
		id:        util.NextID(),
		validator: validator,
	}
	r.current.Store(&entry{val: val, revision: 0})
	return r, nil
}

// String はRefの文字列表現を返す。
func (r *Ref) String() string {
	return fmt.Sprintf("#<Ref@%d: [...]>", r.id)
}

// Type は型名を返す。
func (r *Ref) Type() string { return "ref" }

// Hash はハッシュ値を返す。
func (r *Ref) Hash() uint32 {
	return util.HashString(fmt.Sprintf("ref-%d", r.id))
}

// Equals は参照等価性を確認する。
func (r *Ref) Equals(other types.Value) bool { return r == other }

// IsSelfEvaluating は常にfalseを返す。
func (r *Ref) IsSelfEvaluating() bool { return false }

// CurrentValue は現在のコミット済み値を返す。
func (r *Ref) CurrentValue() types.Value {
	return r.loadEntry().val
}

// SetValidator はバリデータを設定する。
func (r *Ref) SetValidator(validator func(types.Value) bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if validator != nil {
		current := r.loadEntry()
		if !validator(current.val) {
			return ErrValidatorRejected
		}
	}
	r.validator = validator
	return nil
}

// GetValidator は現在のバリデータを返す。
func (r *Ref) GetValidator() func(types.Value) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.validator
}

// ヘルパーメソッド

// loadEntry は現在のエントリを読み込む。
func (r *Ref) loadEntry() *entry {
	return r.current.Load().(*entry)
}

// validate は値を検証する。
func (r *Ref) validate(val types.Value) error {
	r.mu.RLock()
	v := r.validator
	r.mu.RUnlock()

	if v != nil && !v(val) {
		return ErrValidatorRejected
	}
	return nil
}

// storeEntry は新しいエントリを格納する（Store内部用）。
func (r *Ref) storeEntry(val types.Value, revision int64) {
	r.current.Store(&entry{val: val, revision: revision})
}

// AddWatch はウォッチャーを追加する。
func (r *Ref) AddWatch(key types.Value, fn RefWatchFn) error {
	hash, err := coll.ValueHashResult(key)
	if err != nil {
		return err
	}
	for {
		r.mu.RLock()
		generation := r.watchGen
		bucket := append([]refWatcherEntry(nil), r.watchers[hash]...)
		r.mu.RUnlock()

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

		r.mu.Lock()
		if generation != r.watchGen {
			r.mu.Unlock()
			continue
		}
		if r.watchers == nil {
			r.watchers = make(map[uint32][]refWatcherEntry)
		}
		if match >= 0 {
			bucket[match] = refWatcherEntry{key: key, fn: fn}
			r.watchers[hash] = bucket
		} else {
			r.watchers[hash] = append(bucket, refWatcherEntry{key: key, fn: fn})
		}
		r.watchGen++
		r.mu.Unlock()
		return nil
	}
}

// RemoveWatch はウォッチャーを削除する。
func (r *Ref) RemoveWatch(key types.Value) error {
	hash, err := coll.ValueHashResult(key)
	if err != nil {
		return err
	}
	for {
		r.mu.RLock()
		generation := r.watchGen
		bucket := append([]refWatcherEntry(nil), r.watchers[hash]...)
		r.mu.RUnlock()

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

		r.mu.Lock()
		if generation != r.watchGen {
			r.mu.Unlock()
			continue
		}
		bucket = append(bucket[:match], bucket[match+1:]...)
		if len(bucket) == 0 {
			delete(r.watchers, hash)
		} else {
			r.watchers[hash] = bucket
		}
		r.watchGen++
		r.mu.Unlock()
		return nil
	}
}

// notifyWatchers はウォッチャーに値の変更を通知する。
func (r *Ref) notifyWatchers(ctx context.Context, oldVal, newVal types.Value) error {
	r.mu.RLock()
	var watchers []refWatcherEntry
	for _, bucket := range r.watchers {
		watchers = append(watchers, bucket...)
	}
	r.mu.RUnlock()

	var firstErr error
	for _, watcher := range watchers {
		if err := util.InvokeWatcher(func() error {
			return watcher.fn(ctx, watcher.key, r, oldVal, newVal)
		}); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("watch %s failed: %w", watcher.key.String(), err)
		}
	}
	return firstErr
}
