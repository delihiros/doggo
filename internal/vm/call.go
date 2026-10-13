// Package vm はDoggo言語の仮想マシンを実装します。
package vm

import (
	"context"

	"doggo/internal/types"
)

// Apply はGoからDoggo関数を同期的に呼び出すためのエントリポイントです。
func (vm *VM) Apply(ctx context.Context, fn types.Value, args []types.Value) (types.Value, error) {
	return newVMExecutionMachine(vm).Apply(ctx, fn, args)
}
