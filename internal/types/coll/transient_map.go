package coll

import (
	"errors"
	"sync"

	"doggo/internal/types"
)

// ErrTransientNotEditable はトランジェントマップが編集不可になった後に
// 編集操作を行おうとした場合に返されるエラーです。
var ErrTransientNotEditable = errors.New("transient map is no longer editable")

// ErrTransientOwner はトランジェントマップが作成元以外の論理実行から
// 操作された場合に返されるエラーです。
var ErrTransientOwner = errors.New("transient map used outside its owner execution")

// TransientOwner はトランジェントマップを所有する論理実行を識別します。
type TransientOwner struct {
	marker byte
}

func NewTransientOwner() *TransientOwner {
	return &TransientOwner{}
}

// TransientMap は効率的なバッチ構築用の可変マップです。
// Clojure の transient と同様、構築中は破壊的操作が可能で、
// Persistent() で永続的な Map に変換します。
// order フィールドにより挿入順を保持します。
type TransientMap struct {
	mu       sync.RWMutex
	owner    *TransientOwner
	buckets  map[uint32][]*types.MapEntry
	countVal int
	editable bool          // まだ編集可能か
	order    []types.Value // 挿入順のキー
	hashErr  error
}

// NewTransientMap は新しい TransientMap を作成します。
func NewTransientMap() *TransientMap {
	return NewTransientMapOwned(nil)
}

// NewTransientMapOwned は所有者を指定して新しい TransientMap を作成します。
func NewTransientMapOwned(owner *TransientOwner) *TransientMap {
	return &TransientMap{
		owner:    owner,
		buckets:  make(map[uint32][]*types.MapEntry),
		countVal: 0,
		editable: true,
		order:    make([]types.Value, 0),
	}
}

// NewTransientMapFrom は既存の Map から TransientMap を作成します。
func NewTransientMapFrom(m *Map) *TransientMap {
	return NewTransientMapFromOwned(m, nil)
}

// NewTransientMapFromOwned は所有者を指定して既存の Map から TransientMap を作成します。
func NewTransientMapFromOwned(m *Map, owner *TransientOwner) *TransientMap {
	if m == nil {
		return NewTransientMapOwned(owner)
	}

	// バケットをコピー
	newBuckets := make(map[uint32][]*types.MapEntry, len(m.Buckets))
	for h, bucket := range m.Buckets {
		newBucket := make([]*types.MapEntry, len(bucket))
		copy(newBucket, bucket)
		newBuckets[h] = newBucket
	}

	// 挿入順をコピー
	newOrder := make([]types.Value, len(m.Order))
	copy(newOrder, m.Order)

	return &TransientMap{
		owner:    owner,
		buckets:  newBuckets,
		countVal: m.CountVal,
		editable: true,
		order:    newOrder,
		hashErr:  m.hashErr,
	}
}

func (tm *TransientMap) checkOwner(owner *TransientOwner) error {
	if tm.owner != nil && tm.owner != owner {
		return ErrTransientOwner
	}
	return nil
}

// lookupBucket はキーに対応するバケット内のエントリを検索します。
func (tm *TransientMap) lookupBucket(key types.Value) *types.MapEntry {
	h, err := ValueHashResult(key)
	if err != nil && tm.hashErr == nil {
		tm.hashErr = err
	}
	bucket := tm.buckets[h]
	for _, entry := range bucket {
		if entry.Key.Equals(key) {
			return entry
		}
	}
	return nil
}

// Assoc は TransientMap にキーと値を追加/更新します（破壊的操作）。
// 高速化のため、バケットを直接変更します。
// トランジェントが編集不可の場合は ErrTransientNotEditable を返します。
func (tm *TransientMap) Assoc(key, val types.Value) (*TransientMap, error) {
	return tm.AssocOwned(nil, key, val)
}

