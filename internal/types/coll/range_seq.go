package coll

import (
	"fmt"

	"doggo/internal/types"
)

// RangeInt は range シーケンス用の整数型です。
// atom パッケージへの依存を避けるために coll パッケージ内で定義。
type RangeInt int64

func (n RangeInt) String() string         { return fmt.Sprintf("%d", int64(n)) }
func (n RangeInt) Type() string           { return "number" }
func (n RangeInt) Hash() uint32           { return types.HashNumber(n) }
func (n RangeInt) IsSelfEvaluating() bool { return true }
func (n RangeInt) ToFloat64() float64     { return float64(n) }
func (n RangeInt) Int64Value() (int64, bool) {
	return int64(n), true
}
func (n RangeInt) IsZero() bool { return n == 0 }
func (n RangeInt) Equals(other types.Value) bool {
	o, ok := other.(types.Number)
	if !ok {
		return false
	}
	cmp, ordered := types.CompareNumbers(n, o)
	return ordered && cmp == 0
}

// RangeLazySeq は range の最適化された遅延シーケンス実装です。
// クロージャを使わず、start/end/step を直接保持することで、
// first/next の呼び出しを高速化します。
type RangeLazySeq struct {
	start   int64 // 開始値
	end     int64 // 終了値（含まない）
	step    int64 // ステップ
	current int64 // 現在値
}

// コンパイル時インターフェース検証
var (
	_ types.Seq          = (*RangeLazySeq)(nil)
	_ types.LazySeq      = (*RangeLazySeq)(nil)
	_ types.ReducibleSeq = (*RangeLazySeq)(nil)
)

// NewRangeLazySeq は新しい RangeLazySeq を作成します。
func NewRangeLazySeq(start, end, step int64) *RangeLazySeq {
	return &RangeLazySeq{
		start:   start,
		end:     end,
		step:    step,
		current: start,
	}
}

// NewRangeLazySeqFromCurrent は現在値を指定して RangeLazySeq を作成します。
func NewRangeLazySeqFromCurrent(current, end, step int64) *RangeLazySeq {
	return &RangeLazySeq{
		start:   current,
		end:     end,
		step:    step,
		current: current,
	}
}

// isEmpty はシーケンスが空かどうかを判定します。
func (r *RangeLazySeq) isEmpty() bool {
	if r.step > 0 {
		return r.current >= r.end
	}
	return r.current <= r.end
}

func (r *RangeLazySeq) nextInt(current int64) (int64, bool) {
	next := current + r.step
	if r.step > 0 {
		return next, next > current && next < r.end
	}
	return next, r.step < 0 && next < current && next > r.end
}

// First は最初の要素を返します。
func (r *RangeLazySeq) First() types.Value {
	if r.isEmpty() {
		return nil
	}
	return RangeInt(r.current)
}

// Rest は最初の要素を除いた残りのシーケンスを返します。
func (r *RangeLazySeq) Rest() types.Seq {
	if r.isEmpty() {
		return NewList()
	}
	next, ok := r.nextInt(r.current)
	if !ok {
		return NewList()
	}
	return NewRangeLazySeqFromCurrent(next, r.end, r.step)
}

// Next は Rest と同様ですが、空の場合は nil を返します。
func (r *RangeLazySeq) Next() types.Seq {
	if r.isEmpty() {
		return nil
	}
	next, ok := r.nextInt(r.current)
	if !ok {
		return nil
	}
	return NewRangeLazySeqFromCurrent(next, r.end, r.step)
}

// More は Rest と同じですが、nil の代わりに空のリストを返します。
func (r *RangeLazySeq) More() types.Seq {
	return r.Rest()
}

// Cons は先頭に要素を追加した新しいシーケンスを返します。
func (r *RangeLazySeq) Cons(val types.Value) types.Seq {
	// Cons は RangeLazySeq を維持できないので、通常の List に変換
	items := []types.Value{val}
	for !r.isEmpty() {
		items = append(items, r.First())
		next := r.Next()
		if next == nil {
			break
		}
		r = next.(*RangeLazySeq)
	}
	return NewList(items...)
}

