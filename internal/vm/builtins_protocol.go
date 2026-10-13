// builtins_protocol.go はプロトコル関連の組み込み関数を定義します。
//
// プロトコルは Clojure の defprotocol/extend-type に相当する型多態を提供します。
// 型ごとに異なる実装を持つことができます。
//
// 提供する関数:
//   - 定義: defprotocol*
//   - 拡張: extend-type*, extend
//   - 述語: satisfies?, extends?
//   - 取得: get-protocol-impl
package vm

import (
	"context"
	"fmt"

	"doggo/internal/compiler"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var protocolBuiltins = []BuiltinDefinition{
	// defprotocol* はプロトコルインスタンスを作成します。
	// 引数: name (string), methods (vector of keywords/symbols)
	// 戻り値: Protocol
	{
		Name:       "defprotocol*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{
			Fn: func(args ...types.Value) types.Value {
				if err := CheckArgCount("defprotocol*", args, 2); err != nil {
					return err
				}

				// プロトコル名を取得
				var name string
				switch n := args[0].(type) {
				case atom.String:
					name = string(n)
				case types.Symbol:
					name = string(n)
				default:
					return newArgumentError("defprotocol*: name must be a string or symbol, got %s", args[0].Type())
				}

				// メソッド名のリストを取得
				methodsArg := args[1]
				var methods []string

				switch m := methodsArg.(type) {
				case *coll.Vector:
					for i := 0; i < m.Count(); i++ {
						item := m.Nth(i)
						methodName, err := extractMethodName(item)
						if err != nil {
							return newArgumentError("defprotocol*: %s", err.Error())
						}
						methods = append(methods, methodName)
					}
				case *coll.List:
					seqVal := m.Seq()
					if seqVal != nil {
						if seq, ok := seqVal.(types.Seq); ok {
							for seq != nil {
								item := seq.First()
								methodName, err := extractMethodName(item)
								if err != nil {
									return newArgumentError("defprotocol*: %s", err.Error())
								}
								methods = append(methods, methodName)
								seq = seq.Next()
							}
						}
					}
				default:
					return newArgumentError("defprotocol*: methods must be a vector or list, got %s", methodsArg.Type())
				}

				return types.NewProtocol(name, methods)
			},
		},
	},

	// extend-type* は指定された型にプロトコルの実装を追加します。
	// 引数: protocol, type-name (string), impls (map of keyword/symbol -> fn)
	// 戻り値: protocol
	{
		Name:       "extend-type*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{
			CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
				if err := CheckArgRange("extend-type*", args, 3, 4); err != nil {
					return err, nil
				}

				// プロトコルを取得
				protocol, ok := args[0].(*types.Protocol)
				if !ok {
					return newArgumentError("extend-type*: first argument must be a protocol, got %s", args[0].Type()), nil
				}

				// 型名を取得
				var typeName string
				switch t := args[1].(type) {
				case types.Nil:
					typeName = "nil"
				case atom.String:
					typeName = string(t)
				case types.Symbol:
					typeName = string(t)
				default:
					return newArgumentError("extend-type*: type-name must be a string or symbol, got %s", args[1].Type()), nil
				}

				// 実装マップを取得
				implMap, ok := args[2].(types.Map)
				if !ok {
					return newArgumentError("extend-type*: impls must be a map, got %s", args[2].Type()), nil
				}

				// 実装を抽出してプロトコルに登録
				impls := make(map[string]types.Value)
				seqVal := implMap.Seq()
				if seqVal != nil {
					if seq, ok := seqVal.(types.Seq); ok {
						for seq != nil {
							entry := seq.First()
							if mapEntry, ok := entry.(*types.MapEntry); ok {
								methodName, err := extractMethodName(mapEntry.Key)
								if err != nil {
									return newArgumentError("extend-type*: %s", err.Error()), nil
								}
								impls[methodName] = mapEntry.Value
							}
							seq = seq.Next()
						}
					}
				}

				protocol.Extend(typeName, impls)
				var explicitNamespace types.Value
				if len(args) > 3 {
					explicitNamespace = args[3]
				}
				if namespace := protocolImplementationNamespace(ctx, typeName, explicitNamespace); namespace != "" {
					protocol.Extend(compiler.QualifySymbol(namespace, typeName), impls)
				}
				return protocol, nil
			},
		},
	},

	// satisfies? は値がプロトコルを実装しているかを判定します。
	// 引数: protocol, value
	// 戻り値: boolean
	{
		Name: "satisfies?",
		Builtin: &Builtin{
			Fn: func(args ...types.Value) types.Value {
				if err := CheckArgCount("satisfies?", args, 2); err != nil {
					return err
				}

				protocol, ok := args[0].(*types.Protocol)
				if !ok {
					return newArgumentError("satisfies?: first argument must be a protocol, got %s", args[0].Type())
				}

				target := args[1]
				if protocol.Satisfies(target) {
					return atom.True
				}
				return atom.False
			},
		},
	},

	// find-protocol-method* は指定された値とメソッド名に対する実装関数を検索します。
	// 引数: protocol, value, method-name
	// 戻り値: 実装関数 または nil
	{
		Name:       "find-protocol-method*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{
			Fn: func(args ...types.Value) types.Value {
				if err := CheckArgCount("find-protocol-method*", args, 3); err != nil {
					return err
				}

				protocol, ok := args[0].(*types.Protocol)
				if !ok {
					return newArgumentError("find-protocol-method*: first argument must be a protocol, got %s", args[0].Type())
				}

				target := args[1]

				var methodName string
				switch m := args[2].(type) {
				case atom.String:
					methodName = string(m)
				case types.Symbol:
					methodName = string(m)
				case atom.Keyword:
					methodName = string(m)
				default:
					return newArgumentError("find-protocol-method*: method-name must be a string, symbol or keyword, got %s", args[2].Type())
				}

				impl := protocol.FindImpl(target, methodName)
				if impl == nil {
					return atom.NilValue
				}
				return impl
			},
		},
	},

	// protocol? は値がプロトコルかどうかを判定します。
	// 引数: value
	// 戻り値: boolean
	{
		Name: "protocol?",
		Builtin: &Builtin{
			Fn: func(args ...types.Value) types.Value {
				if err := CheckArgCount("protocol?", args, 1); err != nil {
					return err
				}
				if _, ok := args[0].(*types.Protocol); ok {
					return atom.True
				}
				return atom.False
			},
		},
	},

	// type-name は値の型名を文字列で返します。
	// 引数: value
	// 戻り値: string
	{
		Name: "type-name",
		Builtin: &Builtin{
			Fn: func(args ...types.Value) types.Value {
				if err := CheckArgCount("type-name", args, 1); err != nil {
					return err
				}
				if args[0] == nil {
					return atom.NewString("nil")
				}
				return atom.NewString(args[0].Type())
			},
		},
	},

	// invoke-protocol-method はプロトコルメソッドを呼び出します。
	// 引数: protocol, method-name, target, ...args
	// 戻り値: メソッドの戻り値
	{
		Name:       "invoke-protocol-method",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{
			CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
				if err := CheckArgMinCtx("invoke-protocol-method", args, 3); err != nil {
					return nil, err
				}

				protocol, ok := args[0].(*types.Protocol)
				if !ok {
					return nil, newArgumentError("invoke-protocol-method: first argument must be a protocol, got %s", args[0].Type())
				}

				var methodName string
				switch m := args[1].(type) {
				case atom.String:
					methodName = string(m)
				case types.Symbol:
					methodName = string(m)
				case atom.Keyword:
					methodName = string(m)
				default:
					return nil, newArgumentError("invoke-protocol-method: method-name must be a string, symbol or keyword, got %s", args[1].Type())
				}

				target := args[2]

				// 実装を検索
				impl := protocol.FindImpl(target, methodName)
				if impl == nil {
					return nil, fmt.Errorf("no implementation of method %q in protocol %s for type %s",
						methodName, protocol.Name, getTypeName(target))
				}

				// 実装を呼び出す（target を最初の引数として渡す）
				callArgs := make([]types.Value, 0, len(args)-2)
				callArgs = append(callArgs, target)
				callArgs = append(callArgs, args[3:]...)

				vm := GetVM(ctx)
				if vm == nil {
					// VM がない場合は types.Fn として呼び出す
					if fn, ok := impl.(types.Fn); ok {
						return fn.Invoke(ctx, callArgs...)
					}
					return nil, fmt.Errorf("invoke-protocol-method: implementation is not callable")
				}

				return ApplyCallableFast(ctx, vm, impl, callArgs)
			},
		},
	},
}

