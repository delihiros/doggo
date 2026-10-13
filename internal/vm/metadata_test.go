package vm

import (
	"strings"
	"testing"

	"doggo/internal/types/atom"
)

// TestMetaOnSymbol tests that meta works on symbols
func TestMetaOnSymbol(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with-meta on symbol",
			input:    `(meta (with-meta 'foo {:a 1}))`,
			expected: "{:a 1}",
		},
		{
			name:     "meta on reader metadata symbol",
			input:    `(meta '^{:doc "hello"} foo)`,
			expected: `{:doc "hello"}`,
		},
		{
			name:     "meta on symbol without metadata returns empty map",
			input:    `(meta 'foo)`,
			expected: "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestMetaOnList tests that meta works on lists
func TestMetaOnList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with-meta on quoted list",
			input:    `(meta (with-meta '(a b) {:x 9}))`,
			expected: "{:x 9}",
		},
		{
			name:     "meta on reader metadata list",
			input:    `(meta '^{:private true} (defn foo []))`,
			expected: "{:private true}",
		},
		{
			name:     "meta on list without metadata returns empty map",
			input:    `(meta '(a b c))`,
			expected: "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestWithMetaPreservesValue tests that with-meta preserves the original value
func TestWithMetaPreservesValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "symbol value is preserved",
			input:    `(str (with-meta 'foo {:a 1}))`,
			expected: `"foo"`,
		},
		{
			name:     "list value is preserved",
			input:    `(first (with-meta '(a b c) {:x 1}))`,
			expected: "a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestWithMetaRejectsUnsupportedValues(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType string
	}{
		{name: "number", input: `(with-meta 1 {:tag :x})`, wantType: "number"},
		{name: "var", input: `(do (def x 1) (with-meta #'x {:tag :x}))`, wantType: "var"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			_, err := runner.Run(tt.input)
			if err == nil {
				t.Fatalf("with-meta accepted unsupported %s", tt.wantType)
			}
			if !strings.Contains(err.Error(), "metadata is not supported for type "+tt.wantType) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestMetadataDoesNotAffectEqualityRuntime tests that metadata does not affect equality at runtime
func TestMetadataDoesNotAffectEqualityRuntime(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "symbols with different meta are equal",
			input:    `(= (with-meta 'foo {:a 1}) (with-meta 'foo {:b 2}))`,
			expected: true,
		},
		{
			name:     "symbol with meta equals plain symbol",
			input:    `(= (with-meta 'foo {:a 1}) 'foo)`,
			expected: true,
		},
		{
			name:     "lists with different meta are equal",
			input:    `(= (with-meta '(a b) {:a 1}) (with-meta '(a b) {:b 2}))`,
			expected: true,
		},
		{
			name:     "list with meta equals plain list",
			input:    `(= (with-meta '(a b) {:a 1}) '(a b))`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			boolResult, ok := result.(atom.Boolean)
			if !ok {
				t.Fatalf("expected boolean, got %T", result)
			}
			if bool(boolResult) != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, boolResult)
			}
		})
	}
}

// TestVaryMeta tests vary-meta function
func TestVaryMeta(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			// (vary-meta 'foo assoc :a 1) が (with-meta 'foo {:a 1}) と等価
			name:     "vary-meta with assoc on symbol",
			input:    `(meta (vary-meta 'foo assoc :a 1))`,
			expected: "{:a 1}",
		},
		{
			// (vary-meta (with-meta 'foo {:count 0}) update :count inc)
			// が {:count 1} をメタデータに持つ
			name:     "vary-meta with update on existing meta",
			input:    `(:count (meta (vary-meta (with-meta 'foo {:count 0}) update :count inc)))`,
			expected: "1",
		},
		{
			// vary-meta 結果のシンボル値自体は保持される
			name:     "vary-meta preserves symbol value",
			input:    `(str (vary-meta 'bar assoc :x 1))`,
			expected: `"bar"`,
		},
		{
			// vary-meta で複数のキーを追加
			name:     "vary-meta with multiple keys",
			input:    `(count (meta (vary-meta 'baz assoc :a 1 :b 2)))`,
			expected: "2",
		},
		{
			// vary-meta on vector
			name:     "vary-meta on vector",
			input:    `(:tag (meta (vary-meta [1 2 3] assoc :tag :vec)))`,
			expected: ":vec",
		},
		{
			// vary-meta on map
			name:     "vary-meta on map",
			input:    `(:source (meta (vary-meta {:a 1} assoc :source "test")))`,
			expected: `"test"`,
		},
		{
			// vary-meta on list
			name:     "vary-meta on list",
			input:    `(:line (meta (vary-meta '(a b c) assoc :line 42)))`,
			expected: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestMetaOnExistingTypes tests that meta still works on existing supported types
func TestMetaOnExistingTypes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		checkFn func(t *testing.T, result string)
	}{
		{
			name:  "meta on vector",
			input: `(meta (with-meta [1 2 3] {:type :vector}))`,
			checkFn: func(t *testing.T, result string) {
				if !strings.Contains(result, ":type") {
					t.Errorf("expected result to contain :type, got %s", result)
				}
			},
		},
		{
			name:  "meta on map",
			input: `(meta (with-meta {:a 1} {:type :map}))`,
			checkFn: func(t *testing.T, result string) {
				if !strings.Contains(result, ":type") {
					t.Errorf("expected result to contain :type, got %s", result)
				}
			},
		},
		{
			name:  "meta on set",
			input: `(meta (with-meta #{1 2} {:type :set}))`,
			checkFn: func(t *testing.T, result string) {
				if !strings.Contains(result, ":type") {
					t.Errorf("expected result to contain :type, got %s", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.checkFn(t, result.String())
		})
	}
}

func TestReaderMetadataOnCollectionLiterals(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "vector reader metadata",
			input:    `(meta ^{:tag :vector} [1 2 3])`,
			expected: "{:tag :vector}",
		},
		{
			name:     "map reader metadata preserves metadata",
			input:    `(meta ^{:tag :map} {:z 1 :a 2})`,
			expected: "{:tag :map}",
		},
		{
			name:     "set reader metadata",
			input:    `(meta ^{:tag :set} #{1 2})`,
			expected: "{:tag :set}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestWithMetaPreservesMapOrder(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`(with-meta {:z 1 :a 2} {:tag :ordered})`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := result.String(); got != "{:z 1, :a 2}" {
		t.Fatalf("expected map order to be preserved, got %s", got)
	}
}

func TestMetaOnGenericSeq(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "meta on lazy seq",
			input:    `(meta (with-meta (map identity [1 2 3]) {:tag :lazy-seq}))`,
			expected: "{:tag :lazy-seq}",
		},
		{
			name:     "with-meta preserves seq values",
			input:    `(pr-str (with-meta (map identity [1 2 3]) {:tag :lazy-seq}))`,
			expected: `"(1 2 3)"`,
		},
		{
			name:     "metadata does not affect seq equality",
			input:    `(= (with-meta (map identity [1 2 3]) {:tag :lazy-seq}) (map identity [1 2 3]))`,
			expected: "true",
		},
		{
			name:     "with-meta preserves seq laziness",
			input:    `(realized? (with-meta (map identity [1 2 3]) {:tag :lazy-seq}))`,
			expected: "false",
		},
		{
			name:     "seq coercion strips generic seq metadata",
			input:    `(meta (seq (with-meta (map identity [1 2 3]) {:tag :lazy-seq})))`,
			expected: "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			result, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := result.String(); got != tt.expected {
				t.Fatalf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}
