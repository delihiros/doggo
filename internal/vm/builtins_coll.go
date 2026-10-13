// builtins_coll.go はコレクションアクセス操作の組み込み関数を定義します。
// get, assoc, dissoc, keys, vals, contains?, find, key, val, nth をサポートします。
package vm

import (
	"doggo/internal/textutil"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsColl はコレクションアクセス操作の組み込み関数を定義します
var builtinsColl = []BuiltinDefinition{
	{
		Name: "get",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgRange("get", args, 2, 3); err != nil {
				return err
			}
			key := args[1]
			var defaultVal types.Value = atom.NilValue
			if len(args) == 3 {
				defaultVal = args[2]
			}

			switch c := args[0].(type) {
			case *coll.Map:
				entry, lookupErr := c.FindResult(key)
				if lookupErr != nil {
					return newArgumentError("%v", lookupErr)
				}
				if entry == nil {
					return defaultVal
				}
				return entry.Value
			case *coll.Record:
				entry, lookupErr := c.FindResult(key)
				if lookupErr != nil {
					return newArgumentError("%v", lookupErr)
				}
				if entry == nil {
					return defaultVal
				}
				return entry.Value
			case *coll.Vector:
				index, ok := integerForOperation(key)
				if !ok {
					return defaultVal
				}
				i := int(index)
				if i < 0 || i >= len(c.Items) {
					return defaultVal
				}
				return c.Items[i]
			case types.Nil:
				return defaultVal
			default:
				return newArgumentError("get: unsupported collection type %s", args[0].Type())
			}
		}},
	},
	{
		Name: "assoc",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgMin("assoc", args, 3); err != nil {
				return err
			}
			if (len(args)-1)%2 != 0 {
				return newArgumentError("assoc expects an odd number of arguments (coll + key-value pairs)")
			}

			switch c := args[0].(type) {
			case *coll.Map:
				var result types.Map = c
				for i := 1; i < len(args); i += 2 {
					result = result.Assoc(args[i], args[i+1])
				}
				return result
			case *coll.Record:
				var result types.Map = c
				for i := 1; i < len(args); i += 2 {
					result = result.Assoc(args[i], args[i+1])
				}
				return result
			case *coll.Vector:
				newItems := make([]types.Value, len(c.Items))
				copy(newItems, c.Items)
				for i := 1; i < len(args); i += 2 {
					indexValue, ok := integerForOperation(args[i])
					if !ok {
						return newArgumentError("assoc: vector index must be a number, got %s", args[i].Type())
					}
					index := int(indexValue)
					if index < 0 || index > len(newItems) {
						return newArgumentError("assoc: index out of bounds: %d", index)
					}
					if index == len(newItems) {
						newItems = append(newItems, args[i+1])
					} else {
						newItems[index] = args[i+1]
					}
				}
				return coll.NewVector(newItems...)
			case types.Nil:
				var result types.Map = coll.NewMap(nil)
				for i := 1; i < len(args); i += 2 {
					result = result.Assoc(args[i], args[i+1])
				}
				return result
			default:
				return newArgumentError("assoc: unsupported collection type %s", args[0].Type())
			}
		}},
	},
	{
		// hash-map - キーと値のペアからマップを作成
		// (hash-map k1 v1 k2 v2 ...) => {k1 v1, k2 v2, ...}
		Name: "hash-map",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if len(args)%2 != 0 {
				return newArgumentError("hash-map requires an even number of arguments (key-value pairs), got %d", len(args))
			}
			return coll.NewMapFromPairs(args)
		}},
	},
	{
		Name: "dissoc",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgMin("dissoc", args, 2); err != nil {
				return err
			}

			switch c := args[0].(type) {
			case *coll.Map:
				var result types.Map = c
				for i := 1; i < len(args); i++ {
					result = result.Dissoc(args[i])
				}
				return result
			case *coll.Record:
				var result types.Map = c
				for i := 1; i < len(args); i++ {
					result = result.Dissoc(args[i])
				}
				return result
			case types.Nil:
				return atom.NilValue
			default:
				return newArgumentError("dissoc: unsupported collection type %s", args[0].Type())
			}
		}},
	},
	{
		Name: "keys",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("keys", args, 1); err != nil {
				return err
			}

			return mapSequableBuiltinValue("keys", args[0], func(m types.Map) types.Sequable {
				return m.Keys()
			})
		}},
	},
	{
		Name: "vals",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("vals", args, 1); err != nil {
				return err
			}

			return mapSequableBuiltinValue("vals", args[0], func(m types.Map) types.Sequable {
				return m.Vals()
			})
		}},
	},
	{
		Name: "contains?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("contains?", args, 2); err != nil {
				return err
			}

			switch c := args[0].(type) {
			case *coll.Map:
				entry, lookupErr := c.FindResult(args[1])
				if lookupErr != nil {
					return newArgumentError("%v", lookupErr)
				}
				return atom.Boolean(entry != nil)
			case *coll.Record:
				contains, lookupErr := c.ContainsKeyResult(args[1])
				if lookupErr != nil {
					return newArgumentError("%v", lookupErr)
				}
				return atom.Boolean(contains)
			case *coll.Vector:
				index, ok := integerForOperation(args[1])
				if !ok {
					return atom.False
				}
				i := int(index)
				return atom.Boolean(i >= 0 && i < len(c.Items))
			case *coll.Set:
				value, lookupErr := c.GetResult(args[1])
				if lookupErr != nil {
					return newArgumentError("%v", lookupErr)
				}
				return atom.Boolean(value != nil)
			case types.Nil:
				return atom.False
			default:
				return newArgumentError("contains?: unsupported collection type %s", args[0].Type())
			}
		}},
	},
	{
		Name: "find",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("find", args, 2); err != nil {
				return err
			}
			switch m := args[0].(type) {
			case *coll.Map:
				entry, lookupErr := m.FindResult(args[1])
				if lookupErr != nil {
					return newArgumentError("%v", lookupErr)
				}
				if entry == nil {
					return atom.NilValue
				}
				return entry
			case *coll.Record:
				entry, lookupErr := m.FindResult(args[1])
				if lookupErr != nil {
					return newArgumentError("%v", lookupErr)
				}
				if entry == nil {
					return atom.NilValue
				}
				return entry
			default:
				return newArgumentError("find: expected map as first argument, got %s", args[0].Type())
			}
		}},
	},
	{
		Name: "key",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("key", args, 1); err != nil {
				return err
			}
			entry, ok := args[0].(*types.MapEntry)
			if !ok {
				return newArgumentError("key: expected map-entry, got %s", args[0].Type())
			}
			return entry.Key
		}},
	},
	{
		Name: "val",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("val", args, 1); err != nil {
				return err
			}
			entry, ok := args[0].(*types.MapEntry)
			if !ok {
				return newArgumentError("val: expected map-entry, got %s", args[0].Type())
			}
			return entry.Value
		}},
	},
	// nth - インデックスで要素を取得（Vector/List は O(1)、Seq は O(n)）
	{
		Name: "nth",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgRange("nth", args, 2, 3); err != nil {
				return err
			}

			collArg := args[0]
			index, ok := integerForOperation(args[1])
			if !ok {
				return newArgumentError("nth: index must be a number, got %s", args[1].Type())
			}
			n := int(index)
			if n < 0 {
				return newArgumentError("nth: index must be non-negative, got %d", n)
			}

			// デフォルト値（オプション）
			var notFound types.Value = nil
			if len(args) == 3 {
				notFound = args[2]
			}

			switch c := collArg.(type) {
			case *coll.Vector:
				// O(1) アクセス
				if n >= len(c.Items) {
					if notFound != nil {
						return notFound
					}
					return newArgumentError("nth: index %d out of bounds for vector of length %d", n, len(c.Items))
				}
				return c.Items[n]
			case *coll.List:
				// O(1) アクセス
				if n >= len(c.Items) {
					if notFound != nil {
						return notFound
					}
					return newArgumentError("nth: index %d out of bounds for list of length %d", n, len(c.Items))
				}
				return c.Items[n]
			case atom.String:
				length := textutil.CodePointLen(string(c))
				if n >= length {
					if notFound != nil {
						return notFound
					}
					return newArgumentError("nth: index %d out of bounds for string of length %d", n, length)
				}
				item, ok := textutil.CodePointAt(string(c), n)
				if !ok {
					return newArgumentError("nth: index %d out of bounds for string of length %d", n, length)
				}
				return atom.NewCharacter(item)
			case *types.MapEntry, types.Seq:
				seq, _ := coll.SequentialSeq(collArg)
				for i := 0; i < n; i++ {
					if err := sequenceEvalError(seq); err != nil {
						return newArgumentError("%v", err)
					}
					seq = seq.Next()
					if seq == nil {
						if notFound != nil {
							return notFound
						}
						return newArgumentError("nth: index %d out of bounds", n)
					}
				}
				if err := sequenceEvalError(seq); err != nil {
					return newArgumentError("%v", err)
				}
				first := seq.First()
				if first == nil {
					if notFound != nil {
						return notFound
					}
					return newArgumentError("nth: index %d out of bounds", n)
				}
				return first
			case types.Nil:
				if notFound != nil {
					return notFound
				}
				return newArgumentError("nth: cannot get element from nil")
			default:
				return newArgumentError("nth: unsupported collection type %s", collArg.Type())
			}
		}},
	},
}

func mapSequableBuiltinValue(fnName string, value types.Value, sequence func(types.Map) types.Sequable) types.Value {
	switch c := value.(type) {
	case *coll.Map:
		return sequableBuiltinValue(sequence(c))
	case *coll.Record:
		return sequableBuiltinValue(sequence(c))
	case types.Nil:
		return coll.NewList()
	default:
		return newArgumentError("%s: unsupported collection type %s", fnName, value.Type())
	}
}

func sequableBuiltinValue(seq types.Sequable) types.Value {
	if seq == nil {
		return coll.NewList()
	}
	return seq.(types.Value)
}
