// builtins_multimethod.go はマルチメソッド関連の組み込み関数を定義します。
//
// マルチメソッドは Clojure の defmulti/defmethod に相当するランタイム多態を提供します。
// ディスパッチ関数の戻り値に基づいて適切なメソッドが選択されます。
//
// 提供する関数:
//   - 作成: create-multimethod
//   - メソッド追加: add-method
//   - デフォルト設定: prefer-method
//   - 情報取得: methods, get-method
package vm

import (
	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/fun"
)

// builtinsMultimethod はmultimethod関連の組み込み関数を定義します
var builtinsMultimethod = []BuiltinDefinition{
	// create-multimethod - 新しい Multimethod を作成
	// (create-multimethod name dispatch-fn)
	{
		Name: "create-multimethod",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("create-multimethod", args, 2); err != nil {
				return err
			}

			// 第1引数: 名前（文字列）
			var name string
			switch n := args[0].(type) {
			case types.Symbol:
				name = string(n)
			case *types.SymbolWithMeta:
				name = n.Name
			default:
				name = args[0].String()
			}

			// 第2引数: dispatch function（types.Fn または *compiler.Closure）
			dispatchFn := args[1]
			if !isCallable(dispatchFn) {
				return newArgumentError("create-multimethod: dispatch must be a function, got %s", dispatchFn.Type())
			}

			return fun.NewMultimethod(name, dispatchFn)
		}},
	},
	// add-method - Multimethod に method を追加
	// (add-method multimethod dispatch-val method-fn)
	{
		Name: "add-method",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("add-method", args, 3); err != nil {
				return err
			}

			// 第1引数: Multimethod
			mm, ok := args[0].(*fun.Multimethod)
			if !ok {
				return newArgumentError("add-method: first argument must be a multimethod, got %s", args[0].Type())
			}

			// 第2引数: dispatch value
			dispatchVal := args[1]

			// 第3引数: method function（types.Fn または *compiler.Closure）
			methodFn := args[2]
			if !isCallable(methodFn) {
				return newArgumentError("add-method: method must be a function, got %s", methodFn.Type())
			}

			// method を追加（Multimethod 自体は mutable なので同じインスタンスを返す）
			mm.AddMethod(dispatchVal, methodFn)
			return mm
		}},
	},
	// multimethod? - 値が Multimethod かどうかを判定
	{
		Name: "multimethod?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("multimethod?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*fun.Multimethod)
			return atom.Boolean(ok)
		}},
	},
	// methods - Multimethod の全 method を返す
	{
		Name: "methods",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("methods", args, 1); err != nil {
				return err
			}
			mm, ok := args[0].(*fun.Multimethod)
			if !ok {
				return newArgumentError("methods: argument must be a multimethod, got %s", args[0].Type())
			}
			return mm.Methods
		}},
	},
	// get-method - 特定の dispatch value に対応する method を取得
	{
		Name: "get-method",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("get-method", args, 2); err != nil {
				return err
			}
			mm, ok := args[0].(*fun.Multimethod)
			if !ok {
				return newArgumentError("get-method: first argument must be a multimethod, got %s", args[0].Type())
			}
			method := mm.GetMethod(args[1])
			if method == nil {
				return atom.NilValue
			}
			return method
		}},
	},
}

// isCallable は値が呼び出し可能かどうかを判定します
// types.Fn, *compiler.Closure, *Builtin, *fun.Multimethod が呼び出し可能
func isCallable(v types.Value) bool {
	switch v.(type) {
	case types.Fn, *compiler.Closure, *Builtin, *fun.Multimethod:
		return true
	default:
		return false
	}
}
