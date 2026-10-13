package vm

import (
	"fmt"
	"unsafe"

	"doggo/internal/types"
)

// Cell は変更可能な参照を表す内部型です。
// letfn の相互再帰ローカル関数を実現するために使用します。
// ユーザーAPIとしては公開せず、VMの内部実装としてのみ使用します。
type Cell struct {
	Value types.Value
}

// コンパイル時インターフェース検証
var _ types.Value = (*Cell)(nil)

// NewCell は新しいCellを作成します。
func NewCell(val types.Value) *Cell {
	return &Cell{Value: val}
}

// String はCellの文字列表現を返します（デバッグ用）。
func (c *Cell) String() string {
	if c.Value == nil {
		return "#<Cell: nil>"
	}
	return fmt.Sprintf("#<Cell: %s>", c.Value.String())
}

// Type は型名を返します。
func (c *Cell) Type() string {
	return "cell"
}

// Hash はハッシュ値を返します。
func (c *Cell) Hash() uint32 {
	// セルは同一性で比較するため、ポインタベースの簡易ハッシュ
	return uint32(uintptr(unsafe.Pointer(c)) & 0xFFFFFFFF)
}

// Equals は等価性を判定します（参照等価）。
func (c *Cell) Equals(other types.Value) bool {
	return c == other
}

// IsSelfEvaluating はセルが自己評価型かを返します。
func (c *Cell) IsSelfEvaluating() bool {
	return false
}
