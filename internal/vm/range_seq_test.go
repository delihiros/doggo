package vm

import (
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestRangeArityAndStepBehavior(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[
		(vec (take 5 (range)))
		(vec (range 5))
		(vec (range 2 7))
		(vec (range 0 10 2))
		(vec (range 10 0 -2))
		(vec (range 0))
	]`, `[[0 1 2 3 4] [0 1 2 3 4] [2 3 4 5 6] [0 2 4 6 8] [10 8 6 4 2] []]`)
}

func TestFiniteRangeBoundaryBehavior(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[
		(vec (take 3 (range 9223372036854775806 9223372036854775807 2)))
		[(count (range 0 9223372036854775807 2))
		 (count (range 9223372036854775807 -1 -2))]
		(reduce (fn [n _] (if (> n 0) (/ 1 0) 1)) 0
		        (range 9223372036854775806 9223372036854775807 2))
		(vec (range (first (range 3)) 3))
	]`, `[[9223372036854775806] [4611686018427387904 4611686018427387904] 1 [0 1 2]]`)
}

func TestRangeSequenceValueContracts(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[
		[(= (range 0 4 2) (range 0 3 2))
		 (= (range 0 4 2) '(0 2))
		 (= '(0 2) (range 0 4 2))]
		(let [r (range 0 4 2)
		      c (concat [0] [2])
		      b (map inc [-1 1])
		      f (map (fn [x] x) [0 2])]
		  [(get (hash-map r :range) c :missing)
		   (get (hash-map c :concat) r :missing)
		   (get (hash-map r :range) b :missing)
		   (get (hash-map b :builtin-map) r :missing)
		   (get (hash-map r :range) f :missing)
		   (get (hash-map f :fn-map) r :missing)])
		(let [s (cons 1 (range 3))]
		  [(count s) (vec s) (= s '(1 0 1 2))])
		(let [s (cons 1 (take 0 (range 3)))]
		  [(= s '(1)) (= '(1) s) (count s) (vec s)])
		(let [nested (list 1 (range 2))
		      flat (list 1 0 1)]
		  [(count nested) (= nested flat) (vec nested)])
		(let [z (first (range 1))
		      p (first (range 1 2))
		      n (first (range -1 0))]
		  [(number? p) (int? p) (float? p)
		   (zero? z) (pos? p) (neg? n)
		   (nat-int? z) (pos-int? p) (neg-int? n)
		   (nth [10 20] p) (get [10 20] p)
		   (contains? [10 20] p) (assoc [10 20] p 99)
		   (vec (take p [10 20])) (vec (drop p [10 20]))
		   (subs "ab" p) (split-at p [10 20])])
		(let [s (lazy-seq nil)]
		  [(= s '()) (= '() s)
		   (get (hash-map s :lazy) '() :missing)
		   (get (hash-map '() :list) s :missing)])
	]`, `[[true true true] [:range :concat :range :builtin-map :range :fn-map] [4 [1 0 1 2] true] [true true 1 [1]] [2 false [1 (0 1)]] [true true false true true true true true true 20 20 true [10 99] [10] [20] "b" [(10) (20)]] [true true :lazy :list]]`)
}

func TestRangeRejectsInvalidInput(t *testing.T) {
	runner := MustNewRunner()
	assertRunErrorContains(t, runner, `(count (range))`, "infinite-range-seq")
	assertRunErrorContains(t, runner, `(range 1e100)`, "range: end must be a finite number in int64 range")
	assertRunErrorContains(t, runner, `(range 1e100 0)`, "range: start must be a finite number in int64 range")
	assertRunErrorContains(t, runner, `(range 0 1e100)`, "range: end must be a finite number in int64 range")
	assertRunErrorContains(t, runner, `(range 0 10 1e100)`, "range: step must be a finite number in int64 range")
}

func TestVMMappedSeqReduceStopsBeforeInt64Overflow(t *testing.T) {
	seq := NewVMMappedSeq(nil, nil,
		coll.NewRangeLazySeq(9223372036854775806, 9223372036854775807, 2),
		&Builtin{Fn: func(args ...types.Value) types.Value { return args[0] }},
	)
	calls := 0
	_, _, _ = seq.ReduceDirect(func(acc, _ types.Value) types.Value {
		calls++
		if calls > 1 {
			t.Fatal("ReduceDirect advanced past the finite range")
		}
		return acc
	}, atom.NilValue)
}
