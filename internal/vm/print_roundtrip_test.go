package vm

import (
	"fmt"
	"testing"

	"doggo/internal/types/atom"
)

func TestPrStrReadStringPreservesStringsInCollections(t *testing.T) {
	runner := MustNewRunner()
	strings := []struct {
		name    string
		literal string
	}{
		{"plain", `"abc"`},
		{"quote", `"a\"b"`},
		{"literal backslash n", `"a\\n"`},
		{"control characters", `"a\b\f\n\r\tz"`},
		{"escaped NUL", `"a\u0000z"`},
		{"Unicode", `"日本語🙂"`},
	}
	shapes := []struct {
		name string
		form string
	}{
		{"scalar", `%s`},
		{"vector", `[%s]`},
		{"list", `(list %s)`},
		{"map", `{:value %s}`},
		{"set", `#{%s}`},
		{"nested map key", `[{%[1]s (list #{%[1]s})}]`},
	}

	for _, shape := range shapes {
		for _, str := range strings {
			t.Run(shape.name+"/"+str.name, func(t *testing.T) {
				value := fmt.Sprintf(shape.form, str.literal)
				source := fmt.Sprintf(`(let [value %s] (= value (read-string (pr-str value))))`, value)
				result, err := runner.Run(source)
				if err != nil {
					t.Fatalf("read-string(pr-str(%s)) failed: %v", value, err)
				}
				if result != atom.True {
					t.Fatalf("read-string(pr-str(%s)) changed its value", value)
				}
			})
		}
	}
}
