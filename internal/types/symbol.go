package types

import (
	"context"
	"fmt"

	"doggo/internal/types/util"
)

// Symbol はシンボル（変数名、関数名など）を表します。
type Symbol string

func (s Symbol) String() string {
	return string(s)
}

func (s Symbol) Type() string {
	return "symbol"
}

func (s Symbol) Hash() uint32 {
	return util.HashString(string(s))
}

func (s Symbol) Equals(other Value) bool {
	if other == nil {
		return false
	}
	switch o := other.(type) {
	case Symbol:
		return s == o
	case *SymbolWithMeta:
		return string(s) == o.Name
	case *ResolvedSymbol:
		return string(s) == o.Name
	}
	return false
}

// Invoke implements Fn interface.
func (s Symbol) Invoke(ctx context.Context, args ...Value) (Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("symbol invocation requires at least 1 argument (map)")
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("symbol invocation expects 1 or 2 arguments (map, default?), got %d", len(args))
	}

	// マップを取得 (Mapインターフェースを使用)
	m, ok := args[0].(Map)
	if !ok {
		return nil, fmt.Errorf("symbol invocation expects map (Map), got %s", args[0].Type())
	}

	// シンボルをキーとして使用
	key := Value(s)
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

// NewSymbol は新しいシンボルを作成します。
func NewSymbol(name string) Symbol {
	return Symbol(name)
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (s Symbol) IsSelfEvaluating() bool {
	return false
}
