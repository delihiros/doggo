package vm

import (
	"testing"
)

// TestForMacro tests the for list comprehension macro
func TestForMacro(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			// 基本形: 単一バインディング
			name:     "basic for with single binding",
			input:    `(for [x [1 2 3]] (* x x))`,
			expected: "(1 4 9)",
		},
		{
			// ネストしたコレクション処理
			name:     "for with nested operation",
			input:    `(for [x [1 2 3]] (+ x 10))`,
			expected: "(11 12 13)",
		},
		{
			// 空のコレクション
			name:     "for with empty collection",
			input:    `(for [x []] x)`,
			expected: "()",
		},
		{
			// 文字列のリスト
			name:     "for with strings",
			input:    `(for [s ["a" "b"]] (str s "!"))`,
			expected: `("a!" "b!")`,
		},
		// === Phase B 拡張テスト ===
		{
			// :when 修飾子 - 偶数のみフィルタ
			name:     "for with :when even filter",
			input:    `(for [x [1 2 3 4 5 6] :when (even? x)] x)`,
			expected: "(2 4 6)",
		},
		{
			// :when 修飾子 - 奇数のみフィルタ
			name:     "for with :when odd filter",
			input:    `(for [x [1 2 3 4 5] :when (odd? x)] (* x x))`,
			expected: "(1 9 25)",
		},
		{
			// :let 修飾子 - 中間変数
			name:     "for with :let binding",
			input:    `(for [x [1 2 3] :let [y (* x 2)]] y)`,
			expected: "(2 4 6)",
		},
		{
			// :let 修飾子 - 複数の中間変数
			name:     "for with :let multiple bindings",
			input:    `(for [x [1 2 3] :let [y (* x 2) z (+ x y)]] z)`,
			expected: "(3 6 9)",
		},
		{
			// 複数バインディング - 直積
			name:     "for with multiple bindings",
			input:    `(for [x [1 2] y [10 20]] (+ x y))`,
			expected: "(11 21 12 22)",
		},
		{
			// 複数バインディング - 3つのコレクション
			name:     "for with three bindings",
			input:    `(for [a [1] b [2] c [3]] (+ a b c))`,
			expected: "(6)",
		},
		{
			// 複合: :when + :let
			name:     "for with :when and :let combined",
			input:    `(for [x [1 2 3 4] :when (odd? x) :let [y (* x 10)]] y)`,
			expected: "(10 30)",
		},
		{
			// 複合: 複数バインディング + :when
			name:     "for with multiple bindings and :when",
			input:    `(for [x [1 2] y [1 2] :when (not= x y)] [x y])`,
			expected: "([1 2] [2 1])",
		},
		{
			// :while 修飾子 - 条件付き終了
			name:     "for with :while",
			input:    `(for [x [1 2 3 4 5] :while (< x 4)] x)`,
			expected: "(1 2 3)",
		},
		{
			// :while 修飾子 - 非単調述語 (take-while セマンティクス検証)
			// 条件が偽になった時点で終了し、その後の真要素は含まれない
			name:     "for with :while non-monotonic predicate",
			input:    `(for [x [1 2 3] :while (odd? x)] x)`,
			expected: "(1)",
		},
		{
			// :while + :let 複合
			name:     "for with :while and :let",
			input:    `(for [x [1 2 3 4 5] :while (< x 4) :let [y (* x 10)]] y)`,
			expected: "(10 20 30)",
		},
		// === :while 配置制約のテスト ===
		// Note: :while がバインディング直後以外に配置された場合は if フィルタとして動作
		// これは現在の実装の制約であり、decision.md に文書化されている
		{
			// :let の後の :while - フィルタとして動作（早期終了しない）
			name:     "for with :let then :while (filter behavior)",
			input:    `(for [x [1 2 3 4 5] :let [y (* x 2)] :while (< y 8)] y)`,
			expected: "(2 4 6)", // 早期終了ではなくフィルタ: y=2,4,6 は < 8 を満たす
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestDoseqMacro tests the doseq side-effect iteration macro
func TestDoseqMacro(t *testing.T) {
	tests := []struct {
		name           string
		setup          string
		input          string
		expectedResult string
		checkAtom      string
		expectedAtom   string
	}{
		{
			// 基本形: atomに副作用
			name:           "doseq with atom side effect",
			setup:          `(def acc (atom []))`,
			input:          `(doseq [x [1 2 3]] (swap! acc conj x))`,
			expectedResult: "nil",
			checkAtom:      `@acc`,
			expectedAtom:   "[1 2 3]",
		},
		{
			// 空のコレクション
			name:           "doseq with empty collection",
			setup:          `(def acc (atom 0))`,
			input:          `(doseq [x []] (swap! acc inc))`,
			expectedResult: "nil",
			checkAtom:      `@acc`,
			expectedAtom:   "0",
		},
		{
			// 数値の累積
			name:           "doseq with sum",
			setup:          `(def total (atom 0))`,
			input:          `(doseq [n [10 20 30]] (swap! total + n))`,
			expectedResult: "nil",
			checkAtom:      `@total`,
			expectedAtom:   "60",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()

			// Setup
			if tt.setup != "" {
				_, err := runner.Run(tt.setup)
				if err != nil {
					t.Fatalf("setup error: %v", err)
				}
			}

			// Run doseq
			assertRunResult(t, runner, tt.input, tt.expectedResult)

			// Check atom state
			if tt.checkAtom != "" {
				atomResult, err := runner.Run(tt.checkAtom)
				if err != nil {
					t.Fatalf("atom check error: %v", err)
				}
				if atomResult.String() != tt.expectedAtom {
					t.Errorf("expected atom %s, got %s", tt.expectedAtom, atomResult.String())
				}
			}
		})
	}
}

