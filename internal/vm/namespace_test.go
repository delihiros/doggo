package vm

import (
	"strings"
	"testing"
)

// TestNamespaceBasic は基本的な名前空間操作をテストします。
func TestNamespaceBasic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ns_declaration",
			input:    `(ns my.ns) nil`,
			expected: "nil",
		},
		{
			name:     "ns_def_in_namespace",
			input:    `(ns my.ns) (def x 42) x`,
			expected: "42",
		},
		{
			name:     "ns_qualified_access",
			input:    `(ns my.ns) (def x 42) (ns other) my.ns/x`,
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

// TestNamespaceIsolation は名前空間の分離をテストします。
func TestNamespaceIsolation(t *testing.T) {
	runner := MustNewRunner()

	// 名前空間 ns1 で x を定義
	_, err := runner.Run(`(ns ns1) (def x 1)`)
	if err != nil {
		t.Fatalf("unexpected error in ns1: %v", err)
	}

	// 名前空間 ns2 で同じ名前 x を別の値で定義
	_, err = runner.Run(`(ns ns2) (def x 2)`)
	if err != nil {
		t.Fatalf("unexpected error in ns2: %v", err)
	}

	// ns1/x は 1 であるべき
	result1, err := runner.Run(`ns1/x`)
	if err != nil {
		t.Fatalf("unexpected error accessing ns1/x: %v", err)
	}
	if result1.String() != "1" {
		t.Errorf("expected ns1/x = 1, got %s", result1.String())
	}

	// ns2/x は 2 であるべき
	result2, err := runner.Run(`ns2/x`)
	if err != nil {
		t.Fatalf("unexpected error accessing ns2/x: %v", err)
	}
	if result2.String() != "2" {
		t.Errorf("expected ns2/x = 2, got %s", result2.String())
	}
}

// TestNamespaceCoreAccess はコア関数へのアクセスをテストします。
func TestNamespaceCoreAccess(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "access_builtin_from_custom_ns",
			input:    `(ns custom.ns) (first [1 2 3])`,
			expected: "1",
		},
		{
			name:     "access_core_lib_from_custom_ns",
			input:    `(ns custom.ns) (map inc [1 2 3])`,
			expected: "(2 3 4)", // map は遅延シーケンスを返す
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestNamespaceDefaultUser はデフォルト名前空間（user）をテストします。
func TestNamespaceDefaultUser(t *testing.T) {
	runner := MustNewRunner()

	// デフォルトで user 名前空間にいるはず
	_, err := runner.Run(`(def y 100)`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// user/y でアクセス可能
	assertRunResult(t, runner, `user/y`, "100")
}

// TestInNs は in-ns 特殊形式をテストします。
func TestInNs(t *testing.T) {
	runner := MustNewRunner()

	// ns で名前空間を作成
	_, err := runner.Run(`(ns test.ns) (def value 42)`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// in-ns で名前空間を切り替え
	_, err = runner.Run(`(in-ns test.ns)`)
	if err != nil {
		t.Fatalf("unexpected error in in-ns: %v", err)
	}

	// 現在の名前空間から value にアクセス
	assertRunResult(t, runner, `value`, "42")
}

func TestNsSupportsDocImportAndReferClojureExclude(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ns with require clause",
			input:    `(ns my.app (:require [doggo.core :as c])) *ns*`,
			expected: "my.app",
		},
		{
			name:     "ns with import clause",
			input:    `(ns my.app (:import "fmt")) (fmt/Sprintf "%s-%d" "x" 1)`,
			expected: `"x-1"`,
		},
		{
			name:     "ns with docstring",
			input:    `(ns my.app "This is a docstring") *ns*`,
			expected: "my.app",
		},
		{
			name:     "ns with metadata map",
			input:    `(ns my.app {:team "core"}) *ns*`,
			expected: "my.app",
		},
		{
			name:     "ns with symbol metadata and attr map",
			input:    `(ns ^:private my.app "doc" {:team "core"}) *ns*`,
			expected: "my.app",
		},
		{
			name:     "refer-clojure exclude blocks map",
			input:    `(ns my.app (:refer-clojure :exclude [map])) (reduce + 0 [1 2 3])`,
			expected: "6",
		},
		{
			name:     "refer-clojure refer re-enables excluded symbol",
			input:    `(ns my.app (:refer-clojure :exclude [map] :refer [map])) (map inc [1 2 3])`,
			expected: "(2 3 4)",
		},
		{
			name:     "refer-clojure rename adds renamed core symbol",
			input:    `(ns my.app (:refer-clojure :rename {map my-map})) (my-map inc [1 2 3])`,
			expected: "(2 3 4)",
		},
		{
			name:     "refer-clojure rename works for builtins",
			input:    `(ns my.app (:refer-clojure :rename {count len})) (len [1 2 3])`,
			expected: "3",
		},
		{
			name:     "refer-clojure rename works for macros",
			input:    `(ns my.app (:refer-clojure :rename {when my-when})) (my-when true 1)`,
			expected: "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			got, err := runner.Run(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.expected {
				t.Fatalf("expected %s, got %s", tt.expected, got.String())
			}
		})
	}
}

