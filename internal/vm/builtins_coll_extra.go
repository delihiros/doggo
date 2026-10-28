// builtins_coll_extra.go はコレクション操作の追加組み込み関数を定義します。
// merge, zipmap, get-in, assoc-in, update, update-in, merge-with, select-keys,
// second, last, butlast, distinct, dedupe, flatten, frequencies,
// interleave, interpose, partition, partition-by, split-at, split-with をサポートします。
package vm

import (
	"context"
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// builtinsCollExtra は追加コレクション操作の組み込み関数を定義します
var builtinsCollExtra = []BuiltinDefinition{
	// ==========================================================================
	// マップ/ネスト/更新 関数
	// ==========================================================================

	// merge - 複数のマップを結合（TransientMap で最適化）
	{
		Name: "merge",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if len(args) == 0 {
				return coll.NewEmptyMap()
			}

			// 最初の有効なマップを見つけて TransientMap に変換
			var result *coll.TransientMap
			startIdx := 0

			for i, arg := range args {
				if arg == nil || arg == atom.NilValue {
					continue
				}
				m, ok := arg.(*coll.Map)
				if !ok {
					return newArgumentError("merge: expected map, got %s", arg.Type())
				}
				result = coll.NewTransientMapFrom(m)
				startIdx = i + 1
				break
			}

			if result == nil {
				return coll.NewEmptyMap()
			}

			// 残りのマップを TransientMap にインプレース追加
			var assocErr error
		mergeLoop:
			for _, arg := range args[startIdx:] {
				if arg == nil || arg == atom.NilValue {
					continue
				}
				m, ok := arg.(*coll.Map)
				if !ok {
					return newArgumentError("merge: expected map, got %s", arg.Type())
				}
				m.Range(func(k, v types.Value) bool {
					// 返り値を代入し、エラーを記録
					var err error
					result, err = result.Assoc(k, v)
					if err != nil {
						assocErr = err
						return false // イテレーション中断
					}
					return true
				})
				if assocErr != nil {
					break mergeLoop // 外側ループも中断
				}
			}

			if assocErr != nil {
				return newArgumentError("merge: %s", assocErr.Error())
			}

			// 永続マップに変換して返す
			persistent, err := result.Persistent()
			if err != nil {
				return newArgumentError("merge: %s", err.Error())
			}
			return persistent
		}},
	},

	// zipmap - キーと値のコレクションからマップを作成
	{
		Name: "zipmap",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("zipmap", args, 2); err != nil {
				return err
			}

			// キーを取得
			keys, materializeErr := toSlice(args[0])
			if materializeErr != nil {
				return RuntimeErrf("zipmap", "%v", materializeErr)
			}
			if keys == nil {
				return coll.NewEmptyMap()
			}

			// 値を取得
			vals, materializeErr := toSlice(args[1])
			if materializeErr != nil {
				return RuntimeErrf("zipmap", "%v", materializeErr)
			}
			if vals == nil {
				return coll.NewEmptyMap()
			}

			// 短い方の長さに合わせる
			length := len(keys)
			if len(vals) < length {
				length = len(vals)
			}

			result := coll.NewEmptyMap()
			for i := 0; i < length; i++ {
				result = result.Assoc(keys[i], vals[i]).(*coll.Map)
			}
			return result
		}},
	},

	// get-in - ネストしたマップから値を取得
	{
		Name: "get-in",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgRange("get-in", args, 2, 3); err != nil {
				return err
			}

			// デフォルト値
			var defaultVal types.Value = atom.NilValue
			if len(args) == 3 {
				defaultVal = args[2]
			}

			// キーのパス
			keysVal := args[1]
			keys, materializeErr := toSlice(keysVal)
			if materializeErr != nil {
				return RuntimeErrf("get-in", "%v", materializeErr)
			}
			if len(keys) == 0 {
				// 空のキーパスの場合はマップそのものを返す
				return args[0]
			}

			// マップを走査
			current := args[0]
			for _, key := range keys {
				if current == nil || current == atom.NilValue {
					return defaultVal
				}

				switch m := current.(type) {
				case *coll.Map:
					val := m.Get(key)
					if val == nil {
						return defaultVal
					}
					current = val
				case *coll.Vector:
					// ベクターの場合はインデックスアクセス
					index, ok := integerForOperation(key)
					if !ok {
						return defaultVal
					}
					i := int(index)
					if i < 0 || i >= len(m.Items) {
						return defaultVal
					}
					current = m.Items[i]
				default:
					return defaultVal
				}
			}
			return current
		}},
	},

	// assoc-in - ネストしたマップに値を設定
	{
		Name: "assoc-in",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("assoc-in", args, 3); err != nil {
				return err
			}

			m := args[0]
			keysVal := args[1]
			val := args[2]

			keys, materializeErr := toSlice(keysVal)
			if materializeErr != nil {
				return RuntimeErrf("assoc-in", "%v", materializeErr)
			}
			if len(keys) == 0 {
				return val
			}

			return assocInRecursive(m, keys, val)
		}},
	},

	// select-keys - 指定されたキーのみを含むマップを返す
	{
		Name: "select-keys",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("select-keys", args, 2); err != nil {
				return err
			}

			if args[0] == nil || args[0] == atom.NilValue {
				return coll.NewEmptyMap()
			}

			m, ok := args[0].(*coll.Map)
			if !ok {
				return newArgumentError("select-keys: first argument must be a map, got %s", args[0].Type())
			}

			keys, materializeErr := toSlice(args[1])
			if materializeErr != nil {
				return RuntimeErrf("select-keys", "%v", materializeErr)
			}
			if keys == nil {
				return coll.NewEmptyMap()
			}

			result := coll.NewEmptyMap()
			for _, key := range keys {
				val := m.Get(key)
				if val != nil {
					result = result.Assoc(key, val).(*coll.Map)
				}
			}
			return result
		}},
	},

	// ==========================================================================
	// 追加コレクション操作 関数
	// ==========================================================================

	// second - 2番目の要素を返す
	{
		Name: "second",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("second", args, 1); err != nil {
				return err
			}

			if args[0] == nil || args[0] == atom.NilValue {
				return atom.NilValue
			}

			items, materializeErr := toSlice(args[0])
			if materializeErr != nil {
				return RuntimeErrf("second", "%v", materializeErr)
			}
			if len(items) < 2 {
				return atom.NilValue
			}
			return items[1]
		}},
	},

	// last - 最後の要素を返す
	{
		Name: "last",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("last", args, 1); err != nil {
				return err
			}

			if args[0] == nil || args[0] == atom.NilValue {
				return atom.NilValue
			}

			items, materializeErr := toSlice(args[0])
			if materializeErr != nil {
				return RuntimeErrf("last", "%v", materializeErr)
			}
			if len(items) == 0 {
				return atom.NilValue
			}
			return items[len(items)-1]
		}},
	},

	// butlast - 最後の要素を除いたシーケンスを返す
	{
		Name: "butlast",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("butlast", args, 1); err != nil {
				return err
			}

			if args[0] == nil || args[0] == atom.NilValue {
				return atom.NilValue
			}

			items, materializeErr := toSlice(args[0])
			if materializeErr != nil {
				return RuntimeErrf("butlast", "%v", materializeErr)
			}
			if len(items) == 0 {
				return atom.NilValue
			}
			if len(items) == 1 {
				return coll.NewList()
			}
			return coll.NewList(items[:len(items)-1]...)
		}},
	},

	// distinct - 重複を除去したシーケンスを返す
	{
		Name: "distinct",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("distinct", args, 1); err != nil {
				return err
			}

			items, materializeErr := toSlice(args[0])
			if materializeErr != nil {
				return RuntimeErrf("distinct", "%v", materializeErr)
			}
			if len(items) == 0 {
				return coll.NewList()
			}

			// 重複チェック用のセット（Equals比較）
			seen := make([]types.Value, 0)
			result := make([]types.Value, 0)

			for _, item := range items {
				found := false
				for _, s := range seen {
					if s.Equals(item) {
						found = true
						break
					}
				}
				if !found {
					seen = append(seen, item)
					result = append(result, item)
				}
			}
			return coll.NewList(result...)
		}},
	},

	// dedupe - 連続する重複を除去したシーケンスを返す
	{
		Name: "dedupe",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("dedupe", args, 1); err != nil {
				return err
			}

			items, materializeErr := toSlice(args[0])
			if materializeErr != nil {
				return RuntimeErrf("dedupe", "%v", materializeErr)
			}
			if len(items) == 0 {
				return coll.NewList()
			}

			result := []types.Value{items[0]}
			for i := 1; i < len(items); i++ {
				if !items[i].Equals(items[i-1]) {
					result = append(result, items[i])
				}
			}
			return coll.NewList(result...)
		}},
	},

	// flatten - ネストしたコレクションを平坦化
	{
		Name: "flatten",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("flatten", args, 1); err != nil {
				return err
			}

			result := make([]types.Value, 0)
			if err := flattenRecursive(args[0], &result); err != nil {
				return newArgumentError("%v", err)
			}
			return coll.NewList(result...)
		}},
	},

	// frequencies - 各要素の出現回数をマップで返す
	{
		Name: "frequencies",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("frequencies", args, 1); err != nil {
				return err
			}

			items, materializeErr := toSlice(args[0])
			if materializeErr != nil {
				return RuntimeErrf("frequencies", "%v", materializeErr)
			}
			if items == nil {
				return coll.NewEmptyMap()
			}

			// 出現回数をカウント（Equals比較用にキーと回数を別管理）
			type countEntry struct {
				key   types.Value
				count int
			}
			entries := make([]countEntry, 0)

			for _, item := range items {
				found := false
				for i := range entries {
					if entries[i].key.Equals(item) {
						entries[i].count++
						found = true
						break
					}
				}
				if !found {
					entries = append(entries, countEntry{key: item, count: 1})
				}
			}

			result := coll.NewEmptyMap()
			for _, e := range entries {
				result = result.Assoc(e.key, atom.NewNumberInt(int64(e.count))).(*coll.Map)
			}
			return result
		}},
	},

	// interleave - 複数のコレクションを交互に結合
	{
		Name: "interleave",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgMin("interleave", args, 2); err != nil {
				return err
			}

			// 各コレクションをスライスに変換
			colls := make([][]types.Value, len(args))
			minLen := -1
			for i, arg := range args {
				items, materializeErr := toSlice(arg)
				if materializeErr != nil {
					return RuntimeErrf("interleave", "%v", materializeErr)
				}
				if items == nil {
					return coll.NewList()
				}
				colls[i] = items
				if minLen == -1 || len(items) < minLen {
					minLen = len(items)
				}
			}

			result := make([]types.Value, 0, minLen*len(args))
			for i := 0; i < minLen; i++ {
				for _, c := range colls {
					result = append(result, c[i])
				}
			}
			return coll.NewList(result...)
		}},
	},

	// interpose - 各要素の間にセパレータを挿入
	{
		Name: "interpose",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("interpose", args, 2); err != nil {
				return err
			}

			sep := args[0]
			items, materializeErr := toSlice(args[1])
			if materializeErr != nil {
				return RuntimeErrf("interpose", "%v", materializeErr)
			}
			if len(items) == 0 {
				return coll.NewList()
			}
			if len(items) == 1 {
				return coll.NewList(items[0])
			}

			result := make([]types.Value, 0, len(items)*2-1)
			for i, item := range items {
				if i > 0 {
					result = append(result, sep)
				}
				result = append(result, item)
			}
			return coll.NewList(result...)
		}},
	},

	// split-at - 指定位置でコレクションを分割
	{
		Name: "split-at",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("split-at", args, 2); err != nil {
				return err
			}

			nValue, ok := integerForOperation(args[0])
			if !ok {
				return newArgumentError("split-at: first argument must be a number, got %s", args[0].Type())
			}
			n := int(nValue)
			if n < 0 {
				n = 0
			}

			items, materializeErr := toSlice(args[1])
			if materializeErr != nil {
				return RuntimeErrf("split-at", "%v", materializeErr)
			}
			if items == nil {
				return coll.NewVector(coll.NewList(), coll.NewList())
			}

			if n >= len(items) {
				return coll.NewVector(coll.NewList(items...), coll.NewList())
			}

			return coll.NewVector(coll.NewList(items[:n]...), coll.NewList(items[n:]...))
		}},
	},

	// take-nth - Returns every nth element from collection (lazy)
	// (take-nth n coll)
	{
		Name: "take-nth",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("take-nth", args, 2); err != nil {
				return err
			}

			nValue, ok := integerForOperation(args[0])
			if !ok {
				return newArgumentError("take-nth: first argument must be a number, got %s", args[0].Type())
			}
			step := int(nValue)
			if step <= 0 {
				return newArgumentError("take-nth: n must be positive, got %d", step)
			}

			seq, errVal := asSeqValue(args[1], "take-nth")
			if errVal != nil {
				return errVal
			}
			if seq == nil {
				return coll.NewList()
			}

			// Create lazy sequence using closure
			var makeTakeNthLazy func(s types.Seq, n int) *coll.LazySeq
			makeTakeNthLazy = func(s types.Seq, n int) *coll.LazySeq {
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

					// Skip n-1 elements to get to the next element
					rest := s
					for i := 0; i < n && rest != nil; i++ {
						if err := sequenceEvalError(rest); err != nil {
							return nil, err
						}
						rest = rest.Next()
					}

					var tail types.Seq
					if rest != nil {
						tail = makeTakeNthLazy(rest, n)
					}
					if tail == nil {
						return coll.NewList(first), nil
					}
					return coll.NewConsSeq(first, tail), nil
				}, nil)
			}

			return makeTakeNthLazy(seq, step)
		}},
	},

	// nthrest - Returns seq after skipping n elements
	// (nthrest coll n)
	{
		Name: "nthrest",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("nthrest", args, 2); err != nil {
				return err
			}

			nValue, ok := integerForOperation(args[1])
			if !ok {
				return newArgumentError("nthrest: second argument must be a number, got %s", args[1].Type())
			}
			skip := int(nValue)
			if skip < 0 {
				skip = 0
			}

			seq, errVal := asSeqValue(args[0], "nthrest")
			if errVal != nil {
				return errVal
			}
			if seq == nil {
				return coll.NewList()
			}

			// Skip n elements
			for i := 0; i < skip && seq != nil; i++ {
				seq = seq.Next()
			}

			if seq == nil {
				return coll.NewList()
			}

			// Return the remaining sequence directly (no copying)
			// types.Seq implements types.Value, so we can return it directly
			return seq.(types.Value)
		}},
	},
}

