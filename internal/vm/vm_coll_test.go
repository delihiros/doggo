package vm

import (
	"strings"
	"testing"
	"time"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestVectorLiterals(t *testing.T) {
	tests := []vmTestCase{
		// [1 2 3]
		{
			coll.NewVector(atom.NewNumberInt(1), atom.NewNumberInt(2), atom.NewNumberInt(3)),
			[]int{1, 2, 3},
		},
		// [(+ 1 2) 3]
		{
			coll.NewVector(
				coll.NewList(types.Symbol("+"), atom.NewNumberInt(1), atom.NewNumberInt(2)),
				atom.NewNumberInt(3)),
			[]int{3, 3},
		},
	}

	runVmTests(t, tests)
}

func TestGetBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[(get {:a 1} :a)
		(get {:a 1} :b)
		(get {:a 1} :b 99)
		(get [10 20 30] 1)
		(get [10 20] 5)
		(get [10 20] 5 999)
		(get nil :a)
		(get nil :a 42)]`, `[1 nil 99 20 nil 999 nil 42]`)
}

func TestMapLiteralRejectsTransformedInfiniteKey(t *testing.T) {
	type runResult struct {
		err error
	}
	done := make(chan runResult, 1)
	go func() {
		runner := MustNewRunner()
		_, err := runner.Run(`(let [k (map identity (range))] (get {k :ok} k))`)
		done <- runResult{err: err}
	}()

	select {
	case result := <-done:
		if result.err == nil || !strings.Contains(result.err.Error(), "cannot hash transformed infinite sequence") {
			t.Fatalf("Run error = %v, want transformed infinite sequence error", result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("map literal with transformed infinite key did not terminate")
	}
}

func TestAssocBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner,
		`[(assoc {:a 1} :a 99 :b 2) (assoc [10 20 30] 1 99) (assoc nil :x 42)]`,
		`[{:a 99, :b 2} [10 99 30] {:x 42}]`)
}

func TestDissocBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[(dissoc {:a 1 :b 2} :a) (dissoc nil :a)]`, `[{:b 2} nil]`)
}

func TestContainsBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[(contains? {:a 1} :a) (contains? {:a 1} :b)
		(contains? [10 20] 1) (contains? [10 20] 5)
		(contains? #{1 2} 1) (contains? #{1 2} 9)
		(contains? nil :a)]`, `[true false true false true false false]`)
}

func TestNthBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[(nth [10 20 30] 1) (nth [1 2] 10 999)
		(nth (list 100 200 300) 1) (nth (list 1 2) 10 :not-found)
		(nth "hello" 1)]`, `[20 999 200 :not-found \e]`)
}

func TestReduceBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[(reduce (fn [a b] (+ a b)) 0 [1 2 3 4 5])
		(reduce (fn [a b] (+ a b)) [1 2 3])
		(reduce (fn [a b] (+ a b)) [42])
		(reduce + 0 [1 2 3 4 5])
		(reduce + 0 [])]`, `[15 6 42 15 0]`)
}

func TestConjBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[(conj [1 2] 3)
		(conj nil 1 2)
		(contains? (conj #{1 2} 3) 3)
		(get (conj {} [:a 1]) :a)
		(get (conj {} {:a 1 :b 2}) :b)
		(do (defrecord User [name]) (:name (conj (->User "Ada") [:name "Bob"])))]`,
		`[[1 2 3] (2 1) true 1 2 "Bob"]`)
}

func TestDisjBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[(contains? (disj #{1 2 3} 2) 2)
		(contains? (disj #{1 2 3} 2) 1)]`, `[false true]`)
}

func TestFirstRestBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[(first [10 20 30]) (first []) (rest [1 2 3])]`, `[10 nil (2 3)]`)
}

func TestKeysValsBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[(keys {:a 1}) (vals {:a 1}) (keys nil) (vals nil)]`, `[(:a) (1) () ()]`)
}

func TestFindKeyValBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner,
		`(let [e (find {:a 1} :a)] [(key e) (val e) (find {:a 1} :x)])`,
		`[:a 1 nil]`)
}

func TestMapEntryUsesSequentialCollectionOperations(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `
		(let [entry (find {:a 1} :a)]
		  [(= entry [:a 1])
		   (sequential? entry)
		   (seqable? entry)
		   (count entry)
		   (first entry)
		   (rest entry)
		   (nth entry 0)
		   (nth entry 1)
		   (seq entry)])
	`, "[true true true 2 :a (1) :a 1 (:a 1)]")
}

func TestMapBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `(let [xs (map inc [1 2 3])]
		[(first xs) (first (rest xs)) (vec xs) (map inc [])])`, `[2 3 [2 3 4] ()]`)
}

func TestFilterBuiltin(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `(let [xs (filter even? [1 2 3 4])]
		[(first xs) (first (rest xs)) (vec xs) (filter even? [])])`, `[2 4 [2 4] ()]`)
}
