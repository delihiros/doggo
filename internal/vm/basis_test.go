package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/testutil"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestBasisNamespaceExposesCurrentAndInitialSnapshots(t *testing.T) {
	projectRoot := writeBasisTestProject(t)

	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:       projectRoot,
		LoadPaths:         []string{projectRoot},
		GenerationSources: []*project.GenerationSource{{}},
		Lock:              loadBasisTestLock(t, projectRoot),
		BootstrapCore:     true,
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	if _, err := runner.Run(`(require '[doggo.basis :as basis])`); err != nil {
		t.Fatalf("require doggo.basis failed: %v", err)
	}

	value, err := runner.Run(`[(basis/current-basis) (basis/initial-basis)]`)
	if err != nil {
		t.Fatalf("basis script failed: %v", err)
	}

	vector, ok := value.(*coll.Vector)
	if !ok || len(vector.Items) != 2 {
		t.Fatalf("expected [current initial] vector, got %T %v", value, value)
	}

	current := mustMap(t, vector.Items[0])
	initial := mustMap(t, vector.Items[1])

	assertBasisSnapshot(t, current, filepath.ToSlash(projectRoot), []string{filepath.ToSlash(projectRoot)})
	assertBasisSnapshot(t, initial, filepath.ToSlash(projectRoot), []string{filepath.ToSlash(projectRoot)})
}

func TestBasisNamespaceUsesRuntimeLockSnapshotWithoutReopeningLock(t *testing.T) {
	projectRoot := writeBasisTestProject(t)

	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{projectRoot},
		Lock:          loadBasisTestLock(t, projectRoot),
		BootstrapCore: true,
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	if _, err := runner.Run(`(require '[doggo.basis :as basis])`); err != nil {
		t.Fatalf("require doggo.basis failed: %v", err)
	}

	updatedMod := &project.ModFile{
		Module: "github.com/acme/changed",
		Deps:   []string{"pkg:acme/changed@9.9.9"},
	}
	if err := project.WriteModFile(project.ModPath(projectRoot), updatedMod); err != nil {
		t.Fatalf("rewrite mod file: %v", err)
	}

	updatedLock := &project.LockFile{
		LockVersion:         project.LockVersion,
		Generation:          strings.Repeat("b", 64),
		ModLockInputsSHA256: project.HashModLockInputs(updatedMod),
		Deps: []project.LockDep{
			{
				Source:     "pkg:acme/changed@9.9.9",
				Kind:       "git",
				ModuleID:   "go:example.com/acme/changed",
				GoModule:   "example.com/acme/changed",
				Revision:   "v9.9.9",
				Digest:     "sha256:changed",
				InstallDir: "deps/acme-changed",
			},
		},
		Scan: project.LockScan{
			GoPackages: []string{"net/http"},
			ReifyTargets: []project.ReifyTarget{{
				Source: "io.Reader", Runtime: "io.Reader",
			}},
		},
		Build: project.LockBuild{
			Binary: project.GenerationBinaryPath(strings.Repeat("b", 64)),
			SHA256: "beadfeed",
		},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), updatedLock); err != nil {
		t.Fatalf("rewrite lock file: %v", err)
	}
	if err := os.Remove(project.LockPath(projectRoot)); err != nil {
		t.Fatalf("remove lock file: %v", err)
	}

	value, err := runner.Run(`[(basis/current-basis) (basis/initial-basis)]`)
	if err != nil {
		t.Fatalf("basis script failed: %v", err)
	}

	vector, ok := value.(*coll.Vector)
	if !ok || len(vector.Items) != 2 {
		t.Fatalf("expected [current initial] vector, got %T %v", value, value)
	}

	current := mustMap(t, vector.Items[0])
	initial := mustMap(t, vector.Items[1])

	currentModule, ok := current.Get(atom.NewKeyword("module-name")).(atom.String)
	if !ok || string(currentModule) != "github.com/acme/changed" {
		t.Fatalf("unexpected current module-name: %T %v", current.Get(atom.NewKeyword("module-name")), current.Get(atom.NewKeyword("module-name")))
	}
	initialModule, ok := initial.Get(atom.NewKeyword("module-name")).(atom.String)
	if !ok || string(initialModule) != "github.com/acme/basis-demo" {
		t.Fatalf("unexpected initial module-name: %T %v", initial.Get(atom.NewKeyword("module-name")), initial.Get(atom.NewKeyword("module-name")))
	}

	currentDeps := mustVectorStrings(t, current.Get(atom.NewKeyword("deps")))
	assertStringSliceEqual(t, currentDeps, []string{"pkg:acme/changed@9.9.9"})
	initialDeps := mustVectorStrings(t, initial.Get(atom.NewKeyword("deps")))
	assertStringSliceEqual(t, initialDeps, []string{"pkg:acme/math@1.2.3"})

	currentTargets := mustMap(t, current.Get(atom.NewKeyword("generated-targets")))
	currentBuild := mustMap(t, currentTargets.Get(atom.NewKeyword("build")))
	currentBinary, ok := currentBuild.Get(atom.NewKeyword("binary")).(atom.String)
	if !ok || string(currentBinary) != project.GenerationBinaryPath(strings.Repeat("a", 64)) {
		t.Fatalf("unexpected current build binary: %T %v", currentBuild.Get(atom.NewKeyword("binary")), currentBuild.Get(atom.NewKeyword("binary")))
	}

	initialTargets := mustMap(t, initial.Get(atom.NewKeyword("generated-targets")))
	initialBuild := mustMap(t, initialTargets.Get(atom.NewKeyword("build")))
	initialBinary, ok := initialBuild.Get(atom.NewKeyword("binary")).(atom.String)
	if !ok || string(initialBinary) != project.GenerationBinaryPath(strings.Repeat("a", 64)) {
		t.Fatalf("unexpected initial build binary: %T %v", initialBuild.Get(atom.NewKeyword("binary")), initialBuild.Get(atom.NewKeyword("binary")))
	}
}

