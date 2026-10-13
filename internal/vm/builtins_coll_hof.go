// builtins_coll_hof.go は高階関数の組み込み関数を定義します。
// reduce, map, filter, take, drop をサポートします。
package vm

import (
	"context"
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsCollHOF は高階関数の組み込み関数を定義します
var builtinsCollHOF = []BuiltinDefinition{
	// reduce - コレクションを畳み込む（2引数または3引数）
	// 最適化: VMLazySeq に対してはスライス割り当てを行わず直接イテレーション
	{
		Name: "reduce",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("reduce", args, 2, 3); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("reduce: VM not available in context")
			}

			var fn types.Value
			var acc types.Value

			if len(args) == 2 {
				// 2引数版: (reduce f coll)
				fn = args[0]
				collArg := args[1]

				// ReducibleSeq はコレクション側の安全な直接実行を使う。
				if builtin, ok := fn.(*Builtin); ok && (builtin.Fn2 != nil || builtin.Fn != nil) {
					// ReducibleSeq に対しては ReduceDirect を使用
					if reducible, ok := collArg.(types.ReducibleSeq); ok {
						if err := sequenceEvalError(reducible); err != nil {
							return nil, err
						}
						// 最初の要素を取得
						first := reducible.First()
						if first == nil {
							// 空シーケンスの場合は引数なしで関数を呼び出し
							return ApplyCallableFast(ctx, vm, fn, nil)
						}
						// 2番目以降の要素でReduceDirectを実行
						rest := reducible.Next()
						if rest == nil {
							// 1要素のみ
							return first, nil
						}
						if restReducible, ok := rest.(types.ReducibleSeq); ok {
							result, optimized, err := reduceDirectBuiltin(restReducible, builtin, first)
							if err != nil {
								return nil, err
							}
							if optimized {
								return result, nil
							}
						}
					}
				}

				return reduceSeqWithoutInit(ctx, vm, fn, collArg)
			}

			// 3引数版: (reduce f init coll)
			fn = args[0]
			acc = args[1]
			collArg := args[2]

			// ReducibleSeq はコレクション側の安全な直接実行を使う。
			if builtin, ok := fn.(*Builtin); ok && (builtin.Fn2 != nil || builtin.Fn != nil) {
				// ReducibleSeq に対しては ReduceDirect を使用
				if reducible, ok := collArg.(types.ReducibleSeq); ok {
					result, optimized, err := reduceDirectBuiltin(reducible, builtin, acc)
					if err != nil {
						return nil, err
					}
					if optimized {
						return result, nil
					}
				}
			}

			return reduceSeqWithInit(ctx, vm, fn, acc, collArg)
		}},
	},
	// map - 各要素に関数を適用
	// (map f) -> transducer
	// (map f coll) -> mapped-seq（空の場合は lazy-seq）
	{
		Name: "map",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("map", args, 1, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("map: VM not available in context")
			}

			fn := args[0]

			// 1引数: トランスデューサを返す
			if len(args) == 1 {
				runtime, err := requireCallableRuntimeSnapshot(vm, "map")
				if err != nil {
					return nil, err
				}
				return MakeMapTransducer(ctx, runtime, fn), nil
			}

			// 2引数: 遅延シーケンスを返す
			collArg := args[1]

			// コレクションをシーケンスに変換
			seq, err := asSeq(collArg, "map")
			if err != nil {
				return nil, err
			}
			if seq == nil {
				// 空のコレクション: 空の遅延シーケンスを返す
				return coll.NewLazySeq(func(_ types.Environment) (types.Value, error) {
					return nil, nil
				}, nil), nil
			}

			// VMMappedSeq は DirectApply の高速経路と評価エラーの伝播を両立する。
			// これにより ReduceDirect 最適化が有効になり、
			// (reduce + 0 (map (fn [x] ...) (range N))) パターンが高速化される
			return NewVMMappedSeq(ctx, vm, seq, fn), nil
		}},
	},
	// filter - 述語が真を返す要素のみを含む
	// (filter pred) -> transducer
	// (filter pred coll) -> lazy-seq
	{
		Name: "filter",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("filter", args, 1, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("filter: VM not available in context")
			}

			pred := args[0]

			// 1引数: トランスデューサを返す
			if len(args) == 1 {
				runtime, err := requireCallableRuntimeSnapshot(vm, "filter")
				if err != nil {
					return nil, err
				}
				return MakeFilterTransducer(ctx, runtime, pred), nil
			}

			runtime, err := requireCallableRuntimeSnapshot(vm, "filter")
			if err != nil {
				return nil, err
			}

			// 2引数: 遅延シーケンスを返す
			collArg := args[1]

			// コレクションをシーケンスに変換
			seq, err := asSeq(collArg, "filter")
			if err != nil {
				return nil, err
			}
			if seq == nil {
				// 空のコレクション: 空の遅延シーケンスを返す
				return coll.NewLazySeq(func(_ types.Environment) (types.Value, error) {
					return nil, nil
				}, nil), nil
			}

			// フォールバック: 遅延シーケンスを生成する再帰的なヘルパー関数
			var makeFilterLazySeq func(s types.Seq) *coll.LazySeq
			makeFilterLazySeq = func(s types.Seq) *coll.LazySeq {
				evaluate := func(_ types.Environment) (types.Value, error) {
					// 述語に合致する要素を探す
					current := s
					for current != nil {
						if err := sequenceEvalError(current); err != nil {
							return nil, err
						}
						first := current.First()
						if first == nil {
							return nil, nil
						}

						// 述語を適用
						result, err := runtime.ApplyFast(ctx, pred, []types.Value{first})
						if err != nil {
							return nil, err
						}

						if isTruthy(result) {
							// 合致する要素が見つかった
							rest := current.Next()
							var tail types.Seq
							if rest != nil {
								tail = makeFilterLazySeq(rest)
							}

							// Cons セル: (first . tail)
							if tail == nil {
								return coll.NewList(first), nil
							}
							return coll.NewConsSeq(first, tail), nil
						}

						// 次の要素へ
						current = current.Next()
					}
					return nil, nil
				}
				if !mappedRealizationCanReenter(s, pred, nil) {
					return coll.NewNonReentrantLazySeqFrom(s, evaluate, nil)
				}
				return coll.NewLazySeqFrom(s, evaluate, nil)
			}

			return makeFilterLazySeq(seq), nil
		}},
	},
	// take - 先頭n個の要素を取得
	// (take n) -> transducer
	// (take n coll) -> lazy-seq
	{
		Name: "take",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("take", args, 1, 2); err != nil {
				return nil, err
			}

			nValue, ok := integerForOperation(args[0])
			if !ok {
				return nil, newArgumentError("take: first argument must be a number, got %s", args[0].Type())
			}
			n := int(nValue)

			// 1引数: トランスデューサを返す
			if len(args) == 1 {
				return MakeTakeTransducer(n), nil
			}

			// 2引数: 遅延シーケンスを返す
			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("take: VM not available in context")
			}

			collArg := args[1]
			seq, err := asSeq(collArg, "take")
			if err != nil {
				return nil, err
			}
			if seq == nil || n <= 0 {
				return coll.NewFiniteLazySeqFrom(nil, func(_ types.Environment) (types.Value, error) {
					return nil, nil
				}, nil), nil
			}

			var makeTakeLazySeq func(s types.Seq, remaining int) *coll.LazySeq
			makeTakeLazySeq = func(s types.Seq, remaining int) *coll.LazySeq {
				return coll.NewFiniteLazySeqFrom(s, func(_ types.Environment) (types.Value, error) {
					if remaining <= 0 || s == nil {
						return nil, nil
					}
					if err := sequenceEvalError(s); err != nil {
						return nil, err
					}
					first := s.First()
					if first == nil {
						return nil, nil
					}
					rest := s.Next()
					var tail types.Seq
					if rest != nil && remaining > 1 {
						tail = makeTakeLazySeq(rest, remaining-1)
					}
					if tail == nil {
						return coll.NewList(first), nil
					}
					return coll.NewConsSeq(first, tail), nil
				}, nil)
			}

			return makeTakeLazySeq(seq, n), nil
		}},
	},
	// drop - 先頭n個の要素を除去（Clojure 互換）
	// (drop n) -> transducer
	// (drop n coll) -> lazy-seq（遅延性を保持）
	{
		Name: "drop",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("drop", args, 1, 2); err != nil {
				return nil, err
			}

			nValue, ok := integerForOperation(args[0])
			if !ok {
				return nil, newArgumentError("drop: first argument must be a number, got %s", args[0].Type())
			}
			n := int(nValue)

			// 1引数: トランスデューサを返す
			if len(args) == 1 {
				return MakeDropTransducer(n), nil
			}

			// 2引数: 遅延シーケンスを返す（Clojure 互換）
			collArg := args[1]
			seq, err := asSeq(collArg, "drop")
			if err != nil {
				return nil, err
			}

			if seq == nil {
				return coll.NewList(), nil
			}

			return coll.NewLazySeqPreservingInfiniteFrom(seq, func(_ types.Environment) (types.Value, error) {
				current := seq
				for i := 0; i < n && current != nil; i++ {
					if err := sequenceEvalError(current); err != nil {
						return nil, err
					}
					current = current.Next()
				}
				return current, nil
			}, nil), nil
		}},
	},

	// keep - Returns non-nil results of (f item)
	// (keep f coll)
	{
		Name: "keep",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("keep", args, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("keep: VM not available in context")
			}

			fn := args[0]
			collArg := args[1]
			runtime, err := requireCallableRuntimeSnapshot(vm, "keep")
			if err != nil {
				return nil, err
			}

			seq, err := asSeq(collArg, "keep")
			if err != nil {
				return nil, err
			}
			if seq == nil {
				return coll.NewLazySeq(func(_ types.Environment) (types.Value, error) {
					return nil, nil
				}, nil), nil
			}

			var makeKeepLazySeq func(s types.Seq) *coll.LazySeq
			makeKeepLazySeq = func(s types.Seq) *coll.LazySeq {
				return coll.NewLazySeqFrom(s, func(_ types.Environment) (types.Value, error) {
					current := s
					for current != nil {
						if err := sequenceEvalError(current); err != nil {
							return nil, err
						}
						first := current.First()
						if first == nil {
							return nil, nil
						}

						result, err := runtime.ApplyFast(ctx, fn, []types.Value{first})
						if err != nil {
							return nil, err
						}

						// Keep non-nil results (check for Go nil and types.Nil)
						_, isNil := result.(types.Nil)
						if result != nil && !isNil {
							rest := current.Next()
							var tail types.Seq
							if rest != nil {
								tail = makeKeepLazySeq(rest)
							}
							if tail == nil {
								return coll.NewList(result), nil
							}
							return coll.NewConsSeq(result, tail), nil
						}

						current = current.Next()
					}
					return nil, nil
				}, nil)
			}

			return makeKeepLazySeq(seq), nil
		}},
	},

	// remove - Returns items where (pred item) is falsy (opposite of filter)
	// (remove pred coll)
	{
		Name: "remove",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("remove", args, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("remove: VM not available in context")
			}

			pred := args[0]
			collArg := args[1]
			runtime, err := requireCallableRuntimeSnapshot(vm, "remove")
			if err != nil {
				return nil, err
			}

			seq, err := asSeq(collArg, "remove")
			if err != nil {
				return nil, err
			}
			if seq == nil {
				return coll.NewLazySeq(func(_ types.Environment) (types.Value, error) {
					return nil, nil
				}, nil), nil
			}

			var makeRemoveLazySeq func(s types.Seq) *coll.LazySeq
			makeRemoveLazySeq = func(s types.Seq) *coll.LazySeq {
				return coll.NewLazySeqFrom(s, func(_ types.Environment) (types.Value, error) {
					current := s
					for current != nil {
						if err := sequenceEvalError(current); err != nil {
							return nil, err
						}
						first := current.First()
						if first == nil {
							return nil, nil
						}

						result, err := runtime.ApplyFast(ctx, pred, []types.Value{first})
						if err != nil {
							return nil, err
						}

						// Keep items where pred is falsy (opposite of filter)
						if !isTruthy(result) {
							rest := current.Next()
							var tail types.Seq
							if rest != nil {
								tail = makeRemoveLazySeq(rest)
							}
							if tail == nil {
								return coll.NewList(first), nil
							}
							return coll.NewConsSeq(first, tail), nil
						}

						current = current.Next()
					}
					return nil, nil
				}, nil)
			}

			return makeRemoveLazySeq(seq), nil
		}},
	},

	// map-indexed - Returns result of (f index item) for each item
	// (map-indexed f coll)
	{
		Name: "map-indexed",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("map-indexed", args, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("map-indexed: VM not available in context")
			}

			fn := args[0]
			collArg := args[1]
			runtime, err := requireCallableRuntimeSnapshot(vm, "map-indexed")
			if err != nil {
				return nil, err
			}

			seq, err := asSeq(collArg, "map-indexed")
			if err != nil {
				return nil, err
			}
			if seq == nil {
				return coll.NewLazySeq(func(_ types.Environment) (types.Value, error) {
					return nil, nil
				}, nil), nil
			}

			var makeMapIndexedLazySeq func(s types.Seq, idx int) *coll.LazySeq
			makeMapIndexedLazySeq = func(s types.Seq, idx int) *coll.LazySeq {
				return coll.NewLazySeqPreservingInfiniteFrom(s, func(_ types.Environment) (types.Value, error) {
					if s == nil {
						return nil, nil
					}
					if err := sequenceEvalError(s); err != nil {
						return nil, err
					}
					first := s.First()
					if first == nil {
						return nil, nil
					}

					// Call (f index item)
					idxVal := atom.NewNumberInt(int64(idx))
					result, err := runtime.ApplyFast(ctx, fn, []types.Value{idxVal, first})
					if err != nil {
						return nil, err
					}

					rest := s.Next()
					var tail types.Seq
					if rest != nil {
						tail = makeMapIndexedLazySeq(rest, idx+1)
					}
					if tail == nil {
						return coll.NewList(result), nil
					}
					return coll.NewConsSeq(result, tail), nil
				}, nil)
			}

			return makeMapIndexedLazySeq(seq, 0), nil
		}},
	},

	// run! - Runs f on each element for side effects, returns nil
	// Like doseq but as a function
	// (run! println [1 2 3]) => nil (prints 1, 2, 3)
	{
		Name: "run!",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("run!", args, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("run!: VM not available in context")
			}

			fn := args[0]
			collArg := args[1]

			seq, err := asSeq(collArg, "run!")
			if err != nil {
				return nil, err
			}
			if seq == nil {
				return types.Nil{}, nil
			}

			// Iterate and apply f to each element for side effects
			for seq != nil {
				if err := sequenceEvalError(seq); err != nil {
					return nil, err
				}
				first := seq.First()
				if first == nil {
					break
				}

				_, err := ApplyCallableFast(ctx, vm, fn, []types.Value{first})
				if err != nil {
					return nil, err
				}

				seq = seq.Next()
			}

			return types.Nil{}, nil
		}},
	},

	// reduce-kv - Reduce over map with key-value pairs
	// (reduce-kv f init map)
	// f is called with (f acc key val)
	{
		Name: "reduce-kv",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("reduce-kv", args, 3); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("reduce-kv: VM not available in context")
			}

			fn := args[0]
			acc := args[1]
			mapArg := args[2]

			// Optimized path for coll.Map which has AllEntries
			if m, ok := mapArg.(*coll.Map); ok {
				entries := m.AllEntries()
				for _, entry := range entries {
					var err error
					acc, err = ApplyCallableFast(ctx, vm, fn, []types.Value{acc, entry.Key, entry.Value})
					if err != nil {
						return nil, err
					}
				}
				return acc, nil
			}

			// Generic path: iterate as a sequence of map entries
			seq, err := asSeq(mapArg, "reduce-kv")
			if err != nil {
				return nil, err
			}
			if seq == nil {
				return acc, nil
			}
			for seq != nil {
				if err := sequenceEvalError(seq); err != nil {
					return nil, err
				}
				first := seq.First()
				if first == nil {
					break
				}
				// Expect map entry or vector of [k v]
				if entry, ok := first.(*types.MapEntry); ok {
					var err error
					acc, err = ApplyCallableFast(ctx, vm, fn, []types.Value{acc, entry.Key, entry.Value})
					if err != nil {
						return nil, err
					}
				} else if vec, ok := first.(*coll.Vector); ok && vec.Count() == 2 {
					key := vec.Nth(0)
					val := vec.Nth(1)
					var err error
					acc, err = ApplyCallableFast(ctx, vm, fn, []types.Value{acc, key, val})
					if err != nil {
						return nil, err
					}
				}
				seq = seq.Next()
			}
			return acc, nil
		}},
	},

	// reductions - Returns a lazy seq of intermediate reduce values
	// (reductions f coll) or (reductions f init coll)
	{
		Name: "reductions",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgRangeCtx("reductions", args, 2, 3); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("reductions: VM not available in context")
			}
			runtime, err := requireCallableRuntimeSnapshot(vm, "reductions")
			if err != nil {
				return nil, err
			}

			var fn types.Value
			var acc types.Value
			var seq types.Seq
			var seqErr error

			if len(args) == 2 {
				// (reductions f coll) - use first element as initial value
				fn = args[0]
				seq, seqErr = asSeq(args[1], "reductions")
				if seqErr != nil {
					return nil, seqErr
				}
				if seq == nil {
					return coll.NewList(), nil
				}
				if err := sequenceEvalError(seq); err != nil {
					return nil, err
				}
				acc = seq.First()
				if acc == nil {
					return coll.NewList(), nil
				}
				seq = seq.Next()
			} else {
				// (reductions f init coll)
				fn = args[0]
				acc = args[1]
				seq, seqErr = asSeq(args[2], "reductions")
				if seqErr != nil {
					return nil, seqErr
				}
			}

			// Create lazy sequence for intermediate values
			var makeReductionsLazy func(currentAcc types.Value, s types.Seq) *coll.LazySeq
			makeReductionsLazy = func(currentAcc types.Value, s types.Seq) *coll.LazySeq {
				return coll.NewLazySeqPreservingInfiniteFrom(s, func(_ types.Environment) (types.Value, error) {
					if s == nil {
						// No more elements, just return current acc
						return coll.NewList(currentAcc), nil
					}
					if err := sequenceEvalError(s); err != nil {
						return nil, err
					}
					first := s.First()
					if first == nil {
						return coll.NewList(currentAcc), nil
					}

					// Apply function to get next acc
					nextAcc, err := runtime.ApplyFast(ctx, fn, []types.Value{currentAcc, first})
					if err != nil {
						return nil, err
					}

					// Return cons cell with current acc and lazy tail
					tail := makeReductionsLazy(nextAcc, s.Next())
					return coll.NewConsSeq(currentAcc, tail), nil
				}, nil)
			}

			return makeReductionsLazy(acc, seq), nil
		}},
	},

	// keep-indexed - Like map-indexed but keeps only non-nil results (lazy)
	// (keep-indexed f coll)
	{
		Name: "keep-indexed",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("keep-indexed", args, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("keep-indexed: VM not available in context")
			}

			fn := args[0]
			collArg := args[1]
			runtime, err := requireCallableRuntimeSnapshot(vm, "keep-indexed")
			if err != nil {
				return nil, err
			}

			seq, err := asSeq(collArg, "keep-indexed")
			if err != nil {
				return nil, err
			}
			if seq == nil {
				return coll.NewList(), nil
			}

			// Create lazy sequence that skips nil results
			var makeKeepIndexedLazy func(s types.Seq, idx int) *coll.LazySeq
			makeKeepIndexedLazy = func(s types.Seq, idx int) *coll.LazySeq {
				return coll.NewLazySeqFrom(s, func(_ types.Environment) (types.Value, error) {
					// Find next non-nil result
					for s != nil {
						if err := sequenceEvalError(s); err != nil {
							return nil, err
						}
						first := s.First()
						if first == nil {
							return nil, nil
						}

						idxVal := atom.NewNumberInt(int64(idx))
						result, err := runtime.ApplyFast(ctx, fn, []types.Value{idxVal, first})
						if err != nil {
							return nil, err
						}

						s = s.Next()
						idx++

						// Keep only non-nil results
						if _, isNil := result.(types.Nil); !isNil && result != nil {
							// Found a non-nil result, return cons cell
							tail := makeKeepIndexedLazy(s, idx)
							return coll.NewConsSeq(result, tail), nil
						}
					}
					return nil, nil
				}, nil)
			}

			return makeKeepIndexedLazy(seq, 0), nil
		}},
	},
}

