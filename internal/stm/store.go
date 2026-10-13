package stm

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"doggo/internal/types"
)

// 設定定数
const (
	DefaultMaxRetries     = 10000 // デフォルト最大リトライ回数
	DefaultMaxBackoffBits = 10    // バックオフの最大ビット数
)

// Store はSTMのグローバル状態を管理する。
type Store struct {
	revision       int64      // リビジョンカウンタ
	commitMu       sync.Mutex // コミット時の排他ロック
	maxRetries     int        // 最大リトライ回数
	maxBackoffBits int        // バックオフの最大ビット数
}

// NewStore は新しいSTMストアを作成する。
func NewStore() *Store {
	return &Store{
		revision:       0,
		maxRetries:     DefaultMaxRetries,
		maxBackoffBits: DefaultMaxBackoffBits,
	}
}

// StoreOption はStoreの設定オプション。
type StoreOption func(*Store)

// WithMaxRetries は最大リトライ回数を設定する。
func WithMaxRetries(n int) StoreOption {
	return func(s *Store) {
		s.maxRetries = n
	}
}

// WithMaxBackoffBits はバックオフの最大ビット数を設定する。
func WithMaxBackoffBits(bits int) StoreOption {
	return func(s *Store) {
		s.maxBackoffBits = bits
	}
}

// NewStoreWithOptions はオプション付きでSTMストアを作成する。
func NewStoreWithOptions(opts ...StoreOption) *Store {
	s := NewStore()
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// currentRevision は現在のリビジョンを返す。
func (s *Store) currentRevision() int64 {
	return atomic.LoadInt64(&s.revision)
}

// nextRevision はリビジョンをインクリメントして新しい値を返す。
func (s *Store) nextRevision() int64 {
	return atomic.AddInt64(&s.revision, 1)
}

// NewTransaction は新しいトランザクションを作成する。
func (s *Store) NewTransaction() *Transaction {
	return newTransaction(s)
}

// RunInTransaction はトランザクション内で関数を実行する。
func (s *Store) RunInTransaction(ctx context.Context, fn func(context.Context) (types.Value, error)) (types.Value, error) {
	for i := 0; i < s.maxRetries; i++ {
		// コンテキストのキャンセルを確認
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		tx := s.NewTransaction()
		txCtx := WithTransaction(ctx, tx)

		result, err := fn(txCtx)
		if err != nil {
			// リトライエラーの判定
			if errors.Is(err, ErrRetry) {
				s.backoff(i)
				continue
			}
			return nil, err
		}

		committed, err := tx.commit(ctx)
		if err != nil {
			return nil, err
		}
		if committed {
			return result, nil
		}

		s.backoff(i)
	}

	return nil, fmt.Errorf("%w: %d回のリトライ後", ErrMaxRetriesExceeded, s.maxRetries)
}

// commitTransaction はトランザクションをコミットする。
func (s *Store) commitTransaction(ctx context.Context, tx *Transaction) (bool, error) {
	if len(tx.writes) == 0 && len(tx.ensures) == 0 {
		tx.runAfterCommit(ctx)
		return true, nil
	}

	s.commitMu.Lock()

	// 検証フェーズ: 競合チェック
	for r := range tx.writes {
		currentRev := r.loadEntry().revision
		if readRev, ok := tx.reads[r]; ok {
			if currentRev != readRev {
				s.commitMu.Unlock()
				return false, nil
			}
		} else if currentRev > tx.readPoint {
			s.commitMu.Unlock()
			return false, nil
		}
	}

	// ensure対象の検証
	for r, rev := range tx.ensures {
		if r.loadEntry().revision != rev {
			s.commitMu.Unlock()
			return false, nil
		}
	}

	// 変更前の値を保存（ウォッチャー通知用）
	oldVals := make(map[*Ref]types.Value, len(tx.writes))
	for r := range tx.writes {
		oldVals[r] = r.loadEntry().val
	}

	// 書き込みフェーズ
	newRev := s.nextRevision()
	for r, val := range tx.writes {
		r.storeEntry(val, newRev)
	}
	s.commitMu.Unlock()

	// コミット後の通知失敗で、確定済みトランザクションを失敗として返さない。
	for r, newVal := range tx.writes {
		_ = r.notifyWatchers(ctx, oldVals[r], newVal)
	}
	tx.runAfterCommit(ctx)

	return true, nil
}

// backoff は指数バックオフを実行する。
func (s *Store) backoff(attempt int) {
	if attempt == 0 {
		return
	}
	bits := attempt
	if bits > s.maxBackoffBits {
		bits = s.maxBackoffBits
	}
	maxDelay := time.Duration(1<<uint(bits)) * time.Microsecond
	delay := time.Duration(rand.Int63n(int64(maxDelay)))
	time.Sleep(delay)
}
