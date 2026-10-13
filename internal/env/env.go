package env

import (
	"errors"
	"fmt"
	"sync"

	"doggo/internal/stm"
	"doggo/internal/types"
)

// ErrRootNotGlobalEnv はルート環境が *GlobalEnv でない場合に返されるエラーです。
var ErrRootNotGlobalEnv = errors.New("root environment is not *GlobalEnv")

// GlobalEnv はスレッドセーフなルート環境です。
// 通常、プログラム全体で1つ（またはNamespaceごとに1つ）存在します。
type GlobalEnv struct {
	store    sync.Map // map[string]*EnvVar
	stmStore *stm.Store
}

// NewGlobalEnvironment は新しいグローバル環境を作成します。
func NewGlobalEnvironment() *GlobalEnv {
	return &GlobalEnv{
		stmStore: stm.NewStore(),
	}
}

// GetOrCreateVar returns the existing EnvVar or atomically creates it.
func (g *GlobalEnv) GetOrCreateVar(sym types.Symbol) *EnvVar {
	name := string(sym)
	// LoadOrStore はアトミックに実行されます
	v, _ := g.store.LoadOrStore(name, &EnvVar{Name: name})
	return v.(*EnvVar)
}

// Get はEnvVarの値を取得します。
func (g *GlobalEnv) Get(key types.Value) (types.Value, bool) {
	sym, ok := key.(types.Symbol)
	if !ok {
		return nil, false
	}
	name := string(sym)
	if v, ok := g.store.Load(name); ok {
		return v.(*EnvVar).Get(), true
	}
	return nil, false
}

// Set はEnvVarの値を更新します。
func (g *GlobalEnv) Set(key types.Value, val types.Value) error {
	sym, ok := key.(types.Symbol)
	if !ok {
		return fmt.Errorf("cannot set value for non-symbol key: %s", key.Type())
	}
	v := g.GetOrCreateVar(sym)
	v.Set(val)
	return nil
}

// Child は子スコープを作成します。
// Environmentインターフェースの実装です。
func (g *GlobalEnv) Child() types.Environment {
	// グローバル環境の子環境を作成
	return NewLocalEnvironment(g)
}

// Root はルート環境を取得します。
// Environmentインターフェースの実装です。
func (g *GlobalEnv) Root() types.Environment {
	return g
}

// STMStore はSTMストアを取得します。
func (g *GlobalEnv) STMStore() *stm.Store {
	return g.stmStore
}

// GetSTMStore は環境からSTMストアを取得するヘルパー関数です。
// ルート環境が *GlobalEnv でない場合は ErrRootNotGlobalEnv を返します。
func GetSTMStore(e types.Environment) (*stm.Store, error) {
	root := e.Root()
	if g, ok := root.(*GlobalEnv); ok {
		return g.STMStore(), nil
	}
	return nil, ErrRootNotGlobalEnv
}

// EnvVar は可変な値を保持するコンテナです。
// types.Varとは異なり、グローバル環境の内部実装用です。
type EnvVar struct {
	Name string
	val  types.Value
	mu   sync.RWMutex
}

// Set はEnvVarの値を設定します。
func (v *EnvVar) Set(val types.Value) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.val = val
}

// Get はEnvVarの値を取得します。
func (v *EnvVar) Get() types.Value {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.val
}

// LocalEnv は関数呼び出しや let ブロックのための環境です。
// 不変なリンクリスト構造を採用しています。
//
// 設計上の決定 (Why Linked List?):
//
//  1. O(1) Extend: 新しい変数の定義（スコープ拡張）が定数時間で可能。
//     Mapベースの実装ではコピーコストが発生し、定義性能が O(N) 以上に劣化する。
//     （検証済み: キャッシュ導入で Extend が13倍遅延、全体性能が悪化）
//
//  2. Immutability: 親環境への影響を与えずにスコープを分岐できる。
//     クロージャや並行処理で安全にスコープを共有可能。
//
//  3. Trade-off: 変数参照は O(N) だが、通常ローカル変数は少数（<10）であり、
//     線形探索でも十分高速。定義コストの低さが全体的なパフォーマンスに寄与する。
//
// ロックが不要なのも利点（不変構造のため）。
type LocalEnv struct {
	Key    string
	Val    types.Value
	Next   *LocalEnv  // 親のローカル環境
	Global *GlobalEnv // ローカルで見つからなかったらここを見る
}

