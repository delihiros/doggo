package vm

import "testing"

func TestConcat(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[(concat [1] nil '(2) [3])
		(concat)
		(concat "ab" [1])]`, `[(1 2 3) () (\a \b 1)]`)
}

func TestConcatSupportsSequenceOperations(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[(cons 0 (concat [1 2] [3]))
		(sequential? (concat))
		(= (concat [1 2] [3]) '(1 2 3))]`, `[(0 1 2 3) true true]`)
}

func TestConcatRejectsNonSeqableInput(t *testing.T) {
	assertRunErrorContains(t, MustNewRunner(), `(concat [1] 42 [2])`, "must be a collection, got number")
}

func TestConcatIsLazy(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `(take 3 (concat (range) [100]))`, "(0 1 2)")
}
