// builtins_metadata.go はメタデータ操作の組み込み関数を定義します。
//
// メタデータは Clojure の ^{:doc "..."} 形式で値に付加できる情報です。
// Vector, Map, Set, List, Symbol などの型がメタデータをサポートします。
//
// 提供する関数:
//   - 取得: meta
//   - 設定: with-meta
package vm

import (
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsMetadata はメタデータ操作の組み込み関数を定義します
var builtinsMetadata = []BuiltinDefinition{
	// meta - 値のメタデータを取得
	// Clojure互換: Vector, Map, Set, Var, Symbol, List をサポート
	{
		Name: "meta",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("meta", args, 1); err != nil {
				return err
			}

			val := args[0]
			emptyMap := coll.NewEmptyMap()

			switch v := val.(type) {
			case *coll.Vector:
				if v.Metadata != nil {
					return v.Metadata
				}
				return emptyMap
			case *coll.Map:
				if v.Metadata != nil {
					return v.Metadata
				}
				return emptyMap
			case *coll.Set:
				if v.Metadata != nil {
					return v.Metadata
				}
				return emptyMap
			case *atom.Var:
				if v.Metadata != nil {
					return v.Metadata
				}
				return emptyMap
			case *types.SymbolWithMeta:
				if v.Meta != nil {
					// types.Map インターフェースを *coll.Map に変換
					if m, ok := v.Meta.(*coll.Map); ok {
						return m
					}
				}
				return emptyMap
			case *types.ResolvedSymbol:
				if v.Meta != nil {
					if m, ok := v.Meta.(*coll.Map); ok {
						return m
					}
				}
				return emptyMap
			case types.Symbol:
				// 通常のシンボルはメタデータを持たない
				return emptyMap
			case *coll.List:
				if v.Metadata != nil {
					return v.Metadata
				}
				return emptyMap
			case *coll.MetaSeq:
				if v.Metadata != nil {
					return v.Metadata
				}
				return emptyMap
			case *coll.RecordDef:
				if v.Metadata != nil {
					return v.Metadata
				}
				return emptyMap
			case *coll.DeftypeDef:
				if v.Metadata != nil {
					return v.Metadata
				}
				return emptyMap
			default:
				return emptyMap
			}
		}},
	},
	// with-meta - メタデータを付けた値のコピーを返す
	{
		Name: "with-meta",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("with-meta", args, 2); err != nil {
				return err
			}

			val := args[0]
			metaVal := args[1]

			metaMap, ok := metaVal.(*coll.Map)
			if !ok {
				return newArgumentError("with-meta expects a map as second argument, got %s", metaVal.Type())
			}

			withMeta, err := coll.CopyWithMetadata(val, metaMap)
			if err != nil {
				return newArgumentError("with-meta: %v", err)
			}
			return withMeta
		}},
	},
}