func reduceDirectBuiltin(reducible types.ReducibleSeq, builtin *Builtin, init types.Value) (types.Value, bool, error) {
	var reducerError error
	reduceFn := func(acc, value types.Value) types.Value {
		if reducerError != nil {
			return &types.Reduced{Val: atom.NilValue}
		}

		var result types.Value
		if builtin.Fn2 != nil {
			result = builtin.Fn2(acc, value)
		} else {
			result = builtin.Fn(acc, value)
		}
		if errValue, ok := result.(*Error); ok {
			reducerError = vmErrorToDoggoError(errValue)
			return &types.Reduced{Val: errValue}
		}
		if err := coll.CollectionError(result); err != nil {
			reducerError = err
			return &types.Reduced{Val: atom.NilValue}
		}
		return result
	}

	result, optimized, err := reducible.ReduceDirect(reduceFn, init)
	if err != nil {
		return nil, optimized, err
	}
	if reducerError != nil {
		return nil, optimized, reducerError
	}
	return result, optimized, nil
}

func reduceSeqWithoutInit(ctx context.Context, vm *VM, fn types.Value, collArg types.Value) (types.Value, error) {
	var acc types.Value
	hasAcc := false
	err := iterateCollection(collArg, "reduce", func(item types.Value) (bool, error) {
		if !hasAcc {
			acc = item
			hasAcc = true
			return true, nil
		}

		result, err := ApplyCallableFast(ctx, vm, fn, []types.Value{acc, item})
		if err != nil {
			return false, err
		}
		acc = result
		return !types.IsReduced(acc), nil
	})
	if err != nil {
		return nil, err
	}
	if !hasAcc {
		return ApplyCallableFast(ctx, vm, fn, nil)
	}
	return types.Unreduced(acc), nil
}

func reduceSeqWithInit(ctx context.Context, vm *VM, fn, acc, collArg types.Value) (types.Value, error) {
	return reduceCollection(collArg, "reduce", acc, func(acc, item types.Value) (types.Value, error) {
		return ApplyCallableFast(ctx, vm, fn, []types.Value{acc, item})
	})
}
