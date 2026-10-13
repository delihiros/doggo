// Package vm - Phase 10l: Closure Pattern Matching for map+reduce fusion
// クロージャのバイトコードを分析し、単純なパターンを検出して高速実行を可能にする。
package vm

import (
	"encoding/binary"

	"doggo/internal/compiler"
	"doggo/internal/opcode"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

// ClosurePatternType は検出されたクロージャパターンの種類
type ClosurePatternType int

const (
	// PatternNone は最適化不可能なパターン
	PatternNone ClosurePatternType = iota
	// PatternLocalOpConst は (op local const) パターン - 例: (* x 2)
	PatternLocalOpConst
	// PatternConstOpLocal は (op const local) パターン - 例: (- 10 x)
	PatternConstOpLocal
)

// ClosurePattern は単純なクロージャのパターン情報を保持する
type ClosurePattern struct {
	Type       ClosurePatternType
	Op         opcode.Opcode // 演算オペコード (OpMul, OpAdd, etc.)
	Const      types.Value   // 定数値
	ConstIsInt bool          // 定数が整数かどうか
	LocalIdx   int           // ローカル変数インデックス
}

// AnalyzeClosurePattern はクロージャを分析し、最適化可能なパターンを検出する。
// constants は VM の定数プールを指す。
func AnalyzeClosurePattern(cl *compiler.Closure, constants []types.Value) *ClosurePattern {
	if cl == nil || cl.Fn == nil {
		return nil
	}

	// 自由変数がある場合は最適化不可
	if len(cl.Free) > 0 {
		return nil
	}

	// NumParameters が 1 でない場合は最適化不可
	if cl.Fn.NumParameters != 1 {
		return nil
	}

	ins := cl.Fn.Instructions

	// パターン 1: OpGetLocal(0), OpConst(idx), Op, OpReturnValue
	// バイト構造: [0x40, localIdx, 0x00, constIdx(2bytes), Op, 0x51]
	// 長さ: 1+1 + 1+2 + 1 + 1 = 7 bytes
	if len(ins) == 7 &&
		opcode.Opcode(ins[0]) == opcode.OpGetLocal &&
		opcode.Opcode(ins[2]) == opcode.OpConst &&
		opcode.Opcode(ins[6]) == opcode.OpReturnValue {
		op := opcode.Opcode(ins[5])
		if isArithmeticOp(op) {
			localIdx := int(ins[1])
			// localIdx が 0 でない場合は最適化不可（引数は常に index 0）
			if localIdx != 0 {
				return nil
			}
			constIdx := int(binary.BigEndian.Uint16(ins[3:5]))
			if constIdx < len(constants) {
				constVal := constants[constIdx]
				// 定数が数値でない場合は最適化不可
				if !isNumericValue(constVal) {
					return nil
				}
				return &ClosurePattern{
					Type:       PatternLocalOpConst,
					Op:         op,
					Const:      constVal,
					ConstIsInt: isIntegerValue(constVal),
					LocalIdx:   localIdx,
				}
			}
		}
	}

	// パターン 2: OpConst(idx), OpGetLocal(0), Op, OpReturnValue
	// バイト構造: [0x00, constIdx(2bytes), 0x40, localIdx, Op, 0x51]
	// 長さ: 1+2 + 1+1 + 1 + 1 = 7 bytes
	if len(ins) == 7 &&
		opcode.Opcode(ins[0]) == opcode.OpConst &&
		opcode.Opcode(ins[3]) == opcode.OpGetLocal &&
		opcode.Opcode(ins[6]) == opcode.OpReturnValue {
		op := opcode.Opcode(ins[5])
		if isArithmeticOp(op) {
			constIdx := int(binary.BigEndian.Uint16(ins[1:3]))
			localIdx := int(ins[4])
			// localIdx が 0 でない場合は最適化不可
			if localIdx != 0 {
				return nil
			}
			if constIdx < len(constants) {
				constVal := constants[constIdx]
				// 定数が数値でない場合は最適化不可
				if !isNumericValue(constVal) {
					return nil
				}
				return &ClosurePattern{
					Type:       PatternConstOpLocal,
					Op:         op,
					Const:      constVal,
					ConstIsInt: isIntegerValue(constVal),
					LocalIdx:   localIdx,
				}
			}
		}
	}

	return nil
}

// isArithmeticOp は演算がサポートされている算術演算かどうかを判定する
func isArithmeticOp(op opcode.Opcode) bool {
	switch op {
	case opcode.OpAdd, opcode.OpSub, opcode.OpMul, opcode.OpDiv, opcode.OpMod:
		return true
	}
	return false
}

// isNumericValue は値が数値かどうかを判定する
func isNumericValue(v types.Value) bool {
	_, ok := v.(types.Number)
	return ok
}

// isIntegerValue は値が整数かどうかを判定する
func isIntegerValue(v types.Value) bool {
	num, ok := v.(types.Number)
	if !ok {
		return false
	}
	_, ok = types.Int64Value(num)
	return ok
}

// Apply はパターンに基づいて演算を実行する
// 引数が数値でない場合は nil を返す（フォールバックが必要）
func (p *ClosurePattern) Apply(arg types.Value) types.Value {
	if p == nil {
		return nil
	}

	// 引数が数値でない場合は nil を返す（フォールバック）
	argNum, ok := arg.(types.Number)
	if !ok {
		return nil
	}

	switch p.Type {
	case PatternLocalOpConst:
		return p.applyLocalOpConst(argNum)
	case PatternConstOpLocal:
		return p.applyConstOpLocal(argNum)
	}
	return nil
}

// applyLocalOpConst は (op local const) パターンを実行する
// 例: (* x 2) → x * 2
func (p *ClosurePattern) applyLocalOpConst(arg types.Number) types.Value {
	constNum, ok := p.Const.(types.Number)
	if !ok {
		return nil
	}
	argIsInt := isIntegerValue(arg)
	return applyArithmeticOp(p.Op, arg, constNum, argIsInt && p.ConstIsInt)
}

// applyConstOpLocal は (op const local) パターンを実行する
// 例: (- 10 x) → 10 - x
func (p *ClosurePattern) applyConstOpLocal(arg types.Number) types.Value {
	constNum, ok := p.Const.(types.Number)
	if !ok {
		return nil
	}
	argIsInt := isIntegerValue(arg)
	return applyArithmeticOp(p.Op, constNum, arg, p.ConstIsInt && argIsInt)
}

// applyArithmeticOp は算術演算を実行する
// VM の ops.go と同じセマンティクスを維持する
func applyArithmeticOp(op opcode.Opcode, left, right types.Number, bothInt bool) types.Value {
	if op == opcode.OpMod {
		leftInt, leftOK := types.TruncateInt64Value(left)
		rightInt, rightOK := types.TruncateInt64Value(right)
		if !leftOK || !rightOK {
			return nil
		}
		result, ok := types.ModInt64(leftInt, rightInt)
		if !ok {
			return nil
		}
		return atom.NewNumberInt(result)
	}

	if bothInt {
		leftInt, leftOK := types.Int64Value(left)
		rightInt, rightOK := types.Int64Value(right)
		if leftOK && rightOK {
			result, ok := checkedIntOperation(op, leftInt, rightInt)
			if !ok {
				return nil
			}
			return atom.NewNumberInt(result)
		}
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
			return nil // エラー: ゼロ除算 → フォールバック
		}
		result = leftVal / rightVal
		// 除算は常に浮動小数点を返す
		return atom.NewNumberFloat(result)
	default:
		return nil
	}

	return atom.NewNumberFloat(result)
}
