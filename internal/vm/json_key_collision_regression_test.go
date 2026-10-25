package vm

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestJSONRejectsConvertedKeyCollisions(t *testing.T) {
	keys := []struct {
		name  string
		value types.Value
	}{
		{"string", atom.NewString("role")},
		{"keyword", atom.NewKeyword("role")},
		{"symbol", types.NewSymbol("role")},
	}
	for first, firstKey := range keys {
		for second, secondKey := range keys {
			if first == second {
				continue
			}
			for _, values := range []struct {
				name          string
				first, second types.Value
			}{
				{"different values", atom.NewString("guest"), atom.NewString("admin")},
				{"equal values", atom.NewString("same"), atom.NewString("same")},
				{"first value nil", atom.NilValue, atom.NewString("admin")},
			} {
				for _, shape := range []string{"map", "nested map", "map in vector"} {
					t.Run(firstKey.name+" then "+secondKey.name+"/"+values.name+"/"+shape, func(t *testing.T) {
						value := coll.NewMapFromPairs([]types.Value{
							firstKey.value, values.first, secondKey.value, values.second,
						})
						if value.Count() != 2 {
							t.Fatalf("source map lost distinct keys: %s", value.String())
						}
						var input types.Value = value
						switch shape {
						case "nested map":
							input = coll.NewMapFromPairs([]types.Value{atom.NewKeyword("outer"), value})
						case "map in vector":
							input = coll.NewVector(value)
						}
						converted, err := jsonFromDoggoValue(input)
						if err == nil {
							t.Fatalf("collision encoded as %#v, error = %v", converted, err)
						}
						vmErr, ok := err.(*VMError)
						if !ok {
							t.Fatalf("expected VMError, got %T: %v", err, err)
						}
						assertJSONKeyCollisionError(t,
							mustExceptionBuiltin(t, "ex-data").Fn(vmErr.Value),
							mustExceptionBuiltin(t, "ex-message").Fn(vmErr.Value))
					})
				}
			}
		}
	}
}

func TestJSONWriteStringRejectsConvertedKeyCollisions(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.json :as json])`); err != nil {
		t.Fatal(err)
	}
	keys := []string{`"role"`, `:role`, `'role`}
	for first, firstKey := range keys {
		for second, secondKey := range keys {
			if first == second {
				continue
			}
			for _, options := range []string{"", " {:pretty true}"} {
				t.Run(firstKey+" then "+secondKey+options, func(t *testing.T) {
					source := fmt.Sprintf(`
						(try
						  (json/write-string (hash-map %s "guest" %s "admin")%s)
						  (catch [e] [(ex-data e) (ex-message e)]))`, firstKey, secondKey, options)
					value, err := runner.Run(source)
					if err != nil {
						t.Fatal(err)
					}
					caught := mustVector(t, value)
					assertJSONKeyCollisionError(t, caught.Items[0], caught.Items[1])
				})
			}
		}
	}
}

func TestJSONWriteCollisionPreservesExistingFile(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.json :as json])`); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`{:role "guest" "role" "admin"}`, `{:outer [{:role nil "role" "admin"}]}`} {
		for _, options := range []string{"", " {:pretty true}"} {
			t.Run(value+options, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "existing.json")
				original := []byte("preserve these bytes\x00\r\n")
				if err := os.WriteFile(path, original, 0o600); err != nil {
					t.Fatal(err)
				}
				source := fmt.Sprintf(`
					(try (json/write %s %s%s)
					  (catch [e] [(ex-data e) (ex-message e)]))`, value, strconv.Quote(path), options)
				caught, runErr := runner.Run(source)
				actual, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(actual, original) {
					t.Errorf("collision changed existing file: got %q, want %q", actual, original)
				}
				if runErr != nil {
					t.Fatal(runErr)
				}
				result := mustVector(t, caught)
				assertJSONKeyCollisionError(t, result.Items[0], result.Items[1])
			})
		}
	}
}

func TestJSONDistinctKeysKeepDeterministicAndPrettyOutput(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.json :as json])`); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`(hash-map 'z 3 :b 2 "a" nil)`, `(hash-map "a" nil :b 2 'z 3)`} {
		for _, test := range []struct {
			name, options, want string
		}{
			{"compact", "", `{"a":null,"b":2,"z":3}`},
			{"pretty", " {:pretty true}", "{\n  \"a\": null,\n  \"b\": 2,\n  \"z\": 3\n}"},
			{"custom pretty", ` {:pretty {:prefix ">>" :indent "--"}}`, "{\n>>--\"a\": null,\n>>--\"b\": 2,\n>>--\"z\": 3\n>>}"},
		} {
			t.Run(input+"/"+test.name, func(t *testing.T) {
				value, err := runner.Run(`(json/write-string ` + input + test.options + `)`)
				if err != nil {
					t.Fatal(err)
				}
				if got := mustString(t, value); got != test.want {
					t.Fatalf("encoded = %q, want %q", got, test.want)
				}
			})
		}
	}
	value, err := runner.Run(`(json/write-string {":role" 1 :role 2 "app/name" 3 :app/status 4 'app/title 5})`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mustString(t, value), `{":role":1,"app/name":3,"app/status":4,"app/title":5,"role":2}`; got != want {
		t.Fatalf("named key formatting changed: got %q, want %q", got, want)
	}
}

func assertJSONKeyCollisionError(t *testing.T, payload, message types.Value) {
	t.Helper()
	data := mustMap(t, payload)
	if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != "invalid-argument" {
		t.Fatalf("kind = %q, want invalid-argument", got)
	}
	if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != "doggo.json/write-string" {
		t.Fatalf("function = %v, want doggo.json/write-string", data.Get(atom.NewKeyword("function")))
	}
	text := mustString(t, message)
	if !strings.HasPrefix(text, "doggo.json/write-string:") || !strings.Contains(text, "collision") || !strings.Contains(text, `"role"`) {
		t.Fatalf("expected collision message naming JSON key role, got %q", text)
	}
}
