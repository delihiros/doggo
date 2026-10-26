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

func TestXMLOutputRejectsDuplicateAttributeNames(t *testing.T) {
	kinds := []string{"string", "keyword", "symbol", "symbol with meta"}
	values := []struct {
		name          string
		first, second types.Value
	}{
		{"different", atom.NewString("first"), atom.NewString("second")},
		{"equal", atom.NewString("same"), atom.NewString("same")},
		{"both nil", atom.NilValue, atom.NilValue},
		{"first nil", atom.NilValue, atom.NewString("second")},
		{"second nil", atom.NewString("first"), atom.NilValue},
		{"Go nil", nil, nil},
	}
	for _, first := range kinds {
		for _, second := range kinds {
			for _, values := range values {
				for _, nested := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/nested=%t", first, second, values.name, nested), func(t *testing.T) {
						// Array maps preserve ordered entries, including repeated keys
						// of the same type, so every ordered pair reaches conversion.
						attrs, err := coll.NewArrayMap(
							xmlNameRegressionValue(first, "ns:id"), values.first,
							xmlNameRegressionValue(second, "ns:id"), values.second,
						)
						if err != nil {
							t.Fatal(err)
						}
						node := xmlOutputRegressionWrap(coll.NewMapFromPairs([]types.Value{
							xmlTagKey, atom.NewKeyword("item"), xmlAttrsKey, attrs,
						}), nested)
						assertXMLOutputRejected(t, node, "duplicate attribute", "ns:id")
					})
				}
			}
		}
	}
}

func TestXMLOutputRejectsInvalidCharactersAndUTF8(t *testing.T) {
	for _, invalid := range xmlOutputRegressionInvalidStrings() {
		for _, field := range []string{"text", "attribute", "keyword attribute", "symbol attribute", "vector attribute"} {
			for _, nested := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/nested=%t", invalid.name, field, nested), func(t *testing.T) {
					node := xmlOutputRegressionWrap(xmlOutputRegressionNode(field, invalid.text), nested)
					context := "field :content"
					if field != "text" {
						context = "attribute value"
					}
					assertXMLOutputRejected(t, node, context)
				})
			}
		}
	}
}

// XML 1.0 Fifth Edition production [2] includes these endpoints and also
// discouraged (but legal) C1 controls and noncharacters in supplementary planes.
func TestXMLOutputAcceptsXMLCharBoundaries(t *testing.T) {
	valid := []string{"", "\t", "\n", "\r", " "}
	for _, r := range []rune{
		0x7F, 0x85, 0x9F, 0xD7FF, 0xE000, 0xFDD0, 0xFDEF,
		0xFFFD, 0x10000, 0x1FFFE, 0x1FFFF, 0x10FFFE, 0x10FFFF,
	} {
		valid = append(valid, string(r))
	}
	for _, text := range valid {
		for _, field := range []string{"text", "attribute", "keyword attribute", "symbol attribute", "vector attribute"} {
			t.Run(fmt.Sprintf("%q/%s", text, field), func(t *testing.T) {
				node := xmlOutputRegressionNode(field, text)
				if converted, err := xmlDoggoToNode(node); err != nil || converted == nil {
					t.Fatalf("legal XML Char rejected in conversion: node=%v err=%v", converted, err)
				}
				value, err := xmlNameRegressionEmit(t, node)
				if err != nil {
					t.Fatalf("legal XML Char rejected in output: %v", err)
				}
				want := "<?xml version='1.0' encoding='UTF-8'?>\n<item>\n" + text + "\n</item>"
				if field != "text" {
					var stringified string
					switch field {
					case "keyword attribute":
						stringified = ":value" + text
					case "symbol attribute":
						stringified = "value" + text
					case "vector attribute":
						stringified = "[value" + text + "]"
					default:
						stringified = text
					}
					want = "<?xml version='1.0' encoding='UTF-8'?>\n<item a='" + stringified + "'/>"
				}
				if got := mustString(t, value); got != want {
					t.Fatalf("legal XML Char bytes changed: got %q, want %q", got, want)
				}
			})
		}
	}
}

