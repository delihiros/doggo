// builtins_coll_transient.go はトランジェントコレクション操作の組み込み関数を定義します。
// transient, persistent!, assoc! をサポートします。
package vm

import (
	"context"

	"doggo/internal/types"
	"doggo/internal/types/coll"
)

// builtinsCollTransient はトランジェントコレクション操作の組み込み関数を定義します
var builtinsCollTransient = []BuiltinDefinition{
	// transient - 永続的なコレクションをトランジェントに変換
	// (transient coll) -> transient coll
	{
		Name: "transient",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCount("transient", args, 1); err != nil {
				return err, nil
			}
			switch c := args[0].(type) {
			case *coll.Map:
				return coll.NewTransientMapFromOwned(c, transientOwnerFromContext(ctx)), nil
			default:
				return newArgumentError("transient: unsupported type %s", args[0].Type()), nil
			}
		}},
	},
	// persistent! - トランジェントを永続的なコレクションに変換
	// (persistent! tcoll) -> persistent coll
	{
		Name: "persistent!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCount("persistent!", args, 1); err != nil {
				return err, nil
			}
			switch c := args[0].(type) {
			case *coll.TransientMap:
				result, err := c.PersistentOwned(transientOwnerFromContext(ctx))
				if err != nil {
					return newArgumentError("persistent!: %s", err.Error()), nil
				}
				return result, nil
			default:
				return newArgumentError("persistent!: unsupported type %s", args[0].Type()), nil
			}
		}},
	},
	// assoc! - トランジェントマップにキーと値を追加（破壊的操作）
	// (assoc! tcoll key val) -> tcoll
	{
		Name: "assoc!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMin("assoc!", args, 3); err != nil {
				return err, nil
			}
			if (len(args)-1)%2 != 0 {
				return newArgumentError("assoc!: expects an odd number of arguments (coll + key-value pairs)"), nil
			}
			switch c := args[0].(type) {
			case *coll.TransientMap:
				for i := 1; i < len(args); i += 2 {
					_, err := c.AssocOwned(transientOwnerFromContext(ctx), args[i], args[i+1])
					if err != nil {
						return newArgumentError("assoc!: %s", err.Error()), nil
					}
				}
				return c, nil
			default:
				return newArgumentError("assoc!: unsupported type %s (use assoc for persistent collections)", args[0].Type()), nil
			}
		}},
	},
}
