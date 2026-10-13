package vm

import (
	"os"
	"path/filepath"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestRunnerNamespaceServicesRequireValueAppliesAliasReferAndRename(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "mylib.dog"), []byte(`
		(ns mylib)
		(def greeting "Hello")
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create mylib.dog: %v", err)
	}

	runner := mustNewRunnerWithLoadPaths(t, tmpDir)

	spec := coll.NewVector(
		types.Symbol("mylib"),
		atom.NewKeyword("as"), types.Symbol("ml"),
		atom.NewKeyword("refer"), coll.NewVector(types.Symbol("greeting")),
		atom.NewKeyword("rename"), coll.NewMapFromPairs([]types.Value{
			types.Symbol("greeting"), types.Symbol("hi"),
		}),
	)

	if err := runner.runtimeServices.namespaces.RequireValue(spec); err != nil {
		t.Fatalf("RequireValue failed: %v", err)
	}

	aliased, err := runner.Run(`ml/greeting`)
	if err != nil {
		t.Fatalf("alias access failed: %v", err)
	}
	if got := aliased.String(); got != `"Hello"` {
		t.Fatalf("alias access = %s, want %q", got, `"Hello"`)
	}

	referred, err := runner.Run(`hi`)
	if err != nil {
		t.Fatalf("renamed refer access failed: %v", err)
	}
	if got := referred.String(); got != `"Hello"` {
		t.Fatalf("renamed refer access = %s, want %q", got, `"Hello"`)
	}
}

func TestRunnerNamespaceServicesRequireValueAppliesMacroRefers(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "macrolib.dog"), []byte(`
		(ns macrolib)
		(defmacro add-ten [x]
		  (list '+ x 10))
	`), 0644)
	if err != nil {
		t.Fatalf("failed to create macrolib.dog: %v", err)
	}

	runner := mustNewRunnerWithLoadPaths(t, tmpDir)

	spec := coll.NewVector(
		types.Symbol("macrolib"),
		atom.NewKeyword("refer"), coll.NewVector(types.Symbol("add-ten")),
	)

	if err := runner.runtimeServices.namespaces.RequireValue(spec); err != nil {
		t.Fatalf("RequireValue failed: %v", err)
	}

	value, err := runner.Run(`(add-ten 5)`)
	if err != nil {
		t.Fatalf("macro refer call failed: %v", err)
	}
	if got := value.String(); got != "15" {
		t.Fatalf("macro refer result = %s, want 15", got)
	}
}
