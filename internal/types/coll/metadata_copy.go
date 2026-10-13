package coll

import (
	"fmt"

	"doggo/internal/types"
)

// CopyWithMetadata returns a value equivalent to v with metadata attached,
// preserving collection ordering for all map implementations.
func CopyWithMetadata(v types.Value, metadata *Map) (types.Value, error) {
	switch val := v.(type) {
	case *Vector:
		return NewVectorWithMetadata(val.Items, metadata), nil
	case *Map:
		return cloneMapWithMetadata(val, metadata), nil
	case *ArrayMap:
		entries := make([]types.Value, len(val.Entries))
		copy(entries, val.Entries)
		return NewArrayMapWithMetadata(entries, metadata)
	case *SortedMap:
		entries := make([]*types.MapEntry, len(val.Entries))
		copy(entries, val.Entries)
		return NewSortedMapWithMetadata(entries, val.Comparator, metadata), nil
	case *StructMap:
		values := make([]types.Value, len(val.Values))
		copy(values, val.Values)
		return NewStructMapWithMetadata(val.StructDef, values, metadata)
	case *Set:
		return NewSetWithMetadata(val.AllElements(), metadata), nil
	case types.Symbol:
		return types.NewSymbolWithMeta(string(val), metadata), nil
	case *types.SymbolWithMeta:
		return types.NewSymbolWithMeta(val.Name, metadata), nil
	case *types.ResolvedSymbol:
		return types.NewResolvedSymbolWithMeta(val.Name, val.Resolution, metadata), nil
	case *List:
		return NewListWithLineAndMetadata(val.Line, val.Items, metadata), nil
	case *MetaSeq:
		return NewMetaSeq(val.SeqValue, metadata), nil
	case *RecordDef:
		cloned := *val
		cloned.Metadata = metadata
		return &cloned, nil
	case *DeftypeDef:
		cloned := *val
		cloned.Metadata = metadata
		return &cloned, nil
	case types.Seq:
		return NewMetaSeq(val, metadata), nil
	default:
		return v, fmt.Errorf("metadata is not supported for type %s", v.Type())
	}
}

func cloneMapWithMetadata(m *Map, metadata *Map) *Map {
	if m == nil {
		return &Map{
			Buckets:  make(map[uint32][]*types.MapEntry),
			CountVal: 0,
			Metadata: metadata,
			Order:    make([]types.Value, 0),
		}
	}
	cloned := m.clone()
	cloned.Metadata = metadata
	return cloned
}
