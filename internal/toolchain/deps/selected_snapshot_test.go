package deps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/toolchain/generation"
)

func TestSelectedUpdateRetainsUnselectedPathSnapshot(t *testing.T) {
	root, prior := selectedSnapshotFixture(t, map[string][]string{"a": {}, "b": {}}, []string{"./a", "./b"})
	oldB := selectedSnapshotDep(t, prior, "doggo:example/b")
	writeSelectedSnapshotValue(t, root, "a", "2")
	writeSelectedSnapshotValue(t, root, "b", "2")
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return root, true, nil },
		// Match the CLI's default service wiring: full-sync preparation must
		// not turn a selected update into a refresh of all live dependencies.
		PrepareSync: (generation.Service{}).PrepareSync,
		Auditor: Auditor{AuditLock: func(string, string, *project.ModFile, *project.LockFile) ([]DependencyAuditFinding, error) {
			return nil, nil
		}},
	}
	lock, results, err := service.UpdateDependencies("./a", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Source != "./a" || !results[0].Updated {
		t.Fatalf("selected result = %#v", results)
	}
	newB := selectedSnapshotDep(t, lock, oldB.ModuleID)
	if newB != oldB {
		t.Fatalf("unselected b changed: old=%#v new=%#v", oldB, newB)
	}
	content, err := os.ReadFile(filepath.Join(root, ".doggo", "generations", lock.Generation, filepath.FromSlash(newB.InstallDir), "value.dog"))
	if err != nil || string(content) != "(def value 1)\n" {
		t.Fatalf("retained b content = %q, %v", content, err)
	}
	all, allResults, err := service.UpdateDependencies("", true)
	if err != nil {
		t.Fatal(err)
	}
	if selectedSnapshotDep(t, all, oldB.ModuleID).Digest == oldB.Digest || len(allResults) != 2 {
		t.Fatalf("--all did not refresh b: %#v %#v", all, allResults)
	}
}

func TestSelectedUpdateRetainsUnselectedTransitiveSnapshot(t *testing.T) {
	root, prior := selectedSnapshotFixture(t, map[string][]string{"a": {}, "b": {"./child"}, "b/child": {}}, []string{"./a", "./b"})
	oldChild := selectedSnapshotDep(t, prior, "doggo:example/b/child")
	writeSelectedSnapshotValue(t, root, "b/child", "2")
	// An unrelated changed manifest must not remove the old retained closure.
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "b")), &project.ModFile{Module: "changed/b", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	lock, _, err := runUpdateWorkflow(root, updateConfig{targetSource: "./a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := selectedSnapshotDep(t, lock, oldChild.ModuleID); got != oldChild {
		t.Fatalf("unselected child changed: old=%#v new=%#v", oldChild, got)
	}
}

func TestSelectedUpdateRefreshesOnlySelectedClosure(t *testing.T) {
	root, prior := selectedSnapshotFixture(t, map[string][]string{
		"a": {"./removed"}, "b": {"./shared"}, "a/removed": {}, "b/shared": {}, "a/added": {},
	}, []string{"./a", "./b"})
	oldShared := selectedSnapshotDep(t, prior, "doggo:example/b/shared")
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "a")), &project.ModFile{Module: "example/a", Deps: []string{"./added"}}); err != nil {
		t.Fatal(err)
	}
	lock, _, err := runUpdateWorkflow(root, updateConfig{targetSource: "./a"})
	if err != nil {
		t.Fatal(err)
	}
	selectedSnapshotDep(t, lock, "doggo:example/a/added")
	if got := selectedSnapshotDep(t, lock, oldShared.ModuleID); got != oldShared {
		t.Fatalf("retained closure changed: %#v", got)
	}
	for _, dep := range lock.Deps {
		if dep.ModuleID == "doggo:example/a/removed" {
			t.Fatal("unreferenced selected child remains")
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".doggo", "generations", lock.Generation, filepath.FromSlash(selectedSnapshotDep(t, prior, "doggo:example/a/removed").InstallDir))); !os.IsNotExist(err) {
		t.Fatalf("unreferenced selected child directory remains: %v", err)
	}
}

