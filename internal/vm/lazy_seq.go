package vm

import (
	"fmt"
	"sync/atomic"

	"doggo/internal/types"
	"doggo/internal/types/coll"
	"doggo/internal/types/util"
)

// ThunkInvoker は遅延シーケンスのサンクを実行するためのコールバック型です。
// Runner から提供され、クロージャを引数なしで呼び出して結果を返します。
type ThunkInvoker func(thunk types.Value) (types.Value, error)

// VMLazySeq は VM 用の遅延評価シーケンスを表します。
// Clojure の lazy-seq マクロによって作成されます。
type VMLazySeq struct {
	thunk                    types.Value  // 評価する関数（通常は *compiler.Closure）
	invoker                  ThunkInvoker // サンクを実行するためのコールバック
	once                     util.RecursionDetectingOnce
	cached                   types.Value // 評価結果のキャッシュ
	err                      error       // 評価時のエラー
	realized                 atomic.Bool
	infinite                 bool
	preservesInfinite        bool
	boundedSourceWorkPerNode bool
	sources                  []types.Value
}

// KnownInfinite reports whether the sequence constructor guarantees that the
// sequence has no finite end. The marker lets generic collection operations
// reject non-terminating hash and equality work before realizing the sequence.
func (ls *VMLazySeq) KnownInfinite() bool {
	return ls != nil && ls.infinite
}

func (ls *VMLazySeq) PreservesInfiniteLength() bool {
	return ls != nil && ls.preservesInfinite
}

func (ls *VMLazySeq) HasBoundedSourceWorkPerNode() bool {
	return ls != nil && ls.boundedSourceWorkPerNode
}

// Sources returns the declared inputs whose traversal controls this lazy sequence.
func (ls *VMLazySeq) Sources() []types.Seq {
	if ls == nil {
		return nil
	}
	sources := make([]types.Seq, 0, len(ls.sources))
	for _, source := range ls.sources {
		if seq, ok := source.(types.Seq); ok {
			sources = append(sources, seq)
		}
	}
	return sources
}

// RealizedSequence returns the sequence produced directly by this node.
func (ls *VMLazySeq) RealizedSequence() types.Seq {
	if ls == nil || !ls.realized.Load() {
		return nil
	}
	seq, _ := ls.cached.(types.Seq)
	return seq
}

// NewVMLazySeq は新しい VMLazySeq を作成します。
func NewVMLazySeq(thunk types.Value, invoker ThunkInvoker) *VMLazySeq {
	return &VMLazySeq{
		thunk:   thunk,
		invoker: invoker,
	}
}

// String は VMLazySeq の文字列表現を返します。
// 無限シーケンスの場合は最大表示要素数を制限します。
// エラーが発生した場合はエラー詳細を含む文字列を返します。
func (ls *VMLazySeq) String() string {
	return coll.SeqString(ls)
}

// Type は VMLazySeq の型名を返します。
func (ls *VMLazySeq) Type() string {
	return "lazy-seq"
}

// Hash は VMLazySeq のハッシュ値を返します。
func (ls *VMLazySeq) Hash() uint32 {
	return coll.SeqHash(ls)
}

// Equals は VMLazySeq の等価性を判定します。
func (ls *VMLazySeq) Equals(other types.Value) bool {
	return coll.SequentialEquals(ls, other)
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (ls *VMLazySeq) IsSelfEvaluating() bool {
	return false
}

// ensureEvaluated は VMLazySeq が評価されることを保証します。
func (ls *VMLazySeq) ensureEvaluated() error {
	return ls.once.Do(func() {
		defer ls.realized.Store(true)
		if ls.thunk != nil && ls.invoker != nil {
			ls.cached, ls.err = ls.invoker(ls.thunk)
		}
	})
}

// RealizeNode evaluates only this lazy node so collection value operations can
// inspect a returned sequence's provenance before traversing it.
func (ls *VMLazySeq) RealizeNode() error {
	if ls == nil {
		return nil
	}
	if err := ls.ensureEvaluated(); err != nil {
		return err
	}
	return ls.err
}

// Seq は VMLazySeq をシーケンスに変換します（Sequable 実装）。
// このメソッドが呼ばれたときに初めて評価が行われます。
// エラーが発生した場合は coll.LazySeqError を返します。
func (ls *VMLazySeq) Seq() types.Value {
	if ls == nil {
		return nil
	}
	current := ls
	var visited map[*VMLazySeq]struct{}
	var traversal *coll.LazySeqAliasTraversal
	defer func() {
		if traversal != nil {
			traversal.Close()
		}
	}()
	advanceTraversal := func() error {
		if traversal != nil {
			return traversal.Advance()
		}
		var err error
		traversal, err = coll.BeginLazySeqAliasEdge()
		return err
	}
	for {
		if err := current.ensureEvaluated(); err != nil {
			return &coll.LazySeqError{Err: err}
		}
		if current.err != nil {
			return &coll.LazySeqError{Err: current.err}
		}
		if current.cached == nil {
			return nil
		}
		if inner, ok := current.cached.(*VMLazySeq); ok {
			if err := advanceTraversal(); err != nil {
				return &coll.LazySeqError{Err: err}
			}
			if visited == nil {
				visited = map[*VMLazySeq]struct{}{current: {}}
			}
			if _, ok := visited[inner]; ok {
				return &coll.LazySeqError{Err: fmt.Errorf("recursive lazy sequence realization")}
			}
			visited[inner] = struct{}{}
			current = inner
			continue
		}
		if sequable, ok := current.cached.(types.Sequable); ok {
			if _, ok := current.cached.(types.LazySeq); ok {
				if err := advanceTraversal(); err != nil {
					return &coll.LazySeqError{Err: err}
				}
			}
			return sequable.Seq()
		}
		if _, ok := current.cached.(types.Seq); ok {
			return current.cached
		}
		return nil
	}
}

// First は VMLazySeq の最初の要素を返します（Seq 実装）。
func (ls *VMLazySeq) First() types.Value {
	if ls == nil {
		return nil
	}
	seq := ls.Seq()
	if seq == nil {
		return nil
	}
	if iseq, ok := seq.(types.Seq); ok {
		return iseq.First()
	}
	return nil
}

// Next は VMLazySeq の残りの要素を返します（Seq 実装）。
func (ls *VMLazySeq) Next() types.Seq {
	if ls == nil {
		return nil
	}
	seq := ls.Seq()
	if seq == nil {
		return nil
	}
	if iseq, ok := seq.(types.Seq); ok {
		return iseq.Next()
	}
	return nil
}

// More は VMLazySeq の残りの要素を返します（Seq 実装）。
// Next() と同じですが、空の場合は空のシーケンスを返します。
func (ls *VMLazySeq) More() types.Seq {
	if ls == nil {
		return coll.NewList()
	}
	seq := ls.Seq()
	if seq == nil {
		return coll.NewList()
	}
	if iseq, ok := seq.(types.Seq); ok {
		return iseq.More()
	}
	return coll.NewList()
}

// Realized は VMLazySeq が既に評価済みかどうかを返します。
func (ls *VMLazySeq) Realized() bool {
	if ls == nil {
		return false
	}
	return ls.realized.Load()
}
