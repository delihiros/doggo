package vm

import "testing"

func TestMemoizeDistinguishesUnequalArgumentsWithSamePrintedForm(t *testing.T) {
	runner := MustNewRunner()

	got, err := runner.Run(`
		(do
		  (def call-once (memoize (fn [f] (f))))
		  [(call-once (fn [] 1))
		   (call-once (fn [] 2))])`)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "[1 2]" {
		t.Fatalf("memoized results = %s, want [1 2]", got.String())
	}
}
