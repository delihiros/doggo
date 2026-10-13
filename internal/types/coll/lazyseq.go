package coll

import (
	"fmt"
	"sync/atomic"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// LazySeqError は LazySeq 評価時のエラーを表す Value 型です。
// これにより、Seq インターフェースを維持しながらエラーを伝播できます。
type LazySeqError struct {
	Err error
}

// String は LazySeqError の文字列表現を返します。
func (e *LazySeqError) String() string {
	return fmt.Sprintf("LazySeqError: %v", e.Err)
}

// Type は LazySeqError の型名を返します。
func (e *LazySeqError) Type() string {
	return "lazyseq-error"
}

// Hash は LazySeqError のハッシュ値を返します。
// エラーはコレクション要素として使用されることを想定しないため、常に 0 を返します。
func (e *LazySeqError) Hash() uint32 {
	return 0
}

// Equals は他の Value との等価性を判定します。
// エラー同士の等価比較は意味がないため、常に false を返します。
func (e *LazySeqError) Equals(other types.Value) bool {
	return false
}

// IsSelfEvaluating は自己評価型かどうかを返します。
// エラーは評価時に変化しないため、true を返します。
func (e *LazySeqError) IsSelfEvaluating() bool {
	return true
}

// Cause は遅延評価に失敗した原因を返します。
func (e *LazySeqError) Cause() error {
	return e.Err
}

// LazySeq は遅延評価シーケンスを表します。
// Clojureのlazy-seqマクロによって作成されます。
type LazySeq struct {
	evalFn   func(types.Environment) (types.Value, error)
	env      types.Environment
	source   types.Seq
	once     util.RecursionDetectingOnce
	cached   types.Value
	err      error
	realized atomic.Bool
	// preservesInfinite is true only for one-to-one or finite-prefix-removing
	// transformations whose output is guaranteed infinite for an infinite input.
	preservesInfinite bool
	// knownFinite is true when the constructor bounds the output even if the
	// source itself is infinite.
	knownFinite bool
	// nonReentrant is set only by callers that can prove evalFn cannot realize
	// this sequence recursively.
	nonReentrant bool
}

// NewNonReentrantLazySeq avoids recursive-owner tracking for evaluators that
// cannot call back into the sequence being realized.
func NewNonReentrantLazySeq(evalFn func(types.Environment) (types.Value, error), env types.Environment) *LazySeq {
	seq := NewLazySeq(evalFn, env)
	seq.nonReentrant = true
	return seq
}

// NewNonReentrantLazySeqFrom is NewNonReentrantLazySeq with source provenance.
func NewNonReentrantLazySeqFrom(source types.Seq, evalFn func(types.Environment) (types.Value, error), env types.Environment) *LazySeq {
	seq := NewNonReentrantLazySeq(evalFn, env)
	seq.source = source
	return seq
}

// NewLazySeq は新しいLazySeqを作成します。
func NewLazySeq(evalFn func(types.Environment) (types.Value, error), env types.Environment) *LazySeq {
	return &LazySeq{
		evalFn: evalFn,
		env:    env,
	}
}

// NewLazySeqFrom creates a lazy transformation that retains its source.
func NewLazySeqFrom(source types.Seq, evalFn func(types.Environment) (types.Value, error), env types.Environment) *LazySeq {
	seq := NewLazySeq(evalFn, env)
	seq.source = source
	return seq
}

// NewLazySeqPreservingInfiniteFrom creates a lazy transformation that is
// guaranteed to remain infinite when its source is infinite.
func NewLazySeqPreservingInfiniteFrom(source types.Seq, evalFn func(types.Environment) (types.Value, error), env types.Environment) *LazySeq {
	seq := NewLazySeqFrom(source, evalFn, env)
	seq.preservesInfinite = true
	return seq
}

// NewFiniteLazySeqFrom creates a source-backed lazy sequence whose output has
// a finite upper bound.
func NewFiniteLazySeqFrom(source types.Seq, evalFn func(types.Environment) (types.Value, error), env types.Environment) *LazySeq {
	seq := NewLazySeqFrom(source, evalFn, env)
	seq.knownFinite = true
	return seq
}

// String はLazySeqの文字列表現を返します。
func (ls *LazySeq) String() string {
	return seqString(ls)
}

// Type はLazySeqの型名を返します。
func (ls *LazySeq) Type() string {
	return "lazy-seq"
}

func (ls *LazySeq) Hash() uint32 {
	return SeqHash(ls)
}

func (ls *LazySeq) Equals(other types.Value) bool {
	return SequentialEquals(ls, other)
}

// ensureEvaluated はLazySeqが評価されることを保証します。
func (ls *LazySeq) ensureEvaluated() error {
	evaluate := func() {
		defer ls.realized.Store(true)
		if ls.evalFn != nil {
			ls.cached, ls.err = ls.evalFn(ls.env)
		}
	}
	if ls.nonReentrant {
		return ls.once.DoWithoutRecursionCheck(evaluate)
	}
	return ls.once.Do(evaluate)
}

// Seq はLazySeqをシーケンスに変換します（Sequable実装）。
// このメソッドが呼ばれたときに初めて評価が行われます。
// エラーが発生した場合は LazySeqError を返します。
func (ls *LazySeq) Seq() types.Value {
	if ls == nil {
		return nil
	}
	if err := ls.ensureEvaluated(); err != nil {
		return &LazySeqError{Err: err}
	}
	if ls.err != nil {
		return &LazySeqError{Err: ls.err}
	}
	if ls.cached == nil {
		return nil
	}
	// キャッシュされた値がSequableの場合はSeq()を呼び出す
	if sequable, ok := ls.cached.(types.Sequable); ok {
		if _, lazy := ls.cached.(types.LazySeq); lazy {
			traversal, err := BeginLazySeqAliasEdge()
			if err != nil {
				return &LazySeqError{Err: err}
			}
			defer traversal.Close()
		}
		return sequable.Seq()
	}
	// キャッシュされた値がSeqの場合はそのまま返す
	if _, ok := ls.cached.(types.Seq); ok {
		return ls.cached
	}
	// それ以外の場合はnilを返す
	return nil
}

// First はLazySeqの最初の要素を返します（Seq実装）。
func (ls *LazySeq) First() types.Value {
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

// Next はLazySeqの残りの要素を返します（Seq実装）。
func (ls *LazySeq) Next() types.Seq {
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

// More はLazySeqの残りの要素を返します（Seq実装）。
// Next()と同じですが、空の場合は空のシーケンスを返します。
func (ls *LazySeq) More() types.Seq {
	if ls == nil {
		return NewList()
	}
	seq := ls.Seq()
	if seq == nil {
		return NewList()
	}
	if iseq, ok := seq.(types.Seq); ok {
		return iseq.More()
	}
	return NewList()
}

// Realized はLazySeqが既に評価済みかどうかを返します。
func (ls *LazySeq) Realized() bool {
	if ls == nil {
		return false
	}
	return ls.realized.Load()
}

// Source returns the sequence transformed by this lazy sequence, when known.
func (ls *LazySeq) Source() types.Seq {
	if ls == nil {
		return nil
	}
	return ls.source
}

// PreservesInfiniteLength reports whether an infinite source guarantees an
// infinite output for this transformation.
func (ls *LazySeq) PreservesInfiniteLength() bool {
	return ls != nil && ls.preservesInfinite
}

func (ls *LazySeq) KnownFinite() bool {
	return ls == nil || ls.knownFinite
}

// RealizeNode evaluates only this lazy node without traversing the sequence it returns.
func (ls *LazySeq) RealizeNode() error {
	if ls == nil {
		return nil
	}
	if err := ls.ensureEvaluated(); err != nil {
		return err
	}
	return ls.err
}

// RealizedSequence returns the sequence produced directly by this node.
func (ls *LazySeq) RealizedSequence() types.Seq {
	if ls == nil || !ls.realized.Load() {
		return nil
	}
	seq, _ := ls.cached.(types.Seq)
	return seq
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (ls *LazySeq) IsSelfEvaluating() bool {
	return false
}
