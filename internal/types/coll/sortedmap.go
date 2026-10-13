package coll

import (
	"fmt"
	"sort"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// Comparator は2つの値を比較する関数です。
// a < b の場合は負の値、a == b の場合は0、a > b の場合は正の値を返します。
type Comparator func(a, b types.Value) int

// defaultComparator はデフォルトの比較関数です。
// 値の文字列表現を比較します。
func defaultComparator(a, b types.Value) int {
	aStr := a.String()
	bStr := b.String()
	if aStr < bStr {
		return -1
	}
	if aStr > bStr {
		return 1
	}
	return 0
}

// SortedMap はソートされたマップです。
type SortedMap struct {
	Entries    []*types.MapEntry // ソートされたエントリのスライス
	Comparator Comparator        // 比較関数
	Metadata   *Map              // メタデータマップ
}

func (m *SortedMap) String() string {
	if m == nil || len(m.Entries) == 0 {
		return "{}"
	}
	result := "{"
	for i, entry := range m.Entries {
		if i > 0 {
			result += ", "
		}
		result += entry.Key.String() + " " + entry.Value.String()
	}
	result += "}"
	return result
}

func (m *SortedMap) Type() string {
	return "sorted-map"
}

func (m *SortedMap) Hash() uint32 {
	if m == nil {
		return 0
	}
	var hash uint32
	for _, entry := range m.Entries {
		entryHash := entry.Key.Hash()*31 + entry.Value.Hash()
		hash += entryHash
	}
	return util.MixCollectionHash(hash, len(m.Entries))
}

func (m *SortedMap) Equals(other types.Value) bool {
	if m == nil {
		return other == nil
	}
	if other == nil {
		return false
	}
	o, ok := other.(*SortedMap)
	if !ok {
		return false
	}
	if len(m.Entries) != len(o.Entries) {
		return false
	}
	// Since both are sorted maps, entries should be in order?
	// Not necessarily same order if comparators differ.
	// But standard Map equality is set of entries.
	// We'll check existence of keys.
	for _, entry := range m.Entries {
		idx := o.findEntryIndex(entry.Key)
		if idx < 0 {
			return false
		}
		// findEntryIndex uses o.Comparator.
		// If comparators are different, findEntryIndex might fail if ordering logic differs.
		// Ideally we should check comparators equality or fallback to scan.
		// For now assuming compatible comparators or reliance on key lookup.
		ov := o.Entries[idx].Value
		if !entry.Value.Equals(ov) {
			return false
		}
	}
	return true
}

// NewSortedMap は新しいSortedMapを作成します（デフォルトの比較関数を使用）。
func NewSortedMap(entries ...types.Value) (*SortedMap, error) {
	return newSortedMapWithComparator(defaultComparator, entries...)
}

// NewSortedMapBy は比較関数を指定してSortedMapを作成します。
func NewSortedMapBy(comparator Comparator, entries ...types.Value) (*SortedMap, error) {
	return newSortedMapWithComparator(comparator, entries...)
}

func newSortedMapWithComparator(comparator Comparator, entries ...types.Value) (*SortedMap, error) {
	if len(entries)%2 != 0 {
		return nil, fmt.Errorf("sorted-map requires even number of arguments (key-value pairs)")
	}
	mapEntries := make([]*types.MapEntry, 0, len(entries)/2)
	for i := 0; i < len(entries); i += 2 {
		mapEntries = append(mapEntries, types.NewMapEntry(entries[i], entries[i+1]))
	}
	sort.Slice(mapEntries, func(i, j int) bool {
		return comparator(mapEntries[i].Key, mapEntries[j].Key) < 0
	})
	return &SortedMap{
		Entries:    mapEntries,
		Comparator: comparator,
		Metadata:   nil,
	}, nil
}

// NewSortedMapWithMetadata はメタデータ付きのSortedMapを作成します。
func NewSortedMapWithMetadata(entries []*types.MapEntry, comparator Comparator, metadata *Map) *SortedMap {
	// エントリをコピーしてソート
	sortedEntries := make([]*types.MapEntry, len(entries))
	copy(sortedEntries, entries)
	sort.Slice(sortedEntries, func(i, j int) bool {
		return comparator(sortedEntries[i].Key, sortedEntries[j].Key) < 0
	})
	return &SortedMap{
		Entries:    sortedEntries,
		Comparator: comparator,
		Metadata:   metadata,
	}
}

// findEntryIndex はキーのインデックスを検索します（見つからない場合は-1）。
func (m *SortedMap) findEntryIndex(key types.Value) int {
	if m == nil {
		return -1
	}
	// バイナリサーチ
	idx := sort.Search(len(m.Entries), func(i int) bool {
		return m.Comparator(m.Entries[i].Key, key) >= 0
	})
	if idx < len(m.Entries) && m.Comparator(m.Entries[idx].Key, key) == 0 {
		return idx
	}
	return -1
}

// Map実装

// Count はSortedMapのエントリ数を返します。
func (m *SortedMap) Count() int {
	if m == nil {
		return 0
	}
	return len(m.Entries)
}

// Conj はSortedMapにエントリを追加します。
func (m *SortedMap) Conj(item types.Value) types.Value {
	if m == nil {
		newMap, _ := NewSortedMap()
		m = newMap
	}
	// マップの場合は、itemがMapEntryまたは[key value]のベクター、またはマップ全体
	if entry, ok := item.(*types.MapEntry); ok {
		return m.Assoc(entry.Key, entry.Value)
	}
	if vec, ok := item.(*Vector); ok && len(vec.Items) == 2 {
		return m.Assoc(vec.Items[0], vec.Items[1])
	}
	if itemMap, ok := item.(*SortedMap); ok {
		newMap := m
		for _, entry := range itemMap.Entries {
			newMap = newMap.Assoc(entry.Key, entry.Value).(*SortedMap)
		}
		return newMap
	}
	return m
}

// Seq はSortedMapをシーケンス（MapEntryのリスト）に変換します。
func (m *SortedMap) Seq() types.Value {
	if m == nil || len(m.Entries) == 0 {
		return nil
	}
	entries := make([]types.Value, 0, len(m.Entries))
	for _, entry := range m.Entries {
		entries = append(entries, entry)
	}
	return NewList(entries...)
}

// Assoc はSortedMapにキーと値を追加/更新します。
func (m *SortedMap) Assoc(key, val types.Value) types.Map {
	if m == nil {
		result, _ := NewSortedMapBy(defaultComparator, key, val)
		return result
	}
	idx := m.findEntryIndex(key)
	if idx >= 0 {
		// 既存のキーを更新
		newEntries := make([]*types.MapEntry, len(m.Entries))
		copy(newEntries, m.Entries)
		newEntries[idx] = types.NewMapEntry(key, val)
		return NewSortedMapWithMetadata(newEntries, m.Comparator, m.Metadata)
	}
	// 新しいキーを追加（ソート順を維持）
	newEntries := make([]*types.MapEntry, 0, len(m.Entries)+1)
	inserted := false
	for _, entry := range m.Entries {
		if !inserted && m.Comparator(entry.Key, key) > 0 {
			newEntries = append(newEntries, types.NewMapEntry(key, val))
			inserted = true
		}
		newEntries = append(newEntries, entry)
	}
	if !inserted {
		newEntries = append(newEntries, types.NewMapEntry(key, val))
	}
	return NewSortedMapWithMetadata(newEntries, m.Comparator, m.Metadata)
}

// Dissoc はSortedMapからキーを削除します。
func (m *SortedMap) Dissoc(key types.Value) types.Map {
	if m == nil {
		result, _ := NewSortedMapBy(defaultComparator)
		return result
	}
	idx := m.findEntryIndex(key)
	if idx < 0 {
		return m
	}
	// エントリを削除
	newEntries := make([]*types.MapEntry, 0, len(m.Entries)-1)
	newEntries = append(newEntries, m.Entries[:idx]...)
	newEntries = append(newEntries, m.Entries[idx+1:]...)
	return NewSortedMapWithMetadata(newEntries, m.Comparator, m.Metadata)
}

// Get はSortedMapからキーで値を取得します。
func (m *SortedMap) Get(key types.Value) types.Value {
	if m == nil {
		return nil
	}
	idx := m.findEntryIndex(key)
	if idx < 0 {
		return nil
	}
	return m.Entries[idx].Value
}

// Find はSortedMapからキーでエントリを取得します。
func (m *SortedMap) Find(key types.Value) *types.MapEntry {
	if m == nil {
		return nil
	}
	idx := m.findEntryIndex(key)
	if idx < 0 {
		return nil
	}
	return m.Entries[idx]
}

// Keys はSortedMapのキーのシーケンスを返します。
func (m *SortedMap) Keys() types.Sequable {
	if m == nil || len(m.Entries) == 0 {
		return nil
	}
	keys := make([]types.Value, 0, len(m.Entries))
	for _, entry := range m.Entries {
		keys = append(keys, entry.Key)
	}
	return NewList(keys...)
}

// Vals はSortedMapの値のシーケンスを返します。
func (m *SortedMap) Vals() types.Sequable {
	if m == nil || len(m.Entries) == 0 {
		return nil
	}
	vals := make([]types.Value, 0, len(m.Entries))
	for _, entry := range m.Entries {
		vals = append(vals, entry.Value)
	}
	return NewList(vals...)
}

// RSeq は逆順シーケンスを返します。
func (m *SortedMap) RSeq() types.Sequable {
	if m == nil || len(m.Entries) == 0 {
		return nil
	}
	entries := make([]types.Value, 0, len(m.Entries))
	for i := len(m.Entries) - 1; i >= 0; i-- {
		entries = append(entries, m.Entries[i])
	}
	return NewList(entries...)
}

// Invoke implements Fn interface.
func (m *SortedMap) Invoke(args ...types.Value) (types.Value, error) {
	return invokeMapLookup("sorted-map", m.Get, args...)
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (m *SortedMap) IsSelfEvaluating() bool {
	return false
}
