package vm

import "testing"

func TestProtocolDispatch(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`
		(defprotocol Display
			(show [this])
			(append [this other]))
		(extend-protocol Display
			string
				(show [this] (str "string:" this))
				(append [this other] (str this other))
			number
				(show [this] (str "number:" this))
				(append [this other] (+ this other)))
		(extend-type nil Display
			(show [_] "nil")
			(append [_ other] other))
		(extend-type Object Display
			(show [_] "default")
			(append [_ other] other))`); err != nil {
		t.Fatalf("define Display: %v", err)
	}

	assertRunResult(t, runner,
		`[(show "x") (show 2) (show nil) (show [1 2]) (append "a" "b") (append 10 5)]`,
		`["string:x" "number:2" "nil" "default" "ab" 15]`)
}

func TestProtocolPredicates(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`
		(defprotocol Marker (mark [this]))
		(extend-type string Marker (mark [this] this))`); err != nil {
		t.Fatalf("define Marker: %v", err)
	}
	assertRunResult(t, runner,
		`[(protocol? Marker)
		  (satisfies? Marker "x")
		  (satisfies? Marker 1)
		  (extends? Marker "string")
		  (extends? Marker "number")]`,
		`[true true false true false]`)
}

func TestProtocolTypeIntrospection(t *testing.T) {
	assertRunResult(t, MustNewRunner(),
		`[(type-name "hello")
		  (type-name 42)
		  (type-name [1 2 3])
		  (type-name nil)
		  (go-type-name "hello")
		  (go-type-name 42)]`,
		`["string" "number" "vector" "nil" nil nil]`)
}
