// builtins_transducer.go はトランスデューサ関連の組み込み関数を定義します。
//
// トランスデューサは中間データ構造を作成せずにコレクション変換を合成する仕組みです。
// (map inc) や (filter even?) のように1引数で呼び出すとトランスデューサを返します。
//
// 提供する関数:
//   - 合成: comp
//   - 適用: transduce, into
//   - 早期終了: reduced, reduced?
//   - 実行: run!
package vm

import (
	"context"
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

// builtinsTransducer はトランスデューサ関連の組み込み関数を定義
var builtinsTransducer = []BuiltinDefinition{
	// comp - 関数またはトランスデューサを合成
	// トランスデューサが含まれる場合はトランスデューサを返す
	// それ以外は関数を返す
	{
		Name: "comp",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if len(args) == 0 {
				// (comp) -> identity function
				return &Builtin{Fn: func(innerArgs ...types.Value) types.Value {
					if err := CheckArgCount("identity", innerArgs, 1); err != nil {
						return err
					}
					return innerArgs[0]
				}}, nil
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("comp: VM not available in context")
			}

			// すべてがトランスデューサかチェック
			var transducers []*types.Transducer
			allTransducers := true
			for _, arg := range args {
				if t, ok := arg.(*types.Transducer); ok {
					transducers = append(transducers, t)
				} else {
					allTransducers = false
					break
				}
			}

			// すべてがトランスデューサの場合はトランスデューサを合成
			if allTransducers && len(transducers) > 0 {
				return types.ComposeTransducers(transducers...), nil
			}
			runtime, err := requireCallableRuntimeSnapshot(vm, "comp")
			if err != nil {
				return nil, err
			}
			fns := append([]types.Value(nil), args...)

			// それ以外は関数として合成
			// 右から左へ適用: (comp f g h) x => f(g(h(x)))
			return &Builtin{CtxFn: func(ctx context.Context, innerArgs ...types.Value) (types.Value, error) {
				if len(innerArgs) == 0 {
					return nil, fmt.Errorf("composed function requires at least one argument")
				}

				result, err := runtime.ApplyFast(ctx, fns[len(fns)-1], innerArgs)
				if err != nil {
					return nil, err
				}
				for i := len(fns) - 2; i >= 0; i-- {
					result, err = runtime.ApplyFast(ctx, fns[i], []types.Value{result})
					if err != nil {
						return nil, err
					}
				}
				return result, nil
			}}, nil
		}},
	},
	// transduce - トランスデューサを使ってコレクションを変換
	// (transduce xform f init coll)
	{
		Name: "transduce",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("transduce", args, 4); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("transduce: VM not available in context")
			}

			xform, ok := args[0].(*types.Transducer)
			if !ok {
				return nil, newArgumentError("transduce: first argument must be a transducer, got %s", args[0].Type())
			}

			reduceFn := args[1]
			init := args[2]
			collArg := args[3]

			// ベースの還元関数を作成
			baseRf := func(ctx context.Context, acc, input types.Value) (types.Value, error) {
				return ApplyCallableFast(ctx, vm, reduceFn, []types.Value{acc, input})
			}

			// トランスデューサを適用
			xf := xform.Apply(baseRf)

			return reduceCollection(collArg, "transduce", init, func(acc, item types.Value) (types.Value, error) {
				return xf(ctx, acc, item)
			})
		}},
	},
	// into - トランスデューサを使ってコレクションに変換
	// (into to xform from) または (into to from)
	{
		Name: "into",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("into", args, 2, 3); err != nil {
				return nil, err
			}

			to := args[0]

			if len(args) == 2 {
				// (into to from) - トランスデューサなし
				from := args[1]
				return intoWithoutTransducer(to, from)
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("into: VM not available in context")
			}

			// (into to xform from)
			xform, ok := args[1].(*types.Transducer)
			if !ok {
				return nil, newArgumentError("into: second argument must be a transducer, got %s", args[1].Type())
			}
			from := args[2]

			return intoWithTransducer(ctx, to, xform, from)
		}},
	},
	// reduced - 早期終了をマーク
	{
		Name: "reduced",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("reduced", args, 1); err != nil {
				return err
			}
			return &types.Reduced{Val: args[0]}
		}},
	},
	// reduced? - Reduced かどうかをチェック
	{
		Name: "reduced?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("reduced?", args, 1); err != nil {
				return err
			}
			return atom.Boolean(types.IsReduced(args[0]))
		}},
	},
	// unreduced - Reduced をアンラップ
	{
		Name: "unreduced",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("unreduced", args, 1); err != nil {
				return err
			}
			return types.Unreduced(args[0])
		}},
	},
}