// Value インターフェース

// String は文字列表現を返します。
func (r *RangeLazySeq) String() string {
	if r.isEmpty() {
		return "()"
	}
	// 最大10要素まで表示
	result := "("
	count := 0
	current := r.current
	hasMore := true
	for count < 10 && hasMore {
		if count > 0 {
			result += " "
		}
		result += fmt.Sprintf("%d", current)
		current, hasMore = r.nextInt(current)
		count++
	}
	if hasMore {
		result += " ..."
	}
	result += ")"
	return result
}

// Type は型名を返します。
func (r *RangeLazySeq) Type() string {
	return "range-seq"
}

// Hash はハッシュ値を返します。
func (r *RangeLazySeq) Hash() uint32 {
	return SeqHash(r)
}

// Equals は等価性を判定します。
func (r *RangeLazySeq) Equals(other types.Value) bool {
	return SequentialEquals(r, other)
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (r *RangeLazySeq) IsSelfEvaluating() bool {
	return false
}

// Sequable インターフェース

// Seq は自身を返します。
func (r *RangeLazySeq) Seq() types.Value {
	if r.isEmpty() {
		return nil
	}
	return r
}

// GetParams は range パラメータを返します（フュージョン最適化用）。
func (r *RangeLazySeq) GetParams() (start, end, step int64) {
	return r.current, r.end, r.step
}

// Count は要素数を返します。
func (r *RangeLazySeq) Count() int {
	if r.isEmpty() {
		return 0
	}
	var distance, stride uint64
	if r.step > 0 {
		distance = uint64(r.end) - uint64(r.current)
		stride = uint64(r.step)
	} else {
		distance = uint64(r.current) - uint64(r.end)
		stride = uint64(0) - uint64(r.step)
	}
	count := distance / stride
	if distance%stride != 0 {
		count++
	}
	if count > uint64(^uint(0)>>1) {
		return -1
	}
	return int(count)
}

// LazySeq インターフェース

// Realized は評価済みかどうかを返します。
// RangeLazySeq は常に評価済みとみなします（遅延評価の thunk がない）。
func (r *RangeLazySeq) Realized() bool {
	return true
}

// ReducibleSeq インターフェース

// ReduceDirect は最適化された reduce を実行します。
func (r *RangeLazySeq) ReduceDirect(fn types.ReduceFn, init types.Value) (types.Value, bool, error) {
	acc := init
	if r.isEmpty() {
		return acc, true, nil
	}
	for current := r.current; ; {
		acc = fn(acc, RangeInt(current))
		if types.IsReduced(acc) {
			return types.Unreduced(acc), true, nil
		}
		next, ok := r.nextInt(current)
		if !ok {
			break
		}
		current = next
	}
	return acc, true, nil
}

// consSeq は cons で生成されるシーケンスです。
type consSeq struct {
	first types.Value
	rest  types.Seq
}

func NewConsSeq(first types.Value, rest types.Seq) types.Seq {
	return &consSeq{first: first, rest: rest}
}

func (c *consSeq) First() types.Value { return c.first }
func (c *consSeq) Rest() types.Seq {
	if next := c.Next(); next != nil {
		return next
	}
	return NewList()
}
func (c *consSeq) Next() types.Seq {
	if c.rest == nil {
		return nil
	}
	if sequable, ok := c.rest.(types.Sequable); ok && sequable.Seq() == nil {
		return nil
	}
	return c.rest
}
func (c *consSeq) More() types.Seq                { return c.Rest() }
func (c *consSeq) Cons(val types.Value) types.Seq { return &consSeq{first: val, rest: c} }
func (c *consSeq) String() string                 { return seqString(c) }
func (c *consSeq) Type() string                   { return "cons-seq" }
func (c *consSeq) Hash() uint32                   { return SeqHash(c) }
func (c *consSeq) Equals(other types.Value) bool {
	return SequentialEquals(c, other)
}
func (c *consSeq) Source() types.Seq             { return c.rest }
func (c *consSeq) PreservesInfiniteLength() bool { return true }
func (c *consSeq) IsSelfEvaluating() bool        { return false }
func (c *consSeq) Seq() types.Value {
	if c.first == nil {
		return nil
	}
	return c
}

// InfiniteRangeLazySeq は 0 から無限の遅延シーケンスです。
// (range) で生成されます。
type InfiniteRangeLazySeq struct {
	current int64
	step    int64
}

// コンパイル時インターフェース検証
var (
	_ types.Seq     = (*InfiniteRangeLazySeq)(nil)
	_ types.LazySeq = (*InfiniteRangeLazySeq)(nil)
)

// NewInfiniteRangeLazySeq は新しい InfiniteRangeLazySeq を作成します。
func NewInfiniteRangeLazySeq() *InfiniteRangeLazySeq {
	return &InfiniteRangeLazySeq{current: 0, step: 1}
}

// newInfiniteRangeLazySeqFrom は指定された値から始まる InfiniteRangeLazySeq を作成します。
func newInfiniteRangeLazySeqFrom(current, step int64) *InfiniteRangeLazySeq {
	return &InfiniteRangeLazySeq{current: current, step: step}
}

// First は最初の要素を返します。
func (r *InfiniteRangeLazySeq) First() types.Value {
	return RangeInt(r.current)
}

// Rest は最初の要素を除いた残りのシーケンスを返します。
func (r *InfiniteRangeLazySeq) Rest() types.Seq {
	return newInfiniteRangeLazySeqFrom(r.current+r.step, r.step)
}

// Next は Rest と同様ですが、空の場合は nil を返します（無限なので常に non-nil）。
func (r *InfiniteRangeLazySeq) Next() types.Seq {
	return newInfiniteRangeLazySeqFrom(r.current+r.step, r.step)
}

// More は Rest と同じです。
func (r *InfiniteRangeLazySeq) More() types.Seq {
	return r.Rest()
}

// Cons は先頭に要素を追加した新しいシーケンスを返します。
func (r *InfiniteRangeLazySeq) Cons(val types.Value) types.Seq {
	// 無限シーケンスに cons は実際には必要ない場面が多い
	// 簡易実装: 先頭要素 + 元のシーケンスを保持する consSeq を返す
	return &consSeq{first: val, rest: r}
}

// String は文字列表現を返します。
func (r *InfiniteRangeLazySeq) String() string {
	// 最大10要素まで表示
	result := "("
	current := r.current
	for i := 0; i < 10; i++ {
		if i > 0 {
			result += " "
		}
		result += RangeInt(current).String()
		current += r.step
	}
	result += " ...)"
	return result
}

// Type は型名を返します。
func (r *InfiniteRangeLazySeq) Type() string {
	return "infinite-range-seq"
}

// Hash はハッシュ値を返します。
func (r *InfiniteRangeLazySeq) Hash() uint32 {
	return uint32(r.current*31 + r.step)
}

// Equals は等価性を判定します。
func (r *InfiniteRangeLazySeq) Equals(other types.Value) bool {
	if o, ok := other.(*InfiniteRangeLazySeq); ok {
		return r.current == o.current && r.step == o.step
	}
	return false
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (r *InfiniteRangeLazySeq) IsSelfEvaluating() bool {
	return false
}

// Seq は自身を返します。
func (r *InfiniteRangeLazySeq) Seq() types.Value {
	return r
}

// Count は要素数を返します（無限なので -1 を返す）。
func (r *InfiniteRangeLazySeq) Count() int {
	return -1 // 無限
}

// Realized は評価済みかどうかを返します。
func (r *InfiniteRangeLazySeq) Realized() bool {
	return true
}
