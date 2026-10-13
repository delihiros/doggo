package vm

import "testing"

func TestSetRuntimeBehavior(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[
		(= #{} (set []))
		(= #{1 2 1} #{1 2})
		(= #{(+ 1 2) (* 2 3)} #{3 6})
		(#{1 2 3} 2)
		[(set? #{1}) (set? [1]) (set? '(1)) (set? {})]
		(= (set [1 2 3]) #{1 2 3})
		(= (set '(1 2 3)) #{1 2 3})
	]`, `[true true true 2 [true false false false] true true]`)
}
