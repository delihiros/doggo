package vm

import (
	"context"

	"doggo/internal/types"
	"doggo/internal/types/coll"
)

// VMMappedSeq は VM コンテキストを持つ map シーケンスです。
// クロージャを含む任意の callable でも ReduceDirect 最適化を可能にします。
type VMMappedSeq struct {
	coll.SeqBase
	state *vmMappedSeqState
	index int
}

// コンパイル時インターフェース検証
var (
	_ types.Seq          = (*VMMappedSeq)(nil)
	_ types.LazySeq      = (*VMMappedSeq)(nil)
	_ types.ReducibleSeq = (*VMMappedSeq)(nil)
)

// NewVMMappedSeq は新しい VMMappedSeq を作成します。
func NewVMMappedSeq(ctx context.Context, vm *VM, source types.Seq, mapFn types.Value) *VMMappedSeq {
	if vm == nil {
		return newVMMappedSeq(ctx, nil, nil, source, mapFn)
	}
	return newVMMappedSeq(ctx, vm.snapshotCallableRuntime(), vm.constants, source, mapFn)
}

func newVMMappedSeq(ctx context.Context, runtime CallableRuntime, constants []types.Value, source types.Seq, mapFn types.Value) *VMMappedSeq {
	return &VMMappedSeq{state: newVMMappedSeqState(ctx, runtime, constants, source, mapFn)}
}

// Seq interface implementation

// First は最初の要素を返します。
func (vs *VMMappedSeq) First() types.Value {
	if vs == nil {
		return nil
	}
	value, ok, _ := vs.state.valueAt(vs.index)
	if !ok {
		return nil
	}
	return value
}

// Next は残りのシーケンスを返します。
func (vs *VMMappedSeq) Next() types.Seq {
	if vs == nil {
		return nil
	}
	value, ok, err := vs.state.valueAt(vs.index)
	if err != nil || !ok || value == nil || !vs.state.hasNextAfter(vs.index) {
		return nil
	}
	return &VMMappedSeq{state: vs.state, index: vs.index + 1}
}

// More は Next と同様ですが、nil の代わりに空のリストを返します。
func (vs *VMMappedSeq) More() types.Seq {
	next := vs.Next()
	if next == nil {
		return coll.NewList()
	}
	return next
}

// LazySeq interface implementation

// Realized は評価済みかどうかを返します。
func (vs *VMMappedSeq) Realized() bool {
	return vs != nil && vs.state != nil && vs.state.realizedAt(vs.index)
}

// RealizeNode computes this mapped node directly. Unlike an aliasing lazy
// sequence, VMMappedSeq never delegates Seq() to its source, so callers can
// inspect deferred errors without starting a lazy-alias traversal.
func (vs *VMMappedSeq) RealizeNode() error {
	if vs == nil || vs.state == nil {
		return nil
	}
	_, _, err := vs.state.valueAt(vs.index)
	return err
}

// Source は未評価の変換元シーケンスを返します。
func (vs *VMMappedSeq) Source() types.Seq {
	if vs == nil || vs.state == nil {
		return nil
	}
	vs.state.mu.Lock()
	defer vs.state.mu.Unlock()
	return vs.state.source
}

func (vs *VMMappedSeq) PreservesInfiniteLength() bool { return true }

// ReducibleSeq interface implementation

// ReduceDirect は最適化された reduce を実行します。
// 共有キャッシュを直接埋め、map のメモ化契約と低い割当数を両立します。
func (vs *VMMappedSeq) ReduceDirect(reduceFn types.ReduceFn, init types.Value) (types.Value, bool, error) {
	acc := init
	for index := vs.index; ; index++ {
		mapped, ok, err := vs.state.valueAt(index)
		if err != nil {
			return nil, true, err
		}
		if !ok || mapped == nil {
			break
		}
		acc = reduceFn(acc, mapped)
		if types.IsReduced(acc) {
			return types.Unreduced(acc), true, nil
		}
	}
	return acc, true, nil
}

// Value interface implementation

// String は文字列表現を返します。
func (vs *VMMappedSeq) String() string {
	return coll.SeqString(vs)
}

// Type は型名を返します。
func (vs *VMMappedSeq) Type() string {
	return "mapped-seq"
}

func (vs *VMMappedSeq) Hash() uint32 {
	return coll.SeqHash(vs)
}

// Equals は等価性を判定します。
func (vs *VMMappedSeq) Equals(other types.Value) bool {
	return coll.SequentialEquals(vs, other)
}

// Sequable interface implementation

// Seq は自身を返します。
func (vs *VMMappedSeq) Seq() types.Value {
	if vs == nil {
		return nil
	}
	value, ok, err := vs.state.valueAt(vs.index)
	if err != nil {
		return &coll.LazySeqError{Err: err}
	}
	if !ok || value == nil {
		return nil
	}
	return vs
}