func protocolImplementationNamespace(ctx context.Context, typeName string, explicit types.Value) string {
	if compiler.IsQualifiedSymbol(typeName) {
		return ""
	}
	switch namespace := explicit.(type) {
	case types.Symbol:
		return string(namespace)
	case atom.String:
		return string(namespace)
	case *compiler.Closure:
		if namespace.Fn != nil {
			return namespace.Fn.Namespace
		}
	case *compiler.CompiledFunction:
		return namespace.Namespace
	}
	if vm := GetVM(ctx); vm != nil {
		for i := vm.frameIndex - 1; i >= 0; i-- {
			if frame := vm.frames[i]; frame != nil && frame.CurrentNamespace() != "" {
				return frame.CurrentNamespace()
			}
		}
	}
	return namespaceFromContext(ctx)
}

// extractMethodName はメソッド名を抽出します。
func extractMethodName(v types.Value) (string, error) {
	switch m := v.(type) {
	case atom.Keyword:
		return string(m), nil
	case types.Symbol:
		return string(m), nil
	case atom.String:
		return string(m), nil
	default:
		return "", fmt.Errorf("method name must be a keyword, symbol or string, got %s", v.Type())
	}
}

var additionalProtocolBuiltins = []BuiltinDefinition{
	// go-type-name は GoObject の内部の Go 型名を文字列で返します。
	// extend-type で Go の型を指定する際に使用します。
	// 引数: value (GoObject)
	// 戻り値: string (Go の型名) または nil (GoObjectでない場合)
	{
		Name: "go-type-name",
		Builtin: &Builtin{
			Fn: func(args ...types.Value) types.Value {
				if err := CheckArgCount("go-type-name", args, 1); err != nil {
					return err
				}
				if goObj, ok := args[0].(*atom.GoObject); ok {
					return atom.NewString(types.GetGoTypeName(goObj.Unwrap()))
				}
				return atom.NilValue
			},
		},
	},

	// extends? は型がプロトコルを実装しているかを判定します。
	// satisfies? と異なり、値ではなく型名を受け取ります。
	// 引数: protocol, type-name (string)
	// 戻り値: boolean
	{
		Name: "extends?",
		Builtin: &Builtin{
			Fn: func(args ...types.Value) types.Value {
				if err := CheckArgCount("extends?", args, 2); err != nil {
					return err
				}

				protocol, ok := args[0].(*types.Protocol)
				if !ok {
					return newArgumentError("extends?: first argument must be a protocol, got %s", args[0].Type())
				}

				var typeName string
				switch t := args[1].(type) {
				case atom.String:
					typeName = string(t)
				case types.Symbol:
					typeName = string(t)
				default:
					return newArgumentError("extends?: type-name must be a string or symbol, got %s", args[1].Type())
				}

				// 型に対して全てのメソッドが実装されているかチェック
				if protocol.HasImplForType(typeName) {
					return atom.True
				}
				return atom.False
			},
		},
	},
}

// getTypeName は値の型名を取得します（エラーメッセージ用）。
func getTypeName(v types.Value) string {
	if v == nil {
		return "nil"
	}
	return v.Type()
}
