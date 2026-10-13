// Package vm はDoggo言語の仮想マシンを実装します。
package vm

import (
	"doggo/internal/compiler"
	"doggo/internal/types/atom"
)

// executeGo はゴルーチンを起動してクロージャを実行します。
// スタック配置: [..., closure]
func (vm *VM) executeGo() error {
	// クロージャをスタックから取得
	closureVal := vm.pop()
	closure, ok := closureVal.(*compiler.Closure)
	if !ok {
		return vm.newRuntimeError("go: expected closure, got %T", closureVal)
	}

	// goroutine 起動前に共有データをキャプチャ
	// VM プールから返却されると constants 等が nil になるため、
	// goroutine が実行される前に値をキャプチャする必要がある
	runtime := vm.snapshotCallableRuntime()
	if runtime == nil {
		return vm.newRuntimeError("go: callable runtime not initialized")
	}
	ctx := forkBindingContext(vm.ctx)

	// 新しいゴルーチンでクロージャを実行
	go func() {
		// 実行（エラーは無視 - ゴルーチン内なので）
		_, _ = runtime.InvokeClosure(ctx, closure, nil)
	}()

	// go 式は nil を返す
	return vm.push(atom.NilValue)
}
