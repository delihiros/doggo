package schema

import "doggo/internal/types"

type Kind string

const (
	KindAny       Kind = "any"
	KindNil       Kind = "nil"
	KindString    Kind = "string"
	KindKeyword   Kind = "keyword"
	KindSymbol    Kind = "symbol"
	KindBool      Kind = "bool"
	KindInt       Kind = "int"
	KindFloat     Kind = "float"
	KindNumber    Kind = "number"
	KindMap       Kind = "map"
	KindVector    Kind = "vector"
	KindSet       Kind = "set"
	KindList      Kind = "list"
	KindSeq       Kind = "seq"
	KindMaybe     Kind = "maybe"
	KindTuple     Kind = "tuple"
	KindMapOf     Kind = "map-of"
	KindOr        Kind = "or"
	KindAnd       Kind = "and"
	KindEnum      Kind = "enum"
	KindPredicate Kind = "predicate"
)

type MapEntry struct {
	Key    types.Value
	Schema *Schema
}

// Schema is the canonical owner of normalized schema state for validation,
// explain, and generation.
type Schema struct {
	kind           Kind
	raw            types.Value
	element        *Schema
	items          []*Schema
	entries        []MapEntry
	keySchema      *Schema
	valueSchema    *Schema
	enumValues     []types.Value
	predicate      types.Fn
	predicateValue types.Value
}

func (s *Schema) Kind() Kind {
	if s == nil {
		return ""
	}
	return s.kind
}

func (s *Schema) Raw() types.Value {
	if s == nil {
		return nil
	}
	return s.raw
}

func (s *Schema) Element() *Schema {
	if s == nil {
		return nil
	}
	return s.element
}

func (s *Schema) Items() []*Schema {
	if s == nil {
		return nil
	}
	out := make([]*Schema, len(s.items))
	copy(out, s.items)
	return out
}

func (s *Schema) Entries() []MapEntry {
	if s == nil {
		return nil
	}
	out := make([]MapEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

func (s *Schema) KeySchema() *Schema {
	if s == nil {
		return nil
	}
	return s.keySchema
}

func (s *Schema) ValueSchema() *Schema {
	if s == nil {
		return nil
	}
	return s.valueSchema
}

func (s *Schema) EnumValues() []types.Value {
	if s == nil {
		return nil
	}
	out := make([]types.Value, len(s.enumValues))
	copy(out, s.enumValues)
	return out
}

func (s *Schema) Predicate() types.Fn {
	if s == nil {
		return nil
	}
	return s.predicate
}

type Path []types.Value

func (p Path) Push(step types.Value) Path {
	out := make(Path, len(p)+1)
	copy(out, p)
	out[len(p)] = step
	return out
}

func (p Path) String() string {
	return types.FormatCollection([]types.Value(p), "[", "]", "[]")
}

type Problem struct {
	Path      Path
	Expected  types.Value
	Actual    types.Value
	Predicate types.Value
	Branch    *int
	Message   string
}

type Explanation struct {
	Valid    bool
	Schema   *Schema
	Value    types.Value
	Problems []Problem
}
