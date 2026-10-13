// builtins_record_type.go はレコード型と型定義の組み込み関数を定義します。
//
// レコードは Clojure の defrecord に相当する名前付きフィールドを持つ型を提供します。
// 型定義は deftype に相当するプロトコル実装を持つ型を提供します。
//
// 提供する関数:
//   - レコード: make-record-def, make-record-instance, record?
//   - 型: make-type-def, make-type-instance
package vm

import (
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var recordTypeBuiltins = []BuiltinDefinition{
	// ============================================================================
	// defrecord 用ビルトイン
	// ============================================================================

	// make-record-def はレコード定義を作成します。
	// (make-record-def name fields)
	// name: 文字列またはシンボル
	// fields: キーワードのベクター
	{
		Name: "make-record-def",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("make-record-def", args, 2); err != nil {
				return err
			}

			name, fields, errValue := recordTypeDefinitionArgs("make-record-def", args)
			if errValue != nil {
				return errValue
			}
			return coll.NewRecordDef(name, fields)
		}},
	},

	// make-record はレコードインスタンスを作成します（位置引数）。
	// (make-record def & values)
	{
		Name: "make-record",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgMin("make-record", args, 1); err != nil {
				return err
			}

			def, ok := args[0].(*coll.RecordDef)
			if !ok {
				return newArgumentError("make-record: first argument must be a record-def, got %s", args[0].Type())
			}

			values := args[1:]
			return coll.NewRecord(def, values)
		}},
	},

	// make-record-from-map はマップからレコードインスタンスを作成します。
	// (make-record-from-map def map)
	{
		Name: "make-record-from-map",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("make-record-from-map", args, 2); err != nil {
				return err
			}

			def, ok := args[0].(*coll.RecordDef)
			if !ok {
				return newArgumentError("make-record-from-map: first argument must be a record-def, got %s", args[0].Type())
			}

			m, ok := args[1].(types.Map)
			if !ok {
				return newArgumentError("make-record-from-map: second argument must be a map, got %s", args[1].Type())
			}

			return coll.NewRecordFromMap(def, m)
		}},
	},

	// record? はレコードインスタンスかどうかを判定します。
	{
		Name: "record?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("record?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*coll.Record)
			return atom.Boolean(ok)
		}},
	},

	// record-def? はレコード定義かどうかを判定します。
	{
		Name: "record-def?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("record-def?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*coll.RecordDef)
			return atom.Boolean(ok)
		}},
	},

	// ============================================================================
	// deftype 用ビルトイン
	// ============================================================================

	// make-type-def はdeftype定義を作成します。
	// (make-type-def name fields)
	{
		Name: "make-type-def",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("make-type-def", args, 2); err != nil {
				return err
			}

			name, fields, errValue := recordTypeDefinitionArgs("make-type-def", args)
			if errValue != nil {
				return errValue
			}
			return coll.NewDeftypeDef(name, fields)
		}},
	},

	// make-type-instance はdeftypeインスタンスを作成します（位置引数）。
	// (make-type-instance def & values)
	{
		Name: "make-type-instance",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgMin("make-type-instance", args, 1); err != nil {
				return err
			}

			def, ok := args[0].(*coll.DeftypeDef)
			if !ok {
				return newArgumentError("make-type-instance: first argument must be a type-def, got %s", args[0].Type())
			}

			values := args[1:]
			return coll.NewDeftypeInstance(def, values)
		}},
	},

	// type-instance? はdeftypeインスタンスかどうかを判定します。
	{
		Name: "type-instance?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("type-instance?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*coll.DeftypeInstance)
			return atom.Boolean(ok)
		}},
	},

	// type-def? はdeftype定義かどうかを判定します。
	{
		Name: "type-def?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("type-def?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*coll.DeftypeDef)
			return atom.Boolean(ok)
		}},
	},

	// get-field はdeftypeインスタンスのフィールド値を取得します（内部用）。
	// (get-field instance field-name)
	{
		Name: "get-field",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("get-field", args, 2); err != nil {
				return err
			}

			inst, ok := args[0].(*coll.DeftypeInstance)
			if !ok {
				return newArgumentError("get-field: first argument must be a type-instance, got %s", args[0].Type())
			}

			return inst.GetFieldByName(args[1])
		}},
	},
}

func recordTypeDefinitionArgs(fnName string, args []types.Value) (string, []types.Value, types.Value) {
	var name string
	switch n := args[0].(type) {
	case atom.String:
		name = string(n)
	case types.Symbol:
		name = string(n)
	case *types.SymbolWithMeta:
		name = n.Name
	default:
		return "", nil, newArgumentError("%s: name must be a string or symbol, got %s", fnName, args[0].Type())
	}

	var fields []types.Value
	switch f := args[1].(type) {
	case *coll.Vector:
		fields = append(fields, f.Items...)
	case *coll.List:
		fields = append(fields, f.Items...)
	default:
		return "", nil, newArgumentError("%s: fields must be a vector or list, got %s", fnName, args[1].Type())
	}
	return name, fields, nil
}
