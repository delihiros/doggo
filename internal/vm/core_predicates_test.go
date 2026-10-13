package vm

import "testing"

func TestCorePredicates(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[(nil? nil) (nil? 1) (some? nil) (some? false)]`,
		`[true false false true]`)
	assertRunResult(t, runner, `[(true? true) (true? false) (false? false) (false? true)
		(boolean? true) (boolean? nil)]`, `[true false true false true false]`)
	assertRunResult(t, runner, `[(even? 4) (even? 3) (odd? 3) (odd? 4) (zero? 0)
		(zero? 1) (pos? 5) (pos? -5) (neg? -5) (neg? 5)]`,
		`[true false true false true false true false true false]`)
	assertRunResult(t, runner, `[(int? 42) (int? 3.14) (int? "42")
		(float? 3.14) (float? 42) (float? "3.14")]`,
		`[true false false true false false]`)
	assertRunResult(t, runner, `[(nat-int? -1) (nat-int? 0) (nat-int? 1) (nat-int? 1.5)
		(pos-int? -1) (pos-int? 0) (pos-int? 1) (pos-int? 1.5)
		(neg-int? -1) (neg-int? 0) (neg-int? 1) (neg-int? -1.5)]`,
		`[false true true false false false true false true false false false]`)
	assertRunResult(t, runner, `[(seqable? [1]) (seqable? "a") (seqable? nil)
		(seqable? {:a 1}) (seqable? 42)]`, `[true true true true false]`)
	assertRunResult(t, runner, `(realized? [1])`, `true`)
}
