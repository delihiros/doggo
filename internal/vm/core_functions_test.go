package vm

import (
	"testing"
)

// TestCoreExtJuxt tests the juxt function behavior and output ordering
func TestCoreExtJuxt(t *testing.T) {
	r := MustNewRunner()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic juxt with single function
		{"juxt_single_fn", `((juxt inc) 1)`, "[2]"},
		// juxt with three functions - order should be preserved
		{"juxt_three_fns", `((juxt first last count) [1 2 3 4 5])`, "[1 5 5]"},
		// juxt with multiple arguments
		{"juxt_multi_args", `((juxt + -) 10 3)`, "[13 7]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, r, tt.input, tt.expected)
		})
	}
}

// TestCoreExtPartialVariadic tests partial with more than 2 pre-bound arguments
func TestCoreExtPartialVariadic(t *testing.T) {
	r := MustNewRunner()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// partial with 0 pre-bound args
		{"partial_zero_args", `((partial +) 1 2 3)`, "6"},
		// partial with many pre-bound args
		{"partial_many_args", `((partial + 1 2 3 4 5 6 7 8 9) 10)`, "55"},
		// partial with custom function
		{"partial_custom_fn", `((partial (fn [a b c d] (- (+ a b) (+ c d))) 100 50) 30 20)`, "100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, r, tt.input, tt.expected)
		})
	}
}

func TestCoreExtApplyClosureShapes(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[
		(apply (fn [] 42) [])
		(apply (fn [x y] (+ x y)) [10 20])
		(apply (fn [& xs] xs) [1 2 3])
		(apply (fn ([] :zero) ([x y & more] more)) [1 2 3 4])
	]`, `[42 30 (1 2 3) (3 4)]`)
}

func TestCoreExtFindSupportsCompositeKeys(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[
		(find {(list 1) :found} (list 1))
		(find {(list 1) :found} (list 2))
	]`, `[[(1) :found] nil]`)
}

func TestCoreExtSomeEveryShortCircuit(t *testing.T) {
	r := MustNewRunner()
	assertRunResult(t, r, `[(some identity []) (every? identity [])]`, `[nil true]`)
	assertRunResult(t, r, `(let [calls (atom 0)
		result (some (fn [x] (swap! calls inc) (if (= x 2) x nil)) [1 2 3 4 5])]
		[result @calls])`, `[2 2]`)
	assertRunResult(t, r, `(let [calls (atom 0)
		result (every? (fn [x] (swap! calls inc) (< x 3)) [1 2 3 4 5])]
		[result @calls])`, `[false 3]`)
}

func TestCoreExtTypeErrors(t *testing.T) {
	r := MustNewRunner()
	for _, input := range []string{`(reverse 123)`, `(some identity 123)`, `(every? identity 123)`} {
		assertRunErrorContains(t, r, input, "must be a collection")
	}
}

func TestCoreExtNotFunction(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `(map not [true false nil])`, `(false true true)`)
}
