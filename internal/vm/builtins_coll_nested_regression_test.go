package vm

import (
	"fmt"
	"testing"
)

func TestNestedMapUpdatesRejectNonMapNodes(t *testing.T) {
	for _, test := range []struct {
		name, root, keys string
	}{
		{"scalar root at leaf", `42`, `[:a]`},
		{"scalar root before descent", `42`, `[:a :b]`},
		{"false root", `false`, `[:a :b]`},
		{"vector root", `[1 2]`, `[0 :a]`},
		{"scalar leaf parent", `{:a 42 :kept 1}`, `[:a :b]`},
		{"deeper scalar leaf parent", `{:a {:b 42} :kept 1}`, `[:a :b :c]`},
		{"scalar intermediate parent", `{:a 42 :kept 1}`, `[:a :b :c]`},
		{"false child", `{:a false :kept 1}`, `[:a :b]`},
		{"string child", `{:a "text" :kept 1}`, `[:a :b]`},
		{"vector child", `{:a [1 2] :kept 1}`, `[:a 0]`},
		{"list child", `{:a (list 1 2) :kept 1}`, `[:a :b]`},
	} {
		for _, function := range []string{"assoc-in", "update-in"} {
			t.Run(function+"/"+test.name, func(t *testing.T) {
				argument := `9`
				if function == "update-in" {
					argument = `(fn [_] (swap! calls inc) 9)`
				}
				// Catching the error here also detects errors accidentally stored in
				// a returned map. Invalid nodes must never invoke the callback.
				source := fmt.Sprintf(`
					(let [calls (atom 0)]
					  [(try (%s %s %s %s)
					     (catch [e] [(:kind (ex-data e)) (:function (ex-data e))]))
					   @calls])`, function, test.root, test.keys, argument)
				want := fmt.Sprintf(`[[:invalid-argument doggo.core/%s] 0]`, function)
				assertRunResult(t, MustNewRunner(), source, want)
			})
		}
	}
}

func TestNestedMapUpdatesCreateMissingNodesPersistently(t *testing.T) {
	for _, test := range []struct {
		name, root, keys, assocWant, updateWant string
	}{
		{"nil root", `nil`, `[:a :b]`, `{:a {:b 9}}`, `{:a {:b 9}}`},
		{"missing child", `{:kept 1}`, `[:a :b]`, `{:kept 1 :a {:b 9}}`, `{:kept 1 :a {:b 9}}`},
		{"nil child", `{:a nil :kept 1}`, `[:a :b]`, `{:a {:b 9} :kept 1}`, `{:a {:b 9} :kept 1}`},
		{"missing deeper child", `{:a {:kept 1} :kept 2}`, `[:a :b :c]`, `{:a {:kept 1 :b {:c 9}} :kept 2}`, `{:a {:kept 1 :b {:c 9}} :kept 2}`},
		{"existing value", `{:a {:b 1 :kept 2} :kept 3}`, `[:a :b]`, `{:a {:b 9 :kept 2} :kept 3}`, `{:a {:b 10 :kept 2} :kept 3}`},
		{"nil leaf value", `{:a {:b nil} :kept 1}`, `[:a :b]`, `{:a {:b 9} :kept 1}`, `{:a {:b 9} :kept 1}`},
		{"single key", `{:a 1 :kept 2}`, `[:a]`, `{:a 9 :kept 2}`, `{:a 10 :kept 2}`},
	} {
		for _, function := range []string{"assoc-in", "update-in"} {
			t.Run(function+"/"+test.name, func(t *testing.T) {
				argument, want := `9`, test.assocWant
				if function == "update-in" {
					argument = `(fn [current amount] (+ (if (nil? current) 0 current) amount)) 9`
					want = test.updateWant
				}
				source := fmt.Sprintf(`
					(let [original %s
					      updated (%s original %s %s)]
					  [(= updated %s) (= original %s)])`, test.root, function, test.keys, argument, want, test.root)
				assertRunResult(t, MustNewRunner(), source, `[true true]`)
			})
		}
	}
}

func TestNestedMapUpdateCallbackErrorsPropagate(t *testing.T) {
	for _, source := range []string{
		`(update-in {:a {:b 1}} [:a :b] (fn [_] (inc "bad")))`,
		`(update-in nil [:a :b :c] (fn [_] (inc "bad")))`,
	} {
		t.Run(source, func(t *testing.T) {
			assertRunErrorContains(t, MustNewRunner(), source, "inc")
		})
	}
}

func TestNestedMapUpdateEmptyPathBehavior(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `(assoc-in 42 [] 9)`, `9`)
	assertRunResult(t, MustNewRunner(), `
		(let [calls (atom 0)]
		  [(try (update-in nil [] (fn [_] (swap! calls inc)))
		     (catch [e] (:kind (ex-data e))))
		   @calls])`, `[:invalid-argument 0]`)
}
