package agent

import (
	"context"
	"sync"
	"time"

	"doggo/internal/types"
)

// Pool は複数のエージェントを管理する。
type Pool struct {
	agents sync.Map
}

// NewPool は新しいプールを作成する。
func NewPool() *Pool {
	return &Pool{}
}

// Register はエージェントをプールに登録する。
func (p *Pool) Register(a *Agent) {
	p.agents.Store(a, struct{}{})
}

// Unregister はエージェントの登録を解除する。
func (p *Pool) Unregister(a *Agent) {
	p.agents.Delete(a)
}

// Shutdown は登録された全てのエージェントを停止する。
func (p *Pool) Shutdown() {
	p.agents.Range(func(key, _ interface{}) bool {
		if a, ok := key.(*Agent); ok {
			a.Close()
		}
		return true
	})
}

// Count は登録されているエージェント数を返す。
func (p *Pool) Count() int {
	count := 0
	p.agents.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// Await は指定されたエージェントのアクション完了を待機する。
func Await(ctx context.Context, agents ...*Agent) error {
	if isActionContext(ctx) {
		return ErrAwaitInAction
	}
	markers := make([]*action, 0, len(agents))

	for _, agent := range agents {
		if agent == nil || agent.IsClosed() {
			continue
		}

		// 何もしないアクションを送信して、その完了を待つことで同期をとる
		marker := newTrackedAction(ctx, func(_ context.Context, state types.Value, _ []types.Value) (types.Value, error) {
			return state, nil
		}, nil)
		if !agent.trySend(marker, ctx) {
			if err := ctx.Err(); err != nil {
				return err
			}
			continue
		}
		markers = append(markers, marker)
	}

	for _, marker := range markers {
		if err := marker.waitWithContext(ctx); err != nil {
			return err
		}
	}
	return nil
}

// AwaitFor はタイムアウト付きでAwaitを実行する。
func AwaitFor(timeout time.Duration, agents ...*Agent) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Await(ctx, agents...)
}