// TestDotimesMacro tests the dotimes iteration macro
func TestDotimesMacro(t *testing.T) {
	tests := []struct {
		name           string
		setup          string
		input          string
		expectedResult string
		checkAtom      string
		expectedAtom   string
	}{
		{
			// 基本形: 3回ループ
			name:           "dotimes basic",
			setup:          `(def counter (atom 0))`,
			input:          `(dotimes [i 3] (swap! counter inc))`,
			expectedResult: "nil",
			checkAtom:      `@counter`,
			expectedAtom:   "3",
		},
		{
			// インデックス変数の使用
			name:           "dotimes with index",
			setup:          `(def acc (atom []))`,
			input:          `(dotimes [i 4] (swap! acc conj i))`,
			expectedResult: "nil",
			checkAtom:      `@acc`,
			expectedAtom:   "[0 1 2 3]",
		},
		{
			// 0回ループ
			name:           "dotimes with zero",
			setup:          `(def counter (atom 0))`,
			input:          `(dotimes [i 0] (swap! counter inc))`,
			expectedResult: "nil",
			checkAtom:      `@counter`,
			expectedAtom:   "0",
		},
		{
			// 累積計算
			name:           "dotimes with sum",
			setup:          `(def sum (atom 0))`,
			input:          `(dotimes [i 5] (swap! sum + i))`,
			expectedResult: "nil",
			checkAtom:      `@sum`,
			expectedAtom:   "10", // 0+1+2+3+4=10
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()

			// Setup
			if tt.setup != "" {
				_, err := runner.Run(tt.setup)
				if err != nil {
					t.Fatalf("setup error: %v", err)
				}
			}

			// Run dotimes
			assertRunResult(t, runner, tt.input, tt.expectedResult)

			// Check atom state
			if tt.checkAtom != "" {
				atomResult, err := runner.Run(tt.checkAtom)
				if err != nil {
					t.Fatalf("atom check error: %v", err)
				}
				if atomResult.String() != tt.expectedAtom {
					t.Errorf("expected atom %s, got %s", tt.expectedAtom, atomResult.String())
				}
			}
		})
	}
}
