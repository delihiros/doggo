package coll

import (
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// StructDef は構造体定義を表します。
// 複数のStructMapインスタンスで共有されます。
type StructDef struct {
	keys []types.Value // 固定キーセット
}

func (d *StructDef) String() string {
	return "#<struct-def>"
}

func (d *StructDef) Type() string {
	return "struct-def"
}

func (d *StructDef) Hash() uint32 {
	if d == nil {
		return 0
	}
	hash := uint32(1)
	for _, key := range d.keys {
		hash = hash*31 + key.Hash()
	}
	return util.MixCollectionHash(hash, len(d.keys))
}

func (d *StructDef) Equals(other types.Value) bool {
	if d == nil {
		return other == nil
	}
	if other == nil {
		return false
	}
	o, ok := other.(*StructDef)
	if !ok {
		return false
	}
	if len(d.keys) != len(o.keys) {
		return false
	}
	for i, key := range d.keys {
		if !key.Equals(o.keys[i]) {
			return false
		}
	}
	return true
}

// NewStructDef は新しい構造体定義を作成します。
func NewStructDef(keys []types.Value) *StructDef {
	return &StructDef{keys: keys}
}

// Keys は構造体定義のキーを返します。
func (d *StructDef) Keys() []types.Value {
	if d == nil {
		return nil
	}
	return d.keys
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (d *StructDef) IsSelfEvaluating() bool {
	return false
}

// StructMap は固定キーセットを持つマップです。
// キー情報を共有することで効率化を図ります。
type StructMap struct {
	StructDef *StructDef
	Values    []types.Value // キーの順序に対応する値のスライス
	Metadata  *Map          // メタデータマップ
}

func (m *StructMap) String() string {
	if m == nil || len(m.Values) == 0 {
		return "{}"
	}
	result := "{"
	for i, key := range m.StructDef.keys {
		if i > 0 {
			result += ", "
		}
		if i < len(m.Values) {
			result += key.String() + " " + m.Values[i].String()
		}
	}
	result += "}"
	return result
}

func (m *StructMap) Type() string {
	return "struct-map"
}

func (m *StructMap) Hash() uint32 {
	if m == nil {
		return 0
	}
	var hash uint32
	for i, key := range m.StructDef.keys {
		if i < len(m.Values) {
			entryHash := key.Hash()*31 + m.Values[i].Hash()
			hash += entryHash
		}
	}
	return util.MixCollectionHash(hash, len(m.Values))
}

func (m *StructMap) Equals(other types.Value) bool {
	if m == nil {
		return other == nil
	}
	if other == nil {
		return false
	}
	o, ok := other.(*StructMap)
	if !ok {
		return false
	}
	if len(m.Values) != len(o.Values) {
		return false
	}
	// Ensure same StructDef or equivalent?
	// For optimization, if StructDef is same pointer, we can just compare values.
	// If different StructDef, we must compare keys too.
	if m.StructDef == o.StructDef {
		for i, v := range m.Values {
			if !v.Equals(o.Values[i]) {
				return false
			}
		}
		return true
	}
	// Different definitions: check if keys match
	// Map equality is unordered, but StructMap logic relies on definitions.
	// If we want to support equality between different StructDefs but same content:
	// Use generic map equality?
	// For now, matching strict structure equality if StructDefs differ?
	// Or just generic key/value check.
	// Let's use generic key/value check against 'o' as StructMap
	for i, key := range m.StructDef.keys {
		if i >= len(m.Values) {
			break
		}
		v := m.Values[i]
		ov := o.Get(key)
		if ov == nil {
			// check if key exists in o
			if o.findKeyIndex(key) < 0 {
				return false
			}
			if v != nil {
				return false
			} // v is not nil but ov is nil (impossible if Get returns nil for missing key)
			// if v is nil (Go nil), and o.Get returns nil.
			// We need to distinguish "value is nil" from "key missing".
			// StructMap stores values. nil (Go nil) can be a value?
			// If m.Values[i] is nil, then v is nil.
			// o.Get returns nil.
		}
		if v == nil {
			if ov != nil {
				return false
			}
		} else {
			if !v.Equals(ov) {
				return false
			}
		}
	}
	return true
}

// NewStructMap は新しいStructMapを作成します。
func NewStructMap(structDef *StructDef, values []types.Value) (*StructMap, error) {
	if len(values) > len(structDef.keys) {
		return nil, fmt.Errorf("struct-map: too many values")
	}
	// 値の数をキーの数に合わせる（不足分はnilで埋める）
	adjustedValues := make([]types.Value, len(structDef.keys))
	copy(adjustedValues, values)
	return &StructMap{
		StructDef: structDef,
		Values:    adjustedValues,
		Metadata:  nil,
	}, nil
}

// NewStructMapWithMetadata はメタデータ付きのStructMapを作成します。
func NewStructMapWithMetadata(structDef *StructDef, values []types.Value, metadata *Map) (*StructMap, error) {
	if len(values) > len(structDef.keys) {
		return nil, fmt.Errorf("struct-map: too many values")
	}
	adjustedValues := make([]types.Value, len(structDef.keys))
	copy(adjustedValues, values)
	return &StructMap{
		StructDef: structDef,
		Values:    adjustedValues,
		Metadata:  metadata,
	}, nil
}

// findKeyIndex はキーのインデックスを検索します（見つからない場合は-1）。
func (m *StructMap) findKeyIndex(key types.Value) int {
	if m == nil || m.StructDef == nil {
		return -1
	}
	for i, k := range m.StructDef.keys {
		if k == key {
			return i
		}
	}
	return -1
}

// Map実装

// Count はStructMapのエントリ数を返します。
func (m *StructMap) Count() int {
	if m == nil {
		return 0
	}
	return len(m.StructDef.keys)
}

// Conj はStructMapにエントリを追加します。
// ベースキー以外のキーは追加できますが、ベースキーは削除できません。
func (m *StructMap) Conj(item types.Value) types.Value {
	if m == nil {
		return nil
	}
	// マップの場合は、itemがMapEntryまたは[key value]のベクター、またはマップ全体
	if entry, ok := item.(*types.MapEntry); ok {
		return m.Assoc(entry.Key, entry.Value)
	}
	if vec, ok := item.(*Vector); ok && len(vec.Items) == 2 {
		return m.Assoc(vec.Items[0], vec.Items[1])
	}
	if itemMap, ok := item.(*StructMap); ok {
		newMap := m
		for i, key := range itemMap.StructDef.keys {
			if i < len(itemMap.Values) {
				newMap = newMap.Assoc(key, itemMap.Values[i]).(*StructMap)
			}
		}
		return newMap
	}
	return m
}

// Seq はStructMapをシーケンス（MapEntryのリスト）に変換します。
func (m *StructMap) Seq() types.Value {
	if m == nil || len(m.StructDef.keys) == 0 {
		return nil
	}
	entries := make([]types.Value, 0, len(m.StructDef.keys))
	for i, key := range m.StructDef.keys {
		if i < len(m.Values) {
			entries = append(entries, types.NewMapEntry(key, m.Values[i]))
		}
	}
	return NewList(entries...)
}

// Assoc はStructMapにキーと値を追加/更新します。
// ベースキー以外のキーも追加できます。
func (m *StructMap) Assoc(key, val types.Value) types.Map {
	if m == nil {
		return nil
	}
	idx := m.findKeyIndex(key)
	if idx >= 0 {
		// 既存のキーを更新
		newValues := make([]types.Value, len(m.Values))
		copy(newValues, m.Values)
		newValues[idx] = val
		result, _ := NewStructMapWithMetadata(m.StructDef, newValues, m.Metadata)
		return result
	}
	// ベースキー以外のキーは追加できない（StructMapの制限）
	// 通常のMapに変換する必要があるが、ここでは単に無視する
	return m
}

// Dissoc はStructMapからキーを削除します。
// ベースキーは削除できません（Clojure仕様）。
func (m *StructMap) Dissoc(key types.Value) types.Map {
	if m == nil {
		return nil
	}
	idx := m.findKeyIndex(key)
	if idx < 0 {
		// ベースキーでない場合は削除できない
		return m
	}
	// ベースキーは削除できない（Clojure仕様）
	return m
}

// Get はStructMapからキーで値を取得します。
func (m *StructMap) Get(key types.Value) types.Value {
	if m == nil {
		return nil
	}
	idx := m.findKeyIndex(key)
	if idx < 0 || idx >= len(m.Values) {
		return nil
	}
	return m.Values[idx]
}

// Find はStructMapからキーでエントリを取得します。
func (m *StructMap) Find(key types.Value) *types.MapEntry {
	if m == nil {
		return nil
	}
	idx := m.findKeyIndex(key)
	if idx < 0 || idx >= len(m.Values) {
		return nil
	}
	return types.NewMapEntry(m.StructDef.keys[idx], m.Values[idx])
}

// Keys はStructMapのキーのシーケンスを返します。
func (m *StructMap) Keys() types.Sequable {
	if m == nil || len(m.StructDef.keys) == 0 {
		return nil
	}
	keys := make([]types.Value, len(m.StructDef.keys))
	copy(keys, m.StructDef.keys)
	return NewList(keys...)
}

// Vals はStructMapの値のシーケンスを返します。
func (m *StructMap) Vals() types.Sequable {
	if m == nil || len(m.Values) == 0 {
		return nil
	}
	vals := make([]types.Value, len(m.Values))
	copy(vals, m.Values)
	return NewList(vals...)
}

// RSeq は逆順シーケンスを返します。
func (m *StructMap) RSeq() types.Sequable {
	if m == nil || len(m.StructDef.keys) == 0 {
		return nil
	}
	entries := make([]types.Value, 0, len(m.StructDef.keys))
	for i := len(m.StructDef.keys) - 1; i >= 0; i-- {
		if i < len(m.Values) {
			entries = append(entries, types.NewMapEntry(m.StructDef.keys[i], m.Values[i]))
		}
	}
	return NewList(entries...)
}

// Invoke implements Fn interface.
func (m *StructMap) Invoke(args ...types.Value) (types.Value, error) {
	return invokeMapLookup("struct-map", m.Get, args...)
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (m *StructMap) IsSelfEvaluating() bool {
	return false
}
