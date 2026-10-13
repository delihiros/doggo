package vm

import "testing"

func TestDoallDorun(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `(let [calls (atom 0)
		result (dorun (map (fn [x] (swap! calls + x)) [1 2 3]))]
		[result @calls])`, `[nil 6]`)
	assertRunResult(t, runner, `(let [calls (atom 0)
		result (doall (map (fn [x] (do (swap! calls + x) x)) [1 2 3]))]
		[result @calls])`, `[(1 2 3) 6]`)
}

func TestSeqBuiltin(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[(seq [1 2])
		(seq [])
		(seq "ab")
		(seq "")
		(seq nil)
		(seq (map inc [1]))
		(seq (map inc []))
		(seq (filter even? [1 2]))
		(seq (filter even? [1]))]`, `[(1 2) nil (\a \b) nil nil (2) nil (2) nil]`)
}

func TestRestReturnsSeq(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[(rest [1 2 3])
		(list? (rest [1 2 3]))
		(rest [])
		(rest '(1 2 3))]`, `[(2 3) true () (2 3)]`)
}

func TestEmptyBuiltin(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[(empty? [])
		(empty? [1])
		(empty? nil)
		(empty? (map inc [1]))
		(empty? (filter even? [1]))
		(empty? "")
		(empty? "a")
		(empty? {})
		(empty? {:a 1})
		(empty? 42)]`, `[true false true false true true false true false false]`)
}

func TestCountOnLazySeq(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[(count (map inc [1 2 3]))
		(count (filter even? [1 2 3 4]))
		(count (filter even? [1 3 5]))]`, `[3 2 0]`)
}
