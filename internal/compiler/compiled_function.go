package compiler

import (
	"fmt"

	"doggo/internal/opcode"
	"doggo/internal/types"
	"doggo/internal/types/util"
)

// ArityInfo は関数の特定の引数パターン（アリティ）に関する情報を保持します。
type ArityInfo struct {
	NumParameters int  // 引数の数
	Variadic      bool // 可変長引数かどうか
	EntryIP       int  // このアリティの開始命令オフセット
}

func newCompiledFunction(meta bytecodeMetadata, instructions opcode.Instructions, numLocals, numParameters int, variadic bool, namespace, name string, arities []ArityInfo) *CompiledFunction {
	return &CompiledFunction{
		Instructions:  instructions,
		NumLocals:     numLocals,
		NumParameters: numParameters,
		Variadic:      variadic,
		Lines:         append([]int(nil), meta.Lines...),
		Columns:       append([]int(nil), meta.Columns...),
		SourceFile:    meta.SourceFile,
		Namespace:     namespace,
		Name:          name,
		Arities:       append([]ArityInfo(nil), arities...),
	}
}

// CompiledFunction はコンパイルされた関数を表します。
type CompiledFunction struct {
	Instructions  opcode.Instructions
	NumLocals     int         // ローカル変数の数（全アリティでの最大値）
	NumParameters int         // 仮引数の数（デフォルトアリティ）
	Variadic      bool        // 可変長引数を受け取るかどうか（デフォルトアリティ）
	Lines         []int       // 各命令の行番号
	Columns       []int       // 各命令の列番号
	SourceFile    string      // ソースファイル名
	Namespace     string      // 関数がコンパイルされた名前空間
	Name          string      // 関数名（プロファイリング用、空文字列は無名関数）
	Arities       []ArityInfo // 複数アリティの定義（デフォルト以外）
}

// String は関数の文字列表現を返します。
func (cf *CompiledFunction) String() string {
	return "#<compiled-function>"
}

// Type は型名を返します。
func (cf *CompiledFunction) Type() string {
	return "compiled-function"
}

// Hash はハッシュ値を返します。
func (cf *CompiledFunction) Hash() uint32 {
	return util.HashString(fmt.Sprintf("%p", cf))
}

// Equals は等価性を判定します。
func (cf *CompiledFunction) Equals(other types.Value) bool {
	o, ok := other.(*CompiledFunction)
	if !ok {
		return false
	}
	return cf == o
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (cf *CompiledFunction) IsSelfEvaluating() bool {
	return true
}

// Closure はクロージャを表します。
type Closure struct {
	Fn   *CompiledFunction
	Free []types.Value // 自由変数
}

// String はクロージャの文字列表現を返します。
func (cl *Closure) String() string {
	return "#<closure>"
}

// Type は型名を返します。
func (cl *Closure) Type() string {
	return "closure"
}

// Hash はハッシュ値を返します。
func (cl *Closure) Hash() uint32 {
	return util.HashString(fmt.Sprintf("%p", cl))
}

// Equals は等価性を判定します。
func (cl *Closure) Equals(other types.Value) bool {
	o, ok := other.(*Closure)
	if !ok {
		return false
	}
	return cl == o
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (cl *Closure) IsSelfEvaluating() bool {
	return true
}
