package vm

import (
	"bytes"
	"context"
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

func TestXMLNamesRejectInvalidSpelling(t *testing.T) {
	invalid := []string{
		"", " ", "a b", "\troot", "a\nb", "a\rb", "a\u00a0b",
		"1root", "-root", ".root", "\u0300root",
		"a<b", "a>b", "a/b", "a=b", "a'b", `a"b`, "a&b", "a?b", "a!b",
		"a\x00b", "a\x01b", "a\x1fb", "a\x7fb", "a\u0085b",
		"\xff", "a\xff", "\xc0\xaf", "a\xe2\x82", "\xed\xa0\x80", "\xf4\x90\x80\x80",
	}
	for _, name := range invalid {
		for _, kind := range []string{"keyword", "symbol", "symbol with meta", "string"} {
			for _, field := range []string{"tag", "attribute"} {
				if field == "tag" && kind == "string" {
					continue
				}
				for _, nested := range []bool{false, true} {
					t.Run(fmt.Sprintf("%q/%s/%s/nested=%t", name, kind, field, nested), func(t *testing.T) {
						node := xmlNameRegressionNode(field, xmlNameRegressionValue(kind, name))
						if nested {
							node = coll.NewMapFromPairs([]types.Value{
								xmlTagKey, atom.NewKeyword("root"), xmlContentKey, coll.NewVector(node),
							})
						}
						result, err := xmlNameRegressionEmit(t, node)
						if err == nil {
							t.Fatalf("invalid %s name emitted XML: %v", field, result)
						}
						if result != nil {
							t.Fatalf("conversion error returned partial output: %v", result)
						}
						assertXMLNameInvalidArgument(t, err, field)
					})
				}
			}
		}
	}
}

func TestXMLNamesRejectMissingNilAndUnsupportedNames(t *testing.T) {
	var nilSymbol *types.SymbolWithMeta
	for _, test := range []struct {
		name  string
		node  types.Value
		field string
	}{
		{"missing tag", coll.NewMapFromPairs(nil), "tag"},
		{"Go nil tag", xmlNameRegressionNode("tag", nil), "tag"},
		{"Doggo nil tag", xmlNameRegressionNode("tag", atom.NilValue), "tag"},
		{"nil symbol tag", xmlNameRegressionNode("tag", nilSymbol), "tag"},
		{"string tag", xmlNameRegressionNode("tag", atom.NewString("root")), "tag"},
		{"number tag", xmlNameRegressionNode("tag", atom.NewNumberInt(1)), "tag"},
		{"Go nil attribute", xmlNameRegressionNode("attribute", nil), "attribute"},
		{"Doggo nil attribute", xmlNameRegressionNode("attribute", atom.NilValue), "attribute"},
		{"nil symbol attribute", xmlNameRegressionNode("attribute", nilSymbol), "attribute"},
		{"number attribute", xmlNameRegressionNode("attribute", atom.NewNumberInt(1)), "attribute"},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("invalid name panicked instead of returning invalid-argument: %v", recovered)
				}
			}()
			_, err := xmlNameRegressionEmit(t, test.node)
			if err == nil {
				t.Fatal("invalid name was accepted")
			}
			assertXMLNameInvalidArgument(t, err, test.field)
		})
	}
}

