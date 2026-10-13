package coll

import (
	"fmt"
	"strings"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// RecordDef はレコード型の定義を表します。
// 複数のRecordインスタンスで共有されます。
type RecordDef struct {
	Name          string        // 公開されるレコード型名
	QualifiedName string        // 名前空間を含む内部識別子
	Fields        []types.Value // フィールド名（キーワード推奨）
	Metadata      *Map          // 定義メタデータ
}

// NewRecordDef は新しいレコード定義を作成します。
func NewRecordDef(name string, fields []types.Value) *RecordDef {
	return &RecordDef{
		Name:          name,
		QualifiedName: name,
		Fields:        fields,
		Metadata:      nil,
	}
}

func (d *RecordDef) identity() string {
	if d.QualifiedName != "" {
		return d.QualifiedName
	}
	return d.Name
}

func (d *RecordDef) String() string {
	return fmt.Sprintf("#<record-def:%s>", d.Name)
}

func (d *RecordDef) Type() string {
	return "record-def"
}

func (d *RecordDef) Hash() uint32 {
	return util.HashString(d.identity())
}

func (d *RecordDef) Equals(other types.Value) bool {
	if o, ok := other.(*RecordDef); ok {
		return d.identity() == o.identity()
	}
	return false
}

func (d *RecordDef) IsSelfEvaluating() bool {
	return true
}

// Record はレコードインスタンスを表します。
// Mapとして振る舞い、Protocol dispatchにも対応します。
type Record struct {
	Def      *RecordDef    // レコード定義
	Values   []types.Value // フィールド値（定義順）
	Extras   *Map          // 追加フィールド（assocで追加されたもの）
	Metadata *Map          // メタデータ
}

// NewRecord は新しいレコードインスタンスを作成します。
func NewRecord(def *RecordDef, values []types.Value) *Record {
	// 値の数をフィールド数に合わせる
	adjustedValues := make([]types.Value, len(def.Fields))
	copy(adjustedValues, values)
	for i, value := range adjustedValues {
		if value == nil {
			adjustedValues[i] = types.NilValue
		}
	}
	return &Record{
		Def:    def,
		Values: adjustedValues,
		Extras: nil,
	}
}

// NewRecordFromMap はマップからレコードインスタンスを作成します。
// 定義にないキーはExtrasに保存されます。
func NewRecordFromMap(def *RecordDef, m types.Map) *Record {
	values := make([]types.Value, len(def.Fields))
	extraPairs := []types.Value{}

	// フィールド値を取得
	for i, field := range def.Fields {
		val := m.Get(field)
		if val == nil {
			val = types.NilValue
		}
		values[i] = val
	}

	// 追加キーを収集
	seq := m.Seq()
	if seq != nil {
		if s, ok := seq.(types.Seq); ok {
			for s != nil {
				entry := s.First()
				if mapEntry, ok := entry.(*types.MapEntry); ok {
					isField := false
					for _, field := range def.Fields {
						if field.Equals(mapEntry.Key) {
							isField = true
							break
						}
					}
					if !isField {
						extraPairs = append(extraPairs, mapEntry.Key, mapEntry.Value)
					}
				}
				s = s.Next()
			}
		}
	}

	var extras *Map
	if len(extraPairs) > 0 {
		extras = NewMapFromPairs(extraPairs)
	}

	return &Record{
		Def:    def,
		Values: values,
		Extras: extras,
	}
}

func (r *Record) String() string {
	if r == nil {
		return "{}"
	}
	var parts []string
	for i, field := range r.Def.Fields {
		var valStr string
		if i < len(r.Values) && r.Values[i] != nil {
			valStr = r.Values[i].String()
		} else {
			valStr = "nil"
		}
		parts = append(parts, fmt.Sprintf("%s %s", field.String(), valStr))
	}
	// 追加フィールドを出力
	if r.Extras != nil {
		for _, key := range r.Extras.Order {
			val := r.Extras.Get(key)
			parts = append(parts, fmt.Sprintf("%s %s", key.String(), val.String()))
		}
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// Type はレコード型名を返します（Protocol dispatch用）。
func (r *Record) Type() string {
	if r == nil || r.Def == nil {
		return "record"
	}
	return r.Def.Name
}

// ProtocolTypeName は公開名と分離されたProtocol dispatch用の識別子を返します。
func (r *Record) ProtocolTypeName() string {
	if r == nil || r.Def == nil {
		return "record"
	}
	return r.Def.identity()
}

func (r *Record) Hash() uint32 {
	if r == nil {
		return 0
	}
	var hash uint32
	entries, _ := mapEntriesForEquality(r)
	for _, entry := range entries {
		hash += entry.Key.Hash()*31 + entry.Value.Hash()
	}
	return util.MixCollectionHash(hash, r.Count())
}

func (r *Record) Equals(other types.Value) bool {
	if r == nil {
		return other == nil
	}
	if _, ok := other.(types.Map); !ok {
		return false
	}
	equal, err := mapValuesEqualResult(r, other)
	return err == nil && equal
}

func (r *Record) IsSelfEvaluating() bool {
	return true
}

// ============================================================================
// types.Map interface implementation
// ============================================================================

// Get はキーに対応する値を返します。
func (r *Record) Get(key types.Value) types.Value {
	value, _ := r.GetResult(key)
	return value
}

// GetResult returns a field value without discarding hash/equality errors.
func (r *Record) GetResult(key types.Value) (types.Value, error) {
	entry, err := r.FindResult(key)
	if err != nil || entry == nil {
		return nil, err
	}
	return entry.Value, nil
}

func (r *Record) withCollectionError(err error) *Record {
	extras := NewEmptyMap()
	if r.Extras != nil {
		extras = r.Extras.clone()
	}
	if extras.hashErr == nil {
		extras.hashErr = err
	}
	return &Record{Def: r.Def, Values: r.Values, Extras: extras, Metadata: r.Metadata}
}

func (r *Record) findFieldResult(key types.Value) (*types.MapEntry, error) {
	if r == nil {
		return nil, nil
	}
	for i, field := range r.Def.Fields {
		equal, err := ValueEqualsResult(field, key)
		if err != nil {
			return nil, err
		}
		if equal {
			if i < len(r.Values) {
				return types.NewMapEntry(field, r.Values[i]), nil
			}
			return types.NewMapEntry(field, types.NilValue), nil
		}
	}
	return nil, nil
}

// Assoc は新しいキー/値ペアを追加した新しいレコードを返します。
func (r *Record) Assoc(key, val types.Value) types.Map {
	if r == nil {
		return nil
	}

	// フィールドの更新かどうかチェック
	for i, field := range r.Def.Fields {
		equal, err := ValueEqualsResult(field, key)
		if err != nil {
			return r.withCollectionError(err)
		}
		if equal {
			newValues := make([]types.Value, len(r.Values))
			copy(newValues, r.Values)
			if val == nil {
				val = types.NilValue
			}
			newValues[i] = val
			return &Record{
				Def:      r.Def,
				Values:   newValues,
				Extras:   r.Extras,
				Metadata: r.Metadata,
			}
		}
	}

	// Extrasに追加
	var newExtras *Map
	if r.Extras != nil {
		newExtras = r.Extras.Assoc(key, val).(*Map)
	} else {
		newExtras = NewMapFromPairs([]types.Value{key, val})
	}

	return &Record{
		Def:      r.Def,
		Values:   r.Values,
		Extras:   newExtras,
		Metadata: r.Metadata,
	}
}

// Dissoc はキーを削除した新しいレコードを返します。
// フィールドの削除は許可されません（nilに設定されます）。
func (r *Record) Dissoc(key types.Value) types.Map {
	if r == nil {
		return nil
	}

	// フィールドの場合はnilに設定
	for i, field := range r.Def.Fields {
		equal, err := ValueEqualsResult(field, key)
		if err != nil {
			return r.withCollectionError(err)
		}
		if equal {
			newValues := make([]types.Value, len(r.Values))
			copy(newValues, r.Values)
			newValues[i] = types.NilValue
			return &Record{
				Def:      r.Def,
				Values:   newValues,
				Extras:   r.Extras,
				Metadata: r.Metadata,
			}
		}
	}

	// Extrasから削除
	if r.Extras != nil {
		newExtras := r.Extras.Dissoc(key).(*Map)
		return &Record{
			Def:      r.Def,
			Values:   r.Values,
			Extras:   newExtras,
			Metadata: r.Metadata,
		}
	}

	return r
}

// Count はエントリ数を返します。
func (r *Record) Count() int {
	if r == nil {
		return 0
	}
	count := len(r.Def.Fields)
	if r.Extras != nil {
		count += r.Extras.Count()
	}
	return count
}

// Seq はマップエントリのシーケンスを返します。
func (r *Record) Seq() types.Value {
	if r == nil || r.Count() == 0 {
		return nil
	}
	return r.seqFrom(0)
}

// seqFrom は指定インデックスからのシーケンスを返します。
func (r *Record) seqFrom(idx int) types.Seq {
	if idx >= len(r.Def.Fields) {
		// Extrasのシーケンスを返す
		if r.Extras != nil {
			seq := r.Extras.Seq()
			if seq != nil {
				if s, ok := seq.(types.Seq); ok {
					return s
				}
			}
		}
		return nil
	}
	return &recordSeq{record: r, idx: idx}
}

// Keys はキーのシーケンスを返します。
func (r *Record) Keys() types.Sequable {
	if r == nil || r.Count() == 0 {
		return nil
	}
	keys := make([]types.Value, 0, r.Count())
	keys = append(keys, r.Def.Fields...)
	if r.Extras != nil {
		keys = append(keys, r.Extras.Order...)
	}
	return NewList(keys...)
}

// Vals は値のシーケンスを返します。
func (r *Record) Vals() types.Sequable {
	if r == nil || r.Count() == 0 {
		return nil
	}
	vals := make([]types.Value, 0, r.Count())
	vals = append(vals, r.Values...)
	if r.Extras != nil {
		for _, key := range r.Extras.Order {
			vals = append(vals, r.Extras.Get(key))
		}
	}
	return NewList(vals...)
}

// Conj は新しいエントリを追加します（MapEntryまたはベクターを期待）。
func (r *Record) Conj(val types.Value) types.Value {
	if r == nil {
		return nil
	}
	switch v := val.(type) {
	case *types.MapEntry:
		return r.Assoc(v.Key, v.Value)
	case *Vector:
		if v.Count() == 2 {
			return r.Assoc(v.Nth(0), v.Nth(1))
		}
	}
	return r
}

// Empty は空のレコードを返します（同じ定義で）。
func (r *Record) Empty() types.Value {
	if r == nil {
		return nil
	}
	values := make([]types.Value, len(r.Def.Fields))
	for i := range values {
		values[i] = types.NilValue
	}
	return &Record{
		Def:    r.Def,
		Values: values,
		Extras: nil,
	}
}

// ContainsKey はキーが存在するかを返します。
func (r *Record) ContainsKey(key types.Value) bool {
	contains, _ := r.ContainsKeyResult(key)
	return contains
}

// ContainsKeyResult checks membership without discarding hash/equality errors.
func (r *Record) ContainsKeyResult(key types.Value) (bool, error) {
	entry, err := r.FindResult(key)
	return entry != nil, err
}

// FindResult returns a map entry without discarding hash/equality errors.
func (r *Record) FindResult(key types.Value) (*types.MapEntry, error) {
	if r == nil {
		return nil, nil
	}
	entry, err := r.findFieldResult(key)
	if err != nil || entry != nil {
		return entry, err
	}
	if r.Extras != nil {
		return r.Extras.FindResult(key)
	}
	return nil, nil
}

// Find はキーに対応するMapEntryを返します。
func (r *Record) Find(key types.Value) *types.MapEntry {
	entry, _ := r.FindResult(key)
	return entry
}

// RSeq は逆順シーケンスを返します。
func (r *Record) RSeq() types.Sequable {
	if r == nil || r.Count() == 0 {
		return nil
	}
	entries := r.AllEntries()
	reversed := make([]types.Value, len(entries))
	for i := range entries {
		reversed[i] = entries[len(entries)-1-i]
	}
	return NewList(reversed...)
}

// AllEntries は全てのMapEntryをスライスで返します。
func (r *Record) AllEntries() []*types.MapEntry {
	if r == nil {
		return nil
	}
	entries := make([]*types.MapEntry, 0, r.Count())
	for i, field := range r.Def.Fields {
		var val types.Value
		if i < len(r.Values) {
			val = r.Values[i]
		}
		entries = append(entries, &types.MapEntry{Key: field, Value: val})
	}
	if r.Extras != nil {
		entries = append(entries, r.Extras.AllEntries()...)
	}
	return entries
}

// ToGoMap はGo mapに変換します。
func (r *Record) ToGoMap() map[types.Value]types.Value {
	if r == nil {
		return nil
	}
	result := make(map[types.Value]types.Value)
	for i, field := range r.Def.Fields {
		if i < len(r.Values) {
			result[field] = r.Values[i]
		}
	}
	if r.Extras != nil {
		r.Extras.Range(func(key, value types.Value) bool {
			result[key] = value
			return true
		})
	}
	return result
}

// GetMetadata はメタデータを返します。
func (r *Record) GetMetadata() *Map {
	if r == nil {
		return nil
	}
	return r.Metadata
}

// WithMetadata は新しいメタデータを持つレコードを返します。
func (r *Record) WithMetadata(metadata *Map) types.Value {
	if r == nil {
		return nil
	}
	return &Record{
		Def:      r.Def,
		Values:   r.Values,
		Extras:   r.Extras,
		Metadata: metadata,
	}
}

// ============================================================================
// recordSeq - Record用のシーケンス実装
// ============================================================================

type recordSeq struct {
	record *Record
	idx    int
}

func (s *recordSeq) String() string {
	return "(" + s.seqString() + ")"
}

func (s *recordSeq) seqString() string {
	var parts []string
	var current types.Seq = s
	for current != nil {
		entry := current.First()
		if entry == nil {
			break
		}
		parts = append(parts, entry.String())
		current = current.Next()
	}
	return strings.Join(parts, " ")
}

func (s *recordSeq) Type() string {
	return "seq"
}

func (s *recordSeq) Hash() uint32 {
	return SeqHash(s)
}

func (s *recordSeq) Equals(other types.Value) bool {
	return SequentialEquals(s, other)
}

func (s *recordSeq) IsSelfEvaluating() bool {
	return false
}

func (s *recordSeq) First() types.Value {
	if s.idx < len(s.record.Def.Fields) {
		field := s.record.Def.Fields[s.idx]
		var val types.Value
		if s.idx < len(s.record.Values) {
			val = s.record.Values[s.idx]
		}
		return &types.MapEntry{Key: field, Value: val}
	}
	return nil
}

func (s *recordSeq) Next() types.Seq {
	return s.record.seqFrom(s.idx + 1)
}

func (s *recordSeq) Rest() types.Seq {
	next := s.Next()
	if next == nil {
		return NewList()
	}
	return next
}

func (s *recordSeq) More() types.Seq {
	return s.Rest()
}

func (s *recordSeq) Cons(val types.Value) types.Seq {
	return NewConsSeq(val, s)
}

func (s *recordSeq) Count() int {
	return s.record.Count() - s.idx
}

func (s *recordSeq) Seq() types.Value {
	return s
}
