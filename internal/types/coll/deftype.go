package coll

import (
	"fmt"
	"strings"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// DeftypeDef はdeftype型の定義を表します。
// 複数のDeftypeInstanceで共有されます。
type DeftypeDef struct {
	Name          string        // 公開される型名
	QualifiedName string        // 名前空間を含む内部識別子
	Fields        []types.Value // フィールド名（キーワード推奨）
	Metadata      *Map          // 定義メタデータ
}

// NewDeftypeDef は新しいdeftype定義を作成します。
func NewDeftypeDef(name string, fields []types.Value) *DeftypeDef {
	return &DeftypeDef{
		Name:          name,
		QualifiedName: name,
		Fields:        fields,
		Metadata:      nil,
	}
}

func (d *DeftypeDef) identity() string {
	if d.QualifiedName != "" {
		return d.QualifiedName
	}
	return d.Name
}

func (d *DeftypeDef) String() string {
	return fmt.Sprintf("#<type-def:%s>", d.Name)
}

func (d *DeftypeDef) Type() string {
	return "type-def"
}

func (d *DeftypeDef) Hash() uint32 {
	return util.HashString(d.identity())
}

func (d *DeftypeDef) Equals(other types.Value) bool {
	if o, ok := other.(*DeftypeDef); ok {
		return d.identity() == o.identity()
	}
	return false
}

func (d *DeftypeDef) IsSelfEvaluating() bool {
	return true
}

// DeftypeInstance はdeftypeのインスタンスを表します。
// opaqueな型として振る舞い、Mapインターフェースは実装しません。
type DeftypeInstance struct {
	Def    *DeftypeDef   // 型定義
	Values []types.Value // フィールド値（定義順）
}

// NewDeftypeInstance は新しいdeftypeインスタンスを作成します。
func NewDeftypeInstance(def *DeftypeDef, values []types.Value) *DeftypeInstance {
	// 値の数をフィールド数に合わせる
	adjustedValues := make([]types.Value, len(def.Fields))
	copy(adjustedValues, values)
	return &DeftypeInstance{
		Def:    def,
		Values: adjustedValues,
	}
}

func (d *DeftypeInstance) String() string {
	if d == nil || d.Def == nil {
		return "#<type-instance>"
	}
	var parts []string
	for i, field := range d.Def.Fields {
		var valStr string
		if i < len(d.Values) && d.Values[i] != nil {
			valStr = d.Values[i].String()
		} else {
			valStr = "nil"
		}
		parts = append(parts, fmt.Sprintf("%s %s", field.String(), valStr))
	}
	return fmt.Sprintf("#<%s {%s}>", d.Def.Name, strings.Join(parts, ", "))
}

// Type は型名を返します（Protocol dispatch用）。
func (d *DeftypeInstance) Type() string {
	if d == nil || d.Def == nil {
		return "type-instance"
	}
	return d.Def.Name
}

// ProtocolTypeName は公開名と分離されたProtocol dispatch用の識別子を返します。
func (d *DeftypeInstance) ProtocolTypeName() string {
	if d == nil || d.Def == nil {
		return "type-instance"
	}
	return d.Def.identity()
}

func (d *DeftypeInstance) Hash() uint32 {
	if d == nil {
		return 0
	}
	var hash uint32 = util.HashString(d.Def.identity())
	for i, val := range d.Values {
		if val != nil {
			hash = hash*31 + uint32(i) + val.Hash()
		}
	}
	return hash
}

func (d *DeftypeInstance) Equals(other types.Value) bool {
	if d == nil {
		return other == nil
	}
	if other == nil {
		return false
	}

	o, ok := other.(*DeftypeInstance)
	if !ok {
		return false
	}

	// 同じ型定義かチェック
	if d.Def.identity() != o.Def.identity() {
		return false
	}

	// 全フィールドの値を比較
	if len(d.Values) != len(o.Values) {
		return false
	}
	for i, v := range d.Values {
		if v == nil {
			if o.Values[i] != nil {
				return false
			}
		} else if !v.Equals(o.Values[i]) {
			return false
		}
	}
	return true
}

func (d *DeftypeInstance) IsSelfEvaluating() bool {
	return true
}

// GetField はフィールド値を取得します（内部用）。
// deftypeはopaqueなので通常はProtocol経由でアクセスしますが、
// Protocol実装内でフィールドにアクセスするために使用します。
func (d *DeftypeInstance) GetField(idx int) types.Value {
	if d == nil || idx < 0 || idx >= len(d.Values) {
		return nil
	}
	return d.Values[idx]
}

// GetFieldByName はフィールド名で値を取得します（内部用）。
func (d *DeftypeInstance) GetFieldByName(name types.Value) types.Value {
	if d == nil || d.Def == nil {
		return nil
	}
	for i, field := range d.Def.Fields {
		if field.Equals(name) {
			if i < len(d.Values) {
				return d.Values[i]
			}
			return nil
		}
	}
	return nil
}
