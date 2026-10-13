package vm

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chzyer/readline"
)

func TestRunREPLWithRunnerReturnsReadlineInitializationError(t *testing.T) {
	oldNewReadline := replNewReadline
	t.Cleanup(func() { replNewReadline = oldNewReadline })
	replNewReadline = func(*readline.Config) (*readline.Instance, error) {
		return nil, errors.New("readline unavailable")
	}

	err := RunREPLWithRunner(MustNewRunner())
	if err == nil || !strings.Contains(err.Error(), "initialize readline: readline unavailable") {
		t.Fatalf("RunREPLWithRunner() error = %v", err)
	}
}

func TestResolveREPLHistoryPathPrefersEnvironmentVariable(t *testing.T) {
	t.Setenv("DOGGO_REPL_HISTORY", "")

	restore := withREPLPathHooks()
	defer restore()

	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "custom", "history")

	replGetenv = func(key string) string {
		if key == "DOGGO_REPL_HISTORY" {
			return customPath
		}
		return ""
	}
	replUserHomeDir = func() (string, error) {
		return filepath.Join(tmpDir, "home"), nil
	}
	replTempDir = func() string { return filepath.Join(tmpDir, "tmp") }

	var warn bytes.Buffer
	got := resolveREPLHistoryPath(&warn)
	if got != customPath {
		t.Fatalf("expected %s, got %s", customPath, got)
	}
	if warn.Len() != 0 {
		t.Fatalf("expected no warning, got %s", warn.String())
	}
}

func TestResolveREPLHistoryPathUsesHomeByDefault(t *testing.T) {
	t.Setenv("DOGGO_REPL_HISTORY", "")

	restore := withREPLPathHooks()
	defer restore()

	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")

	replGetenv = func(key string) string { return "" }
	replUserHomeDir = func() (string, error) { return homeDir, nil }
	replTempDir = func() string { return filepath.Join(tmpDir, "tmp") }

	var warn bytes.Buffer
	got := resolveREPLHistoryPath(&warn)
	want := filepath.Join(homeDir, ".doggo", "repl_history")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
	if warn.Len() != 0 {
		t.Fatalf("expected no warning, got %s", warn.String())
	}
}

func TestResolveREPLHistoryPathFallsBackToTempWhenHomeUnavailable(t *testing.T) {
	t.Setenv("DOGGO_REPL_HISTORY", "")

	restore := withREPLPathHooks()
	defer restore()

	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "tmp")

	replGetenv = func(key string) string { return "" }
	replUserHomeDir = func() (string, error) { return "", errors.New("home unavailable") }
	replTempDir = func() string { return tempDir }

	var warn bytes.Buffer
	got := resolveREPLHistoryPath(&warn)
	want := filepath.Join(tempDir, "doggo-repl-history")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
	if warn.Len() == 0 {
		t.Fatalf("expected warning output when home directory is unavailable")
	}
}

func TestCompletionCandidatesIncludeCurrentCoreAndBuiltinSymbols(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(ns my.app) (def my-fn 1)`); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	myCandidates := runner.CompletionCandidates("my-")
	if !slices.Contains(myCandidates, "my-fn") {
		t.Fatalf("expected current namespace symbol in completion candidates, got %v", myCandidates)
	}

	coreCandidates := runner.CompletionCandidates("ma")
	if !slices.Contains(coreCandidates, "map") {
		t.Fatalf("expected core symbol in completion candidates, got %v", coreCandidates)
	}

	builtinCandidates := runner.CompletionCandidates("slu")
	if !slices.Contains(builtinCandidates, "slurp") {
		t.Fatalf("expected builtin symbol in completion candidates, got %v", builtinCandidates)
	}
}

func TestCompletionCandidatesRespectsReferClojureExclude(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(ns my.app (:refer-clojure :exclude [map]))`); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	candidates := runner.CompletionCandidates("map")
	if slices.Contains(candidates, "map") {
		t.Fatalf("expected excluded symbol map to be absent, got %v", candidates)
	}
}

func TestCompletionCandidatesIncludeReferredVarsAndMacros(t *testing.T) {
	tmpDir := t.TempDir()

	libSource := `(ns mylib)
(def greeting "hi")
(defmacro greet-macro [] "macro")`
	if err := os.WriteFile(filepath.Join(tmpDir, "mylib.dog"), []byte(libSource), 0644); err != nil {
		t.Fatalf("failed to create mylib.dog: %v", err)
	}

	runner := mustNewRunnerWithLoadPaths(t, tmpDir)

	if _, err := runner.Run(`(ns my.app) (require '[mylib :refer [greeting greet-macro]])`); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	candidates := runner.CompletionCandidates("gr")
	if !slices.Contains(candidates, "greeting") {
		t.Fatalf("expected referred var in completion candidates, got %v", candidates)
	}
	if !slices.Contains(candidates, "greet-macro") {
		t.Fatalf("expected referred macro in completion candidates, got %v", candidates)
	}
}

