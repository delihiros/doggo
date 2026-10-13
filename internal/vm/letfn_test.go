package vm

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

// TestCell は Cell 型の基本動作をテストします。
func TestCell(t *testing.T) {
	t.Run("basic_operations", func(t *testing.T) {
		cell := NewCell(nil)
		if cell.Value != nil {
			t.Errorf("expected nil value, got %v", cell.Value)
		}

		// 値を設定
		cell.Value = &Builtin{Fn: func(args ...types.Value) types.Value {
			return atom.NewNumberInt(42)
		}}
		if cell.Value == nil {
			t.Errorf("expected non-nil value after assignment")
		}

		// 値を取り出して呼び出す
		fn, ok := cell.Value.(*Builtin)
		if !ok {
			t.Fatalf("cell.Value is not a *Builtin, got %T", cell.Value)
		}
		result := fn.Fn()
		if result.String() != "42" {
			t.Errorf("expected 42, got %s", result.String())
		}
	})
}

// TestLetfn は letfn（相互再帰ローカル関数）をテストします。
func TestLetfn(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// 基本的な動作
		{
			name:     "simple_function",
			input:    `(letfn [(f [] 42)] (f))`,
			expected: "42",
		},
		{
			name:     "function_with_param",
			input:    `(letfn [(f [x] (+ x 1))] (f 5))`,
			expected: "6",
		},
		{
			name:     "empty_body_returns_nil",
			input:    `(letfn [(f [] 1)])`,
			expected: "nil",
		},
		{
			name:     "multiple_body_expressions",
			input:    `(letfn [(add [a b] (+ a b))] (add 1 2) (add 3 4))`,
			expected: "7",
		},

		// 相互再帰: even? と odd?
		{
			name: "mutual_recursion_even_odd",
			input: `
				(letfn [(even? [n] (if (zero? n) true (odd? (dec n))))
				        (odd? [n] (if (zero? n) false (even? (dec n))))]
				  (even? 10))
			`,
			expected: "true",
		},
		{
			name: "mutual_recursion_odd_check",
			input: `
				(letfn [(even? [n] (if (zero? n) true (odd? (dec n))))
				        (odd? [n] (if (zero? n) false (even? (dec n))))]
				  (odd? 10))
			`,
			expected: "false",
		},
		{
			name: "mutual_recursion_odd_true",
			input: `
				(letfn [(even? [n] (if (zero? n) true (odd? (dec n))))
				        (odd? [n] (if (zero? n) false (even? (dec n))))]
				  (odd? 7))
			`,
			expected: "true",
		},

		// 自己再帰
		{
			name: "single_recursive_function",
			input: `
				(letfn [(factorial [n]
				          (if (<= n 1)
				            1
				            (* n (factorial (dec n)))))]
				  (factorial 5))
			`,
			expected: "120",
		},

		// 3つ以上の関数
		{
			name: "three_functions",
			input: `
				(letfn [(a [x] (if (zero? x) "a" (b (dec x))))
				        (b [x] (if (zero? x) "b" (c (dec x))))
				        (c [x] (if (zero? x) "c" (a (dec x))))]
				  [(a 0) (a 1) (a 2) (a 3)])
			`,
			expected: `["a" "b" "c" "a"]`,
		},

		// スコープ
		{
			name: "local_scope_does_not_affect_global",
			input: `
				(do
				  (def my-fn (fn [] "global"))
				  (letfn [(my-fn [] "local")]
				    (my-fn)))
			`,
			expected: `"local"`,
		},
		{
			name: "global_unchanged_after_letfn",
			input: `
				(do
				  (def my-fn (fn [] "global"))
				  (letfn [(my-fn [] "local")]
				    (my-fn))
				  (my-fn))
			`,
			expected: `"global"`,
		},
		{
			name: "capture_outer_variable",
			input: `
				(let [x 10]
				  (letfn [(add-x [n] (+ n x))]
				    (add-x 5)))
			`,
			expected: "15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}
