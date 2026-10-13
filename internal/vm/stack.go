// Package vm はDoggo言語の仮想マシンを実装します。
package vm

import (
	"fmt"

	"doggo/internal/types"
)

// push はスタックに値をプッシュします。
func (vm *VM) push(o types.Value) error {
	if vm.sp >= StackSize {
		return fmt.Errorf("stack overflow")
	}
	vm.stack[vm.sp] = o
	vm.sp++
	return nil
}

// pop はスタックから値をポップします。
func (vm *VM) pop() types.Value {
	o := vm.stack[vm.sp-1]
	vm.sp--
	return o
}

// StackTop はスタックトップの値を返します。
func (vm *VM) StackTop() types.Value {
	if vm.sp == 0 {
		return nil
	}
	return vm.stack[vm.sp-1]
}

// LastPoppedStackElem は最後にポップされた要素を返します。
func (vm *VM) LastPoppedStackElem() types.Value {
	return vm.stack[vm.sp]
}

// currentFrame は現在のフレームを返します。
func (vm *VM) currentFrame() *Frame {
	return vm.frames[vm.frameIndex-1]
}

// pushFrame はフレームをプッシュします。
func (vm *VM) pushFrame(f *Frame) {
	vm.frames[vm.frameIndex] = f
	vm.frameIndex++
}

// popFrame はフレームをポップします。
func (vm *VM) popFrame() *Frame {
	vm.frameIndex--
	return vm.frames[vm.frameIndex]
}
