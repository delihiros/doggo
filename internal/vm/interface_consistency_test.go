package vm

import (
	"path/filepath"
	"strconv"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func TestStdlibPrivateSurface(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.repl :as repl] '[doggo.string :as string])`); err != nil {
		t.Fatalf("require failed: %v", err)
	}

	value, err := runner.Run(`
		(nil? (seq (filter
		  (fn [name] (= "-" (subs (str name) 0 1)))
		  (repl/dir 'doggo.string))))`)
	if err != nil {
		t.Fatalf("dir private-surface check failed: %v", err)
	}
	if value.String() != "true" {
		t.Fatalf("expected dir to exclude private vars, got %s", value.String())
	}

	if _, err := runner.Run(`doggo.string/-split`); err == nil {
		t.Fatal("expected qualified private lookup to fail")
	}
	if _, err := runner.Run(`(do (require '[doggo.string :refer :all]) -split)`); err == nil {
		t.Fatal("expected :refer :all to exclude private vars")
	}
}

func TestProtocolBuiltinsRejectMissingAndExtraArguments(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(defprotocol InterfaceConsistencyProtocol (value-of [x]))`); err != nil {
		t.Fatalf("defprotocol failed: %v", err)
	}

	cases := []string{
		`(satisfies? InterfaceConsistencyProtocol)`,
		`(satisfies? InterfaceConsistencyProtocol 1 2)`,
		`(protocol?)`,
		`(protocol? InterfaceConsistencyProtocol 1)`,
		`(type-name)`,
		`(type-name 1 2)`,
		`(go-type-name)`,
		`(go-type-name 1 2)`,
		`(extends? InterfaceConsistencyProtocol)`,
		`(extends? InterfaceConsistencyProtocol "number" 1)`,
	}
	for _, source := range cases {
		if _, err := runner.Run(source); err == nil {
			t.Errorf("expected arity error for %s", source)
		}
	}
}

func TestPublicValidationErrorsHaveCanonicalData(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`
		(require '[doggo.csv :as csv]
		         '[doggo.edn :as edn]
		         '[doggo.http :as http]
		         '[doggo.instant :as instant]
		         '[doggo.json :as json]
		         '[doggo.observability :as observability]
		         '[doggo.os :as os]
		         '[doggo.process :as process]
		         '[doggo.time :as time]
		         '[doggo.url :as url]
		         '[doggo.uuid :as uuid]
		         '[doggo.xml :as xml])`); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		source   string
		function string
	}{
		{`(try (ex-info 1 {}) (catch [e] (ex-data e)))`, "doggo.core/ex-info"},
		{`(try (nth [1] "bad") (catch [e] (ex-data e)))`, "doggo.core/nth"},
		{`(try (take "bad" [1]) (catch [e] (ex-data e)))`, "doggo.core/take"},
		{`(try (observability/log! 42 "message") (catch [e] (ex-data e)))`, "doggo.observability/log!"},
		{`(try (os/getenv 42) (catch [e] (ex-data e)))`, "doggo.os/getenv"},
		{`(try (time/from-epoch-millis "bad") (catch [e] (ex-data e)))`, "doggo.time/from-epoch-millis"},
		{`(try (time/epoch-millis {}) (catch [e] (ex-data e)))`, "doggo.time/epoch-millis"},
		{`(try (time/format (time/from-epoch-millis 0) :bad) (catch [e] (ex-data e)))`, "doggo.time/format"},
		{`(try (time/millis {}) (catch [e] (ex-data e)))`, "doggo.time/millis"},
		{`(try (time/plus (time/from-epoch-millis 0) {}) (catch [e] (ex-data e)))`, "doggo.time/plus"},
		{`(try (time/before? {} (time/from-epoch-millis 0)) (catch [e] (ex-data e)))`, "doggo.time/before?"},
		{`(try (instant/parse-timestamp 42) (catch [e] (ex-data e)))`, "doggo.instant/parse-timestamp"},
		{`(try (http/request 42) (catch [e] (ex-data e)))`, "doggo.http/request"},
		{`(try (url/parse 42) (catch [e] (ex-data e)))`, "doggo.url/parse"},
		{`(try (uuid/parse "not-a-uuid") (catch [e] (ex-data e)))`, "doggo.uuid/parse"},
		{`(try (csv/read-string 42) (catch [e] (ex-data e)))`, "doggo.csv/read-string"},
		{`(try (process/run "echo" {:timeout "bad"}) (catch [e] (ex-data e)))`, "doggo.process/run"},
		{`(try (process/start "echo" {:timeout "bad"}) (catch [e] (ex-data e)))`, "doggo.process/start"},
		{`(try (process/check-output "echo" {:timeout "bad"}) (catch [e] (ex-data e)))`, "doggo.process/check-output"},
		{`(try (json/read-string 42) (catch [e] (ex-data e)))`, "doggo.json/read-string"},
		{`(try (edn/read-string 42) (catch [e] (ex-data e)))`, "doggo.edn/read-string"},
		{`(try (xml/read-string 42) (catch [e] (ex-data e)))`, "doggo.xml/read-string"},
	}
	for _, tt := range tests {
		value, err := runner.Run(tt.source)
		if err != nil {
			t.Fatalf("%s: %v", tt.function, err)
		}
		data := mustMap(t, value)
		if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != "invalid-argument" {
			t.Errorf("%s kind = %s", tt.function, got)
		}
		if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != tt.function {
			t.Errorf("%s function = %v", tt.function, data.Get(atom.NewKeyword("function")))
		}
	}
}

