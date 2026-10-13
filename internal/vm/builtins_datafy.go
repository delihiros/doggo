package vm

import (
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var (
	datafyKindKey      = atom.NewKeyword("kind")
	datafyNamespaceKey = atom.NewKeyword("ns")
	datafyNameKey      = atom.NewKeyword("name")
	datafyQualifiedKey = atom.NewKeyword("qualified-name")
	datafyFieldsKey    = atom.NewKeyword("fields")
	datafyDataKey      = atom.NewKeyword("data")
	datafyMetaKey      = atom.NewKeyword("meta")
	datafyTypeKey      = atom.NewKeyword("type")
)

var builtinsDatafy = []BuiltinDefinition{
	{
		Name:       "datafy-value*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("datafy-value*", args, 1); err != nil {
				return err
			}
			return datafyValue(args[0])
		}},
	},
}

func datafyValue(value types.Value) types.Value {
	if isNilArg(value) {
		return atom.NilValue
	}

	switch v := value.(type) {
	case *atom.Exception:
		return datafyException(v)
	case *coll.RecordDef:
		return datafyRecordDef(v)
	case *coll.Record:
		return datafyRecord(v)
	case *coll.DeftypeDef:
		return datafyTypeDef(v)
	case *coll.DeftypeInstance:
		return datafyTypeInstance(v)
	case *atom.GoObject:
		return datafyGoObject(v)
	default:
		return value
	}
}

func datafyException(exc *atom.Exception) types.Value {
	return canonicalExceptionMapValue(canonicalExceptionDataFromValue(exc))
}

func datafyRecordDef(def *coll.RecordDef) types.Value {
	pairs := definitionPairs(atom.NewKeyword("record-def"), def.Name, def.Fields, def.Metadata)
	return coll.NewMapFromPairs(pairs)
}

func datafyRecord(record *coll.Record) types.Value {
	pairs := nominalValuePairs(atom.NewKeyword("record"), record.Def.Name, record.Def.Metadata)
	pairs = append(pairs,
		datafyDataKey, datafyRecordData(record),
	)
	return coll.NewMapFromPairs(pairs)
}

func datafyTypeDef(def *coll.DeftypeDef) types.Value {
	pairs := definitionPairs(atom.NewKeyword("type-def"), def.Name, def.Fields, def.Metadata)
	return coll.NewMapFromPairs(pairs)
}

func datafyTypeInstance(instance *coll.DeftypeInstance) types.Value {
	pairs := nominalValuePairs(atom.NewKeyword("type-instance"), instance.Def.Name, instance.Def.Metadata)
	pairs = append(pairs,
		datafyDataKey, coll.NewMapFromPairs(typeInstancePairs(instance)),
	)
	return coll.NewMapFromPairs(pairs)
}

func definitionPairs(kind atom.Keyword, name string, fields []types.Value, metadata *coll.Map) []types.Value {
	pairs := []types.Value{
		datafyKindKey, kind,
		datafyNameKey, atom.NewString(name),
	}

	nsName, qualified := definitionIdentity(metadata, name)
	if nsName != "" {
		pairs = append(pairs, datafyNamespaceKey, atom.NewString(nsName))
	}
	if qualified != "" {
		pairs = append(pairs, datafyQualifiedKey, atom.NewString(qualified))
	}
	if fields != nil {
		pairs = append(pairs, datafyFieldsKey, coll.NewVector(fields...))
	}
	if metadata != nil {
		pairs = append(pairs, datafyMetaKey, datafyValue(metadata))
	}
	return pairs
}

func nominalValuePairs(kind atom.Keyword, name string, metadata *coll.Map) []types.Value {
	pairs := []types.Value{
		datafyKindKey, kind,
		datafyNameKey, atom.NewString(name),
	}

	nsName, qualified := definitionIdentity(metadata, name)
	if nsName != "" {
		pairs = append(pairs, datafyNamespaceKey, atom.NewString(nsName))
	}
	if qualified != "" {
		pairs = append(pairs, datafyQualifiedKey, atom.NewString(qualified))
	}
	return pairs
}

func datafyRecordData(record *coll.Record) types.Value {
	pairs := make([]types.Value, 0, len(record.Def.Fields)*2)
	for i, field := range record.Def.Fields {
		var value types.Value = atom.NilValue
		if i < len(record.Values) && !isNilArg(record.Values[i]) {
			value = datafyValue(record.Values[i])
		}
		pairs = append(pairs, field, value)
	}

	if record.Extras != nil {
		if seqValue := record.Extras.Seq(); seqValue != nil {
			if seq, ok := seqValue.(types.Seq); ok {
				for seq != nil {
					item := seq.First()
					entry, ok := item.(*types.MapEntry)
					if ok {
						pairs = append(pairs, entry.Key, datafyValue(entry.Value))
					}
					seq = seq.Next()
				}
			}
		}
	}

	return coll.NewMapFromPairs(pairs)
}

func typeInstancePairs(instance *coll.DeftypeInstance) []types.Value {
	pairs := make([]types.Value, 0, len(instance.Def.Fields)*2)
	for i, field := range instance.Def.Fields {
		var value types.Value = atom.NilValue
		if i < len(instance.Values) && !isNilArg(instance.Values[i]) {
			value = datafyValue(instance.Values[i])
		}
		pairs = append(pairs, field, value)
	}
	return pairs
}

func definitionIdentity(metadata *coll.Map, fallbackName string) (string, string) {
	if metadata == nil {
		return "", fallbackName
	}
	nsName, _ := types.SymbolName(metadata.Get(atom.NewKeyword("ns")))
	name, ok := types.SymbolName(metadata.Get(atom.NewKeyword("name")))
	if !ok || name == "" {
		name = fallbackName
	}
	if nsName == "" {
		return "", name
	}
	return nsName, nsName + "/" + name
}

func datafyGoObject(value *atom.GoObject) types.Value {
	typeName := "nil"
	if value.Val != nil {
		typeName = fmt.Sprintf("%T", value.Val)
	}
	return coll.NewMapFromPairs([]types.Value{
		datafyKindKey, atom.NewKeyword("go-object"),
		datafyTypeKey, atom.NewString(typeName),
	})
}