func TestCompletionCandidatesIncludePrivateMacrosInCurrentNamespace(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(ns my.app) (defmacro ^:private hidden-macro [] 1)`); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	candidates := runner.CompletionCandidates("hidden-")
	if !slices.Contains(candidates, "hidden-macro") {
		t.Fatalf("expected private macro in current namespace completion candidates, got %v", candidates)
	}
}

func TestCompletionCandidatesKeepLocalAndReferredBindingsWhenCoreExcluded(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(ns my.app (:refer-clojure :exclude [map])) (def map 1)`); err != nil {
		t.Fatalf("local setup failed: %v", err)
	}

	localCandidates := runner.CompletionCandidates("map")
	if !slices.Contains(localCandidates, "map") {
		t.Fatalf("expected local map binding in completion candidates, got %v", localCandidates)
	}

	tmpDir := t.TempDir()
	libSource := `(ns mylib)
(def map 1)`
	if err := os.WriteFile(filepath.Join(tmpDir, "mylib.dog"), []byte(libSource), 0644); err != nil {
		t.Fatalf("failed to create mylib.dog: %v", err)
	}

	runner = mustNewRunnerWithLoadPaths(t, tmpDir)
	if _, err := runner.Run(`(ns my.app (:refer-clojure :exclude [map])) (require '[mylib :refer [map]])`); err != nil {
		t.Fatalf("refer setup failed: %v", err)
	}

	referredCandidates := runner.CompletionCandidates("map")
	if !slices.Contains(referredCandidates, "map") {
		t.Fatalf("expected referred map binding in completion candidates, got %v", referredCandidates)
	}
}

func TestCompletionCandidatesIncludeRenamedCoreAliases(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(ns my.app (:refer-clojure :rename {map my-map}))`); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	candidates := runner.CompletionCandidates("my-")
	if !slices.Contains(candidates, "my-map") {
		t.Fatalf("expected renamed core alias in completion candidates, got %v", candidates)
	}
}

func TestMathInternalBuiltinsAreNotPublicSurface(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`(require '[doggo.math :as math] '[doggo.repl :as repl])`); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	assertRunErrorContains(t, runner, `(math-sqrt* 4)`, "undefined variable: math-sqrt*")
	assertRunErrorContains(t, runner, `(doggo.core/math-sqrt* 4)`, "undefined variable: doggo.core/math-sqrt*")

	result, err := runner.Run(`(math/sqrt 4)`)
	if err != nil {
		t.Fatalf("math namespace should still work: %v", err)
	}
	if result.String() != "2" {
		t.Fatalf("expected math/sqrt to return 2, got %s", result.String())
	}

	dirResult, err := runner.Run(`(if (some (fn [sym] (= sym 'math-sqrt*)) (repl/dir 'doggo.core)) true false)`)
	if err != nil {
		t.Fatalf("dir check failed: %v", err)
	}
	if dirResult.String() != "false" {
		t.Fatalf("expected math-sqrt* to stay out of doggo.core dir, got %s", dirResult.String())
	}

	candidates := runner.CompletionCandidates("math-")
	if slices.Contains(candidates, "math-sqrt*") {
		t.Fatalf("expected math-sqrt* to stay out of completion candidates, got %v", candidates)
	}
}

func TestLoadREPLNamespaceMakesDocAndSourceAvailable(t *testing.T) {
	runner := MustNewRunner()

	if _, err := runner.Run(`(doc map)`); err == nil {
		t.Fatalf("expected doc to be unavailable before REPL preload")
	}

	if err := loadREPLNamespace(runner); err != nil {
		t.Fatalf("loadREPLNamespace failed: %v", err)
	}

	if _, err := runner.Run(`(doc map)`); err != nil {
		t.Fatalf("expected doc to be available after REPL preload: %v", err)
	}
	if _, err := runner.Run(`(source map)`); err != nil {
		t.Fatalf("expected source to be available after REPL preload: %v", err)
	}
}

func withREPLPathHooks() func() {
	oldGetenv := replGetenv
	oldUserHomeDir := replUserHomeDir
	oldTempDir := replTempDir
	oldMkdirAll := replMkdirAll
	return func() {
		replGetenv = oldGetenv
		replUserHomeDir = oldUserHomeDir
		replTempDir = oldTempDir
		replMkdirAll = oldMkdirAll
	}
}
