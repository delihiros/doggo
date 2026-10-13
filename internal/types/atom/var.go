package atom

import (
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/coll"
	"doggo/internal/types/util"
)

// Var はグローバル変数を表します。
// Clojureのdefで作成される変数です。
type Var struct {
	Name      types.Symbol // 変数名
	Value     types.Value  // ルートバインディングの値
	Metadata  *coll.Map    // メタデータマップ
	Namespace types.Symbol // 名前空間（現時点では簡略化）
}

func (v *Var) String() string {
	if v == nil {
		return "#<var:nil>"
	}
	return fmt.Sprintf("#<var:%s>", v.Name)
}

func (v *Var) Type() string {
	return "var"
}

func (v *Var) Hash() uint32 {
	if v == nil {
		return 0
	}
	return util.HashString(fmt.Sprintf("%p", v))
}

func (v *Var) Equals(other types.Value) bool {
	if v == nil {
		return other == nil
	}
	if other == nil {
		return false
	}
	o, ok := other.(*Var)
	if !ok {
		return false
	}
	return v == o
}

// NewVar は新しいVarを作成します。
func NewVar(name types.Symbol, value types.Value) *Var {
	return &Var{
		Name:      name,
		Value:     value,
		Metadata:  coll.NewMap(map[types.Value]types.Value{}),
		Namespace: types.Symbol("user"), // デフォルト名前空間
	}
}

// GetMetadata はメタデータの値を取得します。
func (v *Var) GetMetadata(key types.Value) (types.Value, bool) {
	if v == nil || v.Metadata == nil {
		return nil, false
	}
	val := v.Metadata.Get(key)
	return val, val != nil
}

// SetMetadata はメタデータを設定します。
func (v *Var) SetMetadata(key, value types.Value) {
	if v == nil {
		return
	}
	if v.Metadata == nil {
		v.Metadata = coll.NewEmptyMap()
	}
	v.Metadata = v.Metadata.Assoc(key, value).(*coll.Map)
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (v *Var) IsSelfEvaluating() bool {
	return false
}
