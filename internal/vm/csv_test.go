package vm

import (
	"path/filepath"
	"strconv"
	"testing"
	"unicode/utf8"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestCSVDecodeBoundaries(t *testing.T) {
	emptyOptions := coll.NewEmptyMap()
	value, err := csvDecode(csvReadStringFunction, nil, emptyOptions)
	if err != nil || value.String() != "[]" {
		t.Fatalf("empty CSV = %v, %v", value, err)
	}

	headerOptions := coll.NewMapFromPairs([]types.Value{atom.NewKeyword("headers"), atom.True})
	value, err = csvDecode(csvReadStringFunction, []byte("name,age\n"), headerOptions)
	if err != nil || value.String() != "[]" {
		t.Fatalf("header-only CSV = %v, %v", value, err)
	}

	tests := []struct {
		name     string
		data     []byte
		options  types.Value
		argument string
		expected string
	}{
		{"invalid UTF-8", []byte{0xff}, emptyOptions, "source", "valid UTF-8 string"},
		{"malformed CSV", []byte("name\n\"Ada"), emptyOptions, "source", "valid UTF-8 CSV"},
		{"non-map options", nil, atom.NilValue, "options", "map containing only optional :headers boolean"},
		{"unknown option", nil, coll.NewMapFromPairs([]types.Value{atom.NewKeyword("unknown"), atom.True}), "options", "map containing only optional :headers boolean"},
		{"non-boolean headers", nil, coll.NewMapFromPairs([]types.Value{atom.NewKeyword("headers"), atom.NewNumberInt(1)}), "options", "map containing only optional :headers boolean"},
		{"empty header", []byte("name,\nAda,36\n"), headerOptions, "headers", "unique non-empty header strings"},
		{"duplicate header", []byte("name,name\nAda,36\n"), headerOptions, "headers", "unique non-empty header strings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := csvDecode(csvReadStringFunction, tt.data, tt.options)
			if err == nil || result != nil {
				t.Fatalf("csvDecode() = %v, %v", result, err)
			}
			assertCSVArgumentError(t, csvExceptionData(t, err), csvReadStringFunction, tt.argument, tt.expected)
		})
	}
}

func TestCSVEncodeBoundaries(t *testing.T) {
	encoded, err := csvEncode(csvWriteStringFunction, coll.NewVector())
	if err != nil || encoded != "" {
		t.Fatalf("empty rows = %q, %v", encoded, err)
	}

	tests := []struct {
		name     string
		rows     types.Value
		argument string
		expected string
	}{
		{"rows type", atom.NewNumberInt(1), "rows", "vector of non-empty equal-width row vectors"},
		{"empty row", coll.NewVector(coll.NewVector()), "row", "non-empty row vector"},
		{"invalid UTF-8", coll.NewVector(coll.NewVector(atom.NewString("\xff"))), "cell", "valid UTF-8 string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := csvEncode(csvWriteStringFunction, tt.rows)
			if err == nil || encoded != "" {
				t.Fatalf("csvEncode() = %q, %v", encoded, err)
			}
			assertCSVArgumentError(t, csvExceptionData(t, err), csvWriteStringFunction, tt.argument, tt.expected)
			if !utf8.ValidString(err.Error()) {
				t.Fatalf("error is not valid UTF-8: %q", err.Error())
			}
		})
	}
}

func TestCSVReadValidatesOptionsBeforeIO(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.csv :as csv])`); err != nil {
		t.Fatal(err)
	}
	missing := strconv.Quote(filepath.Join(t.TempDir(), "missing.csv"))
	data := csvCaughtErrorData(t, runner, `(csv/read `+missing+` {:headers :yes})`)
	assertCSVErrorKindOwner(t, data, "invalid-argument", csvReadFunction)
}

func csvCaughtErrorData(t *testing.T, runner *Runner, expression string) *coll.Map {
	t.Helper()
	value, err := runner.Run(`(try ` + expression + ` (catch [e] (ex-data e)))`)
	if err != nil {
		t.Fatalf("%s escaped catch: %v", expression, err)
	}
	return mustMap(t, value)
}

func csvExceptionData(t *testing.T, err error) *coll.Map {
	t.Helper()
	vmErr, ok := err.(*VMError)
	if !ok {
		t.Fatalf("expected VMError, got %T: %v", err, err)
	}
	return mustMap(t, mustExceptionBuiltin(t, "ex-data").Fn(vmErr.Value))
}

func assertCSVArgumentError(t *testing.T, data *coll.Map, function, argument, expected string) {
	t.Helper()
	assertCSVErrorKindOwner(t, data, "invalid-argument", function)
	if got := mustString(t, data.Get(atom.NewKeyword("expected"))); got != expected {
		t.Fatalf("expected = %q, want %q", got, expected)
	}
	if got := mustKeywordString(t, data.Get(atom.NewKeyword("argument"))); got != argument {
		t.Fatalf("argument = %s, want %s", got, argument)
	}
	if data.Find(atom.NewKeyword("actual")) == nil {
		t.Fatal("missing safe actual")
	}
}

func assertCSVErrorKindOwner(t *testing.T, data *coll.Map, kind, function string) {
	t.Helper()
	if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != kind {
		t.Fatalf("kind = %s, want %s", got, kind)
	}
	if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != function {
		t.Fatalf("function = %v, want %s", data.Get(atom.NewKeyword("function")), function)
	}
}
