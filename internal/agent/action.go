package agent

import (
	"context"
	"fmt"

	"doggo/internal/types"
)

// ActionFn はアクション関数の型。
type ActionFn func(ctx context.Context, state types.Value, args []types.Value) (types.Value, error)

type actionContextKey struct{}

type action struct {
	ctx  context.Context
	fn   ActionFn
	args []types.Value
	done chan struct{}
}

func newAction(ctx context.Context, fn ActionFn, args []types.Value) *action {
	return &action{ctx: ctx, fn: fn, args: args}
}

func newTrackedAction(ctx context.Context, fn ActionFn, args []types.Value) *action {
	action := newAction(ctx, fn, args)
	action.done = make(chan struct{})
	return action
}

func (a *action) executionContext() context.Context {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, actionContextKey{}, true)
}

func (a *action) execute(state types.Value) (types.Value, error) {
	return a.fn(a.executionContext(), state, a.args)
}

func isActionContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	active, _ := ctx.Value(actionContextKey{}).(bool)
	return active
}

func (a *action) waitWithContext(ctx context.Context) error {
	if a.done == nil {
		return nil
	}
	select {
	case <-a.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CheckSendable は現在の状態でアクションを受け付けられるか確認する。
func (a *Agent) CheckSendable() error {
	if a.IsClosed() {
		return ErrAgentClosed
	}
	a.mu.RLock()
	hasError, mode := a.err != nil, a.errorMode
	a.mu.RUnlock()
	if hasError && mode == ErrorModeFail {
		return fmt.Errorf("%w: %v", ErrAgentFailed, a.err)
	}
	return nil
}

// Send はアクションを非同期実行用キューへ送信する。
func (a *Agent) Send(ctx context.Context, fn ActionFn, args []types.Value) error {
	if err := a.CheckSendable(); err != nil {
		return err
	}
	select {
	case a.actionCh <- newAction(ctx, fn, args):
		return nil
	case <-a.ctx.Done():
		return ErrAgentClosed
	}
}

// SendAndWait はアクションを送信して完了を待機する。
func (a *Agent) SendAndWait(ctx context.Context, fn ActionFn, args []types.Value) error {
	if err := a.CheckSendable(); err != nil {
		return err
	}
	action := newTrackedAction(ctx, fn, args)
	select {
	case a.actionCh <- action:
		return action.waitWithContext(ctx)
	case <-a.ctx.Done():
		return ErrAgentClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}
