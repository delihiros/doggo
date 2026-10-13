package types

import (
	"doggo/internal/types/util"
)

// MapEntry はマップのエントリ（キーと値のペア）を表します。
// Map インターフェースで使用されるため、ここに定義します。
type MapEntry struct {
	Key   Value
	Value Value
}

func (e *MapEntry) String() string {
	return "[" + e.Key.String() + " " + e.Value.String() + "]"
}

func (e *MapEntry) Type() string {
	return "map-entry"
}

func (e *MapEntry) Hash() uint32 {
	hash := uint32(1)
	hash = hash*31 + e.Key.Hash()
	hash = hash*31 + e.Value.Hash()
	return util.MixCollectionHash(hash, 2)
}

func (e *MapEntry) Equals(other Value) bool {
	if other == nil {
		return false
	}
	if o, ok := other.(*MapEntry); ok {
		return e.Key.Equals(o.Key) && e.Value.Equals(o.Value)
	}
	vector, ok := other.(Vector)
	return ok && vector.Count() == 2 && e.Key.Equals(vector.Nth(0)) && e.Value.Equals(vector.Nth(1))
}

// NewMapEntry は新しいMapEntryを作成します。
func NewMapEntry(key, val Value) *MapEntry {
	return &MapEntry{Key: key, Value: val}
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (e *MapEntry) IsSelfEvaluating() bool {
	return true
}
