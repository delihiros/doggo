package vm

import "testing"

func TestTypePredicates(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[(keyword? :foo) (keyword? 42)]`, `[true false]`)
	assertRunResult(t, runner, `[(fn? inc) (fn? (fn [x] x)) (fn? 42)]`, `[true true false]`)
	assertRunResult(t, runner, `[(coll? '(1)) (coll? [1]) (coll? {:a 1}) (coll? #{1}) (coll? 42)]`,
		`[true true true true false]`)
	assertRunResult(t, runner, `[(ifn? inc) (ifn? (fn [x] x)) (ifn? :foo) (ifn? 42)]`,
		`[true true true false]`)
	assertRunResult(t, runner, `[(sequential? '(1)) (sequential? [1]) (sequential? (range 1))
		(sequential? (map inc [1])) (sequential? (filter odd? [1])) (sequential? {:a 1})]`,
		`[true true true true true false]`)
	assertRunResult(t, runner, `[(associative? {:a 1}) (associative? [1]) (associative? '(1))]`,
		`[true true false]`)
	assertRunResult(t, runner, `[(counted? [1]) (counted? 42)]`, `[true false]`)
}