// builtinsCollExtraCtx は高階関数を含む追加コレクション操作の組み込み関数を定義します
var builtinsCollExtraCtx = []BuiltinDefinition{
	// update - マップの値を関数で更新
	{
		Name: "update",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("update", args, 3); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("update: VM not available in context")
			}

			m := args[0]
			key := args[1]
			fn := args[2]
			extraArgs := args[3:]

			// 現在の値を取得（nil なら nil を渡す）
			var currentVal types.Value = atom.NilValue
			if m != nil && m != atom.NilValue {
				if mapVal, ok := m.(*coll.Map); ok {
					val := mapVal.Get(key)
					if val != nil {
						currentVal = val
					}
				}
			}

			// 関数を呼び出す: (fn current-val & extra-args)
			fnArgs := make([]types.Value, 0, 1+len(extraArgs))
			fnArgs = append(fnArgs, currentVal)
			fnArgs = append(fnArgs, extraArgs...)

			newVal, err := ApplyCallableFast(ctx, vm, fn, fnArgs)
			if err != nil {
				return nil, err
			}

			// マップを更新
			if m == nil || m == atom.NilValue {
				return coll.NewEmptyMap().Assoc(key, newVal), nil
			}
			if mapVal, ok := m.(*coll.Map); ok {
				return mapVal.Assoc(key, newVal), nil
			}
			return nil, newArgumentError("update: first argument must be a map, got %s", m.Type())
		}},
	},

	// update-in - ネストしたマップの値を関数で更新
	{
		Name: "update-in",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("update-in", args, 3); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("update-in: VM not available in context")
			}

			m := args[0]
			keysVal := args[1]
			fn := args[2]
			extraArgs := args[3:]

			keys, err := toSlice(keysVal)
			if err != nil {
				return nil, err
			}
			if len(keys) == 0 {
				return nil, newArgumentError("update-in: keys must be a non-empty sequence")
			}

			return updateInRecursive(ctx, vm, m, keys, fn, extraArgs)
		}},
	},

	// merge-with - 衝突時に関数で結合
	{
		Name: "merge-with",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgMinCtx("merge-with", args, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("merge-with: VM not available in context")
			}

			fn := args[0]
			maps := args[1:]

			result := coll.NewEmptyMap()
			for _, arg := range maps {
				if arg == nil || arg == atom.NilValue {
					continue
				}
				m, ok := arg.(*coll.Map)
				if !ok {
					return nil, newArgumentError("merge-with: expected map, got %s", arg.Type())
				}

				var iterErr error
				m.Range(func(k, v types.Value) bool {
					existing := result.Get(k)
					if existing == nil {
						result = result.Assoc(k, v).(*coll.Map)
					} else {
						// 衝突時は関数を呼び出す
						merged, err := ApplyCallableFast(ctx, vm, fn, []types.Value{existing, v})
						if err != nil {
							iterErr = err
							return false
						}
						result = result.Assoc(k, merged).(*coll.Map)
					}
					return true
				})
				if iterErr != nil {
					return nil, iterErr
				}
			}
			return result, nil
		}},
	},

	// partition - コレクションを n 個ずつに分割
	{
		Name: "partition",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgRange("partition", args, 2, 3); err != nil {
				return err
			}

			nValue, ok := integerForOperation(args[0])
			if !ok {
				return newArgumentError("partition: first argument must be a number, got %s", args[0].Type())
			}
			n := nValue
			if n <= 0 {
				return newArgumentError("partition: n must be positive")
			}

			step := n
			collIdx := 1
			if len(args) == 3 {
				stepValue, ok := integerForOperation(args[1])
				if !ok {
					return newArgumentError("partition: step must be a number, got %s", args[1].Type())
				}
				step = stepValue
				if step <= 0 {
					return newArgumentError("partition: step must be positive")
				}
				collIdx = 2
			}

			items, materializeErr := toSlice(args[collIdx])
			if materializeErr != nil {
				return RuntimeErrf("partition", "%v", materializeErr)
			}
			if len(items) == 0 {
				return coll.NewList()
			}

			result := make([]types.Value, 0)
			for i := 0; i < len(items); {
				remaining := len(items) - i
				// Bound n before converting to int or adding to the index.
				if n > int64(remaining) {
					break
				}
				chunk := coll.NewList(items[i : i+int(n)]...)
				result = append(result, chunk)
				if step >= int64(remaining) {
					break
				}
				i += int(step)
			}
			return coll.NewList(result...)
		}},
	},

	// partition-all - Like partition but includes incomplete final group
	// (partition-all n coll) or (partition-all n step coll)
	{
		Name: "partition-all",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgRange("partition-all", args, 2, 3); err != nil {
				return err
			}

			nValue, ok := integerForOperation(args[0])
			if !ok {
				return newArgumentError("partition-all: first argument must be a number, got %s", args[0].Type())
			}
			n := nValue
			if n <= 0 {
				return newArgumentError("partition-all: n must be positive")
			}

			step := n
			collIdx := 1
			if len(args) == 3 {
				stepValue, ok := integerForOperation(args[1])
				if !ok {
					return newArgumentError("partition-all: step must be a number, got %s", args[1].Type())
				}
				step = stepValue
				if step <= 0 {
					return newArgumentError("partition-all: step must be positive")
				}
				collIdx = 2
			}

			items, materializeErr := toSlice(args[collIdx])
			if materializeErr != nil {
				return RuntimeErrf("partition-all", "%v", materializeErr)
			}
			if len(items) == 0 {
				return coll.NewList()
			}

			result := make([]types.Value, 0)
			for i := 0; i < len(items); {
				remaining := len(items) - i
				chunkSize := remaining
				// Clamp n before converting to int or adding to the index.
				if n < int64(chunkSize) {
					chunkSize = int(n)
				}
				chunk := coll.NewList(items[i : i+chunkSize]...)
				result = append(result, chunk)
				if step >= int64(remaining) {
					break
				}
				i += int(step)
			}
			return coll.NewList(result...)
		}},
	},

	// partition-by - 関数の結果でグループ化
	{
		Name: "partition-by",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("partition-by", args, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("partition-by: VM not available in context")
			}

			fn := args[0]
			items, err := toSlice(args[1])
			if err != nil {
				return nil, err
			}
			if len(items) == 0 {
				return coll.NewList(), nil
			}

			result := make([]types.Value, 0)
			currentGroup := []types.Value{items[0]}

			prevVal, err := ApplyCallableFast(ctx, vm, fn, []types.Value{items[0]})
			if err != nil {
				return nil, err
			}

			for i := 1; i < len(items); i++ {
				currVal, err := ApplyCallableFast(ctx, vm, fn, []types.Value{items[i]})
				if err != nil {
					return nil, err
				}

				if currVal.Equals(prevVal) {
					currentGroup = append(currentGroup, items[i])
				} else {
					result = append(result, coll.NewList(currentGroup...))
					currentGroup = []types.Value{items[i]}
					prevVal = currVal
				}
			}
			result = append(result, coll.NewList(currentGroup...))

			return coll.NewList(result...), nil
		}},
	},

	// split-with - 述語で分割
	{
		Name: "split-with",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("split-with", args, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("split-with: VM not available in context")
			}

			pred := args[0]
			items, err := toSlice(args[1])
			if err != nil {
				return nil, err
			}
			if len(items) == 0 {
				return coll.NewVector(coll.NewList(), coll.NewList()), nil
			}

			// 述語が真である限り要素を取る
			splitIdx := len(items)
			for i, item := range items {
				result, err := ApplyCallableFast(ctx, vm, pred, []types.Value{item})
				if err != nil {
					return nil, err
				}
				if !isTruthy(result) {
					splitIdx = i
					break
				}
			}

			return coll.NewVector(coll.NewList(items[:splitIdx]...), coll.NewList(items[splitIdx:]...)), nil
		}},
	},
}

