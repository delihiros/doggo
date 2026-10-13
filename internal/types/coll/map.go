package coll

import (
	"context"
	"fmt"
	"sort"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// Map はマップ（Clojureの {:a 1 :b 2} 形式）を表します。
// 値等価性に基づくハッシュマップとして実装されています。
// Order フィールドにより挿入順を保持します。
type Map struct {
	// Buckets はハッシュ値をキーとするバケットのマップです。
	// 衝突時は同じバケットにエントリを追加します。
	Buckets  map[uint32][]*types.MapEntry
	CountVal int           // エントリ数
	Metadata *Map          // メタデータマップ
	Order    []types.Value // 挿入順のキー（String()出力用）
	hashErr  error
}

// lookupBucket はキーに対応するバケット内のエントリを検索します。
func (m *Map) lookupBucket(key types.Value) *types.MapEntry {
	if m == nil || m.Buckets == nil {
		return nil
	}
	bucket := m.Buckets[key.Hash()]
	for _, entry := range bucket {
		if entry.Key.Equals(key) {
			return entry
		}
	}
	return nil
}

func (m *Map) lookupBucketResult(key types.Value) (*types.MapEntry, error) {
	if m == nil || m.Buckets == nil {
		return nil, nil
	}
	if m.hashErr != nil {
		return nil, m.hashErr
	}
	h, err := ValueHashResult(key)
	if err != nil {
		return nil, err
	}
	bucket := m.Buckets[h]
	for _, entry := range bucket {
		equal, err := ValueEqualsResult(entry.Key, key)
		if err != nil {
			return nil, err
		}
		if equal {
			return entry, nil
		}
	}
	return nil, nil
}

// String は挿入順でマップを出力します。
func (m *Map) String() string {
	if m == nil || m.CountVal == 0 {
		return "{}"
	}
	result := "{"
	first := true
	for _, key := range m.Order {
		entry := m.lookupBucket(key)
		if entry == nil {
			continue // 整合性が壊れている場合はスキップ
		}
		if !first {
			result += ", "
		}
		result += entry.Key.String() + " " + entry.Value.String()
		first = false
	}
	result += "}"
	return result
}

func (m *Map) Type() string {
	return "map"
}

func (m *Map) Hash() uint32 {
	if m == nil {
		return 0
	}
	var hash uint32
	for _, bucket := range m.Buckets {
		for _, entry := range bucket {
			entryHash := entry.Key.Hash()*31 + entry.Value.Hash()
			hash += entryHash
		}
	}
	return util.MixCollectionHash(hash, m.CountVal)
}

func (m *Map) Equals(other types.Value) bool {
	if m == nil {
		return other == nil
	}
	if _, ok := other.(types.Map); !ok {
		return false
	}
	equal, err := mapValuesEqualResult(m, other)
	return err == nil && equal
}

// Invoke implements Fn interface.
func (m *Map) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("map invocation requires at least 1 argument (key)")
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("map invocation expects 1 or 2 arguments (key, default?), got %d", len(args))
	}

	key := args[0]
	entry, err := m.lookupBucketResult(key)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		// キーが見つからない場合、デフォルト値があれば返す
		if len(args) == 2 {
			return args[1], nil
		}
		return nil, nil
	}

	return entry.Value, nil
}

// NewMap は新しいマップを作成します。
// 旧APIとの互換性のため、map[types.Value]types.Value を受け取ります。
// Go map は順序不定のため、キーの文字列表現でソートして決定的な順序を与えます。
func NewMap(entries map[types.Value]types.Value) *Map {
	m := &Map{
		Buckets:  make(map[uint32][]*types.MapEntry),
		CountVal: 0,
		Metadata: nil,
		Order:    make([]types.Value, 0, len(entries)),
	}
	if len(entries) == 0 {
		return m
	}

	// Go map から取り出したキーを安定ソート（文字列表現でソート）
	keys := make([]types.Value, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].String() < keys[j].String()
	})

	for _, k := range keys {
		v := entries[k]
		m = m.assocInternal(k, v)
	}
	return m
}

