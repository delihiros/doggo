package stm

import (
	"context"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// Transaction はSTMトランザクションの状態を管理する。
type Transaction struct {
	id          int64
	readPoint   int64
	store       *Store
	reads       map[*Ref]int64
	writes      map[*Ref]types.Value
	ensures     map[*Ref]int64
	afterCommit []func(context.Context)
}

// txContextKey はコンテキスト用のキー。
type txContextKey struct{}

// WithTransaction はトランザクションをコンテキストに関連付ける。
func WithTransaction(ctx context.Context, tx *Transaction) context.Context {
	return context.WithValue(ctx, txContextKey{}, tx)
}

// GetTransaction はコンテキストからトランザクションを取得する。
func GetTransaction(ctx context.Context) *Transaction {
	tx, _ := ctx.Value(txContextKey{}).(*Transaction)
	return tx
}

// AfterCommit はトランザクションが成功した後に実行する処理を登録する。
func (tx *Transaction) AfterCommit(fn func(context.Context)) {
	tx.afterCommit = append(tx.afterCommit, fn)
}

func (tx *Transaction) runAfterCommit(ctx context.Context) {
	for _, fn := range tx.afterCommit {
		fn(ctx)
	}
}

// newTransaction は新しいトランザクションを作成する。
func newTransaction(s *Store) *Transaction {
	return &Transaction{
		id:        util.NextID(),
		readPoint: s.currentRevision(),
		store:     s,
		reads:     make(map[*Ref]int64),
		writes:    make(map[*Ref]types.Value),
		ensures:   make(map[*Ref]int64),
	}
}

// Deref はトランザクション内でRefの値を読み取る。
func (tx *Transaction) Deref(r *Ref) (types.Value, error) {
	// トランザクション内の書き込みを優先
	if val, ok := tx.writes[r]; ok {
		return val, nil
	}

	// 現在の値を読み取る
	e := r.loadEntry()

	// スナップショット分離: トランザクション開始後に更新された場合はリトライ
	if e.revision > tx.readPoint {
		return nil, ErrRetry
	}

	// 読み取りセットに追加
	tx.reads[r] = e.revision
	return e.val, nil
}

// Set はRefに値を設定する（ref-set）。
func (tx *Transaction) Set(r *Ref, val types.Value) error {
	if err := r.validate(val); err != nil {
		return err
	}
	tx.writes[r] = val
	return nil
}

// Alter はRefの値を更新する（alter）。
func (tx *Transaction) Alter(r *Ref, newVal types.Value) error {
	if err := r.validate(newVal); err != nil {
		return err
	}
	tx.writes[r] = newVal
	return nil
}

// Ensure はRefのバージョンが変わっていないことを保証する（ensure）。
func (tx *Transaction) Ensure(r *Ref) error {
	e := r.loadEntry()
	if e.revision > tx.readPoint {
		return ErrRetry
	}
	tx.ensures[r] = e.revision
	return nil
}

// commit はトランザクションのコミットを試みる。
func (tx *Transaction) commit(ctx context.Context) (bool, error) {
	return tx.store.commitTransaction(ctx, tx)
}
