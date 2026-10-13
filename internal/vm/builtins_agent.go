// builtins_agent.go は Agent（非同期状態管理）関連の組み込み関数を定義します。
//
// Agent は Clojure の agent に相当する非同期状態コンテナです。
// 状態の更新は別の goroutine で非同期に実行されます。
//
// 提供する関数:
//   - 作成: agent
//   - 参照: deref
//   - 更新: send
//   - 待機: await, await-for
//   - 述語: agent?
//   - エラー: agent-error
package vm

import (
	"context"
	"fmt"
	"time"

	"doggo/internal/agent"
	"doggo/internal/stm"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

// builtinsAgent は Agent（非同期状態管理）関連の組み込み関数を定義します
var builtinsAgent = []BuiltinDefinition{
	{
		Name: "agent",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("agent", args, 1); err != nil {
				return err
			}
			return agent.NewAgent(args[0])
		}},
	},
	{
		Name: "agent?",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("agent?", args, 1); err != nil {
				return err
			}
			_, ok := args[0].(*agent.Agent)
			return atom.Boolean(ok)
		}},
	},
	{
		Name: "agent-error",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("agent-error", args, 1); err != nil {
				return err
			}
			a, ok := args[0].(*agent.Agent)
			if !ok {
				return newArgumentError("agent-error: argument must be an agent, got %s", args[0].Type())
			}
			err := a.CurrentError()
			if err == nil {
				return atom.NilValue
			}
			return atom.NewString(err.Error())
		}},
	},
	{
		Name: "restart-agent",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgMin("restart-agent", args, 2); err != nil {
				return err
			}
			a, ok := args[0].(*agent.Agent)
			if !ok {
				return newArgumentError("restart-agent: first argument must be an agent, got %s", args[0].Type())
			}
			newState := args[1]
			clearActions := false
			options := args[2:]
			if len(options)%2 != 0 {
				return newArgumentError("restart-agent: options must be option key-value pairs")
			}
			for i := 0; i < len(options); i += 2 {
				kw, ok := options[i].(atom.Keyword)
				if !ok {
					return newArgumentError("restart-agent: option name must be a keyword, got %s", options[i].Type())
				}
				if string(kw) != "clear-actions" {
					return newArgumentError("restart-agent: unsupported option %s", kw.String())
				}
				value, ok := options[i+1].(atom.Boolean)
				if !ok {
					return newArgumentError("restart-agent: :clear-actions must be boolean, got %s", options[i+1].Type())
				}
				clearActions = bool(value)
			}
			if err := a.Restart(newState, clearActions); err != nil {
				return RuntimeErr("restart-agent", err.Error())
			}
			return a
		}},
	},
	{
		Name: "set-error-mode!",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("set-error-mode!", args, 2); err != nil {
				return err
			}
			a, ok := args[0].(*agent.Agent)
			if !ok {
				return newArgumentError("set-error-mode!: first argument must be an agent, got %s", args[0].Type())
			}
			kw, ok := args[1].(atom.Keyword)
			if !ok {
				return newArgumentError("set-error-mode!: second argument must be a keyword, got %s", args[1].Type())
			}
			switch string(kw) {
			case "fail":
				a.SetErrorMode(agent.ErrorModeFail)
			case "continue":
				a.SetErrorMode(agent.ErrorModeContinue)
			default:
				return newArgumentError("set-error-mode!: mode must be :fail or :continue, got %s", kw)
			}
			return a
		}},
	},
	{
		Name:    "send",
		Builtin: &Builtin{CtxFn: agentSendCtxFn("send", (*agent.Agent).Send)},
	},
	{
		Name: "await",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("await", args, 1); err != nil {
				return nil, err
			}
			agents, err := agentArgs("await", args)
			if err != nil {
				return nil, err
			}
			if err := agent.Await(ctx, agents...); err != nil {
				return nil, err
			}
			if err := firstAgentError(agents); err != nil {
				return nil, fmt.Errorf("await: agent has error: %w", err)
			}
			return atom.NilValue, nil
		}},
	},
	{
		Name: "await-for",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("await-for", args, 2); err != nil {
				return nil, err
			}
			timeoutMs, err := timeExactIntArg(args[0], "doggo.core/await-for")
			if err != nil {
				return nil, err
			}
			if timeoutMs < 0 {
				return nil, fmt.Errorf("await-for: timeout must be non-negative")
			}
			if timeoutMs > timeMaxDurationMilli {
				return nil, fmt.Errorf("await-for: timeout is out of range")
			}
			agents, err := agentArgs("await-for", args[1:])
			if err != nil {
				return nil, err
			}

			waitCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
			defer cancel()
			if err := agent.Await(waitCtx, agents...); err != nil {
				if waitCtx.Err() == context.DeadlineExceeded {
					return atom.False, nil
				}
				return nil, err
			}
			if firstAgentError(agents) != nil {
				return atom.False, nil
			}
			return atom.True, nil
		}},
	},
}

func agentArgs(fnName string, args []types.Value) ([]*agent.Agent, error) {
	agents := make([]*agent.Agent, len(args))
	for i, arg := range args {
		a, ok := arg.(*agent.Agent)
		if !ok {
			return nil, fmt.Errorf("%s: argument %d must be an agent, got %s", fnName, i+1, arg.Type())
		}
		agents[i] = a
	}
	return agents, nil
}

func firstAgentError(agents []*agent.Agent) error {
	for _, a := range agents {
		if err := a.CurrentError(); err != nil {
			return err
		}
	}
	return nil
}

func agentSendCtxFn(fnName string, send func(*agent.Agent, context.Context, agent.ActionFn, []types.Value) error) func(context.Context, ...types.Value) (types.Value, error) {
	return func(ctx context.Context, args ...types.Value) (types.Value, error) {
		if err := CheckArgMinCtx(fnName, args, 2); err != nil {
			return nil, err
		}
		a, ok := args[0].(*agent.Agent)
		if !ok {
			return nil, fmt.Errorf("%s: first argument must be an agent, got %s", fnName, args[0].Type())
		}
		fn := args[1]
		extraArgs := args[2:]

		vm := GetVM(ctx)
		if vm == nil {
			return nil, fmt.Errorf("%s: VM not found in context", fnName)
		}

		// 非同期 action は元の VM がプールに返却された後に走る可能性がある。
		// 実行に必要なランタイム環境は送信時点で値としてキャプチャする。
		runtime := vm.snapshotCallableRuntime()
		if runtime == nil {
			return nil, fmt.Errorf("%s: callable runtime not initialized", fnName)
		}

		actionFn := func(actionCtx context.Context, state types.Value, actionArgs []types.Value) (types.Value, error) {
			fnArgs := make([]types.Value, 1+len(actionArgs))
			fnArgs[0] = state
			copy(fnArgs[1:], actionArgs)
			return runtime.Apply(actionCtx, fn, fnArgs)
		}

		dispatch := func(actionCtx context.Context) error {
			if err := send(a, actionCtx, actionFn, extraArgs); err != nil {
				return fmt.Errorf("%s: %w", fnName, err)
			}
			return nil
		}
		if tx := stm.GetTransaction(ctx); tx != nil {
			if err := a.CheckSendable(); err != nil {
				return nil, fmt.Errorf("%s: %w", fnName, err)
			}
			tx.AfterCommit(func(commitCtx context.Context) {
				_ = dispatch(forkBindingContextFrom(commitCtx, ctx))
			})
			return a, nil
		}
		if err := dispatch(forkBindingContext(ctx)); err != nil {
			return nil, err
		}

		return a, nil
	}
}
