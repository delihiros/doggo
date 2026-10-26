// builtins_atom.go は Atom（同期的状態管理）関連の組み込み関数を定義します。
//
// Atom は Clojure の atom に相当する同期的な状態コンテナです。
// CAS（Compare-And-Swap）ベースの更新により、スレッドセーフな状態変更を提供します。
//
// 提供する関数:
//   - 作成: atom
//   - 参照: deref, @（reader macro）
//   - 更新: swap!, reset!, compare-and-set!
//   - 監視: add-watch, remove-watch
//   - 述語: atom?
package vm

import (
	"context"
	"fmt"

	"doggo/internal/agent"
	stateatom "doggo/internal/atom"
	"doggo/internal/stm"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsAtom は Atom（同期的状態管理）関連の組み込み関数を定義します
var builtinsAtom = []BuiltinDefinition{
	{
		Name: "atom",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("atom", args, 1); err != nil {
				return err
			}
			return stateatom.NewAtom(args[0])
		}},
	},
	{
		Name: "atom?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("atom?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*stateatom.Atom)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "future-call",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("future-call", args, 1); err != nil {
				return nil, err
			}
			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("future-call: VM context is not available")
			}
			runtime := vm.snapshotCallableRuntime()
			if runtime == nil {
				return nil, fmt.Errorf("future-call: callable runtime is not initialized")
			}
			return newFuture(ctx, runtime, args[0]), nil
		}},
	},
	// deref は Atom/Future/Channel/Ref/Agent を扱う横断的な関数
	{
		Name: "deref",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("deref", args, 1); err != nil {
				return nil, err
			}
			// Atomの場合
			if a, ok := args[0].(*stateatom.Atom); ok {
				return a.Deref(), nil
			}
			if future, ok := args[0].(*futureValue); ok {
				return future.Deref(ctx)
			}
			// Channelの場合
			if ch, ok := args[0].(*types.Channel); ok {
				if ctx == nil {
					ctx = context.Background()
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case val, ok := <-ch.Value:
					if !ok {
						return atom.NilValue, nil
					}
					return val, nil
				}
			}
			// Refの場合（STM）
			if ref, ok := args[0].(*stm.Ref); ok {
				if tx := stm.GetTransaction(ctx); tx != nil {
					return tx.Deref(ref)
				}
				return ref.CurrentValue(), nil
			}
			// Agentの場合
			if a, ok := args[0].(*agent.Agent); ok {
				return a.State(), nil
			}
			// GoObject経由の blocking deref（process exit-ref など）
			if goObj, ok := args[0].(*atom.GoObject); ok {
				if derefable, ok := goObj.Unwrap().(contextDeref); ok {
					return derefable.DerefContext(ctx)
				}
				if derefable, ok := goObj.Unwrap().(blockingDeref); ok {
					return derefable.Deref(), nil
				}
			}
			// Varの場合（動的束縛を優先し、なければ root 値）
			if v, ok := args[0].(*atom.Var); ok {
				// 動的束縛を優先的に参照
				if boundVal, found := LookupBinding(ctx, v); found {
					return boundVal, nil
				}
				return v.Value, nil
			}
			return nil, newArgumentError("deref: expected atom, future, channel, ref, agent, go-object, or var, got %s", args[0].Type())
		}},
	},
	{
		Name: "reset!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("reset!", args, 2); err != nil {
				return nil, err
			}
			a, ok := args[0].(*stateatom.Atom)
			if !ok {
				return nil, fmt.Errorf("reset!: expected atom as first argument, got %s", args[0].Type())
			}
			newVal, err := a.ResetContext(ctx, args[1])
			if err != nil {
				return nil, fmt.Errorf("reset!: %s", err.Error())
			}
			return newVal, nil
		}},
	},
	{
		Name: "reset-vals!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("reset-vals!", args, 2); err != nil {
				return nil, err
			}
			a, ok := args[0].(*stateatom.Atom)
			if !ok {
				return nil, fmt.Errorf("reset-vals!: expected atom as first argument, got %s", args[0].Type())
			}
			oldVal, newVal, err := a.ResetValsContext(ctx, args[1])
			if err != nil {
				return nil, fmt.Errorf("reset-vals!: %s", err.Error())
			}
			return coll.NewVector(oldVal, newVal), nil
		}},
	},
	{
		Name: "compare-and-set!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("compare-and-set!", args, 3); err != nil {
				return nil, err
			}
			a, ok := args[0].(*stateatom.Atom)
			if !ok {
				return nil, fmt.Errorf("compare-and-set!: expected atom as first argument, got %s", args[0].Type())
			}
			success, err := a.CompareAndSet(ctx, args[1], args[2])
			if err != nil {
				return nil, fmt.Errorf("compare-and-set!: %s", err.Error())
			}
			return atom.Boolean(success), nil
		}},
	},
	{
		Name: "swap!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("swap!", args, 2); err != nil {
				return nil, err
			}
			a, ok := args[0].(*stateatom.Atom)
			if !ok {
				return nil, fmt.Errorf("swap!: expected atom as first argument, got %s", args[0].Type())
			}
			fn := args[1]
			extraArgs := args[2:]

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("swap!: VM not found in context")
			}

			updateFn := func(oldVal types.Value) (types.Value, error) {
				fnArgs := make([]types.Value, 0, 1+len(extraArgs))
				fnArgs = append(fnArgs, oldVal)
				fnArgs = append(fnArgs, extraArgs...)
				return ApplyCallableFast(ctx, vm, fn, fnArgs)
			}

			return a.Swap(ctx, updateFn)
		}},
	},
	{
		Name: "swap-vals!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("swap-vals!", args, 2); err != nil {
				return nil, err
			}
			a, ok := args[0].(*stateatom.Atom)
			if !ok {
				return nil, fmt.Errorf("swap-vals!: expected atom as first argument, got %s", args[0].Type())
			}
			fn := args[1]
			extraArgs := args[2:]

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("swap-vals!: VM not found in context")
			}

			updateFn := func(oldVal types.Value) (types.Value, error) {
				fnArgs := make([]types.Value, 0, 1+len(extraArgs))
				fnArgs = append(fnArgs, oldVal)
				fnArgs = append(fnArgs, extraArgs...)
				return ApplyCallableFast(ctx, vm, fn, fnArgs)
			}

			oldVal, newVal, err := a.SwapVals(ctx, updateFn)
			if err != nil {
				return nil, err
			}
			return coll.NewVector(oldVal, newVal), nil
		}},
	},
	// add-watch: ウォッチャーを追加（Atom, Agent, Ref をサポート）
	{
		Name: "add-watch",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("add-watch", args, 3); err != nil {
				return nil, err
			}
			target := args[0]
			key := args[1]
			fn := args[2]

			vm := GetVM(ctx)
			if vm == nil {
				return nil, RuntimeErr("add-watch", "VM not found in context")
			}
			runtime, err := requireCallableRuntimeSnapshot(vm, "add-watch")
			if err != nil {
				return nil, err
			}

			// 共通のウォッチャーラッパー
			createWatcher := func(targetObj types.Value) func(context.Context, types.Value, interface{}, types.Value, types.Value) error {
				return func(c context.Context, k types.Value, _ interface{}, oldVal, newVal types.Value) error {
					fnArgs := []types.Value{k, targetObj, oldVal, newVal}
					_, err := runtime.ApplyFast(c, fn, fnArgs)
					return err
				}
			}

			// Atomの場合
			if a, ok := target.(*stateatom.Atom); ok {
				w := createWatcher(a)
				atomW := func(c context.Context, k types.Value, _ *stateatom.Atom, o, n types.Value) error {
					return w(c, k, a, o, n)
				}
				if err := a.AddWatch(key, atomW); err != nil {
					return nil, RuntimeErrf("add-watch", "%v", err)
				}
				return target, nil
			}

			// Agentの場合
			if ag, ok := target.(*agent.Agent); ok {
				w := createWatcher(ag)
				agentW := func(c context.Context, k types.Value, _ *agent.Agent, o, n types.Value) error {
					return w(c, k, ag, o, n)
				}
				if err := ag.AddWatch(key, agentW); err != nil {
					return nil, RuntimeErrf("add-watch", "%v", err)
				}
				return target, nil
			}

			// Refの場合
			if r, ok := target.(*stm.Ref); ok {
				w := createWatcher(r)
				refW := func(c context.Context, k types.Value, _ *stm.Ref, o, n types.Value) error {
					return w(c, k, r, o, n)
				}
				if err := r.AddWatch(key, refW); err != nil {
					return nil, RuntimeErrf("add-watch", "%v", err)
				}
				return target, nil
			}

			return nil, newArgumentError("add-watch: expected atom, agent, or ref, got %s", target.Type())
		}},
	},
	// remove-watch: ウォッチャーを削除
	{
		Name: "remove-watch",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("remove-watch", args, 2); err != nil {
				return err
			}
			target := args[0]
			key := args[1]

			// Atomの場合
			if a, ok := target.(*stateatom.Atom); ok {
				if err := a.RemoveWatch(key); err != nil {
					return RuntimeErrf("remove-watch", "%v", err)
				}
				return target
			}

			// Agentの場合
			if ag, ok := target.(*agent.Agent); ok {
				if err := ag.RemoveWatch(key); err != nil {
					return RuntimeErrf("remove-watch", "%v", err)
				}
				return target
			}

			// Refの場合
			if r, ok := target.(*stm.Ref); ok {
				if err := r.RemoveWatch(key); err != nil {
					return RuntimeErrf("remove-watch", "%v", err)
				}
				return target
			}

			return newArgumentError("remove-watch: expected atom, agent, or ref, got %s", target.Type())
		}},
	},
}