// NewLocalEnvironment はグローバル環境をベースにした空のローカル環境を作ります。
func NewLocalEnvironment(g *GlobalEnv) *LocalEnv {
	return &LocalEnv{Global: g}
}

// Extend は新しい変数を束縛してスコープを拡張します。
// O(1) で作成でき、ロックも不要です。
func (e *LocalEnv) Extend(sym types.Symbol, val types.Value) *LocalEnv {
	return &LocalEnv{
		Key:    string(sym),
		Val:    val,
		Next:   e,        // 親を指す
		Global: e.Global, // Globalへの参照を引き継ぐ
	}
}

// Get は値を検索します。
// Environmentインターフェースの実装です。
func (e *LocalEnv) Get(key types.Value) (types.Value, bool) {
	sym, ok := key.(types.Symbol)
	if !ok {
		return nil, false
	}
	name := string(sym)
	// 1. ローカル変数をリンクリストで探索 (線形探索)
	// ローカル変数は通常数が少ないため、Mapより高速な場合が多い
	for node := e; node != nil; node = node.Next {
		// Keyが空の場合は末端（Globalへのリンク用ノード）とみなす
		if node.Key == name {
			return node.Val, true
		}
		// 末端まで来たらグローバルを見る
		if node.Next == nil && node.Global != nil {
			return node.Global.Get(sym)
		}
	}
	return nil, false
}

// Set はシンボルに値を設定します。
// Environmentインターフェースの実装です。
// Clojureスタイル: ローカル変数は不変のため、ローカル変数の書き換えを禁止し、グローバル変数のみ更新可能
// グローバル変数が存在しない場合は新規作成します（Global.Set は GetOrCreateVar を使用）
func (e *LocalEnv) Set(key types.Value, value types.Value) error {
	sym, ok := key.(types.Symbol)
	if !ok {
		return fmt.Errorf("cannot set value for non-symbol key: %s", key.Type())
	}
	name := string(sym)

	// 1. ローカル変数が存在するかチェック
	for node := e; node != nil; node = node.Next {
		if node.Key == name {
			// Clojureスタイル: ローカル変数は不変
			// もしSchemeスタイルにしたいなら、構造体のValをポインタにする必要がある
			return fmt.Errorf("cannot set local variable %s: locals are immutable", name)
		}
		if node.Next == nil {
			break
		}
	}

	// 2. ローカルになければグローバルを更新（存在しない場合は新規作成）
	if e.Global != nil {
		// Global.Set は GetOrCreateVar を使用するため、存在しない場合は新規作成される
		return e.Global.Set(sym, value)
	}

	return fmt.Errorf("symbol not found or unbound: %s (no global environment)", name)
}

// Child は子スコープを作成します。
// Environmentインターフェースの実装です。
func (e *LocalEnv) Child() types.Environment {
	// 空の拡張を返す（Extendで変数を追加できる）
	return &LocalEnv{
		Next:   e,
		Global: e.Global,
	}
}

// Root はルート環境を取得します。
// Environmentインターフェースの実装です。
func (e *LocalEnv) Root() types.Environment {
	if e.Global == nil {
		// Globalがない場合は自分自身を返す
		return e
	}
	// GlobalEnvを直接返す
	return e.Global
}

// NewEnvironment は新しい環境を作成します。
// GlobalEnvとLocalEnvを使用します。
func NewEnvironment() types.Environment {
	global := NewGlobalEnvironment()
	return NewLocalEnvironment(global)
}
