// Package vm はDoggo言語の仮想マシンを実装します。
package vm

import (
	"fmt"

	"doggo/internal/opcode"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// executeBinaryOperation は二項演算を実行します。
func (vm *VM) executeBinaryOperation(op opcode.Opcode) error {
	right := vm.pop()
	left := vm.pop()

	// types.Number インターフェースで判定（atom.Number, coll.RangeInt 両方対応）
	leftNum, leftOk := left.(types.Number)
	rightNum, rightOk := right.(types.Number)

	if leftOk && rightOk {
		return vm.executeBinaryNumberOperationGeneric(op, leftNum, rightNum)
	}

	return vm.newRuntimeError("unsupported types for binary operation: %s and %s",
		left.Type(), right.Type())
}

// executeBinaryNumberOperationGeneric は汎用数値型の二項演算を実行します。
func (vm *VM) executeBinaryNumberOperationGeneric(op opcode.Opcode, left, right types.Number) error {
	if op == opcode.OpMod {
		leftInt, leftOK := types.TruncateInt64Value(left)
		rightInt, rightOK := types.TruncateInt64Value(right)
		if !leftOK || !rightOK {
			return vm.newRuntimeError("modulo operands must be finite numbers in int64 range")
		}
		result, ok := types.ModInt64(leftInt, rightInt)
		if !ok {
			return vm.newRuntimeError("division by zero")
		}
		return vm.push(atom.NewNumberInt(result))
	}

	leftInt, leftIsInt := types.Int64Value(left)
	rightInt, rightIsInt := types.Int64Value(right)
	if leftIsInt && rightIsInt && op != opcode.OpDiv {
		result, ok := checkedIntOperation(op, leftInt, rightInt)
		if !ok {
			return vm.newRuntimeError("integer overflow")
		}
		return vm.push(atom.NewNumberInt(result))
	}

	leftVal := left.ToFloat64()
	rightVal := right.ToFloat64()

	var result float64

	switch op {
	case opcode.OpAdd:
		result = leftVal + rightVal
	case opcode.OpSub:
		result = leftVal - rightVal
	case opcode.OpMul:
		result = leftVal * rightVal
	case opcode.OpDiv:
		if rightVal == 0 {
			return vm.newRuntimeError("division by zero")
		}
		result = leftVal / rightVal
	default:
		return fmt.Errorf("unknown number operator: %d", op)
	}

	return vm.push(atom.NewNumberFloat(result))
}

// executeNegation は単項マイナスを実行します。
func (vm *VM) executeNegation() error {
	operand := vm.pop()

	num, ok := operand.(types.Number)
	if !ok {
		return vm.newRuntimeError("unsupported type for negation: %s", operand.Type())
	}

	if integer, ok := types.Int64Value(num); ok {
		result, ok := types.CheckedNegInt64(integer)
		if !ok {
			return vm.newRuntimeError("integer overflow")
		}
		return vm.push(atom.NewNumberInt(result))
	}
	return vm.push(atom.NewNumberFloat(-num.ToFloat64()))
}

func checkedIntOperation(op opcode.Opcode, left, right int64) (int64, bool) {
	switch op {
	case opcode.OpAdd:
		return types.CheckedAddInt64(left, right)
	case opcode.OpSub:
		return types.CheckedSubInt64(left, right)
	case opcode.OpMul:
		return types.CheckedMulInt64(left, right)
	default:
		return 0, false
	}
}

// executeComparison は比較演算を実行します。
func (vm *VM) executeComparison(op opcode.Opcode) error {
	right := vm.pop()
	left := vm.pop()

	switch op {
	case opcode.OpEq:
		equal, err := coll.ValueEqualsResult(left, right)
		if err != nil {
			return vm.newRuntimeError("%v", err)
		}
		return vm.push(nativeBoolToBooleanObject(equal))
	case opcode.OpNotEq:
		equal, err := coll.ValueEqualsResult(left, right)
		if err != nil {
			return vm.newRuntimeError("%v", err)
		}
		return vm.push(nativeBoolToBooleanObject(!equal))
	}

	cmp, ordered := types.CompareValues(left, right)
	switch op {
	case opcode.OpGT:
		return vm.push(nativeBoolToBooleanObject(ordered && cmp > 0))
	case opcode.OpGTE:
		return vm.push(nativeBoolToBooleanObject(ordered && cmp >= 0))
	case opcode.OpLT:
		return vm.push(nativeBoolToBooleanObject(ordered && cmp < 0))
	case opcode.OpLTE:
		return vm.push(nativeBoolToBooleanObject(ordered && cmp <= 0))
	default:
		return fmt.Errorf("unknown comparison operator: %d", op)
	}
}

// executeNot は論理否定を実行します。
func (vm *VM) executeNot() error {
	operand := vm.pop()
	return vm.push(nativeBoolToBooleanObject(!isTruthy(operand)))
}

// executeArray はベクターを構築してスタックにプッシュします。
func (vm *VM) executeArray(numElements int) error {
	array := vm.buildArray(vm.sp-numElements, vm.sp)
	vm.sp -= numElements
	return vm.push(array)
}

// executeHash はマップを構築してスタックにプッシュします。
func (vm *VM) executeHash(numElements int) error {
	hash := vm.buildHash(vm.sp-numElements, vm.sp)
	vm.sp -= numElements
	if err := coll.CollectionError(hash); err != nil {
		return err
	}
	return vm.push(hash)
}

// executeSet はセットを構築してスタックにプッシュします。
func (vm *VM) executeSet(numElements int) error {
	set := vm.buildSet(vm.sp-numElements, vm.sp)
	vm.sp -= numElements
	if err := coll.CollectionError(set); err != nil {
		return err
	}
	return vm.push(set)
}

// buildArray はベクターを構築します。
func (vm *VM) buildArray(startIndex, endIndex int) types.Value {
	elements := make([]types.Value, endIndex-startIndex)
	for i := startIndex; i < endIndex; i++ {
		elements[i-startIndex] = vm.stack[i]
	}
	return coll.NewVector(elements...)
}

// buildHash はマップを構築します。
// 挿入順を保持するため NewMapFromPairs を使用します。
func (vm *VM) buildHash(startIndex, endIndex int) types.Value {
	pairs := make([]types.Value, 0, endIndex-startIndex)
	for i := startIndex; i < endIndex; i++ {
		pairs = append(pairs, vm.stack[i])
	}
	return coll.NewMapFromPairs(pairs)
}

// buildSet はセットを構築します。
func (vm *VM) buildSet(startIndex, endIndex int) types.Value {
	elements := make([]types.Value, endIndex-startIndex)
	for i := startIndex; i < endIndex; i++ {
		elements[i-startIndex] = vm.stack[i]
	}
	return coll.NewSet(elements...)
}

// executeIndexExpression はインデックスアクセスを実行します。
func (vm *VM) executeIndexExpression(left, index types.Value) error {
	_, indexIsNumber := index.(types.Number)
	switch {
	case left.Type() == "vector" && indexIsNumber:
		return vm.executeVectorIndex(left, index)
	case left.Type() == "map":
		return vm.executeMapIndex(left, index)
	default:
		return vm.newRuntimeError("index operator not supported: %s", left.Type())
	}
}

// executeVectorIndex はベクターのインデックスアクセスを実行します。
func (vm *VM) executeVectorIndex(vector, index types.Value) error {
	vec := vector.(*coll.Vector)
	integer, ok := integerForOperation(index)
	if !ok {
		return vm.push(atom.NilValue)
	}
	i := int(integer)

	if i < 0 || i >= len(vec.Items) {
		return vm.push(atom.NilValue)
	}
	return vm.push(vec.Items[i])
}

// executeMapIndex はマップのインデックスアクセスを実行します。
func (vm *VM) executeMapIndex(hash, key types.Value) error {
	m := hash.(*coll.Map)
	value := m.Get(key)
	if value == nil {
		return vm.push(atom.NilValue)
	}
	return vm.push(value)
}

// isTruthy は値が真かどうかを判定します。
func isTruthy(obj types.Value) bool {
	if obj == nil {
		return false
	}
	switch obj := obj.(type) {
	case atom.Boolean:
		return bool(obj)
	case types.Nil:
		return false
	default:
		return true
	}
}

// nativeBoolToBooleanObject はGoのboolをBooleanに変換します。
func nativeBoolToBooleanObject(input bool) atom.Boolean {
	if input {
		return atom.True
	}
	return atom.False
}
