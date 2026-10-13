package atom

import (
	"context"
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/util"
)

// Keyword はキーワード（Clojureの :keyword 形式）を表します。
type Keyword string

func (k Keyword) String() string {
	return ":" + string(k)
}

func (k Keyword) Type() string {
	return "keyword"
}

func (k Keyword) Hash() uint32 {
	return util.HashString(string(k))
}

func (k Keyword) Equals(other types.Value) bool {
	if other == nil {
		return false
	}
	if o, ok := other.(Keyword); ok {
		return k == o
	}
	return false
}

// Invoke implements Fn interface.
func (k Keyword) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("keyword invocation requires at least 1 argument (map)")
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("keyword invocation expects 1 or 2 arguments (map, default?), got %d", len(args))
	}

	// マップを取得 (Mapインターフェースを使用)
	m, ok := args[0].(types.Map)
	if !ok {
		return nil, fmt.Errorf("keyword invocation expects map (Map), got %s", args[0].Type())
	}

	// キーワードをキーとして使用
	key := types.Value(k)
	val := m.Get(key)
	if val == nil {
		// キーが見つからない場合、デフォルト値があれば返す
		if len(args) == 2 {
			return args[1], nil
		}
		return nil, nil
	}

	return val, nil
}

// NewKeyword は新しいキーワードを作成します。
func NewKeyword(name string) Keyword {
	return Keyword(name)
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (k Keyword) IsSelfEvaluating() bool {
	return true
}
