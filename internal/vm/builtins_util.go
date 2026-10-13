// builtins_util.go はユーティリティ組み込み関数を定義します。
// memoize, complement, constantly をサポートします。
package vm

import (
	"context"
	"fmt"
	"math/rand"
	"sync"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsUtilCtx はユーティリティ組み込み関数を定義します
var builtinsUtilCtx = []BuiltinDefinition{
	// memoize - 関数の結果をキャッシュ
	{
		Name: "memoize",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("memoize", args, 1); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("memoize: VM not available in context")
			}

			fn := args[0]

			cache := coll.NewEmptyMap()
			var cacheMu sync.RWMutex

			// メモ化された関数を返す
			memoizedFn := &Builtin{
				CtxFn: func(callCtx context.Context, callArgs ...types.Value) (types.Value, error) {
					vm := GetVM(callCtx)
					if vm == nil {
						return nil, fmt.Errorf("memoized function: VM not available in context")
					}

					keyArgs := append([]types.Value(nil), callArgs...)
					key := coll.NewVector(keyArgs...)

					cacheMu.RLock()
					cached := cache.Find(key)
					cacheMu.RUnlock()
					if cached != nil {
						return cached.Value, nil
					}

					// 元の関数を呼び出す
					result, err := applyWithVM(callCtx, vm, fn, callArgs)
					if err != nil {
						return nil, err
					}

					cacheMu.Lock()
					if cached := cache.Find(key); cached != nil {
						result = cached.Value
					} else {
						cache = cache.Assoc(key, result).(*coll.Map)
					}
					cacheMu.Unlock()
					return result, nil
				},
			}

			return memoizedFn, nil
		}},
	},

	// complement - 述語の否定を返す関数を作成
	{
		Name: "complement",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("complement", args, 1); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("complement: VM not available in context")
			}

			fn := args[0]

			// 補関数を返す
			complementFn := &Builtin{
				CtxFn: func(callCtx context.Context, callArgs ...types.Value) (types.Value, error) {
					vm := GetVM(callCtx)
					if vm == nil {
						return nil, fmt.Errorf("complemented function: VM not available in context")
					}

					// 元の関数を呼び出す
					result, err := applyWithVM(callCtx, vm, fn, callArgs)
					if err != nil {
						return nil, err
					}

					// 真偽値を反転
					if isTruthy(result) {
						return atom.False, nil
					}
					return atom.True, nil
				},
			}

			return complementFn, nil
		}},
	},

	// constantly - 常に同じ値を返す関数を作成
	{
		Name: "constantly",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("constantly", args, 1); err != nil {
				return err
			}

			val := args[0]

			// 常に val を返す関数
			constantFn := &Builtin{
				Fn: func(callArgs ...types.Value) types.Value {
					return val
				},
			}

			return constantFn
		}},
	},

	// fnil - Returns a function that replaces nil arguments with defaults
	{
		Name: "fnil",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("fnil", args, 2, 4); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("fnil: VM not available in context")
			}

			fn := args[0]
			defaults := args[1:] // 1-3 default values

			// Return a function that patches nil args with defaults
			fnilFn := &Builtin{
				CtxFn: func(callCtx context.Context, callArgs ...types.Value) (types.Value, error) {
					vm := GetVM(callCtx)
					if vm == nil {
						return nil, fmt.Errorf("fnil function: VM not available in context")
					}

					// Create a copy of args to avoid modifying original
					patchedArgs := make([]types.Value, len(callArgs))
					copy(patchedArgs, callArgs)

					// Replace nil args with defaults
					for i := 0; i < len(defaults) && i < len(patchedArgs); i++ {
						if isNilArg(patchedArgs[i]) {
							patchedArgs[i] = defaults[i]
						}
					}

					return applyWithVM(callCtx, vm, fn, patchedArgs)
				},
			}

			return fnilFn, nil
		}},
	},

	// shuffle - Returns a random permutation of coll as a vector
	{
		Name: "shuffle",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("shuffle", args, 1); err != nil {
				return err
			}

			items, materializeErr := toSlice(args[0])
			if materializeErr != nil {
				return RuntimeErrf("shuffle", "%v", materializeErr)
			}
			if len(items) == 0 {
				return coll.NewVector()
			}

			// Create a copy to avoid modifying the original
			shuffled := make([]types.Value, len(items))
			copy(shuffled, items)

			// Fisher-Yates shuffle
			rand.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})

			return coll.NewVector(shuffled...)
		}},
	},

	// rand-nth - Returns a random element from collection
	{
		Name: "rand-nth",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("rand-nth", args, 1); err != nil {
				return err
			}

			items, materializeErr := toSlice(args[0])
			if materializeErr != nil {
				return RuntimeErrf("rand-nth", "%v", materializeErr)
			}
			if len(items) == 0 {
				return newArgumentError("rand-nth: collection must not be empty")
			}

			idx := rand.Intn(len(items))
			return items[idx]
		}},
	},
}
