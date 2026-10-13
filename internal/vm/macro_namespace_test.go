package vm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMacroNamespaceResolution(t *testing.T) {
	runner := newMacroNamespaceRunner(t)
	if _, err := runner.Run(`(require '[macrolib :as m :refer [original] :rename {original renamed}])`); err != nil {
		t.Fatalf("require macrolib: %v", err)
	}
	assertRunResult(t, runner,
		`[(macrolib/add-one 1) (m/times-two 3) (renamed 4) (macroexpand '(m/times-two 3))]`,
		`[2 6 104 (* 3 2)]`)
	for _, source := range []string{`(times-two 3)`, `(original 4)`} {
		if _, err := runner.Run(source); err == nil {
			t.Fatalf("run %s succeeded without refer", source)
		}
	}

	all := newMacroNamespaceRunner(t)
	if _, err := all.Run(`(require '[macrolib :refer :all])`); err != nil {
		t.Fatalf("require all macrolib macros: %v", err)
	}
	assertRunResult(t, all,
		`[(add-one 1) (times-two 3) (original 4)]`,
		`[2 6 104]`)
}

func TestPrivateMacroNamespaceBoundary(t *testing.T) {
	runner := newMacroNamespaceRunner(t)
	if _, err := runner.Run(`(do (require 'macrolib '[doggo.repl :as repl]) (ns app))`); err != nil {
		t.Fatalf("require macrolib: %v", err)
	}

	assertRunResult(t, runner, `(macrolib/call-hidden 5)`, `6`)
	for _, source := range []string{
		`(macrolib/hidden 5)`,
		`(require '[macrolib :refer [hidden]])`,
	} {
		if _, err := runner.Run(source); err == nil {
			t.Fatalf("private macro escaped through %s", source)
		}
	}
	all := newMacroNamespaceRunner(t)
	if _, err := all.Run(`(require '[macrolib :refer :all])`); err != nil {
		t.Fatalf("refer all: %v", err)
	}
	if _, err := all.Run(`(hidden 5)`); err == nil {
		t.Fatal("private macro escaped through refer all")
	}
	assertRunResult(t, runner,
		`(nil? (some (fn [name] (= name 'hidden)) (repl/dir 'macrolib)))`,
		`true`)
}

func newMacroNamespaceRunner(t *testing.T) *Runner {
	t.Helper()
	dir := t.TempDir()
	source := `(ns macrolib)
(defmacro add-one [x] (list '+ x 1))
(defmacro times-two [x] (list '* x 2))
(defmacro original [x] (list '+ x 100))
(defmacro ^:private hidden [x] (list '+ x 1))
(defn call-hidden [x] (hidden x))`
	if err := os.WriteFile(filepath.Join(dir, "macrolib.dog"), []byte(source), 0o644); err != nil {
		t.Fatalf("write macrolib: %v", err)
	}
	return mustNewRunnerWithLoadPaths(t, dir)
}
