// Package vm はDoggo言語の仮想マシンを実装します。
package vm

import (
	"context"
	"fmt"

	"doggo/internal/compiler"
	"doggo/internal/stm"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

// executeDosync はSTMトランザクションを実行します。
// スタック上のクロージャをトランザクション内で実行します。
func (vm *VM) executeDosync() error {
	// クロージャをスタックから取得
	closureVal := vm.pop()
	closure, ok := closureVal.(*compiler.Closure)
	if !ok {
		return vm.newRuntimeError("dosync: expected closure, got %s", closureVal.Type())
	}

	store := vm.GetOrCreateSTMStore()

	// トランザクション内でクロージャを実行
	ctx := bindVMContext(vm.ctx, vm)
	var result types.Value
	var err error
	if stm.GetTransaction(ctx) != nil {
		result, err = vm.Apply(ctx, closure, nil)
	} else {
		result, err = store.RunInTransaction(ctx, func(txCtx context.Context) (types.Value, error) {
			return vm.Apply(txCtx, closure, nil)
		})
	}

	if err != nil {
		return fmt.Errorf("dosync: %w", err)
	}

	// 結果をスタックにプッシュ
	if result == nil {
		return vm.push(atom.NilValue)
	}
	return vm.push(result)
}

// GetOrCreateSTMStore はSTMストアを返します。ない場合は作成します。
func (vm *VM) GetOrCreateSTMStore() *stm.Store {
	return newVMExecutionMachine(vm).stmStore()
}

// SetSTMStore はSTMストアを設定します。
func (vm *VM) SetSTMStore(store *stm.Store) {
	newVMExecutionMachine(vm).setSTMStore(store)
}