// intoWithoutTransducer はトランスデューサなしで into を実行
func intoWithoutTransducer(to, from types.Value) (types.Value, error) {
	return reduceCollection(from, "into", to, conjValue)
}

// intoWithTransducer はトランスデューサを使って into を実行
func intoWithTransducer(ctx context.Context, to types.Value, xform *types.Transducer, from types.Value) (types.Value, error) {
	// conj を還元関数として使用
	baseRf := func(ctx context.Context, acc, input types.Value) (types.Value, error) {
		return conjValue(acc, input)
	}

	// トランスデューサを適用
	xf := xform.Apply(baseRf)

	return reduceCollection(from, "into", to, func(acc, item types.Value) (types.Value, error) {
		return xf(ctx, acc, item)
	})
}

// reduceCollection は seq coercion した入力を reducing step で畳み込みます。
// step が Reduced を返した場合はそこで停止し、最終結果は常に Unreduced で返します。
func reduceCollection(collArg types.Value, fnName string, init types.Value, step func(acc, item types.Value) (types.Value, error)) (types.Value, error) {
	acc := init
	iterErr := iterateCollection(collArg, fnName, func(item types.Value) (bool, error) {
		if types.IsReduced(acc) {
			return false, nil
		}

		result, err := step(acc, item)
		if err != nil {
			return false, err
		}
		acc = result

		if types.IsReduced(acc) {
			return false, nil
		}
		return true, nil
	})
	if iterErr != nil {
		return nil, iterErr
	}
	return types.Unreduced(acc), nil
}

// iterateCollection は seq coercion を通して各要素に対して関数を呼び出す。
// callback が false を返すと早期終了し、error はそのまま呼び出し元へ伝播します。
func iterateCollection(collArg types.Value, fnName string, fn func(types.Value) (bool, error)) error {
	seq, err := asSeq(collArg, fnName)
	if err != nil {
		return err
	}
	for seq != nil {
		if err := sequenceEvalError(seq); err != nil {
			return err
		}
		item := seq.First()
		if item == nil {
			break
		}
		keepGoing, err := fn(item)
		if err != nil {
			return err
		}
		if !keepGoing {
			break
		}
		seq = seq.Next()
	}
	return nil
}

// MakeMapTransducer は map トランスデューサを作成
func MakeMapTransducer(ctx context.Context, runtime CallableRuntime, f types.Value) *types.Transducer {
	return types.NewTransducer("map", func(rf types.ReducingFn) types.ReducingFn {
		return func(ctx context.Context, acc, input types.Value) (types.Value, error) {
			// f を input に適用
			mapped, err := runtime.ApplyFast(ctx, f, []types.Value{input})
			if err != nil {
				return nil, err
			}
			// 変換後の値で還元関数を呼び出す
			return rf(ctx, acc, mapped)
		}
	})
}

// MakeFilterTransducer は filter トランスデューサを作成
func MakeFilterTransducer(ctx context.Context, runtime CallableRuntime, pred types.Value) *types.Transducer {
	return types.NewTransducer("filter", func(rf types.ReducingFn) types.ReducingFn {
		return func(ctx context.Context, acc, input types.Value) (types.Value, error) {
			// pred を input に適用
			result, err := runtime.ApplyFast(ctx, pred, []types.Value{input})
			if err != nil {
				return nil, err
			}
			// truthy なら還元関数を呼び出す
			if isTruthy(result) {
				return rf(ctx, acc, input)
			}
			// falsy ならアキュムレータをそのまま返す
			return acc, nil
		}
	})
}

// MakeTakeTransducer は take トランスデューサを作成
func MakeTakeTransducer(n int) *types.Transducer {
	return types.NewTransducer("take", func(rf types.ReducingFn) types.ReducingFn {
		count := 0
		return func(ctx context.Context, acc, input types.Value) (types.Value, error) {
			if count >= n {
				return &types.Reduced{Val: acc}, nil
			}
			count++
			result, err := rf(ctx, acc, input)
			if err != nil || types.IsReduced(result) || count < n {
				return result, err
			}
			return &types.Reduced{Val: result}, nil
		}
	})
}

// MakeDropTransducer は drop トランスデューサを作成
func MakeDropTransducer(n int) *types.Transducer {
	return types.NewTransducer("drop", func(rf types.ReducingFn) types.ReducingFn {
		count := 0
		return func(ctx context.Context, acc, input types.Value) (types.Value, error) {
			if count < n {
				count++
				return acc, nil
			}
			return rf(ctx, acc, input)
		}
	})
}