func TestXMLWriteRejectsDuplicateAttributesPubliclyBeforeFileWrite(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.xml :as xml])`); err != nil {
		t.Fatal(err)
	}
	keys := []string{`"id"`, `:id`, `'id`, `(with-meta 'id {:note true})`}
	for firstIndex, first := range keys {
		for secondIndex, second := range keys {
			if firstIndex == secondIndex || firstIndex >= 2 && secondIndex >= 2 {
				continue // Equal map keys replace one another before XML conversion.
			}
			for _, values := range [][2]string{{`"first"`, `"second"`}, {`"same"`, `"same"`}, {`nil`, `nil`}, {`nil`, `"second"`}, {`"first"`, `nil`}} {
				for _, nested := range []bool{false, true} {
					form := `{:tag :item :attrs (hash-map ` + first + ` ` + values[0] + ` ` + second + ` ` + values[1] + `)}`
					if nested {
						form = `{:tag :root :content ["valid prefix" {:tag :valid} ` + form + `]}`
					}
					t.Run(form, func(t *testing.T) { assertXMLPublicOutputRejected(t, runner, form) })
				}
			}
		}
	}
}

func TestXMLWriteRejectsInvalidCharactersPubliclyBeforeFileWrite(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.xml :as xml])`); err != nil {
		t.Fatal(err)
	}
	index := 0
	for _, invalid := range xmlOutputRegressionInvalidStrings() {
		for _, field := range []string{"text", "attribute", "keyword attribute", "symbol attribute", "vector attribute"} {
			for _, nested := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/nested=%t", invalid.name, field, nested), func(t *testing.T) {
					// Bind the actual bytes at the host boundary so the Doggo reader
					// cannot replace or reject malformed UTF-8 before the public API.
					name := "xml-output-fixture-" + strconv.Itoa(index)
					index++
					value := xmlOutputRegressionWrap(xmlOutputRegressionNode(field, invalid.text), nested)
					runner.initializeCoreGlobalIfMissing(name, value)
					assertXMLPublicOutputRejected(t, runner, "core/"+name)
				})
			}
		}
	}
}

