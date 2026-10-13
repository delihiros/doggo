package coll

import (
	"context"
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// Set はセット（Clojureの #{:a :b :c} 形式）を表します。
// ハッシュバケットによる O(1) ルックアップを提供します。
// Order フィールドにより挿入順を保持します。
type Set struct {
	Buckets  map[uint32][]types.Value // ハッシュ → バケット
	CountVal int                      // 要素数
	Metadata *Map                     // メタデータマップ
	Order    []types.Value            // 挿入順
	hashErr  error
}

// lookupBucket はハッシュバケットから要素を検索します。
// 見つかった場合は要素とインデックスを返します。
func (s *Set) lookupBucket(item types.Value) (types.Value, int) {
	if s == nil || s.Buckets == nil {
		return nil, -1
	}
	bucket := s.Buckets[item.Hash()]
	for i, elem := range bucket {
		if elem.Equals(item) {
			return elem, i
		}
	}
	return nil, -1
}

func (s *Set) lookupBucketResult(item types.Value) (types.Value, int, error) {
	if s == nil || s.Buckets == nil {
		return nil, -1, nil
	}
	if s.hashErr != nil {
		return nil, -1, s.hashErr
	}
	h, err := ValueHashResult(item)
	if err != nil {
		return nil, -1, err
	}
	bucket := s.Buckets[h]
	for i, elem := range bucket {
		equal, err := ValueEqualsResult(elem, item)
		if err != nil {
			return nil, -1, err
		}
		if equal {
			return elem, i, nil
		}
	}
	return nil, -1, nil
}

// String は挿入順でセットを出力します。
func (s *Set) String() string {
	if s == nil || s.CountVal == 0 {
		return "#{}"
	}
	result := "#{"
	first := true
	for _, elem := range s.Order {
		if !first {
			result += " "
		}
		result += elem.String()
		first = false
	}
	result += "}"
	return result
}

func (s *Set) Type() string {
	return "set"
}

func (s *Set) Hash() uint32 {
	if s == nil {
		return 0
	}
	var hash uint32
	for _, bucket := range s.Buckets {
		for _, elem := range bucket {
			hash += elem.Hash()
		}
	}
	return util.MixCollectionHash(hash, s.CountVal)
}

func (s *Set) Equals(other types.Value) bool {
	if s == nil {
		return other == nil
	}
	if other == nil {
		return false
	}
	o, ok := other.(*Set)
	if !ok {
		return false
	}
	if s.CountVal != o.CountVal {
		return false
	}
	// Equalsメソッドを使用して値ベースの比較を行う
	for _, bucket := range s.Buckets {
		for _, elem := range bucket {
			if !o.containsValue(elem) {
				return false
			}
		}
	}
	return true
}

// Invoke implements Fn interface.
func (s *Set) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("set invocation requires at least 1 argument (member)")
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("set invocation expects 1 argument (member), got %d", len(args))
	}

	member := args[0]
	// Equalsメソッドを使用して値の比較を行う
	found, _, err := s.lookupBucketResult(member)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return member, nil
	}

	return nil, nil
}

// containsValue はセットに指定された値が含まれているかを、Equalsメソッドを使って判定します。
// ハッシュバケットにより O(1) で動作します。
func (s *Set) containsValue(item types.Value) bool {
	elem, _ := s.lookupBucket(item)
	return elem != nil
}

// Contains は要素がセットに含まれているかを O(1) で判定します。
//
// 値ベースの等価性（Equals メソッド）を使用して比較します。
// ハッシュバケット構造により、セットのサイズに関わらず定数時間で動作します。
//
// nil レシーバの場合は false を返します。
//
// 例:
//
//	s := NewSet(atom.NewKeyword("a"), atom.NewKeyword("b"))
//	s.Contains(atom.NewKeyword("a")) // true
//	s.Contains(atom.NewKeyword("c")) // false
func (s *Set) Contains(item types.Value) bool {
	return s.containsValue(item)
}

// findElement はセット内で指定された値と等しい要素を見つけて返します。
// ハッシュバケットにより O(1) で動作します。
func (s *Set) findElement(item types.Value) types.Value {
	elem, _ := s.lookupBucket(item)
	return elem
}

// AllElements は全要素をスライスで返します。
//
// 要素は挿入順で返されます。
// nil レシーバの場合は nil を返します。
// 返されるスライスは内部データのコピーなので、安全に変更できます。
//
// 例:
//
//	s := NewSet(atom.NewKeyword("a"), atom.NewKeyword("b"))
//	elements := s.AllElements() // [:a :b]（挿入順）
func (s *Set) AllElements() []types.Value {
	if s == nil {
		return nil
	}
	result := make([]types.Value, len(s.Order))
	copy(result, s.Order)
	return result
}

// NewSet は新しいセットを作成します。
//
// 重複する要素は値ベースの比較（Equals）で除去されます。
// 最初に現れた要素が保持され、後の重複は無視されます。
// 挿入順は保持されます。
//
// 例:
//
//	s := NewSet(atom.NewKeyword("a"), atom.NewKeyword("b"), atom.NewKeyword("a"))
//	// 結果: #{:a :b}（:a の重複は除去）
func NewSet(elements ...types.Value) *Set {
	set := &Set{
		Buckets:  make(map[uint32][]types.Value),
		CountVal: 0,
		Metadata: nil,
		Order:    make([]types.Value, 0, len(elements)),
	}
	for _, elem := range elements {
		h, err := ValueHashResult(elem)
		if err != nil {
			if set.hashErr == nil {
				set.hashErr = err
			}
			continue
		}
		// 値ベースの比較で重複をチェック
		if !bucketContainsValue(set.Buckets[h], elem) {
			set.Buckets[h] = append(set.Buckets[h], elem)
			set.Order = append(set.Order, elem)
			set.CountVal++
		}
	}
	return set
}

