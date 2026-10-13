package coll

import (
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// ArrayMap はキー順序を保持するマップです。
// 小さなマップ用の線形検索実装です。
// 一定サイズを超えるとHashMapに変換されるべきですが、現在はそのまま使用します。
type ArrayMap struct {
	Entries  []types.Value // [key1, val1, key2, val2, ...] の形式
	Metadata *Map          // メタデータマップ
}

const (
	// ArrayMapMaxSize はArrayMapの最大サイズです。
	// これを超えるとHashMapに変換することを推奨します。
	ArrayMapMaxSize = 16
)

func (m *ArrayMap) String() string {
	if m == nil || len(m.Entries) == 0 {
		return "{}"
	}
	result := "{"
	for i := 0; i < len(m.Entries); i += 2 {
		if i > 0 {
			result += ", "
		}
		result += m.Entries[i].String() + " " + m.Entries[i+1].String()
	}
	result += "}"
	return result
}

func (m *ArrayMap) Type() string {
	return "array-map"
}

func (m *ArrayMap) Hash() uint32 {
	if m == nil {
		return 0
	}
	var hash uint32
	for i := 0; i < len(m.Entries); i += 2 {
		entryHash := m.Entries[i].Hash()*31 + m.Entries[i+1].Hash()
		hash += entryHash
	}
	return util.MixCollectionHash(hash, len(m.Entries)/2)
}

func (m *ArrayMap) Equals(other types.Value) bool {
	if m == nil {
		return other == nil
	}
	if other == nil {
		return false
	}
	o, ok := other.(*ArrayMap)
	if !ok {
		return false
	}
	if len(m.Entries) != len(o.Entries) {
		return false
	}
	for i := 0; i < len(m.Entries); i += 2 {
		k := m.Entries[i]
		v := m.Entries[i+1]
		idx := o.findIndex(k)
		if idx < 0 {
			return false
		}
		// Find value in o
		// ArrayMap is linear so findIndex returns index of key. Value is at index+1
		if !v.Equals(o.Entries[idx+1]) {
			return false
		}
	}
	return true
}

// NewArrayMap は新しいArrayMapを作成します。
func NewArrayMap(entries ...types.Value) (*ArrayMap, error) {
	if len(entries)%2 != 0 {
		return nil, fmt.Errorf("array-map requires even number of arguments (key-value pairs)")
	}
	return &ArrayMap{
		Entries:  entries,
		Metadata: nil,
	}, nil
}

// NewArrayMapWithMetadata はメタデータ付きのArrayMapを作成します。
func NewArrayMapWithMetadata(entries []types.Value, metadata *Map) (*ArrayMap, error) {
	if len(entries)%2 != 0 {
		return nil, fmt.Errorf("array-map requires even number of arguments (key-value pairs)")
	}
	return &ArrayMap{
		Entries:  entries,
		Metadata: metadata,
	}, nil
}

// findIndex はキーのインデックスを検索します（見つからない場合は-1）。
func (m *ArrayMap) findIndex(key types.Value) int {
	if m == nil {
		return -1
	}
	for i := 0; i < len(m.Entries); i += 2 {
		if m.Entries[i] == nil {
			if key == nil {
				return i
			}
		} else if m.Entries[i].Equals(key) {
			return i
		}
	}
	return -1
}

// Map実装

// Count はArrayMapのエントリ数を返します。
func (m *ArrayMap) Count() int {
	if m == nil {
		return 0
	}
	return len(m.Entries) / 2
}

// Conj はArrayMapにエントリを追加します。
func (m *ArrayMap) Conj(item types.Value) types.Value {
	if m == nil {
		newMap, _ := NewArrayMap()
		m = newMap
	}
	// マップの場合は、itemがMapEntryまたは[key value]のベクター、またはマップ全体
	if entry, ok := item.(*types.MapEntry); ok {
		return m.Assoc(entry.Key, entry.Value)
	}
	if vec, ok := item.(*Vector); ok && len(vec.Items) == 2 {
		return m.Assoc(vec.Items[0], vec.Items[1])
	}
	if itemMap, ok := item.(*ArrayMap); ok {
		newMap := m
		for i := 0; i < len(itemMap.Entries); i += 2 {
			newMap = newMap.Assoc(itemMap.Entries[i], itemMap.Entries[i+1]).(*ArrayMap)
		}
		return newMap
	}
	return m
}

// Seq はArrayMapをシーケンス（MapEntryのリスト）に変換します。
func (m *ArrayMap) Seq() types.Value {
	if m == nil || len(m.Entries) == 0 {
		return nil
	}
	entries := make([]types.Value, 0, len(m.Entries)/2)
	for i := 0; i < len(m.Entries); i += 2 {
		entries = append(entries, types.NewMapEntry(m.Entries[i], m.Entries[i+1]))
	}
	return NewList(entries...)
}

// Assoc はArrayMapにキーと値を追加/更新します。
func (m *ArrayMap) Assoc(key, val types.Value) types.Map {
	if m == nil {
		newMap, _ := NewArrayMap()
		m = newMap
	}
	idx := m.findIndex(key)
	if idx >= 0 {
		// 既存のキーを更新
		newEntries := make([]types.Value, len(m.Entries))
		copy(newEntries, m.Entries)
		newEntries[idx+1] = val
		result, _ := NewArrayMapWithMetadata(newEntries, m.Metadata)
		return result
	}
	// 新しいキーを追加
	newEntries := append(m.Entries, key, val)
	result, _ := NewArrayMapWithMetadata(newEntries, m.Metadata)
	return result
}

// Dissoc はArrayMapからキーを削除します。
func (m *ArrayMap) Dissoc(key types.Value) types.Map {
	if m == nil {
		result, _ := NewArrayMap()
		return result
	}
	idx := m.findIndex(key)
	if idx < 0 {
		return m
	}
	// キーと値を削除
	newEntries := make([]types.Value, 0, len(m.Entries)-2)
	newEntries = append(newEntries, m.Entries[:idx]...)
	newEntries = append(newEntries, m.Entries[idx+2:]...)
	result, _ := NewArrayMapWithMetadata(newEntries, m.Metadata)
	return result
}

// Get はArrayMapからキーで値を取得します。
func (m *ArrayMap) Get(key types.Value) types.Value {
	if m == nil {
		return nil
	}
	idx := m.findIndex(key)
	if idx < 0 {
		return nil
	}
	return m.Entries[idx+1]
}

// Find はArrayMapからキーでエントリを取得します。
func (m *ArrayMap) Find(key types.Value) *types.MapEntry {
	if m == nil {
		return nil
	}
	idx := m.findIndex(key)
	if idx < 0 {
		return nil
	}
	return types.NewMapEntry(m.Entries[idx], m.Entries[idx+1])
}

// Keys はArrayMapのキーのシーケンスを返します。
func (m *ArrayMap) Keys() types.Sequable {
	if m == nil || len(m.Entries) == 0 {
		return nil
	}
	keys := make([]types.Value, 0, len(m.Entries)/2)
	for i := 0; i < len(m.Entries); i += 2 {
		keys = append(keys, m.Entries[i])
	}
	return NewList(keys...)
}

// Vals はArrayMapの値のシーケンスを返します。
func (m *ArrayMap) Vals() types.Sequable {
	if m == nil || len(m.Entries) == 0 {
		return nil
	}
	vals := make([]types.Value, 0, len(m.Entries)/2)
	for i := 1; i < len(m.Entries); i += 2 {
		vals = append(vals, m.Entries[i])
	}
	return NewList(vals...)
}

// RSeq は逆順シーケンスを返します。
func (m *ArrayMap) RSeq() types.Sequable {
	if m == nil || len(m.Entries) == 0 {
		return nil
	}
	entries := make([]types.Value, 0, len(m.Entries)/2)
	for i := len(m.Entries) - 2; i >= 0; i -= 2 {
		entries = append(entries, types.NewMapEntry(m.Entries[i], m.Entries[i+1]))
	}
	return NewList(entries...)
}

// Invoke implements Fn interface.
func (m *ArrayMap) Invoke(args ...types.Value) (types.Value, error) {
	return invokeMapLookup("array-map", m.Get, args...)
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (m *ArrayMap) IsSelfEvaluating() bool {
	return false
}
