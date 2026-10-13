package vm

import (
	"os"
	"path/filepath"
	"testing"

	"doggo/internal/vm/engine"
)

func TestCLINamespaceTracksRunnerCommandLineArgs(t *testing.T) {
	runner := MustNewRunner()
	runner.SetCommandLineArgs([]string{"alpha", "beta"})
	if _, err := runner.Run(`(require '[doggo.cli :as cli])`); err != nil {
		t.Fatalf("require doggo.cli: %v", err)
	}

	checkArgs := func(want string) {
		t.Helper()
		value, err := runner.Run(`[cli/*command-line-args*
			(cli/command-line-args)
			(nil? cli/*command-line-args*)
			(cli/main (fn [& args] (vec args)))]`)
		if err != nil {
			t.Fatal(err)
		}
		if got := value.String(); got != want {
			t.Fatalf("args = %s, want %s", got, want)
		}
	}

	checkArgs(`[["alpha" "beta"] ["alpha" "beta"] false ["alpha" "beta"]]`)
	runner.SetCommandLineArgs([]string{})
	checkArgs(`[[] [] false []]`)
}

func TestCLINamespaceEntrypointPredicateUsesRunnerEntryPoint(t *testing.T) {
	runner := MustNewRunner()
	runner.SetEntryPointFile("src/app/main.dog")

	value, err := runner.RunWithOrigin(`
		(require '[doggo.cli :as cli])
		(cli/entrypoint?)
	`, engine.SourceOrigin{File: "src/app/main.dog"})
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	if got, want := value.String(), `true`; got != want {
		t.Fatalf("unexpected result: want %s, got %s", want, got)
	}
}

func TestCLINamespaceEntrypointPredicateRejectsRequiredFile(t *testing.T) {
	runner := MustNewRunner()
	runner.SetEntryPointFile("src/app/main.dog")

	value, err := runner.RunWithOrigin(`
		(require '[doggo.cli :as cli])
		(cli/entrypoint?)
	`, engine.SourceOrigin{File: "src/app/lib.dog"})
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	if got, want := value.String(), `false`; got != want {
		t.Fatalf("unexpected result: want %s, got %s", want, got)
	}
}

func TestCLINamespaceEntrypointPredicateRejectsRequiredHelperCall(t *testing.T) {
	projectRoot := t.TempDir()
	helperPath := filepath.Join(projectRoot, "helper.dog")
	if err := os.WriteFile(helperPath, []byte(`(ns helper
  (:require [doggo.cli :as cli]))

(def check-entrypoint
  (fn []
    (cli/entrypoint?)))
`), 0644); err != nil {
		t.Fatalf("write helper: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{ProjectRoot: projectRoot, BootstrapCore: true})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	entryPath := filepath.Join(projectRoot, "entry.dog")
	runner.SetEntryPointFile(entryPath)

	value, err := runner.RunWithOrigin(`
		(require '[helper :as helper])
		(helper/check-entrypoint)
	`, engine.SourceOrigin{File: filepath.ToSlash(entryPath)})
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	if got, want := value.String(), `false`; got != want {
		t.Fatalf("unexpected result: want %s, got %s", want, got)
	}
}