// TestNsRejectsUnsupportedClauses は ns が未対応の追加引数を明示的に拒否することをテストします。
func TestNsRejectsUnsupportedClauses(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSubstr string
	}{
		{
			name:       "ns with use clause",
			input:      `(ns my.app (:use [some.lib]))`,
			wantSubstr: ":use is not supported; use (:require [some.lib :refer :all]) or explicit :refer instead",
		},
		{
			name:       "ns with gen-class clause",
			input:      `(ns my.app (:gen-class))`,
			wantSubstr: ":gen-class is not supported; doggo does not generate JVM classes",
		},
		{
			name:       "ns with invalid import spec",
			input:      `(ns my.app (:import 123))`,
			wantSubstr: "ns :import entries must be string literals",
		},
		{
			name:       "ns with refer-clojure exclude non-vector",
			input:      `(ns my.app (:refer-clojure :exclude map))`,
			wantSubstr: ":exclude value must be a vector",
		},
		{
			name:       "ns with refer-clojure exclude non-symbol",
			input:      `(ns my.app (:refer-clojure :exclude [map "x"]))`,
			wantSubstr: ":exclude vector must contain symbols",
		},
		{
			name:       "ns with refer-clojure refer non-vector",
			input:      `(ns my.app (:refer-clojure :refer map))`,
			wantSubstr: ":refer value must be :all or a vector",
		},
		{
			name:       "ns with refer-clojure rename non-map",
			input:      `(ns my.app (:refer-clojure :rename [map my-map]))`,
			wantSubstr: ":rename value must be a map",
		},
		{
			name:       "ns with refer-clojure rename non-symbol key",
			input:      `(ns my.app (:refer-clojure :rename {:map my-map}))`,
			wantSubstr: ":rename map keys must be symbols",
		},
		{
			name:       "ns with refer-clojure rename non-symbol value",
			input:      `(ns my.app (:refer-clojure :rename {map :my-map}))`,
			wantSubstr: ":rename map values must be symbols",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			_, err := runner.Run(tt.input)
			if err == nil {
				t.Fatalf("expected error but got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("expected error to contain %q, got: %v", tt.wantSubstr, err)
			}
		})
	}
}

func TestNsReferClojureExcludeBlocksCoreSymbolAndMacro(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSubstr string
	}{
		{
			name:       "excluded function symbol",
			input:      `(ns my.app (:refer-clojure :exclude [map])) (map inc [1 2 3])`,
			wantSubstr: "undefined variable: map",
		},
		{
			name:       "excluded macro symbol",
			input:      `(ns my.app (:refer-clojure :exclude [when])) (when true 1)`,
			wantSubstr: "undefined variable: when",
		},
		{
			name:       "excluded optimized builtin",
			input:      `(ns my.app (:refer-clojure :exclude [+])) (+ 1 2)`,
			wantSubstr: "undefined variable: +",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			_, err := runner.Run(tt.input)
			if err == nil {
				t.Fatalf("expected error but got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("expected error to contain %q, got: %v", tt.wantSubstr, err)
			}
		})
	}
}

func TestNsReferClojureRenameExcludesOriginalSymbol(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSubstr string
	}{
		{
			name:       "rename excludes original function symbol",
			input:      `(ns my.app (:refer-clojure :rename {map my-map})) (map inc [1 2 3])`,
			wantSubstr: "undefined variable: map",
		},
		{
			name:       "rename excludes original macro symbol",
			input:      `(ns my.app (:refer-clojure :rename {when my-when})) (when true 1)`,
			wantSubstr: "undefined variable: when",
		},
		{
			name:       "rename excludes original optimized builtin",
			input:      `(ns my.app (:refer-clojure :rename {+ add})) (+ 1 2)`,
			wantSubstr: "undefined variable: +",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			_, err := runner.Run(tt.input)
			if err == nil {
				t.Fatalf("expected error but got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("expected error to contain %q, got: %v", tt.wantSubstr, err)
			}
		})
	}
}

func TestNsReferClojureRenameDoesNotLeakAcrossNamespaces(t *testing.T) {
	runner := MustNewRunner()

	got, err := runner.Run(`(ns ns.one (:refer-clojure :rename {map map1})) (map1 inc [1 2 3])`)
	if err != nil {
		t.Fatalf("unexpected error in ns.one: %v", err)
	}
	if got.String() != "(2 3 4)" {
		t.Fatalf("expected mapped result in ns.one, got %s", got.String())
	}

	_, err = runner.Run(`(ns ns.two) (map1 inc [1 2 3])`)
	if err == nil {
		t.Fatalf("expected undefined variable in ns.two, got nil")
	}
	if !strings.Contains(err.Error(), "undefined variable: map1") {
		t.Fatalf("expected undefined variable error, got: %v", err)
	}
}

func TestNsReferClojureRenameFailureDoesNotMutateNamespace(t *testing.T) {
	runner := MustNewRunner()

	if _, err := runner.Run(`(ns my.app) (def map1 42)`); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	_, err := runner.Run(`(ns my.app (:refer-clojure :rename {map map1}))`)
	if err == nil {
		t.Fatalf("expected rename target conflict error, got nil")
	}
	if !strings.Contains(err.Error(), ":rename target already bound: map1") {
		t.Fatalf("unexpected rename error: %v", err)
	}

	got, err := runner.Run(`(in-ns 'my.app) (map inc [1 2 3])`)
	if err != nil {
		t.Fatalf("map should remain available after failed rename: %v", err)
	}
	if got.String() != "(2 3 4)" {
		t.Fatalf("expected mapped result, got %s", got.String())
	}
}