func TestOptionMapErrorsHaveCanonicalArgumentDetails(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`
		(require '[doggo.io :as io]
		         '[doggo.json :as json]
		         '[doggo.pprint :as pprint]
		         '[doggo.process :as process]
		         '[doggo.time :as time])`); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		source   string
		function string
	}{
		{`(io/slurp "/tmp/x" 1)`, "doggo.io/slurp"},
		{`(json/read-string "{}" 1)`, "doggo.json/read-string"},
		{`(pprint/pprint-str 1 1)`, "doggo.pprint/pprint-str"},
		{`(process/run "true" 1)`, "doggo.process/run"},
		{`(time/format (time/from-epoch-millis 0) 1)`, "doggo.time/format"},
	}
	for _, tt := range tests {
		value, err := runner.Run(`(try ` + tt.source + `
			(catch [e]
			  [(:kind (ex-data e))
			   (:function (ex-data e))
			   (:argument (ex-data e))
			   (:expected (ex-data e))
			   (:actual (ex-data e))]))`)
		if err != nil {
			t.Fatalf("%s: %v", tt.function, err)
		}
		want := `[:invalid-argument ` + tt.function + ` :options "map" 1]`
		if value.String() != want {
			t.Errorf("%s = %s, want %s", tt.function, value.String(), want)
		}
	}
}

func TestRuntimeFailuresHaveCanonicalData(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`
		(require '[doggo.io :as io]
		         '[doggo.process :as process])`); err != nil {
		t.Fatal(err)
	}

	missingPath := strconv.Quote(filepath.Join(t.TempDir(), "missing"))
	tests := []struct {
		source   string
		function string
	}{
		{`(try (io/slurp ` + missingPath + `) (catch [e] (ex-data e)))`, "doggo.io/slurp"},
		{`(try (process/start ` + missingPath + `) (catch [e] (ex-data e)))`, "doggo.process/start"},
	}
	for _, tt := range tests {
		value, err := runner.Run(tt.source)
		if err != nil {
			t.Fatalf("%s: %v", tt.function, err)
		}
		data := mustMap(t, value)
		if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != "runtime-failure" {
			t.Errorf("%s kind = %s", tt.function, got)
		}
		if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != tt.function {
			t.Errorf("%s function = %v", tt.function, data.Get(atom.NewKeyword("function")))
		}
	}
}

func TestSerializationIOErrorsOwnPublicFunctionAndPreserveCause(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`
		(require '[doggo.csv :as csv]
		         '[doggo.edn :as edn]
		         '[doggo.json :as json]
		         '[doggo.xml :as xml])`); err != nil {
		t.Fatal(err)
	}

	missing := strconv.Quote(filepath.Join(t.TempDir(), "missing"))
	directory := strconv.Quote(t.TempDir())
	tests := []struct {
		source   string
		function string
		cause    string
	}{
		{`(json/read ` + missing + `)`, "doggo.json/read", "doggo.io/slurp"},
		{`(edn/read ` + missing + `)`, "doggo.edn/read", "doggo.io/slurp"},
		{`(xml/read ` + missing + `)`, "doggo.xml/read", "doggo.io/slurp"},
		{`(csv/read ` + missing + `)`, "doggo.csv/read", "doggo.io/slurp"},
		{`(json/write {} ` + directory + `)`, "doggo.json/write", "doggo.io/spit"},
		{`(edn/write {} ` + directory + `)`, "doggo.edn/write", "doggo.io/spit"},
		{`(xml/write {:tag :root :attrs nil :content nil} ` + directory + `)`, "doggo.xml/write", "doggo.io/spit"},
		{`(csv/write [["a"]] ` + directory + `)`, "doggo.csv/write", "doggo.io/spit"},
	}
	for _, tt := range tests {
		source := `(try ` + tt.source + `
			(catch [e]
			  (let [cause (ex-cause e)]
			    [(:kind (ex-data e))
			     (:function (ex-data e))
			     (if cause (:kind (ex-data cause)) nil)
			     (if cause (:function (ex-data cause)) nil)])))`
		value, err := runner.Run(source)
		if err != nil {
			t.Fatalf("%s: %v", tt.function, err)
		}
		want := `[:runtime-failure ` + tt.function + ` :runtime-failure ` + tt.cause + `]`
		if value.String() != want {
			t.Errorf("%s = %s, want %s", tt.function, value.String(), want)
		}
	}
}

func TestIODescriptorsAreStrictAndSerializerErrorsRetainDetails(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`
		(require '[doggo.io :as io]
		         '[doggo.json :as json])`); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		source   string
		function string
		argument string
		cause    string
	}{
		{`(io/slurp {:type :unexpected :path 1})`, "doggo.io/slurp", "source", "nil"},
		{`(io/spit {:type :unexpected :path 1} "x")`, "doggo.io/spit", "target", "nil"},
		{`(json/read {:type :reader-spec :path 1})`, "doggo.json/read", "source", "doggo.io/slurp"},
		{`(json/write {} {:type :writer-spec :path 1})`, "doggo.json/write", "target", "doggo.io/spit"},
	}
	for _, tt := range tests {
		source := `(try ` + tt.source + `
			(catch [e]
			  [(:kind (ex-data e))
			   (:function (ex-data e))
			   (:argument (ex-data e))
			   (:expected (ex-data e))
			   (if (ex-cause e)
			     (:function (ex-data (ex-cause e)))
			     nil)]))`
		value, err := runner.Run(source)
		if err != nil {
			t.Fatalf("%s: %v", tt.function, err)
		}
		want := `[:invalid-argument ` + tt.function + ` :` + tt.argument + ` "path string or matching doggo.io descriptor" ` + tt.cause + `]`
		if value.String() != want {
			t.Errorf("%s = %s, want %s", tt.function, value.String(), want)
		}
	}
}
