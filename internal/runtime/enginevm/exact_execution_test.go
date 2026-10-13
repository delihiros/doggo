package enginevm

import (
	"os"
	"path/filepath"
	"testing"

	"doggo/internal/project"
	"doggo/internal/runtime/composition"
	"doggo/internal/runtime/profile"
	"doggo/internal/testutil"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestNewInitializedExactExecutionOwnsGraphHandshake(t *testing.T) {
	projectRoot := t.TempDir()
	engineCfg := composition.StandardEngineConfig(composition.StandardConfig{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{projectRoot},
		BootstrapCore: false,
		Profile:       profile.Default(),
	})
	exact, err := ResolveExactConfig(Config{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{projectRoot},
		BootstrapCore: false,
		Profile:       profile.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}

	initialized, err := NewInitializedExactExecution(NewGraphHandles(engineCfg), exact)
	if err != nil {
		t.Fatal(err)
	}
	if initialized.Execution == nil || initialized.Runner == nil {
		t.Fatal("expected initialized execution and vmRunner runner")
	}

	value, err := initialized.Runner.Run("load-file")
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != "#<builtin:load-file>" {
		t.Fatalf("load-file result = %s", got)
	}
}

func TestNewExactExecutionSnapshotsExactProfileForDeferredInitialization(t *testing.T) {
	projectRoot := t.TempDir()
	exact, err := ResolveExactConfig(Config{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{projectRoot},
		BootstrapCore: false,
		Profile:       profile.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}

	exec, err := newExactRuntimeExecution(NewGraphHandles(exact.Engine), exact)
	if err != nil {
		t.Fatal(err)
	}

	exact.ExactProfile = exact.ExactProfile.SetModules(nil).SetFeatures(nil)

	if err := InitializeRuntimeExecution(exec); err != nil {
		t.Fatal(err)
	}
	value, err := RequireVMRunner(exec)
	if err != nil {
		t.Fatal(err)
	}
	got, err := value.Run("load-file")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "#<builtin:load-file>" {
		t.Fatalf("load-file result = %s", got.String())
	}
}

func TestNewExactExecutionRebuildsRunnerBackedGraphHandlesFromConfiguredGraph(t *testing.T) {
	projectRoot := writeExactRuntimeBasisTestProject(t)
	dependencyPath := filepath.Join(projectRoot, "deps", "dep-one")
	engineCfg := composition.StandardEngineConfig(composition.StandardConfig{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{projectRoot, dependencyPath},
		BootstrapCore: false,
		Profile:       profile.Default(),
	})
	graph := NewGraphHandles(engineCfg)
	exact, err := ResolveExactConfig(Config{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{projectRoot, dependencyPath},
		BootstrapCore: false,
		Profile:       profile.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}

	exec, err := newExactRuntimeExecution(graph, exact)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeRuntimeExecution(exec); err != nil {
		t.Fatal(err)
	}

	currentBasis, err := exec.Basis().Current()
	if err != nil {
		t.Fatal(err)
	}
	wantLoadPaths := []string{
		filepath.ToSlash(projectRoot),
		filepath.ToSlash(dependencyPath),
	}
	if got := exactBasisLoadPaths(t, currentBasis); len(got) != len(wantLoadPaths) || got[0] != wantLoadPaths[0] || got[1] != wantLoadPaths[1] {
		t.Fatalf("basis load paths = %v, want %v", got, wantLoadPaths)
	}

	if _, err := exec.EvalString(`(require '[dep-one.core :as dep])`); err != nil {
		t.Fatalf("execution did not observe configured load paths: %v", err)
	}
	got, err := exec.EvalString(`dep-one.core/answer`)
	if err != nil {
		t.Fatalf("dependency namespace should stay loaded after require: %v", err)
	}
	if got.String() != "42" {
		t.Fatalf("unexpected dependency answer: %s", got.String())
	}
}

func TestNewExactExecutionExposesRunnerBackedSourceRegistry(t *testing.T) {
	projectRoot := t.TempDir()
	engineCfg := composition.StandardEngineConfig(composition.StandardConfig{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{projectRoot},
		BootstrapCore: false,
		Profile:       profile.Default(),
	})
	exact, err := ResolveExactConfig(Config{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{projectRoot},
		BootstrapCore: false,
		Profile:       profile.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}

	exec, err := newExactRuntimeExecution(NewGraphHandles(engineCfg), exact)
	if err != nil {
		t.Fatal(err)
	}

	const src = "(+ 1 2)"
	got, err := exec.EvalString(src)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "3" {
		t.Fatalf("unexpected eval result: %s", got.String())
	}

	content, ok := exec.Sources().LookupContent("<repl>#1")
	if !ok {
		t.Fatal("expected exact execution sources to expose runner-registered repl source")
	}
	if content != src {
		t.Fatalf("source content = %q, want %q", content, src)
	}
}

func writeExactRuntimeBasisTestProject(t *testing.T) string {
	t.Helper()

	projectRoot := testutil.CanonicalTempDir(t)
	mod := &project.ModFile{
		Module: "github.com/acme/runtime-graph-demo",
		Deps:   []string{"pkg:acme/dep-one@1.2.3"},
	}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod file: %v", err)
	}

	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     "pkg:acme/dep-one@1.2.3",
				Kind:       "git",
				Revision:   "v1.2.3",
				Digest:     "sha256:abc123",
				InstallDir: "deps/dep-one",
			},
		},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	depDir := filepath.Join(projectRoot, "deps", "dep-one", "dep-one")
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatalf("mkdir dep dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "core.dog"), []byte("(ns dep-one.core)\n(def answer 42)\n"), 0o644); err != nil {
		t.Fatalf("write dependency source: %v", err)
	}

	return projectRoot
}

func exactBasisLoadPaths(t *testing.T, value types.Value) []string {
	t.Helper()

	basisMap, ok := value.(*coll.Map)
	if !ok {
		t.Fatalf("basis value type = %T, want *coll.Map", value)
	}
	loadPathsValue := basisMap.Get(atom.NewKeyword("load-paths"))
	if loadPathsValue == nil {
		t.Fatal("basis missing :load-paths")
	}
	loadPathsVector, ok := loadPathsValue.(*coll.Vector)
	if !ok {
		t.Fatalf(":load-paths type = %T, want *coll.Vector", loadPathsValue)
	}
	loadPaths := make([]string, 0, len(loadPathsVector.Items))
	for _, item := range loadPathsVector.Items {
		str, ok := item.(atom.String)
		if !ok {
			t.Fatalf("load-path item type = %T, want atom.String", item)
		}
		loadPaths = append(loadPaths, string(str))
	}
	return loadPaths
}