func TestBasisNamespaceInitialBasisPreservesStartupErrorWhileCurrentBasisCanRecover(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(project.ModPath(projectRoot), []byte("{:module "), 0644); err != nil {
		t.Fatalf("write invalid mod file: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot:   projectRoot,
		LoadPaths:     []string{projectRoot},
		BootstrapCore: true,
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	if _, err := runner.Run(`(require '[doggo.basis :as basis])`); err != nil {
		t.Fatalf("require doggo.basis failed: %v", err)
	}

	if _, err := runner.Run(`(basis/initial-basis)`); err == nil {
		t.Fatal("expected initial-basis to preserve startup mod parse error")
	}

	fixedMod := &project.ModFile{
		Module: "github.com/acme/recovered",
		Deps:   []string{"pkg:acme/recovered@1.0.0"},
	}
	if err := project.WriteModFile(project.ModPath(projectRoot), fixedMod); err != nil {
		t.Fatalf("rewrite mod file: %v", err)
	}

	current, err := runner.Run(`(basis/current-basis)`)
	if err != nil {
		t.Fatalf("expected current-basis to recover after mod fix: %v", err)
	}

	currentMap := mustMap(t, current)
	currentModule, ok := currentMap.Get(atom.NewKeyword("module-name")).(atom.String)
	if !ok || string(currentModule) != "github.com/acme/recovered" {
		t.Fatalf("unexpected recovered module-name: %T %v", currentMap.Get(atom.NewKeyword("module-name")), currentMap.Get(atom.NewKeyword("module-name")))
	}

	if _, err := runner.Run(`(basis/initial-basis)`); err == nil {
		t.Fatal("expected initial-basis to keep startup error after recovery")
	}
}

func writeBasisTestProject(t *testing.T) string {
	t.Helper()

	projectRoot := testutil.CanonicalTempDir(t)
	mod := &project.ModFile{
		Module: "github.com/acme/basis-demo",
		Deps:   []string{"pkg:acme/math@1.2.3"},
	}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod file: %v", err)
	}

	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		Generation:          strings.Repeat("a", 64),
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     "pkg:acme/math@1.2.3",
				Kind:       "git",
				ModuleID:   "go:example.com/acme/math",
				GoModule:   "example.com/acme/math",
				Revision:   "v1.2.3",
				Digest:     "sha256:abc123",
				InstallDir: "deps/acme-math",
			},
		},
		Scan: project.LockScan{
			GoPackages: []string{"encoding/json", "fmt"},
			ReifyTargets: []project.ReifyTarget{{
				Source: "fmt.Stringer", Runtime: "fmt.Stringer",
			}},
		},
		Build: project.LockBuild{
			Binary: project.GenerationBinaryPath(strings.Repeat("a", 64)),
			SHA256: "deadbeef",
		},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	return projectRoot
}

func loadBasisTestLock(t *testing.T, projectRoot string) *project.LockFile {
	t.Helper()
	lock, err := project.LoadLockFile(project.LockPath(projectRoot))
	if err != nil {
		t.Fatal(err)
	}
	return lock
}

