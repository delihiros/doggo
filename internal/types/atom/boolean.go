package atom

import (
	"doggo/internal/types"
)

// Boolean は真偽値を表します。
// Clojureのtrueとfalseを表現します。
type Boolean bool

func (b Boolean) String() string {
	if b {
		return "true"
	}
	return "false"
}

func (b Boolean) Type() string {
	return "boolean"
}

func (b Boolean) Hash() uint32 {
	if b {
		return 1
	}
	return 0
}

func (b Boolean) Equals(other types.Value) bool {
	if other == nil {
		return false
	}
	if o, ok := other.(Boolean); ok {
		return b == o
	}
	return false
}

// NewBoolean は新しいBooleanを作成します。
func NewBoolean(val bool) Boolean {
	return Boolean(val)
}

// True はtrueのBoolean値を返します。
var True Boolean = Boolean(true)

// False はfalseのBoolean値を返します。
var False Boolean = Boolean(false)

// IsSelfEvaluating は自己評価型かどうかを返します。
func (b Boolean) IsSelfEvaluating() bool {
	return true
}

// NilValue はnilの値を返します。
var NilValue = types.NilValue
