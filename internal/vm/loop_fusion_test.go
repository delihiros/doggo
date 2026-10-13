package vm

import (
	"testing"
)

// TestLoopFusionEquivalence はループフュージョンが元の結果と同値であることをテストします。
// 最適化が有効でも無効でも同じ結果を返すことを保証します。
func TestLoopFusionEquivalence(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// map → reduce パターン
		{
			name: "map then reduce",
			input: `
				(->> [1 2 3 4 5]
					(map (fn [x] (* x 2)))
					(reduce + 0))
			`,
			expected: "30", // (1*2 + 2*2 + 3*2 + 4*2 + 5*2) = 30
		},
		// filter → reduce パターン
		{
			name: "filter then reduce",
			input: `
				(->> [1 2 3 4 5 6]
					(filter (fn [x] (> x 3)))
					(reduce + 0))
			`,
			expected: "15", // 4 + 5 + 6 = 15
		},
		// map → filter → reduce パターン（完全なパイプライン）
		{
			name: "map filter reduce pipeline",
			input: `
				(->> [1 2 3 4 5]
					(map (fn [x] (* x 2)))
					(filter (fn [x] (> x 4)))
					(reduce + 0))
			`,
			expected: "24", // 6 + 8 + 10 = 24 (2,4 filtered out)
		},
		// 空コレクションの場合
		{
			name: "empty collection",
			input: `
				(->> []
					(map (fn [x] (* x 2)))
					(filter (fn [x] (> x 0)))
					(reduce + 0))
			`,
			expected: "0",
		},
		// 単一要素の場合
		{
			name: "single element",
			input: `
				(->> [5]
					(map (fn [x] (* x 3)))
					(reduce + 100))
			`,
			expected: "115", // 100 + 15
		},
		// filter がすべてを除外する場合
		{
			name: "filter removes all",
			input: `
				(->> [1 2 3]
					(filter (fn [x] (> x 10)))
					(reduce + 0))
			`,
			expected: "0",
		},
		// 複雑な変換
		{
			name: "complex transformation",
			input: `
				(->> (range 1 11)
					(map (fn [x] (* x x)))
					(filter (fn [x] (= 0 (mod x 2))))
					(reduce + 0))
			`,
			expected: "220", // 4 + 16 + 36 + 64 + 100 = 220
		},
		// リストでも動作
		{
			name: "with list collection",
			input: `
				(->> '(1 2 3 4)
					(map inc)
					(reduce * 1))
			`,
			expected: "120", // 2 * 3 * 4 * 5 = 120
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestLoopFusionSideEffects は副作用の呼び出し順序と回数が正しいことをテストします。
// 最適化は可観測な動作を変えてはいけません。
func TestLoopFusionSideEffects(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedCalls string // 呼び出し順序を記録した結果
		expectedValue string // 最終的な計算結果
	}{
		{
			name: "map preserves call order",
			input: `
				(def call-log (atom []))
				(def logging-double
					(fn [x]
						(swap! call-log conj [:map x])
						(* x 2)))
				(->> [1 2 3]
					(map logging-double)
					(reduce + 0))
				@call-log
			`,
			expectedCalls: "[[:map 1] [:map 2] [:map 3]]",
		},
		{
			name: "filter preserves call order",
			input: `
				(def call-log (atom []))
				(def logging-pred
					(fn [x]
						(swap! call-log conj [:filter x])
						(> x 1)))
				(->> [1 2 3]
					(filter logging-pred)
					(reduce + 0))
				@call-log
			`,
			expectedCalls: "[[:filter 1] [:filter 2] [:filter 3]]",
		},
		{
			name: "map filter reduce preserves order",
			input: `
				(def call-log (atom []))
				(def logging-map
					(fn [x]
						(swap! call-log conj [:map x])
						(* x 2)))
				(def logging-filter
					(fn [x]
						(swap! call-log conj [:filter x])
						(> x 2)))
				(def logging-reduce
					(fn [acc x]
						(swap! call-log conj [:reduce acc x])
						(+ acc x)))
				(->> [1 2 3]
					(map logging-map)
					(filter logging-filter)
					(reduce logging-reduce 0))
				@call-log
			`,
			// 融合版の動作: 各要素が map→filter→(条件通過なら)reduce の順で処理される
			// 要素1: map(1)→filter(2)→skip (2 <= 2)
			// 要素2: map(2)→filter(4)→reduce(0,4)
			// 要素3: map(3)→filter(6)→reduce(4,6)
			expectedCalls: "[[:map 1] [:filter 2] [:map 2] [:filter 4] [:reduce 0 4] [:map 3] [:filter 6] [:reduce 4 6]]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expectedCalls)
		})
	}
}

// TestLoopFusionNotApplied は最適化が適用されないケースをテストします。
// 安全でない場合は最適化しないことを確認します。
func TestLoopFusionNotApplied(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// map がシャドーイングされている（ローカル束縛は優先されない場合）
		// 注: Clojure では let でシャドーイングしても ->> 展開時は
		// グローバルの map が使われる。これは現在の Doggo の動作と同じ。
		{
			name: "shadowed map function",
			input: `
				(let [my-map (fn [f coll] [99])]
					(->> [1 2 3]
						(my-map inc)
						(reduce + 0)))
			`,
			expected: "99", // カスタム関数 my-map が [99] を返すので reduce は 99
		},
		// reduce のみ（融合対象なし）
		{
			name: "reduce only no fusion target",
			input: `
				(->> [1 2 3 4 5]
					(reduce + 0))
			`,
			expected: "15",
		},
		// map のみ（reduce なし、融合対象なし）
		{
			name: "map only no reduce",
			input: `
				(vec (->> [1 2 3]
					(map inc)))
			`,
			expected: "[2 3 4]",
		},
		// reduce に init がない場合（2引数版）
		{
			name: "reduce without init",
			input: `
				(->> [1 2 3 4 5]
					(map inc)
					(reduce +))
			`,
			expected: "20", // (2 + 3 + 4 + 5 + 6) = 20
		},
		// 中間に他の操作がある場合
		{
			name: "intermediate operation breaks fusion",
			input: `
				(->> [1 2 3 4 5]
					(map inc)
					(take 3)
					(reduce + 0))
			`,
			expected: "9", // 2 + 3 + 4 = 9
		},
		// ネストされたパイプラインでも正しく動作
		{
			name: "nested pipelines",
			input: `
				(->> [1 2 3]
					(map (fn [x] (->> [x] (map inc) (reduce + 0))))
					(reduce + 0))
			`,
			expected: "9", // (2 + 3 + 4) = 9
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestLoopFusionWithBuiltins は組み込み関数との互換性をテストします。
func TestLoopFusionWithBuiltins(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// 組み込み関数 inc/dec を使用
		{
			name: "with builtin inc",
			input: `
				(->> [1 2 3 4 5]
					(map inc)
					(reduce + 0))
			`,
			expected: "20", // 2+3+4+5+6
		},
		// 組み込み関数 even?/odd? を使用
		{
			name: "with builtin even?",
			input: `
				(->> [1 2 3 4 5 6]
					(filter even?)
					(reduce + 0))
			`,
			expected: "12", // 2+4+6
		},
		// 組み込み関数の組み合わせ
		{
			name: "builtin combination",
			input: `
				(->> (range 1 11)
					(filter odd?)
					(map (fn [x] (* x x)))
					(reduce + 0))
			`,
			expected: "165", // 1+9+25+49+81
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}
