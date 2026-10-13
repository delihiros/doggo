package coll

import (
	"context"
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// Vector はベクター（Clojureの [1 2 3] 形式）を表します。
type Vector struct {
	Items    []types.Value
	Metadata *Map // メタデータマップ
}

func (v *Vector) String() string {
	if v == nil {
		return "[]"
	}
	return types.FormatCollection(v.Items, "[", "]", "[]")
}

func (v *Vector) Type() string {
	return "vector"
}

func (v *Vector) Hash() uint32 {
	if v == nil {
		return 0
	}
	hash := uint32(1)
	for _, item := range v.Items {
		h := uint32(0)
		if item != nil {
			h = item.Hash()
		}
		hash = hash*31 + h
	}
	return util.MixCollectionHash(hash, len(v.Items))
}

func (v *Vector) Equals(other types.Value) bool {
	if v == nil {
		return other == nil
	}
	return SequentialEquals(v, other)
}

// Invoke implements Fn interface.
func (v *Vector) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("vector invocation requires at least 1 argument (index)")
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("vector invocation expects 1 or 2 arguments (index, default?), got %d", len(args))
	}

	// インデックスを取得
	idx, ok := args[0].(types.Number)
	if !ok {
		return nil, fmt.Errorf("vector invocation expects number index, got %s", args[0].Type())
	}

	index := int(idx.ToFloat64())
	if index < 0 || index >= len(v.Items) {
		// 範囲外の場合、デフォルト値があれば返す
		if len(args) == 2 {
			return args[1], nil
		}
		return nil, nil
	}

	return v.Items[index], nil
}

// NewVector は新しいベクターを作成します。
func NewVector(items ...types.Value) *Vector {
	return &Vector{
		Items:    items,
		Metadata: nil,
	}
}

// NewVectorWithMetadata はメタデータ付きのベクターを作成します。
func NewVectorWithMetadata(items []types.Value, metadata *Map) *Vector {
	return &Vector{
		Items:    items,
		Metadata: metadata,
	}
}

// Vector実装

// Count はベクターの要素数を返します。
func (v *Vector) Count() int {
	if v == nil {
		return 0
	}
	return len(v.Items)
}

// Conj はベクターに要素を追加します（末尾に追加）。
func (v *Vector) Conj(item types.Value) types.Value {
	if v == nil {
		return NewVector(item)
	}
	return NewVector(append(v.Items, item)...)
}

// Seq はベクターをシーケンスに変換します。
func (v *Vector) Seq() types.Value {
	if v == nil || len(v.Items) == 0 {
		return nil
	}
	return NewList(v.Items...)
}

// Nth はインデックスで要素を取得します。
func (v *Vector) Nth(index int) types.Value {
	if v == nil || index < 0 || index >= len(v.Items) {
		return nil
	}
	return v.Items[index]
}

// Assoc はインデックスで要素を設定します。
func (v *Vector) Assoc(index int, val types.Value) types.Vector {
	if v == nil {
		return nil
	}
	if index < 0 || index >= len(v.Items) {
		return v
	}
	newItems := make([]types.Value, len(v.Items))
	copy(newItems, v.Items)
	newItems[index] = val
	return NewVector(newItems...)
}

// Pop は最後の要素を除いたベクターを返します。
func (v *Vector) Pop() types.Vector {
	if v == nil || len(v.Items) == 0 {
		return NewVector()
	}
	return NewVector(v.Items[:len(v.Items)-1]...)
}

// RSeq は逆順シーケンスを返します。
func (v *Vector) RSeq() types.Sequable {
	if v == nil || len(v.Items) == 0 {
		return nil
	}
	reversed := make([]types.Value, len(v.Items))
	for i := range v.Items {
		reversed[i] = v.Items[len(v.Items)-1-i]
	}
	return NewList(reversed...)
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (v *Vector) IsSelfEvaluating() bool {
	return false
}