// NewMapFromPairs は [k0, v0, k1, v1, ...] の順序を保持してマップを作成します。
func NewMapFromPairs(pairs []types.Value) *Map {
	m := &Map{
		Buckets:  make(map[uint32][]*types.MapEntry),
		CountVal: 0,
		Metadata: nil,
		Order:    make([]types.Value, 0, len(pairs)/2),
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		m = m.assocInternal(pairs[i], pairs[i+1])
	}
	return m
}

// NewEmptyMap は空のマップを作成します。
func NewEmptyMap() *Map {
	return &Map{
		Buckets:  make(map[uint32][]*types.MapEntry),
		CountVal: 0,
		Metadata: nil,
		Order:    make([]types.Value, 0),
	}
}

// assocInternal は内部でキーと値を追加/更新します（破壊的操作）。
// 新しいMapの構築時にのみ使用します。
func (m *Map) assocInternal(key, val types.Value) *Map {
	h, err := ValueHashResult(key)
	if err != nil {
		if m.hashErr == nil {
			m.hashErr = err
		}
		return m
	}
	bucket := m.Buckets[h]

	// 既存のキーを検索
	for i, entry := range bucket {
		if entry.Key.Equals(key) {
			// 既存のキーを更新（Orderは変更しない）
			bucket[i] = types.NewMapEntry(key, val)
			return m
		}
	}

	// 新しいキーを追加
	m.Buckets[h] = append(bucket, types.NewMapEntry(key, val))
	m.Order = append(m.Order, key)
	m.CountVal++
	return m
}

// Map実装

// Count はマップのエントリ数を返します。
func (m *Map) Count() int {
	if m == nil {
		return 0
	}
	return m.CountVal
}

// Conj はマップにエントリを追加します。
func (m *Map) Conj(item types.Value) types.Value {
	if m == nil {
		m = NewEmptyMap()
	}
	// マップの場合は、itemがMapEntryまたは[key value]のベクター、またはマップ全体
	if entry, ok := item.(*types.MapEntry); ok {
		return m.Assoc(entry.Key, entry.Value)
	}
	if vec, ok := item.(*Vector); ok && len(vec.Items) == 2 {
		return m.Assoc(vec.Items[0], vec.Items[1])
	}
	if itemMap, ok := item.(*Map); ok {
		result := m.clone()
		// itemMapの挿入順を尊重して追加
		for _, key := range itemMap.Order {
			entry := itemMap.lookupBucket(key)
			if entry != nil {
				result = result.assocInternal(entry.Key, entry.Value)
			}
		}
		return result
	}
	return m
}

// clone はマップのコピーを作成します。
func (m *Map) clone() *Map {
	newBuckets := make(map[uint32][]*types.MapEntry, len(m.Buckets))
	for h, bucket := range m.Buckets {
		newBucket := make([]*types.MapEntry, len(bucket))
		copy(newBucket, bucket)
		newBuckets[h] = newBucket
	}
	newOrder := make([]types.Value, len(m.Order))
	copy(newOrder, m.Order)
	return &Map{
		Buckets:  newBuckets,
		CountVal: m.CountVal,
		Metadata: m.Metadata,
		Order:    newOrder,
		hashErr:  m.hashErr,
	}
}

// Seq はマップをシーケンス（MapEntryのリスト）に変換します。
// 挿入順で返します。
func (m *Map) Seq() types.Value {
	if m == nil || m.CountVal == 0 {
		return nil
	}
	entries := make([]types.Value, 0, m.CountVal)
	for _, key := range m.Order {
		entry := m.lookupBucket(key)
		if entry != nil {
			entries = append(entries, entry)
		}
	}
	return NewList(entries...)
}

// Assoc はマップにキーと値を追加/更新します。
func (m *Map) Assoc(key, val types.Value) types.Map {
	if m == nil {
		m = NewEmptyMap()
	}
	newMap := m.clone()
	newMap.assocInternal(key, val)
	return newMap
}

