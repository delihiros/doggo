package vm

import (
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/opcode"
	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func TestAnalyzeClosurePattern_LocalOpConst(t *testing.T) {
	// (fn [x] (* x 2)) のバイトコードを手動で構築
	// OpGetLocal 0, OpConst 0, OpMul, OpReturnValue
	ins := opcode.Instructions{
		byte(opcode.OpGetLocal), 0, // local index 0
		byte(opcode.OpConst), 0, 0, // const index 0 (big endian)
		byte(opcode.OpMul),
		byte(opcode.OpReturnValue),
	}

	fn := &compiler.CompiledFunction{
		Instructions:  ins,
		NumLocals:     1,
		NumParameters: 1,
	}
	cl := &compiler.Closure{Fn: fn, Free: nil}

	constants := []types.Value{atom.NewNumberInt(2)}

	pattern := AnalyzeClosurePattern(cl, constants)

	if pattern == nil {
		t.Fatal("expected pattern to be detected, got nil")
	}
	if pattern.Type != PatternLocalOpConst {
		t.Errorf("expected PatternLocalOpConst, got %v", pattern.Type)
	}
	if pattern.Op != opcode.OpMul {
		t.Errorf("expected OpMul, got %v", pattern.Op)
	}
	if pattern.LocalIdx != 0 {
		t.Errorf("expected LocalIdx 0, got %d", pattern.LocalIdx)
	}
	if !pattern.ConstIsInt {
		t.Error("expected ConstIsInt to be true")
	}

	// Apply テスト
	result := pattern.Apply(atom.NewNumberInt(5))
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	num, ok := result.(*atom.Number)
	if !ok {
		t.Fatalf("expected *atom.Number, got %T", result)
	}
	if num.ToFloat64() != 10 {
		t.Errorf("expected 10, got %v", num.ToFloat64())
	}
	// 両方整数なので結果も整数
	if num.IntVal == nil {
		t.Error("expected integer result")
	}
}

func TestAnalyzeClosurePattern_ConstOpLocal(t *testing.T) {
	// (fn [x] (- 10 x)) のバイトコードを手動で構築
	// OpConst 0, OpGetLocal 0, OpSub, OpReturnValue
	ins := opcode.Instructions{
		byte(opcode.OpConst), 0, 0, // const index 0 (big endian)
		byte(opcode.OpGetLocal), 0, // local index 0
		byte(opcode.OpSub),
		byte(opcode.OpReturnValue),
	}

	fn := &compiler.CompiledFunction{
		Instructions:  ins,
		NumLocals:     1,
		NumParameters: 1,
	}
	cl := &compiler.Closure{Fn: fn, Free: nil}

	constants := []types.Value{atom.NewNumberInt(10)}

	pattern := AnalyzeClosurePattern(cl, constants)

	if pattern == nil {
		t.Fatal("expected pattern to be detected, got nil")
	}
	if pattern.Type != PatternConstOpLocal {
		t.Errorf("expected PatternConstOpLocal, got %v", pattern.Type)
	}
	if pattern.Op != opcode.OpSub {
		t.Errorf("expected OpSub, got %v", pattern.Op)
	}

	// Apply テスト: (- 10 3) = 7
	result := pattern.Apply(atom.NewNumberInt(3))
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	num, ok := result.(*atom.Number)
	if !ok {
		t.Fatalf("expected *atom.Number, got %T", result)
	}
	if num.ToFloat64() != 7 {
		t.Errorf("expected 7, got %v", num.ToFloat64())
	}
}

func TestAnalyzeClosurePattern_WithFreeVars(t *testing.T) {
	// 自由変数がある場合は最適化不可
	ins := opcode.Instructions{
		byte(opcode.OpGetLocal), 0,
		byte(opcode.OpConst), 0, 0,
		byte(opcode.OpMul),
		byte(opcode.OpReturnValue),
	}

	fn := &compiler.CompiledFunction{
		Instructions:  ins,
		NumLocals:     1,
		NumParameters: 1,
	}
	// 自由変数がある
	cl := &compiler.Closure{Fn: fn, Free: []types.Value{atom.NewNumberInt(1)}}

	constants := []types.Value{atom.NewNumberInt(2)}

	pattern := AnalyzeClosurePattern(cl, constants)

	if pattern != nil {
		t.Error("expected nil pattern for closure with free variables")
	}
}

func TestAnalyzeClosurePattern_UnsupportedPattern(t *testing.T) {
	// サポートされていないパターン（長さが違う）
	ins := opcode.Instructions{
		byte(opcode.OpGetLocal), 0,
		byte(opcode.OpReturnValue),
	}

	fn := &compiler.CompiledFunction{
		Instructions:  ins,
		NumLocals:     1,
		NumParameters: 1,
	}
	cl := &compiler.Closure{Fn: fn, Free: nil}

	constants := []types.Value{}

	pattern := AnalyzeClosurePattern(cl, constants)

	if pattern != nil {
		t.Error("expected nil pattern for unsupported bytecode")
	}
}

func TestAnalyzeClosurePattern_WrongNumParameters(t *testing.T) {
	// NumParameters が 1 でない場合は最適化不可
	ins := opcode.Instructions{
		byte(opcode.OpGetLocal), 0,
		byte(opcode.OpConst), 0, 0,
		byte(opcode.OpMul),
		byte(opcode.OpReturnValue),
	}

	fn := &compiler.CompiledFunction{
		Instructions:  ins,
		NumLocals:     2,
		NumParameters: 2, // 2引数
	}
	cl := &compiler.Closure{Fn: fn, Free: nil}

	constants := []types.Value{atom.NewNumberInt(2)}

	pattern := AnalyzeClosurePattern(cl, constants)

	if pattern != nil {
		t.Error("expected nil pattern for closure with NumParameters != 1")
	}
}

func TestAnalyzeClosurePattern_WrongLocalIdx(t *testing.T) {
	// localIdx が 0 でない場合は最適化不可
	ins := opcode.Instructions{
		byte(opcode.OpGetLocal), 1, // local index 1 (not 0)
		byte(opcode.OpConst), 0, 0,
		byte(opcode.OpMul),
		byte(opcode.OpReturnValue),
	}

	fn := &compiler.CompiledFunction{
		Instructions:  ins,
		NumLocals:     2,
		NumParameters: 1,
	}
	cl := &compiler.Closure{Fn: fn, Free: nil}

	constants := []types.Value{atom.NewNumberInt(2)}

	pattern := AnalyzeClosurePattern(cl, constants)

	if pattern != nil {
		t.Error("expected nil pattern for closure with localIdx != 0")
	}
}

func TestAnalyzeClosurePattern_NonNumericConst(t *testing.T) {
	// 定数が数値でない場合は最適化不可
	ins := opcode.Instructions{
		byte(opcode.OpGetLocal), 0,
		byte(opcode.OpConst), 0, 0,
		byte(opcode.OpMul),
		byte(opcode.OpReturnValue),
	}

	fn := &compiler.CompiledFunction{
		Instructions:  ins,
		NumLocals:     1,
		NumParameters: 1,
	}
	cl := &compiler.Closure{Fn: fn, Free: nil}

	// 非数値の定数
	constants := []types.Value{atom.String("hello")}

	pattern := AnalyzeClosurePattern(cl, constants)

	if pattern != nil {
		t.Error("expected nil pattern for non-numeric constant")
	}
}

func TestApply_NonNumericArg(t *testing.T) {
	// 非数値引数の場合は nil を返す（フォールバック）
	pattern := &ClosurePattern{
		Type:       PatternLocalOpConst,
		Op:         opcode.OpMul,
		Const:      atom.NewNumberInt(2),
		ConstIsInt: true,
		LocalIdx:   0,
	}

	// 非数値引数
	result := pattern.Apply(atom.String("hello"))
	if result != nil {
		t.Error("expected nil for non-numeric argument")
	}
}

func TestApplyArithmeticOp_AllOps(t *testing.T) {
	tests := []struct {
		op       opcode.Opcode
		left     int64
		right    int64
		expected float64
		isInt    bool // 結果が整数かどうか
	}{
		{opcode.OpAdd, 5, 3, 8, true},
		{opcode.OpSub, 10, 4, 6, true},
		{opcode.OpMul, 6, 7, 42, true},
		{opcode.OpDiv, 20, 4, 5, false}, // 除算は常に浮動小数点
		{opcode.OpMod, 17, 5, 2, true},
	}

	for _, tc := range tests {
		result := applyArithmeticOp(tc.op, atom.NewNumberInt(tc.left), atom.NewNumberInt(tc.right), tc.op != opcode.OpDiv)
		if result == nil {
			t.Errorf("op=%v: expected result, got nil", tc.op)
			continue
		}
		num, ok := result.(*atom.Number)
		if !ok {
			t.Errorf("op=%v: expected *atom.Number, got %T", tc.op, result)
			continue
		}
		if num.ToFloat64() != tc.expected {
			t.Errorf("op=%v: expected %v, got %v", tc.op, tc.expected, num.ToFloat64())
		}
		// 整数かどうかのチェック
		if tc.isInt && num.IntVal == nil {
			t.Errorf("op=%v: expected integer result", tc.op)
		}
		if !tc.isInt && num.IntVal != nil {
			t.Errorf("op=%v: expected float result", tc.op)
		}
	}
}

func TestApplyArithmeticOp_DivByZero(t *testing.T) {
	result := applyArithmeticOp(opcode.OpDiv, atom.NewNumberInt(10), atom.NewNumberInt(0), false)
	if result != nil {
		t.Error("expected nil for division by zero")
	}

	result = applyArithmeticOp(opcode.OpMod, atom.NewNumberInt(10), atom.NewNumberInt(0), true)
	if result != nil {
		t.Error("expected nil for modulo by zero")
	}
}

func TestApplyArithmeticOp_ModSemantics(t *testing.T) {
	// Clojure mod semantics: result has same sign as divisor
	tests := []struct {
		left     int64
		right    int64
		expected int64
	}{
		{7, 3, 1},    // positive mod positive
		{-7, 3, 2},   // negative mod positive -> positive
		{7, -3, -2},  // positive mod negative -> negative
		{-7, -3, -1}, // negative mod negative
	}

	for _, tc := range tests {
		result := applyArithmeticOp(opcode.OpMod, atom.NewNumberInt(tc.left), atom.NewNumberInt(tc.right), true)
		if result == nil {
			t.Errorf("mod(%d, %d): expected result, got nil", tc.left, tc.right)
			continue
		}
		num, ok := result.(*atom.Number)
		if !ok {
			t.Errorf("mod(%d, %d): expected *atom.Number, got %T", tc.left, tc.right, result)
			continue
		}
		if num.ToFloat64() != float64(tc.expected) {
			t.Errorf("mod(%d, %d): expected %d, got %v", tc.left, tc.right, tc.expected, num.ToFloat64())
		}
	}
}

func TestApplyArithmeticOp_FloatResult(t *testing.T) {
	// 浮動小数点の入力
	result := applyArithmeticOp(opcode.OpAdd, atom.NewNumberFloat(1.5), atom.NewNumberInt(2), false)
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	num, ok := result.(*atom.Number)
	if !ok {
		t.Fatalf("expected *atom.Number, got %T", result)
	}
	if num.ToFloat64() != 3.5 {
		t.Errorf("expected 3.5, got %v", num.ToFloat64())
	}
	// 片方が浮動小数点なので結果も浮動小数点
	if num.IntVal != nil {
		t.Error("expected float result")
	}
}

func TestReduceWithPatternPreservesLargeRangeIntegers(t *testing.T) {
	assertRunResult(t, MustNewRunner(),
		`(reduce + 0 (map (fn [x] (+ x 1)) (range 9007199254740992 9007199254740994)))`,
		"18014398509481987")
}
