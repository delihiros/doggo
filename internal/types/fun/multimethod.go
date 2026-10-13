package fun

import (
	"context"
	"fmt"
	"sync"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
	"doggo/internal/types/util"
)

// Multimethod は多重ディスパッチ関数を表します。
// dispatch function の戻り値に基づいて、登録された method を選択して実行します。
type Multimethod struct {
	Name     string        // マルチメソッド名（デバッグ用）
	Dispatch types.Value   // dispatch function（types.Fn を想定）
	Methods  *coll.Map     // dispatch-val → method（types.Fn）のマッピング
	mu       sync.RWMutex  // method 登録時の排他制御
}

// NewMultimethod は新しい Multimethod を作成します。
func NewMultimethod(name string, dispatchFn types.Value) *Multimethod {
	return &Multimethod{
		Name:     name,
		Dispatch: dispatchFn,
		Methods:  coll.NewEmptyMap(),
	}
}

// AddMethod は dispatch value に対応する method を登録します。
// 既存の dispatch value がある場合は上書きされます。
func (m *Multimethod) AddMethod(dispatchVal, method types.Value) *Multimethod {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Methods = m.Methods.Assoc(dispatchVal, method).(*coll.Map)
	return m
}

// GetMethod は dispatch value に対応する method を取得します。
// 見つからない場合は nil を返します。
func (m *Multimethod) GetMethod(dispatchVal types.Value) types.Value {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Methods.Get(dispatchVal)
}

// String は multimethod の文字列表現を返します。
func (m *Multimethod) String() string {
	if m.Name != "" {
		return "#<multimethod:" + m.Name + ">"
	}
	return "#<multimethod>"
}

// Type は型名を返します。
func (m *Multimethod) Type() string {
	return "multimethod"
}

// Hash はハッシュ値を返します（ポインタアドレスベース）。
func (m *Multimethod) Hash() uint32 {
	return util.HashString(fmt.Sprintf("%p", m))
}

// Equals は等価性を判定します（同一性比較）。
func (m *Multimethod) Equals(other types.Value) bool {
	if m == nil {
		return other == nil
	}
	if other == nil {
		return false
	}
	o, ok := other.(*Multimethod)
	if !ok {
		return false
	}
	return m == o
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (m *Multimethod) IsSelfEvaluating() bool {
	return true
}

// Invoke は multimethod を呼び出します。
// 1. dispatch function を引数で呼び出して dispatch value を得る
// 2. dispatch value に対応する method を探す
// 3. なければ :default を探す
// 4. method を同じ引数で呼び出す
func (m *Multimethod) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	// dispatch function を呼び出して dispatch value を取得
	dispatchFn, ok := m.Dispatch.(types.Fn)
	if !ok {
		return nil, fmt.Errorf("multimethod %s: dispatch is not a function", m.Name)
	}

	dispatchVal, err := dispatchFn.Invoke(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("multimethod %s: dispatch function error: %v", m.Name, err)
	}

	// dispatch value に対応する method を探す
	method := m.GetMethod(dispatchVal)

	// 見つからなければ :default を試す
	if method == nil {
		method = m.GetMethod(atom.NewKeyword("default"))
	}

	// それでも見つからなければエラー
	if method == nil {
		return nil, fmt.Errorf("multimethod %s: no method for dispatch value %s", m.Name, dispatchVal.String())
	}

	// method を呼び出す
	methodFn, ok := method.(types.Fn)
	if !ok {
		return nil, fmt.Errorf("multimethod %s: method for %s is not a function", m.Name, dispatchVal.String())
	}

	return methodFn.Invoke(ctx, args...)
}

