package cli

import (
	"testing"

	"doggo/internal/project"
)

func TestNewStandardVMRunnerUsesCLIHostSurface(t *testing.T) {
	runner, err := newStandardVMRunner(project.RuntimeConfig{ProjectRoot: t.TempDir()}, true)
	if err != nil {
		t.Fatalf("newStandardVMRunner: %v", err)
	}

	got, err := runner.Run("(count [1 2 3])")
	if err != nil {
		t.Fatalf("count run: %v", err)
	}
	if got.String() != "3" {
		t.Fatalf("count result = %s, want 3", got.String())
	}

	if got, err := runner.Run(`(do (println "hi") 7)`); err != nil || got.String() != "7" {
		t.Fatalf("println should be available on the CLI host surface: got=%v err=%v", got, err)
	}

	if _, err := runner.Run(`(require '[doggo.io])`); err != nil {
		t.Fatalf("doggo.io should load on the CLI host surface: %v", err)
	}
	if got, err := runner.Run(`(doggo.io/basename "a/b.txt")`); err != nil || got.String() != `"b.txt"` {
		t.Fatalf("doggo.io should load on the CLI host surface: got=%v err=%v", got, err)
	}

	if _, err := runner.Run(`(io-file* "sample.txt")`); err == nil {
		t.Fatal("private host builtin io-file* should not be directly visible to user code")
	}
}

func TestNewStandardVMRunnerUsesGenerationSources(t *testing.T) {
	_, script := writeGenerationSourceCLIProject(t)
	session, err := project.OpenGenerationSession(script)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	runner, err := newStandardVMRunner(session.Config, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(`(require '[dep.core :as dep])`); err != nil {
		t.Fatalf("require generation dependency: %v", err)
	}
	got, err := runner.Run(`dep/value`)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "42" {
		t.Fatalf("dependency value = %s, want 42", got.String())
	}
}
