// Package agent はClojureスタイルのエージェント機能を提供する。
package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"doggo/internal/types"
	"doggo/internal/types/coll"
	"doggo/internal/types/util"
)

// ErrorMode はエラー発生時のエージェントの挙動を定義する。
type ErrorMode int

const (
	// ErrorModeFail はエラー発生時にエージェントを停止する（デフォルト）。
	ErrorModeFail ErrorMode = iota
	// ErrorModeContinue はエラー発生時も処理を継続する。
	ErrorModeContinue
)

// WatchFn は状態変更を監視する関数型。
type WatchFn func(ctx context.Context, key types.Value, agent *Agent, oldState, newState types.Value) error

// ErrorHandlerFn はエラーを処理する関数型。
type ErrorHandlerFn func(agent *Agent, err error)

// Agent は状態を管理し、非同期で更新を行うアクター。
type Agent struct {
	id    int64
	state atomic.Pointer[agentState]

	// mu は以下のフィールドを保護する
	mu           sync.RWMutex
	validator    func(types.Value) bool
	watchers     map[uint32][]watcherEntry
	watchGen     uint64
	errorHandler ErrorHandlerFn
	errorMode    ErrorMode
	err          error

	// アクション処理用
	actionCh chan *action

	// ライフサイクル管理
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type agentState struct {
	value types.Value
}

type watcherEntry struct {
	key types.Value
	fn  WatchFn
}

// インターフェース実装の保証
var _ types.Value = (*Agent)(nil)

// NewAgent は新しいエージェントを作成する。
func NewAgent(initialState types.Value) *Agent {
	ctx, cancel := context.WithCancel(context.Background())
	a := &Agent{
		id:        util.NextID(),
		watchers:  make(map[uint32][]watcherEntry),
		errorMode: ErrorModeFail,
		actionCh:  make(chan *action, 1024),
		ctx:       ctx,
		cancel:    cancel,
	}
	a.state.Store(&agentState{value: initialState})

	a.wg.Add(1)
	go a.processActions()
	return a
}

// NewAgentWithValidator はバリデータ付きエージェントを作成する。
func NewAgentWithValidator(initialState types.Value, validator func(types.Value) bool) (*Agent, error) {
	if validator != nil && !validator(initialState) {
		return nil, ErrValidatorRejected
	}
	a := NewAgent(initialState)
	a.validator = validator
	return a, nil
}

// processActions はアクションを順次処理する。
func (a *Agent) processActions() {
	defer a.wg.Done()

	for {
		select {
		case <-a.ctx.Done():
			return
		case action := <-a.actionCh:
			a.executeAction(action)
		}
	}
}

// executeAction は単一のアクションを実行する。
func (a *Agent) executeAction(action *action) {
	defer func() {
		if action.done != nil {
			close(action.done)
		}
	}()

	a.mu.RLock()
	hasError := a.err != nil
	mode := a.errorMode
	a.mu.RUnlock()

	if hasError && mode == ErrorModeFail {
		return
	}

	oldState := a.state.Load().value
	newState, err := action.execute(oldState)

	if err != nil {
		a.handleError(err)
		return
	}

	// バリデータ取得
	a.mu.RLock()
	validator := a.validator
	a.mu.RUnlock()

	if validator != nil && !validator(newState) {
		a.handleError(ErrValidatorRejected)
		return
	}

	a.state.Store(&agentState{value: newState})

	if err := a.notifyWatchers(action.executionContext(), oldState, newState); err != nil {
		a.handleError(err)
	}
}

func (a *Agent) handleError(err error) {
	a.mu.Lock()
	a.err = err
	handler := a.errorHandler
	a.mu.Unlock()

	if handler != nil {
		handler(a, err)
	}
}

func (a *Agent) notifyWatchers(ctx context.Context, oldState, newState types.Value) error {
	if types.Identical(oldState, newState) {
		return nil
	}
	a.mu.RLock()
	// スナップショットを作成してロック時間を短縮
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

// String はエージェントの文字列表現を返す。
func (a *Agent) String() string {
	a.mu.RLock()
	hasErr := a.err != nil
	a.mu.RUnlock()

	if hasErr {
		return fmt.Sprintf("#<Agent@%d FAILED: ...>", a.id)
	}
	return fmt.Sprintf("#<Agent@%d: ...>", a.id)
}

// Type は型名を返す。
func (a *Agent) Type() string { return "agent" }

// Hash はハッシュ値を返す。
func (a *Agent) Hash() uint32 {
	return util.HashString(fmt.Sprintf("agent-%d", a.id))
}

// Equals は参照等価性を確認する。
func (a *Agent) Equals(other types.Value) bool { return a == other }

// IsSelfEvaluating は常にfalseを返す。
func (a *Agent) IsSelfEvaluating() bool { return false }

// State は現在の状態を返す。
func (a *Agent) State() types.Value {
	return a.state.Load().value
}

// CurrentError は現在保存されている非同期アクションのエラーを返す。
func (a *Agent) CurrentError() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.err
}

// SetValidator はバリデータを設定する。
func (a *Agent) SetValidator(validator func(types.Value) bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if validator != nil {
		current := a.State()
		if !validator(current) {
			return ErrValidatorRejected
		}
	}
	a.validator = validator
	return nil
}

// GetValidator は現在のバリデータを返す。
func (a *Agent) GetValidator() func(types.Value) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.validator
}

// AddWatch は監視関数を追加する。
func (a *Agent) AddWatch(key types.Value, fn WatchFn) error {
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
func (a *Agent) RemoveWatch(key types.Value) error {
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

// SetErrorHandler はエラーハンドラを設定する。
func (a *Agent) SetErrorHandler(handler ErrorHandlerFn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.errorHandler = handler
}

// GetErrorHandler はエラーハンドラを返す。
func (a *Agent) GetErrorHandler() ErrorHandlerFn {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.errorHandler
}

// SetErrorMode はエラーモードを設定する。
func (a *Agent) SetErrorMode(mode ErrorMode) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.errorMode = mode
}

// GetErrorMode はエラーモードを返す。
func (a *Agent) GetErrorMode() ErrorMode {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.errorMode
}

// Restart はエージェントの状態をリセットし、エラーをクリアする。
func (a *Agent) Restart(newState types.Value, clearActions bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.validator != nil && !a.validator(newState) {
		return ErrValidatorRejected
	}

	a.err = nil
	a.state.Store(&agentState{value: newState})

	if clearActions {
		// チャネル内のアクションを全て破棄する
	loop:
		for {
			select {
			case action := <-a.actionCh:
				if action.done != nil {
					close(action.done)
				}
			default:
				break loop
			}
		}
	}
	return nil
}

// Close はエージェントを停止する。
func (a *Agent) Close() {
	a.cancel()
}

// IsClosed はエージェントが停止しているかを確認する。
func (a *Agent) IsClosed() bool {
	select {
	case <-a.ctx.Done():
		return true
	default:
		return false
	}
}

// trySend はアクションの送信を試みる。
func (a *Agent) trySend(action *action, ctx context.Context) bool {
	select {
	case <-a.ctx.Done():
		return false
	case a.actionCh <- action:
		return true
	case <-ctx.Done():
		return false
	}
}
