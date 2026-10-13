package deps

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/toolchain/generation"
	"doggo/internal/vm"
)

func TestSelectedUpdateRejectsReplacementOfSharedDirectPath(t *testing.T) {
	for _, fixture := range []struct {
		name, parent, childSource string
	}{
		{"sibling", "b", "../a"},
		{"contextual alias", "outer/b", "../../a/./"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, module := range []string{"example/a", "example/z"} {
				t.Run(module, func(t *testing.T) {
					root, prior := sharedDirectSnapshotFixture(t, fixture.parent, fixture.childSource)
					before := readSharedSnapshotLock(t, root)
					if err := project.WriteModFile(project.ModPath(filepath.Join(root, "a")), &project.ModFile{Module: module, Deps: []string{}}); err != nil {
						t.Fatal(err)
					}
					writeSharedSnapshotModule(t, root, "2")
					service := Service{ResolveProjectRoot: func() (string, bool, error) { return root, true, nil }}
					lock, results, err := service.UpdateDependencies("./a", false)
					if err == nil || !strings.Contains(err.Error(), "identity conflict") {
						t.Errorf("incompatible shared direct replacement = lock %#v, results %#v, error %v", lock, results, err)
					}
					assertSharedSnapshotUnchanged(t, root, prior, before)
					assertSharedSnapshotRuntime(t, root, "1")
				})
			}
		})
	}
}

func TestSelectedUpdateRejectsRenamedSharedChild(t *testing.T) {
	root, prior := selectedSnapshotFixture(t, map[string][]string{
		"a": {"./shared"}, "outer/b": {"../../a/shared"}, "a/shared": {},
	}, []string{"./a", "./outer/b"})
	before := readSharedSnapshotLock(t, root)
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "a", "shared")), &project.ModFile{Module: "example/renamed-child", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	writeSelectedSnapshotValue(t, root, "a/shared", "2")
	lock, results, err := runUpdateWorkflow(root, updateConfig{targetSource: "./a"})
	if err == nil || !strings.Contains(err.Error(), "identity conflict") {
		t.Errorf("renamed shared child = lock %#v, results %#v, error %v", lock, results, err)
	}
	assertSharedSnapshotUnchanged(t, root, prior, before)
}

func TestSelectedUpdateAllowsIndependentDirectModuleRename(t *testing.T) {
	root, prior := selectedSnapshotFixture(t, map[string][]string{"a": {}, "b": {}}, []string{"./a", "./b"})
	oldB := selectedSnapshotDep(t, prior, "doggo:example/b")
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "a")), &project.ModFile{Module: "example/z", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	writeSelectedSnapshotValue(t, root, "a", "2")
	writeSelectedSnapshotValue(t, root, "b", "2")
	lock, results, err := runUpdateWorkflow(root, updateConfig{targetSource: "./a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Updated || lock.Generation == prior.Generation {
		t.Fatalf("independent rename = %#v, %#v", lock, results)
	}
	if got := selectedSnapshotDep(t, lock, oldB.ModuleID); got != oldB {
		t.Fatalf("independent rename refreshed b: old=%#v new=%#v", oldB, got)
	}
	selectedSnapshotDep(t, lock, "doggo:example/z")
	for _, dep := range lock.Deps {
		if dep.ModuleID == "doggo:example/a" {
			t.Fatal("independent rename retained old selected identity")
		}
	}
}

func TestSelectedUpdateDoesNotConflateSameSourceAtDistinctParents(t *testing.T) {
	root, prior := selectedSnapshotFixture(t, map[string][]string{
		"a": {"./child"}, "b": {"./child"}, "a/child": {}, "b/child": {},
	}, []string{"./a", "./b"})
	oldChild := selectedSnapshotDep(t, prior, "doggo:example/b/child")
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "a", "child")), &project.ModFile{Module: "example/renamed-child", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	writeSelectedSnapshotValue(t, root, "a/child", "2")
	writeSelectedSnapshotValue(t, root, "b/child", "2")
	lock, _, err := runUpdateWorkflow(root, updateConfig{targetSource: "./a"})
	if err != nil {
		t.Fatal(err)
	}
	selectedSnapshotDep(t, lock, "doggo:example/renamed-child")
	if got := selectedSnapshotDep(t, lock, oldChild.ModuleID); got != oldChild {
		t.Fatalf("distinct retained child changed: old=%#v new=%#v", oldChild, got)
	}
}

func sharedDirectSnapshotFixture(t *testing.T, parent, childSource string) (string, *project.LockFile) {
	t.Helper()
	root := t.TempDir()
	for _, fixture := range []struct {
		name string
		deps []string
	}{
		{"a", []string{}},
		{parent, []string{childSource}},
	} {
		dir := filepath.Join(root, filepath.FromSlash(fixture.name))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := project.WriteModFile(project.ModPath(dir), &project.ModFile{Module: "example/" + fixture.name, Deps: fixture.deps}); err != nil {
			t.Fatal(err)
		}
	}
	writeSharedSnapshotModule(t, root, "1")
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/root", Deps: []string{"./a", "./" + parent}}); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort()
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	return root, prepared.Lock
}

func writeSharedSnapshotModule(t *testing.T, root, value string) {
	t.Helper()
	dir := filepath.Join(root, "a", "calc")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "core.dog"), []byte("(ns calc.core)\n(def value "+value+")\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func readSharedSnapshotLock(t *testing.T, root string) []byte {
	t.Helper()
	content, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func assertSharedSnapshotUnchanged(t *testing.T, root string, prior *project.LockFile, before []byte) {
	t.Helper()
	if after := readSharedSnapshotLock(t, root); !bytes.Equal(after, before) {
		t.Errorf("refused shared replacement changed lock")
	}
	current, err := project.LoadLockFile(project.LockPath(root))
	if err != nil || current.Generation != prior.Generation {
		t.Errorf("refused shared replacement changed current generation: %#v, %v", current, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".doggo", "generations"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, []string{prior.Generation}) {
		t.Errorf("refused shared replacement changed generation directories: %v", names)
	}
}

func assertSharedSnapshotRuntime(t *testing.T, root, expected string) {
	t.Helper()
	session, err := project.OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	runner, err := vm.NewRunnerWithConfig(vm.RunnerConfig{
		ProjectRoot: session.Config.ProjectRoot, LoadPaths: session.Config.LoadPaths,
		GenerationSources: session.Config.GenerationSources, Lock: session.Config.Lock, BootstrapCore: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := runner.Run("(require 'calc.core)\ncalc.core/value")
	if err != nil || value.String() != expected {
		t.Fatalf("retained runtime value = %v, error %v (want %s)", value, err, expected)
	}
}