func TestSelectedUpdateKeepsRemovedChildRequiredByRetainedRoot(t *testing.T) {
	root, prior := selectedSnapshotFixture(t, map[string][]string{"a": {"./shared"}, "a/b": {"../shared"}, "a/shared": {}}, []string{"./a", "./a/b"})
	oldShared := selectedSnapshotDep(t, prior, "doggo:example/a/shared")
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "a")), &project.ModFile{Module: "example/a", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	lock, _, err := runUpdateWorkflow(root, updateConfig{targetSource: "./a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := selectedSnapshotDep(t, lock, oldShared.ModuleID); got != oldShared {
		t.Fatalf("shared child changed: %#v", got)
	}
}

func TestSelectedUpdateRejectsChangedSharedChild(t *testing.T) {
	root, _ := selectedSnapshotFixture(t, map[string][]string{"a": {"./shared"}, "a/b": {"../shared"}, "a/shared": {}}, []string{"./a", "./a/b"})
	before, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	writeSelectedSnapshotValue(t, root, "a/shared", "2")
	_, _, err = runUpdateWorkflow(root, updateConfig{targetSource: "./a"})
	if err == nil || !strings.Contains(err.Error(), "identity conflict") {
		t.Fatalf("selected update accepted incompatible shared child: %v", err)
	}
	after, err := os.ReadFile(project.LockPath(root))
	if err != nil || string(after) != string(before) {
		t.Fatalf("failed update changed lock: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".doggo", "generations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".stage-") {
			t.Fatalf("failed update left stage %s", entry.Name())
		}
	}
}

func TestSelectedUpdateRetainsSharedChildWithDifferentContextualAliases(t *testing.T) {
	root, prior := selectedSnapshotFixture(t, map[string][]string{"a": {"./shared"}, "outer/b": {"../../a/shared"}, "a/shared": {}}, []string{"./a", "./outer/b"})
	oldShared := selectedSnapshotDep(t, prior, "doggo:example/a/shared")
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "a")), &project.ModFile{Module: "example/a", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	lock, _, err := runUpdateWorkflow(root, updateConfig{targetSource: "./a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := selectedSnapshotDep(t, lock, oldShared.ModuleID); got != oldShared {
		t.Fatalf("shared contextual child changed: %#v", got)
	}
}

func TestSelectedUpdateRetainsAmbiguousUnselectedDirectIdentity(t *testing.T) {
	root, prior := selectedSnapshotFixture(t, map[string][]string{"a": {"./child"}, "a/child": {}, "child": {}}, []string{"./a", "./child"})
	oldDirect := selectedSnapshotDep(t, prior, "doggo:example/child")
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "child")), &project.ModFile{Module: "example/a/child", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	lock, _, err := runUpdateWorkflow(root, updateConfig{targetSource: "./a"})
	if err != nil {
		if !strings.Contains(err.Error(), "ambiguous lock entries") {
			t.Fatalf("unrelated identity error = %v", err)
		}
		after, readErr := os.ReadFile(project.LockPath(root))
		if readErr != nil || string(before) != string(after) {
			t.Fatalf("ambiguous update changed prior lock: %v", readErr)
		}
		return
	}
	if got := selectedSnapshotDep(t, lock, oldDirect.ModuleID); got != oldDirect {
		t.Fatalf("unselected direct identity changed: old=%#v new=%#v", oldDirect, got)
	}
}

func TestSelectedUpdateRejectsUnprovenSiblingClosureWithoutChangingLock(t *testing.T) {
	root, prior := selectedSnapshotFixture(t, map[string][]string{"a": {}, "b": {"../child"}, "child": {}}, []string{"./a", "./b"})
	oldChild := selectedSnapshotDep(t, prior, "doggo:example/child")
	before, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	writeSelectedSnapshotValue(t, root, "a", "2")
	writeSelectedSnapshotValue(t, root, "child", "2")
	_, _, err = runUpdateWorkflow(root, updateConfig{targetSource: "./a"})
	if err == nil || !strings.Contains(err.Error(), "unproven retained path context") {
		t.Fatalf("unproven sibling update = %v", err)
	}
	after, readErr := os.ReadFile(project.LockPath(root))
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("unproven sibling update changed lock: %v", readErr)
	}
	all, results, err := runUpdateWorkflow(root, updateConfig{updateAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || selectedSnapshotDep(t, all, oldChild.ModuleID).Digest == oldChild.Digest {
		t.Fatalf("--all did not resolve the fresh sibling closure: %#v %#v", all, results)
	}
}

func TestSelectedUpdateRejectsAmbiguousSelectedOldIdentity(t *testing.T) {
	root, _ := selectedSnapshotFixture(t, map[string][]string{"a": {"./child"}, "a/child": {}, "b": {"../child"}, "child": {}}, []string{"./a", "./b", "./child"})
	before, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "child")), &project.ModFile{Module: "example/a/child", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	_, _, err = runUpdateWorkflow(root, updateConfig{targetSource: "./child"})
	if err == nil || !strings.Contains(err.Error(), "ambiguous lock entries") {
		t.Fatalf("ambiguous selected old identity = %v", err)
	}
	after, readErr := os.ReadFile(project.LockPath(root))
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("ambiguous selected update changed old lock: %v", readErr)
	}
	_, results, err := runUpdateWorkflow(root, updateConfig{updateAll: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Source == "./child" {
			if !result.Updated || result.OldDigest != "" || result.OldRevision != "" || result.NewDigest == "" {
				t.Fatalf("full update inferred ambiguous old baseline: %#v", result)
			}
			return
		}
	}
	t.Fatalf("full update did not report refreshed child: %#v", results)
}

func TestFullUpdateResultsReportAmbiguousOldBaselineAsRefreshed(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "child"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "child")), &project.ModFile{Module: "example/nested", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	old := []project.LockDep{
		{Source: "./child", Kind: "path", ModuleID: "doggo:example/original", Digest: "original"},
		{Source: "./child", Kind: "path", ModuleID: "doggo:example/nested", Digest: "nested"},
	}
	current := []project.LockDep{{Source: "./child", Kind: "path", ModuleID: "doggo:example/nested", Digest: "nested"}}
	results, err := buildDependencyUpdateResults(root, []string{"./child"}, old, current, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Updated || results[0].OldDigest != "" || results[0].OldRevision != "" || results[0].NewDigest != "nested" {
		t.Fatalf("ambiguous old result borrowed live identity: %#v", results)
	}
}

func selectedSnapshotFixture(t *testing.T, graph map[string][]string, direct []string) (string, *project.LockFile) {
	t.Helper()
	root := t.TempDir()
	for name, children := range graph {
		dir := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := project.WriteModFile(project.ModPath(dir), &project.ModFile{Module: "example/" + name, Deps: children}); err != nil {
			t.Fatal(err)
		}
		writeSelectedSnapshotValue(t, root, name, "1")
	}
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/root", Deps: direct}); err != nil {
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

func writeSelectedSnapshotValue(t *testing.T, root, name, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name), "value.dog"), []byte("(def value "+value+")\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func selectedSnapshotDep(t *testing.T, lock *project.LockFile, moduleID string) project.LockDep {
	t.Helper()
	for _, dep := range lock.Deps {
		if dep.ModuleID == moduleID {
			return dep
		}
	}
	t.Fatalf("missing module %s in %#v", moduleID, lock.Deps)
	return project.LockDep{}
}