// Dissoc はマップからキーを削除します。
func (m *Map) Dissoc(key types.Value) types.Map {
	if m == nil {
		return NewEmptyMap()
	}

	entry, err := m.lookupBucketResult(key)
	if err != nil {
		newMap := m.clone()
		if newMap.hashErr == nil {
			newMap.hashErr = err
		}
		return newMap
	}
	if entry == nil {
		return m
	}

	h, err := ValueHashResult(key)
	if err != nil {
		newMap := m.clone()
		if newMap.hashErr == nil {
			newMap.hashErr = err
		}
		return newMap
	}
	bucket := m.Buckets[h]

	foundIdx := -1
	for i, candidate := range bucket {
		if candidate == entry {
			foundIdx = i
			break
		}
	}

	if foundIdx == -1 {
		// キーが存在しない場合は元のマップを返す
		return m
	}

	// コピーを作成してから削除
	newMap := m.clone()
	newBucket := newMap.Buckets[h]
	// スライスから要素を削除
	newMap.Buckets[h] = append(newBucket[:foundIdx], newBucket[foundIdx+1:]...)
	newMap.CountVal--

	// バケットが空になった場合は削除
	if len(newMap.Buckets[h]) == 0 {
		delete(newMap.Buckets, h)
	}

	// Orderからもキーを削除
	for i, k := range newMap.Order {
		equal, err := ValueEqualsResult(k, key)
		if err != nil {
			if newMap.hashErr == nil {
				newMap.hashErr = err
			}
			return newMap
		}
		if equal {
			newMap.Order = append(newMap.Order[:i], newMap.Order[i+1:]...)
			break
		}
	}

	return newMap
}

// Get はマップからキーで値を取得します。
func (m *Map) Get(key types.Value) types.Value {
	if m == nil {
		return nil
	}
	entry := m.lookupBucket(key)
	if entry == nil {
		return nil
	}
	return entry.Value
}

// Find はマップからキーでエントリを取得します。
func (m *Map) Find(key types.Value) *types.MapEntry {
	if m == nil {
		return nil
	}
	entry := m.lookupBucket(key)
	return entry
}

// FindResult returns a map entry without discarding lazy hash/equality errors.
func (m *Map) FindResult(key types.Value) (*types.MapEntry, error) {
	if m == nil {
		return nil, nil
	}
	return m.lookupBucketResult(key)
}

// Keys はマップのキーのシーケンスを返します（挿入順）。
func (m *Map) Keys() types.Sequable {
	if m == nil || m.CountVal == 0 {
		return nil
	}
	keys := make([]types.Value, len(m.Order))
	copy(keys, m.Order)
	return NewList(keys...)
}

// Vals はマップの値のシーケンスを返します（挿入順）。
func (m *Map) Vals() types.Sequable {
	if m == nil || m.CountVal == 0 {
		return nil
	}
	vals := make([]types.Value, 0, m.CountVal)
	for _, key := range m.Order {
		entry := m.lookupBucket(key)
		if entry != nil {
			vals = append(vals, entry.Value)
		}
	}
	return NewList(vals...)
}

// RSeq は逆順シーケンスを返します（挿入順の逆）。
func (m *Map) RSeq() types.Sequable {
	if m == nil || m.CountVal == 0 {
		return nil
	}
	entries := make([]types.Value, 0, m.CountVal)
	for i := len(m.Order) - 1; i >= 0; i-- {
		entry := m.lookupBucket(m.Order[i])
		if entry != nil {
			entries = append(entries, entry)
		}
	}
	return NewList(entries...)
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (m *Map) IsSelfEvaluating() bool {
	return false
}

// Range はマップの各エントリに対して関数を呼び出します（挿入順）。
// 関数がfalseを返すとイテレーションを停止します。
func (m *Map) Range(fn func(key, value types.Value) bool) {
	if m == nil {
		return
	}
	for _, key := range m.Order {
		entry := m.lookupBucket(key)
		if entry != nil {
			if !fn(entry.Key, entry.Value) {
				return
			}
		}
	}
}

// AllEntries はすべてのエントリを[]*types.MapEntryとして返します（挿入順）。
func (m *Map) AllEntries() []*types.MapEntry {
	if m == nil {
		return nil
	}
	result := make([]*types.MapEntry, 0, m.CountVal)
	for _, key := range m.Order {
		entry := m.lookupBucket(key)
		if entry != nil {
			result = append(result, entry)
		}
	}
	return result
}
