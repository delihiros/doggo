package vm

import "testing"

func TestNewResolvesTypesAndInitializesValues(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `(do
		(import "reflect")
		(import "strings")
		[(. (new strings.Builder) String)
		 (. (new "strings.Builder") String)
		 (. (new strings/Builder) String)
		 (. (new reflect/StructField {:Name "Visible"}) Name)
		 (. (new strings/NewReader) Len)])`, `["" "" "" "Visible" 0]`)
}

func TestNewCopiesSingleFieldStruct(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `(do
		(import "net/http")
		(let [source (new "net/http.ProtocolError" {:ErrorString "boom"})
		      copied (new "net/http.ProtocolError" source)]
		  (. copied ErrorString)))`, `"boom"`)
}

func TestNewRejectsInvalidStructInitialization(t *testing.T) {
	runner := MustNewRunner()
	for _, expression := range []string{
		`(do (import "reflect") (new reflect/StructField 42))`,
		`(do (import "time") (new time/Time 1 2 3))`,
		`(do (import "reflect") (new reflect/StructField {:Nmae "Visible"}))`,
	} {
		if value, err := runner.Run(expression); err == nil {
			t.Fatalf("Run(%q) = %s, want initialization error", expression, value.String())
		}
	}
}