func TestXMLOutputPreservesEscapesOrderingWhitespaceAndLexicalNames(t *testing.T) {
	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.xml :as xml])
		(xml/write-string
		  {:tag (keyword "ns:名前")
		   :attrs (hash-map "z" "<&>'\"\t\n\r" :ID nil 'id 12
		                    (keyword "p:id") "p" (keyword "q:id") "q"
		                    (keyword "xmlns:p") "urn:same" (keyword "xmlns:q") "urn:same")
		   :content ["\t\n\r<&>'\"名前😀" {:tag :child :attrs {:id true}} "  "]})`)
	if err != nil {
		t.Fatal(err)
	}
	want := "<?xml version='1.0' encoding='UTF-8'?>\n<ns:名前 ID='nil' id='12' p:id='p' q:id='q' xmlns:p='urn:same' xmlns:q='urn:same' z='&lt;&amp;&gt;&apos;&quot;\t\n\r'>\n\t\n\r&lt;&amp;&gt;'\"名前😀\n<child id='true'/>\n  \n</ns:名前>"
	if got := mustString(t, value); got != want {
		t.Fatalf("XML output contract changed: got %q, want %q", got, want)
	}
	// Repeated equal map keys retain ordinary map replacement semantics; the
	// duplicate-name check applies to entries still present during conversion.
	value, err = runner.Run(`(xml/write-string {:tag :root :attrs (hash-map :id "old" :id "new")})`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mustString(t, value), "<?xml version='1.0' encoding='UTF-8'?>\n<root id='new'/>"; got != want {
		t.Fatalf("ordinary map replacement changed: got %q, want %q", got, want)
	}
}

type xmlOutputRegressionString struct{ name, text string }

func xmlOutputRegressionInvalidStrings() []xmlOutputRegressionString {
	var invalid []xmlOutputRegressionString
	for r := rune(0); r < 0x20; r++ {
		if r != '\t' && r != '\n' && r != '\r' {
			invalid = append(invalid, xmlOutputRegressionString{fmt.Sprintf("%U", r), "before" + string(r) + "after"})
		}
	}
	return append(invalid,
		xmlOutputRegressionString{"U+FFFE", "before\ufffeafter"},
		xmlOutputRegressionString{"U+FFFF", "before\uffffafter"},
		xmlOutputRegressionString{"invalid leading byte", "before\xffafter"},
		xmlOutputRegressionString{"lone continuation", "before\x80after"},
		xmlOutputRegressionString{"overlong", "before\xc0\xafafter"},
		xmlOutputRegressionString{"truncated", "before\xe2\x82"},
		xmlOutputRegressionString{"high surrogate", "before\xed\xa0\x80after"},
		xmlOutputRegressionString{"low surrogate", "before\xed\xbf\xbfafter"},
		xmlOutputRegressionString{"above Unicode maximum", "before\xf4\x90\x80\x80after"},
	)
}

func xmlOutputRegressionNode(field, text string) types.Value {
	if field == "text" {
		return coll.NewMapFromPairs([]types.Value{
			xmlTagKey, atom.NewKeyword("item"), xmlContentKey, coll.NewVector(atom.NewString(text)),
		})
	}
	var value types.Value = atom.NewString(text)
	switch field {
	case "keyword attribute":
		value = atom.NewKeyword("value" + text)
	case "symbol attribute":
		value = types.NewSymbol("value" + text)
	case "vector attribute":
		value = coll.NewVector(types.NewSymbol("value" + text))
	}
	return coll.NewMapFromPairs([]types.Value{
		xmlTagKey, atom.NewKeyword("item"),
		xmlAttrsKey, coll.NewMapFromPairs([]types.Value{atom.NewKeyword("a"), value}),
	})
}

func xmlOutputRegressionWrap(node types.Value, nested bool) types.Value {
	if !nested {
		return node
	}
	return coll.NewMapFromPairs([]types.Value{
		xmlTagKey, atom.NewKeyword("root"),
		xmlContentKey, coll.NewVector(atom.NewString("valid prefix"),
			coll.NewMapFromPairs([]types.Value{xmlTagKey, atom.NewKeyword("valid")}), node),
	})
}

func assertXMLOutputRejected(t *testing.T, value types.Value, contexts ...string) {
	t.Helper()
	converted, err := xmlDoggoToNode(value)
	if err == nil || converted != nil {
		t.Errorf("invalid XML accepted in conversion: node=%v err=%v", converted, err)
	}
	result, err := xmlNameRegressionEmit(t, value)
	if err == nil {
		t.Fatalf("invalid XML emitted without rejection: %v", result)
	}
	if result != nil {
		t.Errorf("conversion error returned partial output: %v", result)
	}
	vmErr, ok := err.(*VMError)
	if !ok {
		t.Fatalf("expected VMError, got %T: %v", err, err)
	}
	data := mustMap(t, mustExceptionBuiltin(t, "ex-data").Fn(vmErr.Value))
	if got := mustKeywordString(t, data.Get(atom.NewKeyword("kind"))); got != "invalid-argument" {
		t.Errorf("kind = %q, want invalid-argument", got)
	}
	if got, ok := types.SymbolName(data.Get(atom.NewKeyword("function"))); !ok || got != "doggo.xml/write-string" {
		t.Errorf("function = %v, want doggo.xml/write-string", data.Get(atom.NewKeyword("function")))
	}
	message := mustString(t, mustExceptionBuiltin(t, "ex-message").Fn(vmErr.Value))
	for _, context := range append(contexts, "doggo.xml/write-string:") {
		if !strings.Contains(message, context) {
			t.Errorf("message = %q, want context %q", message, context)
		}
	}
}

func assertXMLPublicOutputRejected(t *testing.T, runner *Runner, nodeForm string) {
	t.Helper()
	value, err := runner.Run(`(try (xml/write-string ` + nodeForm + `)
		(catch [e] [(:kind (ex-data e)) (:function (ex-data e))]))`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := value.String(), "[:invalid-argument doggo.xml/write-string]"; got != want {
		t.Errorf("public write-string result = %s, want %s", got, want)
	}
	path := filepath.Join(t.TempDir(), "existing.xml")
	original := []byte("<?xml version='1.0'?><original>preserve these bytes</original>\r\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	value, runErr := runner.Run(`(try (xml/write ` + nodeForm + ` ` + strconv.Quote(path) + `)
		(catch [e] [(:kind (ex-data e)) (:function (ex-data e))
		            (:kind (ex-data (ex-cause e))) (:function (ex-data (ex-cause e)))]))`)
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, original) {
		t.Errorf("invalid XML changed existing file: got %q, want %q", actual, original)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	if got, want := value.String(), "[:invalid-argument doggo.xml/write :invalid-argument doggo.xml/write-string]"; got != want {
		t.Errorf("public write result = %s, want %s", got, want)
	}
}