func assertBasisSnapshot(t *testing.T, snapshot *coll.Map, wantProjectRoot string, wantLoadPaths []string) {
	t.Helper()

	projectRoot, ok := snapshot.Get(atom.NewKeyword("project-root")).(atom.String)
	if !ok || string(projectRoot) != wantProjectRoot {
		t.Fatalf("unexpected project-root: %T %v", snapshot.Get(atom.NewKeyword("project-root")), snapshot.Get(atom.NewKeyword("project-root")))
	}

	moduleName, ok := snapshot.Get(atom.NewKeyword("module-name")).(atom.String)
	if !ok || string(moduleName) != "github.com/acme/basis-demo" {
		t.Fatalf("unexpected module-name: %T %v", snapshot.Get(atom.NewKeyword("module-name")), snapshot.Get(atom.NewKeyword("module-name")))
	}

	deps := mustVectorStrings(t, snapshot.Get(atom.NewKeyword("deps")))
	assertStringSliceEqual(t, deps, []string{"pkg:acme/math@1.2.3"})

	lockDepsValue := snapshot.Get(atom.NewKeyword("lock-deps"))
	lockDeps, ok := lockDepsValue.(*coll.Vector)
	if !ok || len(lockDeps.Items) != 1 {
		t.Fatalf("unexpected lock-deps: %T %v", lockDepsValue, lockDepsValue)
	}
	lockDep := mustMap(t, lockDeps.Items[0])
	source, ok := lockDep.Get(atom.NewKeyword("source")).(atom.String)
	if !ok || string(source) != "pkg:acme/math@1.2.3" {
		t.Fatalf("unexpected lock dep source: %T %v", lockDep.Get(atom.NewKeyword("source")), lockDep.Get(atom.NewKeyword("source")))
	}
	kind, ok := lockDep.Get(atom.NewKeyword("kind")).(atom.Keyword)
	if !ok || string(kind) != "git" {
		t.Fatalf("unexpected lock dep kind: %T %v", lockDep.Get(atom.NewKeyword("kind")), lockDep.Get(atom.NewKeyword("kind")))
	}
	moduleID, ok := lockDep.Get(atom.NewKeyword("module-id")).(atom.String)
	if !ok || string(moduleID) != "go:example.com/acme/math" {
		t.Fatalf("unexpected lock dep module-id: %T %v", lockDep.Get(atom.NewKeyword("module-id")), lockDep.Get(atom.NewKeyword("module-id")))
	}
	goModule, ok := lockDep.Get(atom.NewKeyword("go-module")).(atom.String)
	if !ok || string(goModule) != "example.com/acme/math" {
		t.Fatalf("unexpected lock dep go-module: %T %v", lockDep.Get(atom.NewKeyword("go-module")), lockDep.Get(atom.NewKeyword("go-module")))
	}
	revision, ok := lockDep.Get(atom.NewKeyword("revision")).(atom.String)
	if !ok || string(revision) != "v1.2.3" {
		t.Fatalf("unexpected lock dep revision: %T %v", lockDep.Get(atom.NewKeyword("revision")), lockDep.Get(atom.NewKeyword("revision")))
	}
	if removed := lockDep.Get(atom.NewKeyword("resolved-ref")); removed != nil {
		t.Fatalf("unexpected removed resolved-ref: %v", removed)
	}

	loadPaths := mustVectorStrings(t, snapshot.Get(atom.NewKeyword("load-paths")))
	assertStringSliceEqual(t, loadPaths, wantLoadPaths)

	targets := mustMap(t, snapshot.Get(atom.NewKeyword("generated-targets")))
	goPackages := mustVectorStrings(t, targets.Get(atom.NewKeyword("go-packages")))
	assertStringSliceEqual(t, goPackages, []string{"encoding/json", "fmt"})
	reifyTargets, ok := targets.Get(atom.NewKeyword("reify-targets")).(*coll.Vector)
	if !ok || len(reifyTargets.Items) != 1 {
		t.Fatalf("unexpected reify-targets: %T %v", targets.Get(atom.NewKeyword("reify-targets")), targets.Get(atom.NewKeyword("reify-targets")))
	}
	reifyTarget := mustMap(t, reifyTargets.Items[0])
	for _, key := range []string{"source", "runtime"} {
		value, ok := reifyTarget.Get(atom.NewKeyword(key)).(atom.String)
		if !ok || string(value) != "fmt.Stringer" {
			t.Fatalf("unexpected reify target %s: %T %v", key, reifyTarget.Get(atom.NewKeyword(key)), reifyTarget.Get(atom.NewKeyword(key)))
		}
	}
	if removed := targets.Get(atom.NewKeyword("reify-interfaces")); removed != nil {
		t.Fatalf("unexpected removed reify-interfaces: %v", removed)
	}
	build := mustMap(t, targets.Get(atom.NewKeyword("build")))
	binary, ok := build.Get(atom.NewKeyword("binary")).(atom.String)
	if !ok || string(binary) != project.GenerationBinaryPath(strings.Repeat("a", 64)) {
		t.Fatalf("unexpected build binary: %T %v", build.Get(atom.NewKeyword("binary")), build.Get(atom.NewKeyword("binary")))
	}
}

func mustVectorStrings(t *testing.T, value types.Value) []string {
	t.Helper()

	vector, ok := value.(*coll.Vector)
	if !ok {
		t.Fatalf("expected vector, got %T", value)
	}

	out := make([]string, 0, len(vector.Items))
	for _, item := range vector.Items {
		str, ok := item.(atom.String)
		if !ok {
			t.Fatalf("expected string item, got %T", item)
		}
		out = append(out, string(str))
	}
	return out
}

func assertStringSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("unexpected slice length: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected slice value at %d: got=%v want=%v", i, got, want)
		}
	}
}