// These are the inclusive endpoints and excluded gaps in XML 1.0 Fifth
// Edition productions [4], [4a], and [5], not Go's older XML reader tables.
func TestXMLNamesFifthEditionRangeBoundaries(t *testing.T) {
	startRanges := [][2]rune{
		{'A', 'Z'}, {'a', 'z'}, {0xC0, 0xD6}, {0xD8, 0xF6}, {0xF8, 0x2FF},
		{0x370, 0x37D}, {0x37F, 0x1FFF}, {0x200C, 0x200D}, {0x2070, 0x218F},
		{0x2C00, 0x2FEF}, {0x3001, 0xD7FF}, {0xF900, 0xFDCF}, {0xFDF0, 0xFFFD},
		{0x10000, 0xEFFFF},
	}
	for _, interval := range startRanges {
		for _, r := range interval {
			for _, name := range []string{string(r), "a" + string(r)} {
				t.Run(fmt.Sprintf("accepted/%U/%q", r, name), func(t *testing.T) {
					assertXMLNameEmits(t, name)
				})
			}
		}
	}
	for _, r := range []rune{':', '_', '-', '.', '0', '9', 0xB7, 0x300, 0x36F, 0x203F, 0x2040} {
		t.Run(fmt.Sprintf("continuation/%U", r), func(t *testing.T) {
			assertXMLNameEmits(t, "a"+string(r))
			if r != ':' && r != '_' {
				_, err := xmlNameRegressionEmit(t, xmlNameRegressionNode("tag", atom.NewKeyword(string(r))))
				if err == nil {
					t.Fatalf("continuation-only character %U accepted at start", r)
				}
				assertXMLNameInvalidArgument(t, err, "tag")
			}
		})
	}
	for _, r := range []rune{
		'@', '[', '^', '`', '{', ',', '/', ';', 0xB6, 0xB8, 0xBF, 0xD7, 0xF7, 0x37E,
		0x2000, 0x200B, 0x200E, 0x203E, 0x2041, 0x206F, 0x2190, 0x2BFF, 0x2FF0,
		0x3000, 0xE000, 0xF8FF, 0xFDD0, 0xFDEF, 0xFFFE, 0xFFFF, 0xF0000, 0x10FFFF,
	} {
		for _, name := range []string{string(r), "a" + string(r)} {
			t.Run(fmt.Sprintf("excluded/%U/%q", r, name), func(t *testing.T) {
				for _, field := range []string{"tag", "attribute"} {
					_, err := xmlNameRegressionEmit(t, xmlNameRegressionNode(field, atom.NewKeyword(name)))
					if err == nil {
						t.Fatalf("excluded character %U accepted in %s name %q", r, field, name)
					}
					assertXMLNameInvalidArgument(t, err, field)
				}
			})
		}
	}
}

func TestXMLNamesSupportedTypesAndLexicalSpelling(t *testing.T) {
	for _, name := range []string{
		"root", "_root", "Root_09.-", "éclair", "名前", "a\u0301", "a\u036f", "a\u203f\u2040",
		"ns:root", "xmlns", "xmlns:ns", ":", ":root", "root:", "a::b",
		// Newly legal Fifth Edition names are writer controls only: encoding/xml
		// still uses older Appendix B name tables and cannot round trip them all.
		"\u200croot", "\u037froot", "\U0001f600", "\ufffd",
	} {
		t.Run(name, func(t *testing.T) { assertXMLNameEmits(t, name) })
	}
}

func TestXMLWriteStringRejectsInvalidNamesPublicly(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.xml :as xml])`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "a b", "1root", "a/b", "a<b"} {
		for _, form := range xmlNameRegressionPublicNodes(strconv.Quote(name)) {
			t.Run(name+"/"+form, func(t *testing.T) {
				value, err := runner.Run(`(try (xml/write-string ` + form + `)
					(catch [e] [(:kind (ex-data e)) (:function (ex-data e))]))`)
				if err != nil {
					t.Fatal(err)
				}
				if got, want := value.String(), "[:invalid-argument doggo.xml/write-string]"; got != want {
					t.Fatalf("result = %s, want %s", got, want)
				}
			})
		}
	}
}

func TestXMLWriteInvalidNamePreservesExistingFile(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.xml :as xml])`); err != nil {
		t.Fatal(err)
	}
	for _, form := range xmlNameRegressionPublicNodes(`"a b"`) {
		t.Run(form, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "existing.xml")
			original := []byte("preserve these bytes\x00\r\n")
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			source := `(try (xml/write ` + form + ` ` + strconv.Quote(path) + `)
				(catch [e] [(:kind (ex-data e)) (:function (ex-data e))
				            (:kind (ex-data (ex-cause e))) (:function (ex-data (ex-cause e)))]))`
			value, runErr := runner.Run(source)
			actual, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actual, original) {
				t.Errorf("invalid name changed existing file: got %q, want %q", actual, original)
			}
			if runErr != nil {
				t.Fatal(runErr)
			}
			if got, want := value.String(), "[:invalid-argument doggo.xml/write :invalid-argument doggo.xml/write-string]"; got != want {
				t.Fatalf("result = %s, want %s", got, want)
			}
		})
	}
}

