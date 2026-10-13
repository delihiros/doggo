package atom

import (
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// GoObject はGoの任意の値を包むラッパーです。
// Go呼び出し機能のために使用されます。
type GoObject struct {
	Val interface{} // Goの構造体やプリミティブへの参照
}

func (g *GoObject) String() string {
	if g.Val == nil {
		return "#<go-object:nil>"
	}
	return fmt.Sprintf("#<go-object:%T>", g.Val)
}

func (g *GoObject) Type() string {
	return "go-object"
}

func (g *GoObject) Hash() uint32 {
	if g == nil {
		return 0
	}
	return util.HashString(fmt.Sprintf("%p", g))
}

func (g *GoObject) Equals(other types.Value) bool {
	o, ok := other.(*GoObject)
	return ok && g == o
}

// Unwrap はGoの値を取り出します。
func (g *GoObject) Unwrap() interface{} {
	return g.Val
}

// NewGoObject は新しいGoObjectを作成します。
func NewGoObject(val interface{}) *GoObject {
	return &GoObject{Val: val}
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (g *GoObject) IsSelfEvaluating() bool {
	return true
}
