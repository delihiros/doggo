// builtins_sort.go はソートと集計の組み込み関数を定義します。
// group-by, sort, sort-by をサポートします。
package vm

import (
	"context"
	"fmt"
	"math"
	"sort"

	"doggo/internal/types"
	"doggo/internal/types/coll"
)

// builtinsSortCtx はソートと集計の組み込み関数を定義します
var builtinsSortCtx = []BuiltinDefinition{
	// group-by - 関数の結果でグループ化してマップを返す
	{
		Name: "group-by",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("group-by", args, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("group-by: VM not available in context")
			}

			fn := args[0]
			items, err := toSlice(args[1])
			if err != nil {
				return nil, err
			}
			if len(items) == 0 {
				return coll.NewEmptyMap(), nil
			}

			// グループを管理（Equals比較のため、キーとベクターを別管理）
			type groupEntry struct {
				key    types.Value
				values []types.Value
			}
			groups := make([]groupEntry, 0)

			for _, item := range items {
				// 関数を適用してグループキーを取得
				groupKey, err := ApplyCallableFast(ctx, vm, fn, []types.Value{item})
				if err != nil {
					return nil, err
				}

				// 既存のグループを検索
				found := false
				for i := range groups {
					if groups[i].key.Equals(groupKey) {
						groups[i].values = append(groups[i].values, item)
						found = true
						break
					}
				}
				if !found {
					groups = append(groups, groupEntry{key: groupKey, values: []types.Value{item}})
				}
			}

			// 結果のマップを構築
			result := coll.NewEmptyMap()
			for _, g := range groups {
				result = result.Assoc(g.key, coll.NewVector(g.values...)).(*coll.Map)
			}
			return result, nil
		}},
	},

	// sort - コレクションをソート（比較可能な要素のみ）
	{
		Name: "sort",
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("sort", args, 1); err != nil {
				return err
			}

			items, err := toSlice(args[0])
			if err != nil {
				return RuntimeErrf("sort", "%v", err)
			}
			if len(items) == 0 {
				return coll.NewList()
			}

			// コピーを作成してソート
			sorted := make([]types.Value, len(items))
			copy(sorted, items)

			sort.SliceStable(sorted, func(i, j int) bool {
				return compareValues(sorted[i], sorted[j]) < 0
			})

			return coll.NewList(sorted...)
		}},
	},

	// sort-by - 関数の結果でソート
	{
		Name: "sort-by",
		Builtin: &Builtin{CtxFn: func(ctx context.Context, args ...types.Value) (types.Value, error) {
			if err := CheckArgCountCtx("sort-by", args, 2); err != nil {
				return nil, err
			}

			vm := GetVM(ctx)
			if vm == nil {
				return nil, fmt.Errorf("sort-by: VM not available in context")
			}

			keyFn := args[0]
			items, err := toSlice(args[1])
			if err != nil {
				return nil, err
			}
			if len(items) == 0 {
				return coll.NewList(), nil
			}

			// 各要素のソートキーを事前計算
			type sortEntry struct {
				value types.Value
				key   types.Value
			}
			entries := make([]sortEntry, len(items))

			for i, item := range items {
				key, err := ApplyCallableFast(ctx, vm, keyFn, []types.Value{item})
				if err != nil {
					return nil, err
				}
				entries[i] = sortEntry{value: item, key: key}
			}

			// ソートキーでソート
			sort.SliceStable(entries, func(i, j int) bool {
				return compareValues(entries[i].key, entries[j].key) < 0
			})

			// 結果を構築
			result := make([]types.Value, len(entries))
			for i, e := range entries {
				result[i] = e.value
			}

			return coll.NewList(result...), nil
		}},
	},
}

// compareValues は2つの値を比較します。
// 数値: 数値順
// 文字列: 辞書順
// その他: 文字列表現で比較
// 戻り値: 負=a<b, 0=a==b, 正=a>b
func compareValues(a, b types.Value) int {
	if cmp, ordered := compareValuesOrdered(a, b); ordered {
		return cmp
	}

	// Relational operators keep NaN unordered, but sort requires a total order.
	// Place NaN after all ordered numeric values and keep NaNs stable.
	aNumber, aIsNumber := a.(types.Number)
	bNumber, bIsNumber := b.(types.Number)
	aNaN := aIsNumber && math.IsNaN(aNumber.ToFloat64())
	bNaN := bIsNumber && math.IsNaN(bNumber.ToFloat64())
	switch {
	case aNaN && bNaN:
		return 0
	case aNaN:
		return 1
	case bNaN:
		return -1
	default:
		return 0
	}
}

func compareValuesOrdered(a, b types.Value) (int, bool) {
	return types.CompareValues(a, b)
}
