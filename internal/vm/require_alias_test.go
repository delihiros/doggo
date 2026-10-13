package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireOptions(t *testing.T) {
	t.Run("alias", func(t *testing.T) {
		runner := newRequireOptionsRunner(t)
		if _, err := runner.Run(`(require '[mylib :as ml])`); err != nil {
			t.Fatalf("require alias: %v", err)
		}
		assertRunResult(t, runner,
			`[ml/greeting ml/value mylib/secret]`,
			`["Hello" 42 999]`)
	})
	t.Run("refer", func(t *testing.T) {
		runner := newRequireOptionsRunner(t)
		if _, err := runner.Run(`(require '[mylib :refer [greeting value]])`); err != nil {
			t.Fatalf("require refer: %v", err)
		}
		assertRunResult(t, runner,
			`[greeting value mylib/secret]`,
			`["Hello" 42 999]`)
		if _, err := runner.Run(`secret`); err == nil {
			t.Fatal("non-referred symbol was available")
		}
	})
	t.Run("refer all", func(t *testing.T) {
		runner := newRequireOptionsRunner(t)
		if _, err := runner.Run(`(require '[mylib :refer :all])`); err != nil {
			t.Fatalf("require all: %v", err)
		}
		assertRunResult(t, runner,
			`[greeting value secret]`,
			`["Hello" 42 999]`)
	})
	t.Run("rename", func(t *testing.T) {
		runner := newRequireOptionsRunner(t)
		if _, err := runner.Run(`(require '[mylib :refer [value] :rename {value renamed}])`); err != nil {
			t.Fatalf("require rename: %v", err)
		}
		assertRunResult(t, runner,
			`[renamed mylib/value]`,
			`[42 42]`)
		if _, err := runner.Run(`value`); err == nil {
			t.Fatal("original referred name remained available after rename")
		}
	})
	t.Run("alias and refer", func(t *testing.T) {
		runner := newRequireOptionsRunner(t)
		if _, err := runner.Run(`(require '[mylib :as ml :refer [greeting]])`); err != nil {
			t.Fatalf("require alias and refer: %v", err)
		}
		assertRunResult(t, runner,
			`[greeting ml/value]`,
			`["Hello" 42]`)
	})
	t.Run("ns with multiple requires", func(t *testing.T) {
		runner := newRequireOptionsRunner(t)
		if _, err := runner.Run(`(ns my.app (:require [mylib :as m] [other :as o]))`); err != nil {
			t.Fatalf("ns require: %v", err)
		}
		assertRunResult(t, runner, `(+ m/value o/value)`, `50`)
	})
}

func TestRequireProtectedTypeBindings(t *testing.T) {
	explicit := newRequireOptionsRunner(t)
	assertRunErrorContains(t, explicit, `(require '[mylib :refer [User]])`, "symbol not found")
	if _, err := explicit.Run(`(require '[mylib :refer [->User map->User]])`); err != nil {
		t.Fatalf("refer constructors: %v", err)
	}
	assertRunResult(t, explicit, `(:name (->User "Ada"))`, `"Ada"`)

	all := newRequireOptionsRunner(t)
	if _, err := all.Run(`(require '[mylib :refer :all])`); err != nil {
		t.Fatalf("refer all: %v", err)
	}
	if _, err := all.Run(`User`); err == nil {
		t.Fatal("protected type binding was referred")
	}
	assertRunResult(t, all, `(do (def User 42) [User (:name (->User "Bea"))])`, `[42 "Bea"]`)
}

func TestRequireOptionErrors(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"existing symbol", `(do (def greeting :local) (require '[mylib :refer [greeting]]))`, "already exists"},
		{"missing symbol", `(require '[mylib :refer [missing]])`, "symbol not found"},
		{"builtin alias", `(do (ns my.app (:refer-clojure :rename {map my-map})) (require '[mylib :refer [value] :rename {value my-map}]))`, "already exists"},
		{"macro alias", `(do (ns my.app (:refer-clojure :rename {when my-when})) (require '[mylib :refer [value] :rename {value my-when}]))`, "already exists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newRequireOptionsRunner(t).Run(tt.source)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func newRequireOptionsRunner(t *testing.T) *Runner {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"mylib.dog": `(ns mylib)
(def greeting "Hello")
(def value 42)
(def secret 999)
(defrecord User [name])`,
		"other.dog": `(ns other) (def value 8)`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return mustNewRunnerWithLoadPaths(t, dir)
}
