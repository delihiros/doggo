// builtins_binding.go は動的束縛関連の組み込み関数を定義します。
//
// 動的束縛は Clojure の binding に相当する実行コンテキスト固有の値を提供します。
// ^:dynamic メタデータを持つ Var に対して一時的な値を設定できます。
//
// 提供する関数:
//   - 束縛操作: push-dynamic-bindings, pop-dynamic-bindings
//   - Var 操作: dynamically-bound?
package vm

import (
	"context"
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsBinding は動的束縛関連の組み込み関数を定義します
var builtinsBinding = []BuiltinDefinition{
	// push-dynamic-bindings - 新しい束縛フレームをプッシュ
	// 引数: マップ（キー: Var, 値: 束縛値）
	// 各キーの Var は ^:dynamic メタデータを持っている必要がある
	{
		Name: "push-dynamic-bindings",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("push-dynamic-bindings", args, 1); err != nil {
				return nil, err
			}

			bindingMap, ok := args[0].(*coll.Map)
			if !ok {
				return nil, newArgumentError("push-dynamic-bindings requires a map, got %s", args[0].Type())
			}

			// Var -> Value のマップを構築
			bindings := make(map[*atom.Var]types.Value)
			var validationErr error

			bindingMap.Range(func(k, v types.Value) bool {
				varObj, ok := k.(*atom.Var)
				if !ok {
					validationErr = fmt.Errorf("push-dynamic-bindings: key must be a Var, got %s", k.Type())
					return false
				}

				// ^:dynamic チェック
				if !IsDynamicVar(varObj) {
					//lint:ignore ST1005 language-facing diagnostic intentionally names Var
					validationErr = fmt.Errorf("push-dynamic-bindings: Var %s is not dynamic. Only ^:dynamic vars can be bound.", varObj.Name)
					return false
				}

				bindings[varObj] = v
				return true
			})

			if validationErr != nil {
				return nil, validationErr
			}

			PushDynamicBindings(ctx, bindings)

			return atom.NilValue, nil
		}},
	},
	// pop-dynamic-bindings - 最新の束縛フレームをポップ
	{
		Name: "pop-dynamic-bindings",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("pop-dynamic-bindings", args, 0); err != nil {
				return nil, err
			}

			PopDynamicBindings(ctx)

			return atom.NilValue, nil
		}},
	},
	// dynamically-bound? - Var が動的に束縛されているか（root 値ではなく）
	{
		Name: "dynamically-bound?",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("dynamically-bound?", args, 1); err != nil {
				return nil, err
			}

			varObj, ok := args[0].(*atom.Var)
			if !ok {
				return nil, newArgumentError("dynamically-bound? requires a Var, got %s", args[0].Type())
			}

			_, found := LookupBinding(ctx, varObj)
			return atom.Boolean(found), nil
		}},
	},
	// var? - 値がVarかどうかをチェック
	{
		Name: "var?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("var?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*atom.Var)
			return atom.Boolean(ok)
		}},
	},
}
