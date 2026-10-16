package deps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/toolchain/generation"
)

func TestSelectedUpdateRejectsSharedGoReplacementRename(t *testing.T) {
	root, prior := sharedGoReplacementFixture(t)
	before := readSharedSnapshotLock(t, root)
	writeSharedGoFile(t, root, "shared/value.go", "package shared\nconst Value = 2\n")
	_, _, err := runUpdateWorkflow(root, updateConfig{targetSource: "./a", audit: (Auditor{}).AuditPreparedLock})
	if err == nil || !strings.Contains(err.Error(), "identity conflict") {
		t.Fatalf("same-module content conflict = %v", err)
	}
	assertSharedSnapshotUnchanged(t, root, prior, before)

	writeSharedGoFile(t, root, "shared/go.mod", "module audit.invalid/renamed\n\ngo 1.24.0\n")
	writeSharedGoFile(t, root, "shared/value.go", "package renamed\nconst Value = 2\n")
	lock, results, err := runUpdateWorkflow(root, updateConfig{targetSource: "./a", audit: (Auditor{}).AuditPreparedLock})
	if err == nil {
		t.Errorf("selected update accepted mismatched shared Go replacement: lock %#v, results %#v", lock, results)
	} else {
		for _, want := range []string{"audit.invalid/a", "audit.invalid/shared", "../shared", "audit.invalid/renamed"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %v, want %q", err, want)
			}
		}
	}
	assertSharedSnapshotUnchanged(t, root, prior, before)
	assertSharedGoHostValues(t, root, prior, "audit.invalid/a", "1:1")
}

func TestSelectedUpdateAllowsConsistentSharedGoReplacementRename(t *testing.T) {
	root, prior := sharedGoReplacementFixture(t)
	oldB := selectedSnapshotDep(t, prior, "go:audit.invalid/b")
	oldShared := selectedSnapshotDep(t, prior, "go:audit.invalid/shared")
	writeSharedGoFile(t, root, "shared/go.mod", "module audit.invalid/renamed\n\ngo 1.24.0\n")
	writeSharedGoFile(t, root, "shared/value.go", "package renamed\nconst Value = 2\n")
	writeSharedGoConsumer(t, root, "a", "audit.invalid/a", "audit.invalid/renamed")
	lock, results, err := runUpdateWorkflow(root, updateConfig{targetSource: "./a", audit: (Auditor{}).AuditPreparedLock})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Updated || lock.Generation == prior.Generation {
		t.Fatalf("consistent replacement rename = lock %#v, results %#v", lock, results)
	}
	for _, retained := range []project.LockDep{oldB, oldShared} {
		if got := selectedSnapshotDep(t, lock, retained.ModuleID); got != retained {
			t.Errorf("retained dependency changed: before %#v, after %#v", retained, got)
		}
	}
	selectedSnapshotDep(t, lock, "go:audit.invalid/renamed")
	assertSharedGoHostValues(t, root, lock, "audit.invalid/a", "2:1")
}

func TestSelectedUpdateAllowsIndependentGoModuleRename(t *testing.T) {
	root, prior := sharedGoReplacementFixture(t)
	oldB := selectedSnapshotDep(t, prior, "go:audit.invalid/b")
	writeSharedGoFile(t, root, "a/go.mod", "module audit.invalid/renamed-a\n\ngo 1.24.0\n")
	writeSharedGoFile(t, root, "a/value.go", "package a\nfunc Value() int { return 2 }\n")
	lock, results, err := runUpdateWorkflow(root, updateConfig{targetSource: "./a", audit: (Auditor{}).AuditPreparedLock})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Updated || lock.Generation == prior.Generation {
		t.Fatalf("independent Go module rename = lock %#v, results %#v", lock, results)
	}
	if got := selectedSnapshotDep(t, lock, oldB.ModuleID); got != oldB {
		t.Fatalf("independent rename changed retained b: before %#v, after %#v", oldB, got)
	}
	selectedSnapshotDep(t, lock, "go:audit.invalid/renamed-a")
	assertSharedGoHostValues(t, root, lock, "audit.invalid/renamed-a", "2:1")
}

func sharedGoReplacementFixture(t *testing.T) (string, *project.LockFile) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		writeSharedGoConsumer(t, root, name, "audit.invalid/"+name, "audit.invalid/shared")
	}
	writeSharedGoFile(t, root, "shared/go.mod", "module audit.invalid/shared\n\ngo 1.24.0\n")
	writeSharedGoFile(t, root, "shared/value.go", "package shared\nconst Value = 1\n")
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "audit.invalid/root", Deps: []string{"./a", "./b"}}); err != nil {
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

func writeSharedGoConsumer(t *testing.T, root, name, modulePath, sharedPath string) {
	t.Helper()
	writeSharedGoFile(t, root, name+"/go.mod", fmt.Sprintf("module %s\n\ngo 1.24.0\n\nrequire %s v0.0.0\nreplace %s => ../shared\n", modulePath, sharedPath, sharedPath))
	writeSharedGoFile(t, root, name+"/value.go", fmt.Sprintf("package %s\nimport shared %q\nfunc Value() int { return shared.Value }\n", name, sharedPath))
}

func writeSharedGoFile(t *testing.T, root, path, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertSharedGoHostValues(t *testing.T, root string, lock *project.LockFile, selectedPath, want string) {
	t.Helper()
	probe := filepath.Join(t.TempDir(), "main.go")
	source := fmt.Sprintf("package main\nimport (\"fmt\"; a %q; \"audit.invalid/b\")\nfunc main() { fmt.Printf(\"%%d:%%d\", a.Value(), b.Value()) }\n", selectedPath)
	if err := os.WriteFile(probe, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "-mod=readonly", probe)
	cmd.Dir = filepath.Join(root, ".doggo", "generations", lock.Generation, "toolchain")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != want {
		t.Fatalf("generated host values = %q, error %v, want %q", output, err, want)
	}
}
