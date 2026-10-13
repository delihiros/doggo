package vm

import (
	"testing"

	"doggo/internal/compiler"
	"doggo/internal/syntax/frontend"
	"doggo/internal/types"
)

// ベンチマーク用のヘルパー関数

func parseSource(source string) (types.Value, error) {
	return frontend.ReadFirstForm("<benchmark>", source)
}

// BenchmarkVMFibonacci はVMでのフィボナッチ計算のベンチマークです。
func BenchmarkVMFibonacci(b *testing.B) {
	source := `
		(loop [n 20 a 0 b 1]
			(if (= n 0)
				a
				(recur (- n 1) b (+ a b))))
	`

	parsed, err := parseSource(source)
	if err != nil {
		b.Fatal(err)
	}

	// シンボルテーブルを初期化
	symbolTable := compiler.NewSymbolTable()
	for i, builtin := range DefaultBuiltinCatalog().Definitions() {
		symbolTable.DefineBuiltin(i, builtin.Name)
	}

	// コンパイル
	comp := compiler.NewWithState(symbolTable, compiler.NewConstantStore(nil))
	if err := compileRuntimeValue(b, comp, parsed); err != nil {
		b.Fatal(err)
	}
	bytecode := comp.Bytecode()

	machine, err := New(bytecode)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := machine.Reset(bytecode); err != nil {
			b.Fatal(err)
		}
		if err := machine.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVMSum はVMでの累積計算のベンチマークです。
func BenchmarkVMSum(b *testing.B) {
	source := `
		(loop [sum 0 n 100]
			(if (> n 0)
				(recur (+ sum n) (- n 1))
				sum))
	`

	parsed, err := parseSource(source)
	if err != nil {
		b.Fatal(err)
	}

	symbolTable := compiler.NewSymbolTable()
	for i, builtin := range DefaultBuiltinCatalog().Definitions() {
		symbolTable.DefineBuiltin(i, builtin.Name)
	}

	comp := compiler.NewWithState(symbolTable, compiler.NewConstantStore(nil))
	if err := compileRuntimeValue(b, comp, parsed); err != nil {
		b.Fatal(err)
	}
	bytecode := comp.Bytecode()

	machine, err := New(bytecode)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := machine.Reset(bytecode); err != nil {
			b.Fatal(err)
		}
		if err := machine.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVMArithmetic はVMでの算術演算のベンチマークです。
func BenchmarkVMArithmetic(b *testing.B) {
	source := `(+ (* 2 3) (- 10 4))`

	parsed, err := parseSource(source)
	if err != nil {
		b.Fatal(err)
	}

	symbolTable := compiler.NewSymbolTable()
	comp := compiler.NewWithState(symbolTable, compiler.NewConstantStore(nil))
	if err := compileRuntimeValue(b, comp, parsed); err != nil {
		b.Fatal(err)
	}
	bytecode := comp.Bytecode()

	// VMを事前に作成してリセットで再利用
	machine, err := New(bytecode)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := machine.Reset(bytecode); err != nil {
			b.Fatal(err)
		}
		if err := machine.Run(); err != nil {
			b.Fatal(err)
		}
	}
}