// NewSetWithMetadata はメタデータ付きのセットを作成します。
// 重複する要素は値ベースの比較（Equals）で除去されます。
// 挿入順を保持します。
func NewSetWithMetadata(elements []types.Value, metadata *Map) *Set {
	set := &Set{
		Buckets:  make(map[uint32][]types.Value),
		CountVal: 0,
		Metadata: metadata,
		Order:    make([]types.Value, 0, len(elements)),
	}
	for _, elem := range elements {
		h, err := ValueHashResult(elem)
		if err != nil {
			if set.hashErr == nil {
				set.hashErr = err
			}
			continue
		}
		// 値ベースの比較で重複をチェック
		if !bucketContainsValue(set.Buckets[h], elem) {
			set.Buckets[h] = append(set.Buckets[h], elem)
			set.Order = append(set.Order, elem)
			set.CountVal++
		}
	}
	return set
}

// IPersistentSet/Sequable実装

// Count はセットの要素数を返します。
func (s *Set) Count() int {
	if s == nil {
		return 0
	}
	return s.CountVal
}

// Conj はセットに要素を追加します。
// 既に同値の要素が存在する場合は追加しません（値ベースの比較）。
// 挿入順を保持します。
func (s *Set) Conj(item types.Value) types.Value {
	if s == nil {
		return NewSet(item)
	}
	h, hashErr := ValueHashResult(item)
	if hashErr != nil {
		if s.hashErr != nil {
			hashErr = s.hashErr
		}
		return &Set{
			Buckets:  s.Buckets,
			CountVal: s.CountVal,
			Metadata: s.Metadata,
			Order:    s.Order,
			hashErr:  hashErr,
		}
	}
	// 値ベースの比較で重複をチェック
	if bucketContainsValue(s.Buckets[h], item) {
		return s // 既に存在するので変更なし
	}

	// 既存のバケットをコピー
	newSet := &Set{
		Buckets:  make(map[uint32][]types.Value, len(s.Buckets)),
		CountVal: s.CountVal + 1,
		Metadata: s.Metadata,
		Order:    make([]types.Value, len(s.Order), len(s.Order)+1),
		hashErr:  s.hashErr,
	}
	for h, bucket := range s.Buckets {
		newBucket := make([]types.Value, len(bucket))
		copy(newBucket, bucket)
		newSet.Buckets[h] = newBucket
	}
	copy(newSet.Order, s.Order)

	// 新しい要素を追加
	newSet.Buckets[h] = append(newSet.Buckets[h], item)
	newSet.Order = append(newSet.Order, item)
	return newSet
}

func bucketContainsValue(bucket []types.Value, item types.Value) bool {
	for _, existing := range bucket {
		if existing.Equals(item) {
			return true
		}
	}
	return false
}

// Seq はセットをシーケンスに変換します（挿入順）。
func (s *Set) Seq() types.Value {
	if s == nil || s.CountVal == 0 {
		return nil
	}
	elements := make([]types.Value, len(s.Order))
	copy(elements, s.Order)
	return NewList(elements...)
}

// Disj はセットから要素を削除した新しいセットを返します。
// 挿入順を保持します。
func (s *Set) Disj(item types.Value) *Set {
	if s == nil {
		return NewSet()
	}

	// 値ベースの比較で削除対象を見つける
	elem, idx, err := s.lookupBucketResult(item)
	if err != nil {
		return &Set{
			Buckets:  s.Buckets,
			CountVal: s.CountVal,
			Metadata: s.Metadata,
			Order:    s.Order,
			hashErr:  err,
		}
	}
	if elem == nil {
		// 要素が存在しない場合は元のセットを返す
		return s
	}

	h, err := ValueHashResult(item)
	if err != nil {
		return &Set{
			Buckets:  s.Buckets,
			CountVal: s.CountVal,
			Metadata: s.Metadata,
			Order:    s.Order,
			hashErr:  err,
		}
	}
	newSet := &Set{
		Buckets:  make(map[uint32][]types.Value, len(s.Buckets)),
		CountVal: s.CountVal - 1,
		Metadata: s.Metadata,
		Order:    make([]types.Value, 0, len(s.Order)-1),
		hashErr:  s.hashErr,
	}

	// バケットをコピー（削除対象のバケットは別処理）
	for bh, bucket := range s.Buckets {
		if bh == h {
			// 削除対象のバケット
			if len(bucket) > 1 {
				newBucket := make([]types.Value, 0, len(bucket)-1)
				newBucket = append(newBucket, bucket[:idx]...)
				newBucket = append(newBucket, bucket[idx+1:]...)
				newSet.Buckets[bh] = newBucket
			}
			// len(bucket) == 1 の場合はバケットごと削除（何もしない）
		} else {
			newBucket := make([]types.Value, len(bucket))
			copy(newBucket, bucket)
			newSet.Buckets[bh] = newBucket
		}
	}

	// Orderから削除
	for _, e := range s.Order {
		equal, err := ValueEqualsResult(e, item)
		if err != nil {
			newSet.hashErr = err
			return newSet
		}
		if !equal {
			newSet.Order = append(newSet.Order, e)
		}
	}

	return newSet
}

// Get はセットに要素が含まれていればその要素を返し、なければnilを返します。
// Equalsメソッドを使用して値の比較を行います。
func (s *Set) Get(item types.Value) types.Value {
	return s.findElement(item)
}

// GetResult returns a set element without discarding lazy hash/equality errors.
func (s *Set) GetResult(item types.Value) (types.Value, error) {
	value, _, err := s.lookupBucketResult(item)
	return value, err
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (s *Set) IsSelfEvaluating() bool {
	return false
}
