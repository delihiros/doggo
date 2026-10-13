package vm

import "testing"

// TestDefnMacro tests the defn macro
func TestDefnMacro(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple defn",
			input:    `(do (defn add [a b] (+ a b)) (add 2 3))`,
			expected: "5",
		},
		{
			name:     "defn with multiple expressions",
			input:    `(do (defn greet [name] (str "Hello, " name "!")) (greet "World"))`,
			expected: `"Hello, World!"`,
		},
		{
			name:     "recursive defn",
			input:    `(do (defn fact [n] (if (= n 0) 1 (* n (fact (- n 1))))) (fact 5))`,
			expected: "120",
		},
		{
			name:     "defn with closure",
			input:    `(do (defn make-adder [x] (fn [y] (+ x y))) ((make-adder 10) 5))`,
			expected: "15",
		},
		// 複数アリティ関数のテスト
		{
			name:     "multi-arity defn: zero args",
			input:    `(do (defn greet ([] "Hello") ([name] (str "Hello, " name "!"))) (greet))`,
			expected: `"Hello"`,
		},
		{
			name:     "multi-arity defn: one arg",
			input:    `(do (defn greet ([] "Hello") ([name] (str "Hello, " name "!"))) (greet "World"))`,
			expected: `"Hello, World!"`,
		},
		{
			name:     "multi-arity defn: three arities",
			input:    `(do (defn sum ([] 0) ([a] a) ([a b] (+ a b))) (+ (sum) (sum 10) (sum 20 30)))`,
			expected: "60",
		},
		{
			name:     "multi-arity defn: recursive fibonacci",
			input:    `(do (defn fib ([] (fib 1 1)) ([a b] (cons a (lazy-seq (fib b (+ a b)))))) (take 8 (fib)))`,
			expected: "(1 1 2 3 5 8 13 21)",
		},
		{
			name:     "multi-arity defn: self-call between arities",
			input:    `(do (defn default-or ([] (default-or 42)) ([x] x)) (+ (default-or) (default-or 8)))`,
			expected: "50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestMultiArityFn tests the fn special form with multiple arities
func TestMultiArityFn(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "fn with two arities: zero args",
			input:    `((fn ([] 0) ([x] x)))`,
			expected: "0",
		},
		{
			name:     "fn with two arities: one arg",
			input:    `((fn ([] 0) ([x] x)) 42)`,
			expected: "42",
		},
		{
			name:     "fn with variadic arity",
			input:    `((fn ([] "none") ([& args] args)) 1 2 3)`,
			expected: "(1 2 3)",
		},
		{
			name:     "fn with mixed arities",
			input:    `((fn ([a] a) ([a b] (+ a b)) ([a b c] (+ a b c))) 10 20)`,
			expected: "30",
		},
		{
			name:     "fn with closure in multi-arity",
			input:    `(let [x 100] ((fn ([] x) ([y] (+ x y))) 5))`,
			expected: "105",
		},
		{
			name:     "fn with closure in multi-arity: zero-arg case",
			input:    `(let [x 100] ((fn ([] x) ([y] (+ x y)))))`,
			expected: "100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestIfLetMacro tests the if-let macro
func TestIfLetMacro(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "if-let with truthy value",
			input:    `(if-let [x 42] (+ x 1) 0)`,
			expected: "43",
		},
		{
			name:     "if-let with nil",
			input:    `(if-let [x nil] (+ x 1) 0)`,
			expected: "0",
		},
		{
			name:     "if-let with false",
			input:    `(if-let [x false] :then :else)`,
			expected: ":else",
		},
		{
			name:     "if-let with computed value",
			input:    `(if-let [x (+ 1 2)] (* x 10) 0)`,
			expected: "30",
		},
		{
			name:     "if-let with first of vector",
			input:    `(if-let [x (first [1 2 3])] (+ x 10) 0)`,
			expected: "11",
		},
		{
			name:     "if-let with first of empty vector",
			input:    `(if-let [x (first [])] (+ x 10) 0)`,
			expected: "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestWhenLetMacro tests the when-let macro
func TestWhenLetMacro(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "when-let with truthy value",
			input:    `(when-let [x 42] (+ x 1))`,
			expected: "43",
		},
		{
			name:     "when-let with nil",
			input:    `(when-let [x nil] (+ x 1))`,
			expected: "nil",
		},
		{
			name:     "when-let with false",
			input:    `(when-let [x false] :then)`,
			expected: "nil",
		},
		{
			name:     "when-let with multiple expressions in body",
			input:    `(when-let [x 5] (+ x 1) (* x 2))`,
			expected: "10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("runner error: %s", err)
			}
			if result == nil {
				if tt.expected != "nil" {
					t.Errorf("expected %q, got nil", tt.expected)
				}
				return
			}
			if result.String() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result.String())
			}
		})
	}
}

// TestWhenMacro tests the when macro with multiple expressions
func TestWhenMacro(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "when with single expression",
			input:    `(when true 42)`,
			expected: "42",
		},
		{
			name:     "when with false condition",
			input:    `(when false 42)`,
			expected: "nil",
		},
		{
			name:     "when with multiple expressions",
			input:    `(do (def a (atom 0)) (when true (swap! a inc) (swap! a inc)) @a)`,
			expected: "2",
		},
		{
			name:     "when with three expressions",
			input:    `(do (def b (atom 0)) (when true (swap! b inc) (swap! b inc) (swap! b inc)) @b)`,
			expected: "3",
		},
		{
			name:     "when-not with multiple expressions",
			input:    `(do (def c (atom 10)) (when-not false (swap! c inc) (swap! c inc)) @c)`,
			expected: "12",
		},
		{
			name:     "unless with multiple expressions",
			input:    `(do (def d (atom 5)) (unless false (swap! d inc) (swap! d inc)) @d)`,
			expected: "7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("runner error: %s", err)
			}
			if result == nil {
				if tt.expected != "nil" {
					t.Errorf("expected %q, got nil", tt.expected)
				}
				return
			}
			if result.String() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result.String())
			}
		})
	}
}

// TestTimeMacro tests the time macro
func TestTimeMacro(t *testing.T) {
	runner := MustNewRunner()

	// The time macro should return the result of the expression
	assertRunResult(t, runner, `(time (+ 1 1))`, "2")
}

// TestNanoTime tests the process-relative nano-time builtin.
func TestNanoTime(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`(let [now (nano-time)]
		(and (int? now)
		     (<= 0 now)
		     (< now 100000000000000000)))`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if got := result.String(); got != "true" {
		t.Errorf("nano-time should be process-relative, got %q", got)
	}
}

// TestSymbolPredicate tests the symbol? builtin
func TestSymbolPredicate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "symbol? on quoted symbol",
			input:    `(symbol? 'x)`,
			expected: "true",
		},
		{
			name:     "symbol? on number",
			input:    `(symbol? 42)`,
			expected: "false",
		},
		{
			name:     "symbol? on string",
			input:    `(symbol? "hello")`,
			expected: "false",
		},
		{
			name:     "symbol? on keyword",
			input:    `(symbol? :keyword)`,
			expected: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}
