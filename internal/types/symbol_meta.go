package types

import (
	"context"
	"fmt"

	"doggo/internal/types/util"
)

// SymbolWithMeta はメタデータ付きのシンボルを表します。
// 通常の Symbol との互換性を保ちつつ、メタデータを保持します。
// Equals/Hash は Name のみで決定され、メタデータは無視されます（Clojure互換）。
type SymbolWithMeta struct {
	Name string // シンボル名
	Meta Map    // メタデータ（types.Map インターフェース）
}

// NewSymbolWithMeta はメタデータ付きのシンボルを作成します。
func NewSymbolWithMeta(name string, meta Map) *SymbolWithMeta {
	return &SymbolWithMeta{Name: name, Meta: meta}
}

// String はシンボル名を返します。
func (s *SymbolWithMeta) String() string {
	return s.Name
}

// Type は型名を返します。
func (s *SymbolWithMeta) Type() string {
	return "symbol"
}

// Hash はシンボル名のハッシュ値を返します（メタデータは無視）。
func (s *SymbolWithMeta) Hash() uint32 {
	return util.HashString(s.Name)
}

// Equals はシンボル名が等しいかどうかを判定します（メタデータは無視）。
// 通常の Symbol とも等価比較できます。
func (s *SymbolWithMeta) Equals(other Value) bool {
	return symbolEqualsName(s.Name, other)
}

// Invoke implements Fn interface（マップからの値取得）。
func (s *SymbolWithMeta) Invoke(ctx context.Context, args ...Value) (Value, error) {
	return invokeSymbolMapLookup(s.Name, args)
}

func symbolEqualsName(name string, other Value) bool {
	if other == nil {
		return false
	}
	switch o := other.(type) {
	case Symbol:
		return name == string(o)
	case *SymbolWithMeta:
		return name == o.Name
	case *ResolvedSymbol:
		return name == o.Name
	}
	return false
}

func invokeSymbolMapLookup(name string, args []Value) (Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("symbol invocation requires at least 1 argument (map)")
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("symbol invocation expects 1 or 2 arguments (map, default?), got %d", len(args))
	}

	// マップを取得
	m, ok := args[0].(Map)
	if !ok {
		return nil, fmt.Errorf("symbol invocation expects map, got %s", args[0].Type())
	}

	key := Symbol(name)
	val := m.Get(key)
	if val == nil {
		if len(args) == 2 {
			return args[1], nil
		}
		return nil, nil
	}

	return val, nil
}

// IsSelfEvaluating は自己評価型かどうかを返します（シンボルは評価される）。
func (s *SymbolWithMeta) IsSelfEvaluating() bool {
	return false
}

// --- ヘルパー関数（Symbol と SymbolWithMeta を統一的に扱う） ---

// SymbolName は値がシンボル（Symbol または SymbolWithMeta）の場合、その名前を返します。
// シンボルでない場合は ("", false) を返します。
func SymbolName(v Value) (string, bool) {
	switch s := v.(type) {
	case Symbol:
		return string(s), true
	case *SymbolWithMeta:
		return s.Name, true
	case *ResolvedSymbol:
		return s.Name, true
	}
	return "", false
}

// IsSymbol は値がシンボル（Symbol または SymbolWithMeta）かどうかを判定します。
func IsSymbol(v Value) bool {
	switch v.(type) {
	case Symbol, *SymbolWithMeta, *ResolvedSymbol:
		return true
	}
	return false
}

// AsSymbol は値をシンボル名に変換します。
// Symbol の場合はそのまま返し、SymbolWithMeta の場合は Name から Symbol を作成します。
// シンボルでない場合は ("", false) を返します。
func AsSymbol(v Value) (Symbol, bool) {
	switch s := v.(type) {
	case Symbol:
		return s, true
	case *SymbolWithMeta:
		return Symbol(s.Name), true
	case *ResolvedSymbol:
		return Symbol(s.Name), true
	}
	return "", false
}