func TestXMLNamesPreservePrefixesUnicodeAndWhitespace(t *testing.T) {
	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.xml :as xml])
		(let [node {:tag (keyword "ns:名前")
		            :attrs (hash-map (keyword "ns:é") "1" "xmlns" "urn:default" (keyword "xmlns:ns") "urn:x" 'id "i")
		            :content [{:tag (keyword "ns:child") :content ["txt"]}]}
		      emitted (xml/write-string node)
		      restored (xml/read-string emitted)]
		  [emitted (= (:tag restored) (keyword "ns:名前"))
		   (= (:attrs restored) {:id "i" (keyword "ns:é") "1" :xmlns "urn:default" (keyword "xmlns:ns") "urn:x"})
		   (= (:content restored) [{:tag (keyword "ns:child") :attrs nil :content ["\ntxt\n"]}])])`)
	if err != nil {
		t.Fatal(err)
	}
	result := mustVector(t, value)
	want := "<?xml version='1.0' encoding='UTF-8'?>\n<ns:名前 id='i' ns:é='1' xmlns='urn:default' xmlns:ns='urn:x'>\n<ns:child>\ntxt\n</ns:child>\n</ns:名前>"
	if got := mustString(t, result.Items[0]); got != want {
		t.Fatalf("emitted = %q, want %q", got, want)
	}
	for _, restored := range result.Items[1:] {
		if restored.String() != "true" {
			t.Fatalf("prefix/Unicode reader round trip changed: %s", result.String())
		}
	}
}

func xmlNameRegressionPublicNodes(quotedName string) []string {
	tag := `{:tag (keyword ` + quotedName + `)}`
	attr := `{:tag :root :attrs (hash-map ` + quotedName + ` "x")}`
	return []string{tag, attr, `{:tag :root :content [` + tag + `]}`, `{:tag :root :content [` + attr + `]}`}
}

func xmlNameRegressionValue(kind, name string) types.Value {
	switch kind {
	case "keyword":
		return atom.NewKeyword(name)
	case "symbol":
		return types.NewSymbol(name)
	case "symbol with meta":
		return types.NewSymbolWithMeta(name, coll.NewMapFromPairs(nil))
	default:
		return atom.NewString(name)
	}
}

func xmlNameRegressionNode(field string, name types.Value) types.Value {
	if field == "tag" {
		return coll.NewMapFromPairs([]types.Value{xmlTagKey, name})
	}
	// An array map also allows testing nil keys at this conversion boundary
	// without dereferencing them in a hash-map constructor first.
	attrs, _ := coll.NewArrayMap(name, atom.NewString("x"))
	return coll.NewMapFromPairs([]types.Value{xmlTagKey, atom.NewKeyword("root"), xmlAttrsKey, attrs})
}

func xmlNameRegressionEmit(t *testing.T, value types.Value) (types.Value, error) {
	t.Helper()
	for _, builtin := range builtinsXML {
		if builtin.Name == "xml-emit-string*" {
			return builtin.Builtin.CtxFn(context.Background(), value)
		}
	}
	t.Fatal("missing xml-emit-string* builtin")
	return nil, nil
}

func assertXMLNameEmits(t *testing.T, name string) {
	t.Helper()
	for _, kind := range []string{"keyword", "symbol", "symbol with meta", "string"} {
		for _, field := range []string{"tag", "attribute"} {
			if field == "tag" && kind == "string" {
				continue
			}
			value, err := xmlNameRegressionEmit(t, xmlNameRegressionNode(field, xmlNameRegressionValue(kind, name)))
			if err != nil {
				t.Fatalf("valid %s %s name %q rejected: %v", kind, field, name, err)
			}
			want := "<?xml version='1.0' encoding='UTF-8'?>\n<" + name + "/>"
			if field == "attribute" {
				want = "<?xml version='1.0' encoding='UTF-8'?>\n<root " + name + "='x'/>"
			}
			if got := mustString(t, value); got != want {
				t.Fatalf("valid name spelling changed: got %q, want %q", got, want)
			}
		}
	}
}

func assertXMLNameInvalidArgument(t *testing.T, err error, field string) {
	t.Helper()
	vmErr, ok := err.(*VMError)
	if !ok {
		t.Fatalf("expected VMError, got %T: %v", err, err)
	}
	data := mustMap(t, mustExceptionBuiltin(t, "ex-data").Fn(vmErr.Value))
	if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != "invalid-argument" {
		t.Fatalf("kind = %q, want invalid-argument", got)
	}
	if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != "doggo.xml/write-string" {
		t.Fatalf("function = %v, want doggo.xml/write-string", data.Get(atom.NewKeyword("function")))
	}
	message := mustString(t, mustExceptionBuiltin(t, "ex-message").Fn(vmErr.Value))
	wantField := "field :tag"
	if field == "attribute" {
		wantField = "attribute key"
	}
	if !strings.HasPrefix(message, "doggo.xml/write-string:") || !strings.Contains(message, wantField) {
		t.Fatalf("message = %q, want XML function and %q context", message, wantField)
	}
}