// assocInRecursive はネストしたマップに値を設定する再帰ヘルパーです。
func assocInRecursive(m types.Value, keys []types.Value, val types.Value) types.Value {
	var mapVal *coll.Map
	if m == nil || m == atom.NilValue {
		mapVal = coll.NewEmptyMap()
	} else {
		var ok bool
		mapVal, ok = m.(*coll.Map)
		if !ok {
			return newArgumentError("assoc-in: expected map or nil, got %s", m.Type())
		}
	}

	if len(keys) == 1 {
		// 最後のキー: 値を設定
		return mapVal.Assoc(keys[0], val)
	}

	// 中間キー: 再帰的に更新
	key := keys[0]
	rest := keys[1:]

	inner := mapVal.Get(key)
	newInner := assocInRecursive(inner, rest, val)
	if err, ok := newInner.(*Error); ok {
		return err
	}
	return mapVal.Assoc(key, newInner)
}

// updateInRecursive はネストしたマップの値を関数で更新する再帰ヘルパーです。
func updateInRecursive(ctx context.Context, vm *VM, m types.Value, keys []types.Value, fn types.Value, extraArgs []types.Value) (types.Value, error) {
	var mapVal *coll.Map
	if m == nil || m == atom.NilValue {
		mapVal = coll.NewEmptyMap()
	} else {
		var ok bool
		mapVal, ok = m.(*coll.Map)
		if !ok {
			return nil, newArgumentError("update-in: expected map or nil, got %s", m.Type())
		}
	}

	if len(keys) == 1 {
		// 最後のキー: 関数を適用
		key := keys[0]
		var currentVal types.Value = atom.NilValue
		if val := mapVal.Get(key); val != nil {
			currentVal = val
		}

		fnArgs := make([]types.Value, 0, 1+len(extraArgs))
		fnArgs = append(fnArgs, currentVal)
		fnArgs = append(fnArgs, extraArgs...)

		newVal, err := ApplyCallableFast(ctx, vm, fn, fnArgs)
		if err != nil {
			return nil, err
		}

		return mapVal.Assoc(key, newVal), nil
	}

	// 中間キー: 再帰的に更新
	key := keys[0]
	rest := keys[1:]

	inner := mapVal.Get(key)
	newInner, err := updateInRecursive(ctx, vm, inner, rest, fn, extraArgs)
	if err != nil {
		return nil, err
	}

	return mapVal.Assoc(key, newInner), nil
}

// flattenRecursive はネストしたコレクションを再帰的に平坦化します。
func flattenRecursive(v types.Value, result *[]types.Value) error {
	if v == nil || v == atom.NilValue {
		return nil
	}

	switch c := v.(type) {
	case *coll.Vector:
		for _, item := range c.Items {
			if err := flattenRecursive(item, result); err != nil {
				return err
			}
		}
	case *coll.List:
		for _, item := range c.Items {
			if err := flattenRecursive(item, result); err != nil {
				return err
			}
		}
	case types.Seq:
		seq := c
		for seq != nil {
			if err := sequenceEvalError(seq); err != nil {
				return err
			}
			first := seq.First()
			if first == nil {
				break
			}
			if err := flattenRecursive(first, result); err != nil {
				return err
			}
			seq = seq.Next()
		}
	default:
		// アトム値はそのまま追加
		*result = append(*result, v)
	}
	return nil
}
