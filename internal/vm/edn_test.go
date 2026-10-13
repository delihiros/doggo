package vm

import (
	"strings"
	"testing"

	"doggo/internal/types/atom"
)

func TestEDNReadStringAllAppliesTaggedLiteralOptions(t *testing.T) {
	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.edn :as edn])
		(edn/read-string-all "#app/user {:id 7} #app/missing 8"
		                     {:readers {'app/user (fn [value] (:id value))}
		                      :default (fn [tag value] [(str tag) value])
		                      :location? true})
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != `[7 ["app/missing" 8]]` {
		t.Fatalf("read-string-all result = %s", got)
	}
}

func TestEDNWriteOptions(t *testing.T) {
	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.edn :as edn])
		[(edn/write-string [1 [2]] {:pretty? true})
		 (edn/write-string [1 [2]] {:pretty? true :indent 4})
		 (edn/write-string :ok {:newline? true})
		 (edn/write-string (edn/read-string "{:b 2, \"a\" 1, 3 :three}") {:sort-keys? true})]
	`)
	if err != nil {
		t.Fatal(err)
	}

	vector := mustVector(t, value)
	if got := mustString(t, vector.Items[0]); !strings.Contains(got, "\n") {
		t.Fatalf("pretty output = %q", got)
	}
	if got := mustString(t, vector.Items[1]); got != "[\n    1\n    [\n        2\n    ]\n]" {
		t.Fatalf("indent output = %q", got)
	}
	if got := mustString(t, vector.Items[2]); got != ":ok\n" {
		t.Fatalf("newline output = %q", got)
	}
	if got := mustString(t, vector.Items[3]); got != `{"a" 1, 3 :three, :b 2}` {
		t.Fatalf("sorted output = %q", got)
	}
}

func TestEDNWriteRejectsNonscalarSortKeys(t *testing.T) {
	runner := MustNewRunner()
	assertRunErrorContains(t, runner,
		`(require '[doggo.edn :as edn]) (edn/write-string (edn/read-string "{[1] :value}") {:sort-keys? true})`,
		"cannot sort map with non-scalar key")
}

func TestEDNReadLocationAwareBuiltInFailure(t *testing.T) {
	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.edn :as edn])
		(try
		  (edn/read-string "#inst 1" {:location? true})
		  nil
		  (catch [e] (ex->map e)))
	`)
	if err != nil {
		t.Fatal(err)
	}

	view := mustMap(t, value)
	if view.Count() != 4 {
		t.Fatalf("expected four canonical keys, got %s", view.String())
	}
	if trace := mustVector(t, view.Get(atom.NewKeyword("trace"))); len(trace.Items) == 0 {
		t.Fatal("expected source trace")
	}
	if got := mustString(t, view.Get(atom.NewKeyword("message"))); got != "#inst data reader expected string" {
		t.Fatalf("diagnostic message = %q", got)
	}
}

func TestEDNRejectsLanguageOnlySyntaxAndValues(t *testing.T) {
	runner := MustNewRunner()
	for _, input := range []string{
		`(edn/read-string "'foo")`,
		`(edn/read-string "#\"a+\"")`,
		`(edn/write-string (read-string "#\"a+\""))`,
		`(edn/read-string "1 2")`,
		`(do (defrecord User [name]) (edn/write-string (->User "Ada")))`,
		`(edn/write-string (with-meta [1 2] {:tag :x}))`,
	} {
		assertRunErrorContains(t, runner, `(require '[doggo.edn :as edn]) `+input, "edn")
	}
}
