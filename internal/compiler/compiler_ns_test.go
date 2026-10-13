package compiler

import (
	"os"
	"strings"
	"testing"

	"doggo/internal/compiler/sexpr"
	"doggo/internal/syntax/span"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestCompileNsStoresMergedNamespaceMetadata(t *testing.T) {
	c := New()

	if _, err := compileTestSource(t, c, `(ns ^{:private true} my.app "from-docstring" {:team "core" :doc "from-attr"})`); err != nil {
		t.Fatalf("compile error: %v", err)
	}

	meta := c.symbolTable.GetNamespaceMetadata("my.app")
	if meta == nil {
		t.Fatalf("expected namespace metadata to be stored")
	}

	if got := meta.Get(atom.NewKeyword("private")); got != atom.True {
		t.Fatalf("expected :private true, got %v", got)
	}
	if got := meta.Get(atom.NewKeyword("team")); got == nil || got.String() != `"core"` {
		t.Fatalf("expected :team \"core\", got %v", got)
	}
	if got := meta.Get(atom.NewKeyword("doc")); got == nil || got.String() != `"from-attr"` {
		t.Fatalf("expected :doc to be overridden by attr-map, got %v", got)
	}
}

func TestLiteralMetadataCompilerDoesNotUseRuntimeMetadataBridge(t *testing.T) {
	for _, fileName := range []string{"literal_metadata.go", "compiler_ns.go", "compiler_macro.go"} {
		src, err := os.ReadFile(fileName)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		if strings.Contains(text, "runtimeMetadataFromForm") {
			t.Fatalf("%s should not use runtimeMetadataFromForm", fileName)
		}
	}
}

func TestCompileNsReferClojureExcludeStoresExcludeSet(t *testing.T) {
	c := New()

	if _, err := compileTestSource(t, c, "(ns my.app (:refer-clojure :exclude [map when]))"); err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if !c.symbolTable.IsCoreSymbolExcludedInNamespace("my.app", "map") {
		t.Fatalf("expected map to be excluded in my.app")
	}
	if !c.symbolTable.IsCoreSymbolExcludedInNamespace("my.app", "when") {
		t.Fatalf("expected when to be excluded in my.app")
	}
}

func TestCompileNsReferClojureReferRemovesExclude(t *testing.T) {
	c := New()
	c.symbolTable.DefineGlobalInNamespace(CoreNamespace, "map")

	if _, err := compileTestSource(t, c, "(ns my.app (:refer-clojure :exclude [map] :refer [map]))"); err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if c.symbolTable.IsCoreSymbolExcludedInNamespace("my.app", "map") {
		t.Fatalf("expected map to be re-included in my.app")
	}
}

func TestCompileNsReferClojureRenameAddsAliasAndExcludesOriginal(t *testing.T) {
	c := New()
	source := c.symbolTable.DefineGlobalInNamespace(CoreNamespace, "map")

	if _, err := compileTestSource(t, c, "(ns my.app (:refer-clojure :rename {map my-map}))"); err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if !c.symbolTable.IsCoreSymbolExcludedInNamespace("my.app", "map") {
		t.Fatalf("expected map to be excluded in my.app")
	}

	renamed, ok := c.symbolTable.Resolve("my-map")
	if !ok {
		t.Fatalf("expected my-map to be resolvable")
	}
	if renamed.Index != source.Index {
		t.Fatalf("expected renamed symbol to reuse core index, got=%d want=%d", renamed.Index, source.Index)
	}
}

func TestCompileNsReferClojureRenamePreservesCanonicalBuiltinLayout(t *testing.T) {
	c := New()
	source := c.symbolTable.DefineBuiltin(0, "count")

	if _, err := compileTestSource(t, c, "(ns my.app (:refer-clojure :rename {count len}))"); err != nil {
		t.Fatalf("compile ns error: %v", err)
	}

	renamed, ok := c.symbolTable.Resolve("len")
	if !ok {
		t.Fatalf("expected len to be resolvable")
	}
	if renamed.Index != source.Index {
		t.Fatalf("expected len to reuse count index, got=%d want=%d", renamed.Index, source.Index)
	}

	bytecode, err := compileTestSource(t, c, "(len [1 2 3])")
	if err != nil {
		t.Fatalf("compile call error: %v", err)
	}

	layout := bytecode.BuiltinNames
	if len(layout) != 1 || layout[0] != "count" {
		t.Fatalf("expected canonical builtin layout [count], got %#v", layout)
	}
}

func TestCompileNsReferClojureRenameRejectsUnknownCoreSymbol(t *testing.T) {
	c := New()

	_, err := compileTestSource(t, c, "(ns my.app (:refer-clojure :rename {not-found x}))")
	if err == nil {
		t.Fatalf("expected compile error, got nil")
	}
	if !strings.Contains(err.Error(), ":rename symbol not found in core: not-found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileNsReferClojureRollbackOnOptionError(t *testing.T) {
	c := New()
	c.symbolTable.DefineGlobalInNamespace(CoreNamespace, "map")

	_, err := compileTestSource(t, c, "(ns my.app (:refer-clojure :exclude [map] :refer :invalid))")
	if err == nil {
		t.Fatalf("expected compile error, got nil")
	}
	if !strings.Contains(err.Error(), ":refer value must be :all or a vector") {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.symbolTable.IsCoreSymbolExcludedInNamespace("my.app", "map") {
		t.Fatalf("rollback failed: map should not stay excluded after compile error")
	}
}

func TestCompileNsImportRejectsNonStringLiteral(t *testing.T) {
	c := New()

	_, err := compileTestSource(t, c, "(ns my.app (:import 123))")
	if err == nil {
		t.Fatalf("expected compile error, got nil")
	}
	if !strings.Contains(err.Error(), "ns :import entries must be string literals") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileNsRejectsUseAndGenClassWithGuidance(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantSubstr string
	}{
		{
			name:       "use clause",
			source:     "(ns my.app (:use [some.lib]))",
			wantSubstr: ":use is not supported; use (:require [some.lib :refer :all]) or explicit :refer instead",
		},
		{
			name:       "gen-class clause",
			source:     "(ns my.app (:gen-class))",
			wantSubstr: ":gen-class is not supported; doggo does not generate JVM classes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New()
			_, err := compileTestSource(t, c, tt.source)
			if err == nil {
				t.Fatalf("expected compile error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCompileDefAddsSourceMetadataByDefault(t *testing.T) {
	c := New()
	c.SetSourceFile("src/main.dog")
	source := "   (def ^{:doc \"answer\"} answer 42)"
	c.SetSourceContentProvider(func(file string) (string, bool) {
		if file == "src/main.dog" {
			return source, true
		}
		return "", false
	})

	formMeta := sexpr.NewMap([]sexpr.MapEntry{
		{
			Key:   sexpr.NewKeyword("doc", span.Unknown(""), nil),
			Value: sexpr.NewString("answer", span.Unknown(""), nil),
		},
	}, span.Unknown(""), nil)
	formSpan := span.New("src/main.dog", 12, 4, 12, 35)
	formSpan.StartOffset = 3
	formSpan.EndOffset = len(source)
	form := sexpr.NewList(
		[]sexpr.Form{
			sexpr.NewSymbol("def", span.Unknown(""), nil),
			sexpr.NewSymbol("answer", span.Unknown(""), formMeta),
			sexpr.NewNumber("42", span.Unknown(""), nil),
		},
		formSpan,
		nil,
	)

	if err := compileTestForm(t, c, form); err != nil {
		t.Fatalf("compile error: %v", err)
	}

	meta := findDefMetadataByName(c.Bytecode().Constants, "answer")
	if meta == nil {
		t.Fatalf("expected def metadata to be emitted")
	}

	assertMapValueString(t, meta, "file", `"src/main.dog"`)
	assertMapValueString(t, meta, "line", "12")
	assertMapValueString(t, meta, "column", "4")
	assertMapValueString(t, meta, "ns", "user")
	assertMapValueString(t, meta, "name", "answer")
	assertMapValueString(t, meta, "doc", `"answer"`)
	assertMapValueString(t, meta, "source", `"(def ^{:doc "answer"} answer 42)"`)
}

func findDefMetadataByName(constants []types.Value, name string) *coll.Map {
	nameKey := atom.NewKeyword("name")
	for _, constant := range constants {
		meta, ok := constant.(*coll.Map)
		if !ok {
			continue
		}
		value := meta.Get(nameKey)
		if value == nil {
			continue
		}
		if value.String() == name {
			return meta
		}
	}
	return nil
}

func assertMapValueString(t *testing.T, m *coll.Map, key, want string) {
	t.Helper()
	value := m.Get(atom.NewKeyword(key))
	if value == nil {
		t.Fatalf("expected key :%s in metadata", key)
	}
	if value.String() != want {
		t.Fatalf("expected :%s to be %s, got %s", key, want, value.String())
	}
}
