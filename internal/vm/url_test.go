package vm

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestURLRejectsDistinctInvalidInputs(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.url :as url])`); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		expression string
		function   string
		argument   string
	}{
		{"parse userinfo", `(url/parse "https://user@example.test/a")`, urlParseFunction, "url"},
		{"parse invalid host UTF-8", `(url/parse "https://%FF/")`, urlParseFunction, "url"},
		{"parse invalid path UTF-8", `(url/parse "/%FF")`, urlParseFunction, "url"},
		{"parse invalid query UTF-8", `(url/parse "/?x=%FF")`, urlParseFunction, "url"},
		{"parse invalid fragment UTF-8", `(url/parse "/#%FF")`, urlParseFunction, "url"},
		{"format extra key", `(url/format {:port "80"})`, urlFormatFunction, "components"},
		{"format non-string value", `(url/format {:path 1})`, urlFormatFunction, "path"},
		{"format bad scheme", `(url/format {:scheme "1http"})`, urlFormatFunction, "scheme"},
		{"format host injection", `(url/format {:host "example.test/path"})`, urlFormatFunction, "host"},
		{"format bad path escape", `(url/format {:path "/%"})`, urlFormatFunction, "path"},
		{"format path delimiter injection", `(url/format {:path "/a?b"})`, urlFormatFunction, "path"},
		{"format bad query escape", `(url/format {:query "x=%"})`, urlFormatFunction, "query"},
		{"format fragment injection", `(url/format {:query "x=1#bad"})`, urlFormatFunction, "query"},
		{"resolve non-string ref", `(url/resolve "https://example.test/a" 42)`, urlResolveFunction, "ref"},
		{"encode non-string value", `(url/encode-query {"a" 1})`, urlEncodeQueryFunction, "a"},
		{"encode non-string vector item", `(url/encode-query {"a" ["ok" 1]})`, urlEncodeQueryFunction, "a"},
		{"decode invalid UTF-8", `(url/decode-query "a=%FF")`, urlDecodeQueryFunction, "query"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := runner.Run(`(try ` + tt.expression + ` (catch [e] (ex-data e)))`)
			if err != nil {
				t.Fatalf("validation escaped catch: %v", err)
			}
			assertURLArgumentError(t, mustMap(t, value), tt.function, tt.argument)
		})
	}
}

func TestURLRejectsDirectInvalidUTF8(t *testing.T) {
	tests := []struct {
		name     string
		builtin  string
		args     []types.Value
		function string
		argument string
	}{
		{"parse", "url-parse*", []types.Value{atom.NewString("\xff")}, urlParseFunction, "url"},
		{"format value", "url-format*", []types.Value{coll.NewMapFromPairs([]types.Value{atom.NewKeyword("path"), atom.NewString("\xff")})}, urlFormatFunction, "path"},
		{"format key", "url-format*", []types.Value{coll.NewMapFromPairs([]types.Value{atom.NewKeyword("\xff"), atom.NewString("")})}, urlFormatFunction, "components"},
		{"encode key", "url-encode-query*", []types.Value{coll.NewMapFromPairs([]types.Value{atom.NewString("\xff"), atom.NewString("1")})}, urlEncodeQueryFunction, "query"},
		{"encode value", "url-encode-query*", []types.Value{coll.NewMapFromPairs([]types.Value{atom.NewString("a"), atom.NewString("\xff")})}, urlEncodeQueryFunction, "a"},
		{"decode", "url-decode-query*", []types.Value{atom.NewString("\xff")}, urlDecodeQueryFunction, "query"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builtin, ok := DefaultBuiltinCatalog().lookup(tt.builtin)
			if !ok {
				t.Fatalf("missing builtin %s", tt.builtin)
			}
			result, err := builtin.CtxFn(context.Background(), tt.args...)
			if err == nil || result != nil {
				t.Fatalf("%s = %v, %v", tt.builtin, result, err)
			}
			if !utf8.ValidString(err.Error()) {
				t.Fatalf("error is not valid UTF-8: %q", err.Error())
			}
			data := urlExceptionData(t, err)
			assertURLArgumentError(t, data, tt.function, tt.argument)
			if got := mustString(t, data.Get(atom.NewKeyword("actual"))); got != "<invalid UTF-8>" {
				t.Fatalf("safe actual = %q", got)
			}
		})
	}
}

func TestURLDecodeQueryOrdersKeysDeterministically(t *testing.T) {
	builtin, ok := DefaultBuiltinCatalog().lookup("url-decode-query*")
	if !ok {
		t.Fatal("missing url-decode-query*")
	}
	const want = `{"a" ["1"], "m" ["2"], "z" ["3"]}`
	for range 100 {
		value, err := builtin.CtxFn(context.Background(), atom.NewString("z=3&a=1&m=2"))
		if err != nil {
			t.Fatal(err)
		}
		if got := value.String(); got != want {
			t.Fatalf("decoded query order = %s, want %s", got, want)
		}
	}
}

func assertURLArgumentError(t *testing.T, data *coll.Map, function, argument string) {
	t.Helper()
	if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != "invalid-argument" {
		t.Fatalf("kind = %s", got)
	}
	if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != function {
		t.Fatalf("function = %v, want %s", data.Get(atom.NewKeyword("function")), function)
	}
	if got := mustKeywordString(t, data.Get(atom.NewKeyword("argument"))); got != argument {
		t.Fatalf("argument = %s, want %s", got, argument)
	}
	if got := mustString(t, data.Get(atom.NewKeyword("expected"))); strings.TrimSpace(got) == "" {
		t.Fatal("expected is empty")
	}
	if data.Get(atom.NewKeyword("actual")) == nil {
		t.Fatal("actual is missing")
	}
}

func urlExceptionData(t *testing.T, err error) *coll.Map {
	t.Helper()
	vmErr, ok := err.(*VMError)
	if !ok {
		t.Fatalf("expected VMError, got %T: %v", err, err)
	}
	return mustMap(t, mustExceptionBuiltin(t, "ex-data").Fn(vmErr.Value))
}
