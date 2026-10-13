package vm

import (
	"testing"
)

// runDestructureTest はテストケースを実行するヘルパー関数
func runDestructureTest(t *testing.T, input string, expected string) {
	t.Helper()
	assertRunResult(t, MustNewRunner(), input, expected)
}

// ======================
// let シーケンス分解テスト
// ======================

func TestLetSeqDestructureBasic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic two elements",
			input:    `(let [[a b] [1 2]] (+ a b))`,
			expected: "3",
		},
		{
			name:     "basic three elements",
			input:    `(let [[a b c] [1 2 3]] (+ a b c))`,
			expected: "6",
		},
		{
			name:     "from list",
			input:    `(let [[a b] '(10 20)] (+ a b))`,
			expected: "30",
		},
		{
			name:     "ignore extra elements",
			input:    `(let [[a b] [1 2 3 4 5]] (+ a b))`,
			expected: "3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}

func TestLetSeqDestructureWithRest(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "rest captures remaining",
			input:    `(let [[a & rest] [1 2 3]] (first rest))`,
			expected: "2",
		},
		{
			name:     "rest with two fixed",
			input:    `(let [[a b & rest] [1 2 3 4 5]] (first rest))`,
			expected: "3",
		},
		{
			name:     "rest is empty when no remaining",
			input:    `(let [[a b & rest] [1 2]] (nil? rest))`,
			expected: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}

func TestLetSeqDestructureWithAs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "as binds whole collection",
			input:    `(let [[a b :as all] [1 2]] (count all))`,
			expected: "2",
		},
		{
			name:     "as with rest",
			input:    `(let [[a & rest :as all] [1 2 3]] (+ (count rest) (count all)))`,
			expected: "5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}

func TestLetSeqDestructureNested(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "nested vectors",
			input:    `(let [[[a b] [c d]] [[1 2] [3 4]]] (+ a b c d))`,
			expected: "10",
		},
		{
			name:     "mixed nesting",
			input:    `(let [[a [b c]] [1 [2 3]]] (+ a b c))`,
			expected: "6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}

// ======================
// let マップ分解テスト
// ======================

func TestLetMapDestructureBasic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "keyword keys",
			input:    `(let [{a :a b :b} {:a 1 :b 2}] (+ a b))`,
			expected: "3",
		},
		{
			name:     "missing key returns nil",
			input:    `(let [{a :a b :b} {:a 1}] (nil? b))`,
			expected: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}

func TestLetMapDestructureKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "keys shorthand",
			input:    `(let [{:keys [x y]} {:x 1 :y 2}] (+ x y))`,
			expected: "3",
		},
		{
			name:     "keys with extra map entries",
			input:    `(let [{:keys [a]} {:a 10 :b 20 :c 30}] a)`,
			expected: "10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}

func TestLetMapDestructureStrs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strs shorthand",
			input:    `(let [{:strs [a b]} {"a" 1 "b" 2}] (+ a b))`,
			expected: "3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}

func TestLetMapDestructureOr(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "or provides default",
			input:    `(let [{:keys [a] :or {a 99}} {}] a)`,
			expected: "99",
		},
		{
			name:     "or not used when key exists",
			input:    `(let [{:keys [a] :or {a 99}} {:a 1}] a)`,
			expected: "1",
		},
		{
			name:     "multiple defaults",
			input:    `(let [{:keys [a b] :or {a 10 b 20}} {:a 1}] (+ a b))`,
			expected: "21",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}

func TestLetMapDestructureAs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "as binds whole map",
			input:    `(let [{:keys [a] :as m} {:a 1 :b 2}] (count (keys m)))`,
			expected: "2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}

// ======================
// fn パラメータ分解テスト
// ======================

func TestFnSeqDestructure(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic fn destructure",
			input:    `((fn [[a b]] (+ a b)) [1 2])`,
			expected: "3",
		},
		{
			name:     "fn with rest",
			input:    `((fn [[a & rest]] (+ a (count rest))) [1 2 3 4])`,
			expected: "4",
		},
		{
			name:     "multiple params with destructure",
			input:    `((fn [x [a b]] (+ x a b)) 10 [1 2])`,
			expected: "13",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}

func TestFnMapDestructure(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic map destructure",
			input:    `((fn [{:keys [x y]}] (+ x y)) {:x 1 :y 2})`,
			expected: "3",
		},
		{
			name:     "map destructure with default",
			input:    `((fn [{:keys [a] :or {a 100}}] a) {})`,
			expected: "100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}

func TestFnNestedDestructure(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "nested in fn",
			input:    `((fn [[[a b]]] (+ a b)) [[1 2]])`,
			expected: "3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}

// ======================
// 実用的なユースケース
// ======================

func TestDestructurePracticalUseCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "map with destructure over vector of pairs",
			input:    `(first (map (fn [[k v]] (+ k v)) [[1 2] [3 4]]))`,
			expected: "3",
		},
		{
			name:     "reduce with destructure",
			input:    `(reduce (fn [acc [a b]] (+ acc a b)) 0 [[1 2] [3 4]])`,
			expected: "10",
		},
		{
			name:     "filter with map destructure",
			input:    `(count (filter (fn [{:keys [x]}] (> x 5)) [{:x 3} {:x 7} {:x 10}]))`,
			expected: "2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDestructureTest(t, tt.input, tt.expected)
		})
	}
}
