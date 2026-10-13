// builtins_stm.go は STM（Software Transactional Memory）関連の組み込み関数を定義します。
//
// STM は Clojure の ref/dosync に相当するトランザクショナルな状態管理を提供します。
// 複数の ref を一貫性を保って更新できます。
//
// 提供する関数:
//   - 作成: ref
//   - 参照: deref
//   - トランザクション内更新: alter, ref-set, ensure
//   - 述語: ref?
package vm

import (
	"context"
	"fmt"

	"doggo/internal/stm"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

// builtinsSTM は STM（Software Transactional Memory）関連の組み込み関数を定義します
var builtinsSTM = []BuiltinDefinition{
	{
		Name: "ref",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("ref", args, 1); err != nil {
				return err
			}
			return stm.NewRef(args[0])
		}},
	},
	{
		Name: "ref?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("ref?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*stm.Ref)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "ref-set",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("ref-set", args, 2); err != nil {
				return nil, err
			}
			ref, ok := args[0].(*stm.Ref)
			if !ok {
				return nil, newArgumentError("ref-set: first argument must be a ref, got %s", args[0].Type())
			}
			tx := stm.GetTransaction(ctx)
			if tx == nil {
				return nil, fmt.Errorf("ref-set: must be called inside dosync")
			}
			if err := tx.Set(ref, args[1]); err != nil {
				return nil, err
			}
			return args[1], nil
		}},
	},
	{
		Name: "alter",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("alter", args, 2); err != nil {
				return nil, err
			}
			ref, ok := args[0].(*stm.Ref)
			if !ok {
				return nil, newArgumentError("alter: first argument must be a ref, got %s", args[0].Type())
			}
			fn := args[1]
			extraArgs := args[2:]

			tx := stm.GetTransaction(ctx)
			if tx == nil {
				return nil, fmt.Errorf("alter: must be called inside dosync")
			}

			currentVal, err := tx.Deref(ref)
			if err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("alter: VM not found in context")
			}

			fnArgs := make([]types.Value, 1+len(extraArgs))
			fnArgs[0] = currentVal
			copy(fnArgs[1:], extraArgs)

			newVal, err := ApplyCallableFast(ctx, vm, fn, fnArgs)
			if err != nil {
				return nil, fmt.Errorf("alter: %w", err)
			}

			if err := tx.Alter(ref, newVal); err != nil {
				return nil, err
			}

			return newVal, nil
		}},
	},
	{
		Name: "ensure",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("ensure", args, 1); err != nil {
				return nil, err
			}
			ref, ok := args[0].(*stm.Ref)
			if !ok {
				return nil, newArgumentError("ensure: argument must be a ref, got %s", args[0].Type())
			}
			tx := stm.GetTransaction(ctx)
			if tx == nil {
				return nil, fmt.Errorf("ensure: must be called inside dosync")
			}
			if err := tx.Ensure(ref); err != nil {
				return nil, err
			}
			return ref.CurrentValue(), nil
		}},
	},
}