// AssocOwned は所有者を検証してキーと値を追加/更新します。
func (tm *TransientMap) AssocOwned(owner *TransientOwner, key, val types.Value) (*TransientMap, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if err := tm.checkOwner(owner); err != nil {
		return nil, err
	}
	if !tm.editable {
		return nil, ErrTransientNotEditable
	}

	h, err := ValueHashResult(key)
	if err != nil {
		return nil, err
	}
	bucket := tm.buckets[h]

	// 既存のキーを検索
	for i, entry := range bucket {
		equal, err := ValueEqualsResult(entry.Key, key)
		if err != nil {
			return nil, err
		}
		if equal {
			// 既存のキーを更新（インプレース、orderは変更しない）
			bucket[i] = types.NewMapEntry(key, val)
			return tm, nil
		}
	}

	// 新しいキーを追加
	tm.buckets[h] = append(bucket, types.NewMapEntry(key, val))
	tm.order = append(tm.order, key)
	tm.countVal++
	return tm, nil
}

// Dissoc は TransientMap からキーを削除します（破壊的操作）。
// トランジェントが編集不可の場合は ErrTransientNotEditable を返します。
func (tm *TransientMap) Dissoc(key types.Value) (*TransientMap, error) {
	return tm.DissocOwned(nil, key)
}

// DissocOwned は所有者を検証してキーを削除します。
func (tm *TransientMap) DissocOwned(owner *TransientOwner, key types.Value) (*TransientMap, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if err := tm.checkOwner(owner); err != nil {
		return nil, err
	}
	if !tm.editable {
		return nil, ErrTransientNotEditable
	}

	h, err := ValueHashResult(key)
	if err != nil {
		return nil, err
	}
	bucket := tm.buckets[h]

	// キーを検索
	for i, entry := range bucket {
		equal, err := ValueEqualsResult(entry.Key, key)
		if err != nil {
			return nil, err
		}
		if equal {
			// 削除
			tm.buckets[h] = append(bucket[:i], bucket[i+1:]...)
			tm.countVal--

			// バケットが空になった場合は削除
			if len(tm.buckets[h]) == 0 {
				delete(tm.buckets, h)
			}

			// orderからもキーを削除
			for j, k := range tm.order {
				if k.Equals(key) {
					tm.order = append(tm.order[:j], tm.order[j+1:]...)
					break
				}
			}
			return tm, nil
		}
	}

	return tm, nil
}

// Get は TransientMap からキーで値を取得します。
func (tm *TransientMap) Get(key types.Value) types.Value {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	entry := tm.lookupBucket(key)
	if entry == nil {
		return nil
	}
	return entry.Value
}

// Count は TransientMap のエントリ数を返します。
func (tm *TransientMap) Count() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.countVal
}

// Persistent は TransientMap を永続的な Map に変換します。
// この呼び出し後、TransientMap は編集不可になります。
// トランジェントが既に編集不可の場合は ErrTransientNotEditable を返します。
func (tm *TransientMap) Persistent() (*Map, error) {
	return tm.PersistentOwned(nil)
}

// PersistentOwned は所有者を検証して永続的な Map に変換します。
func (tm *TransientMap) PersistentOwned(owner *TransientOwner) (*Map, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if err := tm.checkOwner(owner); err != nil {
		return nil, err
	}
	if !tm.editable {
		return nil, ErrTransientNotEditable
	}
	if tm.hashErr != nil {
		return nil, tm.hashErr
	}

	tm.editable = false

	return &Map{
		Buckets:  tm.buckets,
		CountVal: tm.countVal,
		Metadata: nil,
		Order:    tm.order,
		hashErr:  tm.hashErr,
	}, nil
}

// Value インターフェースの実装

// String は TransientMap の文字列表現を返します（挿入順）。
func (tm *TransientMap) String() string {
	if tm == nil {
		return "#<transient {}>"
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.countVal == 0 {
		return "#<transient {}>"
	}
	result := "#<transient {"
	first := true
	for _, key := range tm.order {
		entry := tm.lookupBucket(key)
		if entry == nil {
			continue
		}
		if !first {
			result += ", "
		}
		result += entry.Key.String() + " " + entry.Value.String()
		first = false
	}
	result += "}>"
	return result
}

// Type は型名を返します。
func (tm *TransientMap) Type() string {
	return "transient-map"
}

// Hash はハッシュ値を返します。
func (tm *TransientMap) Hash() uint32 {
	return 0 // transient はハッシュキーとして使用不可
}

// Equals は等価性を判定します。
func (tm *TransientMap) Equals(other types.Value) bool {
	return tm == other // アイデンティティベース
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (tm *TransientMap) IsSelfEvaluating() bool {
	return true
}
