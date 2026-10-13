// Package vm はDoggo言語の仮想マシンを実装します。
package vm

import (
	"fmt"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// executeDefVar は Var 定義（def）を実行します。
// 既存の Var があれば値を更新し、なければ新しい Var を作成してグローバルに格納します。
//
// globalIndex: グローバル変数のインデックス
// metaIndex: メタデータの定数プールインデックス（0 の場合はメタデータなし）
// newVal: 定義する値
func (vm *VM) executeDefVar(globalIndex, metaIndex int, newVal types.Value) {
	slot := vm.globals[globalIndex]
	var metadata *coll.Map
	if metaIndex > 0 && metaIndex <= len(vm.constants) {
		if meta, ok := vm.constants[metaIndex-1].(*coll.Map); ok {
			metadata = meta
		}
	}

	// 既存のVarがあれば更新、なければ新規作成
	if v, ok := slot.(*atom.Var); ok {
		v.Value = newVal
		v.Metadata = metadata
		return
	}

	// 新しいVarを作成
	// シンボル名は定数プールから取得するか、インデックスベースで生成
	varName := types.Symbol(fmt.Sprintf("var-%d", globalIndex))
	newVar := atom.NewVar(varName, newVal)

	newVar.Metadata = metadata

	vm.globals[globalIndex] = newVar
}
