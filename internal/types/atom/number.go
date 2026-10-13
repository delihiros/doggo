package atom

import (
	"fmt"

	"doggo/internal/types"
)

// Number は数値を表します。
// int64とfloat64の両方をサポートします。
// NOTE: 現在の実装ではポインタを使用していますが、将来的にはより簡潔な設計
// （例：常にfloat64を使用し、必要に応じて整数として扱う）を検討する余地があります。
type Number struct {
	IntVal   *int64
	FloatVal *float64
}

func (n *Number) String() string {
	if n.IntVal != nil {
		return fmt.Sprintf("%d", *n.IntVal)
	}
	if n.FloatVal != nil {
		return fmt.Sprintf("%g", *n.FloatVal)
	}
	return "0"
}

func (n *Number) Type() string {
	return "number"
}

func (n *Number) Hash() uint32 {
	if n == nil {
		return 0
	}
	return types.HashNumber(n)
}

func (n *Number) Equals(other types.Value) bool {
	if n == nil {
		return other == nil
	}
	if other == nil {
		return false
	}
	if num, ok := other.(types.Number); ok {
		cmp, ordered := types.CompareNumbers(n, num)
		return ordered && cmp == 0
	}
	return false
}

// NewNumberInt は整数の数値を作成します。
func NewNumberInt(val int64) *Number {
	return &Number{IntVal: &val}
}

// NewNumberFloat は浮動小数点数の数値を作成します。
func NewNumberFloat(val float64) *Number {
	return &Number{FloatVal: &val}
}

// ToFloat64 はNumberをfloat64に変換します。
func (n *Number) ToFloat64() float64 {
	if n == nil {
		return 0
	}
	if n.FloatVal != nil {
		return *n.FloatVal
	}
	if n.IntVal != nil {
		return float64(*n.IntVal)
	}
	return 0
}

// IsZero はNumberがゼロかどうかを判定します。
func (n *Number) IsZero() bool {
	if n == nil {
		return true
	}
	if n.IntVal != nil {
		return *n.IntVal == 0
	}
	if n.FloatVal != nil {
		return *n.FloatVal == 0
	}
	return true
}

// IsInt はNumberが整数かどうかを判定します。
func (n *Number) IsInt() bool {
	if n == nil {
		return false
	}
	return n.IntVal != nil
}

// Int64Value は Number が正確な int64 のときその値を返します。
func (n *Number) Int64Value() (int64, bool) {
	if n == nil || n.IntVal == nil {
		return 0, false
	}
	return *n.IntVal, true
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (n *Number) IsSelfEvaluating() bool {
	return true
}
