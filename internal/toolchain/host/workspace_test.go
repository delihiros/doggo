package host

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"doggo/internal/project"
)

func TestMaterializeHostWorkspaceUsesOwnedDependencyReplacementsWithoutTidying(t *testing.T) {
	toolchainRoot, depsRoot, readLog := hostTestPaths(t)
	depRoot := filepath.Join(depsRoot, "module")
	if err := os.MkdirAll(depRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depRoot, "go.mod"), []byte("module example.com/dep\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	deps := []project.LockDep{{GoModule: "example.com/dep", InstallDir: "deps/module"}}
	tree, stagePath := openHostTestTree(t, toolchainRoot)

	if err := MaterializeHostWorkspaceTree(tree, stagePath, deps); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(toolchainRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := filepath.Rel(toolchainRoot, depRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "example.com/dep v0.0.0") || !strings.Contains(string(content), "example.com/dep => "+filepath.ToSlash(replacement)) {
		t.Fatalf("go.mod = %q, want relative dependency replacement %q", content, filepath.ToSlash(replacement))
	}
	if filepath.IsAbs(replacement) || strings.Contains(string(content), depRoot) {
		t.Fatalf("go.mod leaked absolute dependency root: %q", content)
	}
	if log := readLog(); len(log) != 0 {
		t.Fatalf("go command log = %q, want no command before generation", log)
	}
}

func TestTidyHostWorkspaceRunsAfterGeneration(t *testing.T) {
	toolchainRoot, depsRoot, readLog := hostTestPaths(t)
	_ = depsRoot
	tree, stagePath := openHostTestTree(t, toolchainRoot)
	if err := MaterializeHostWorkspaceTree(tree, stagePath, nil); err != nil {
		t.Fatal(err)
	}
	if err := TidyHostWorkspaceTree(tree, stagePath); err != nil {
		t.Fatal(err)
	}
	if log := readLog(); len(log) != 1 || log[0] != "mod tidy" {
		t.Fatalf("go command log = %q, want [mod tidy]", log)
	}
}

func TestTidyHostWorkspaceDoesNotMutateReplacementAfterFinalVerification(t *testing.T) {
	toolchainRoot, depsRoot, _ := hostTestPaths(t)
	_ = depsRoot
	tree, stagePath := openHostTestTree(t, toolchainRoot)
	if err := MaterializeHostWorkspaceTree(tree, stagePath, nil); err != nil {
		t.Fatal(err)
	}
	moved := toolchainRoot + "-owned"
	beforeGoModTidyForTest = func(workspace string) {
		if err := os.Rename(workspace, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(workspace, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workspace, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeGoModTidyForTest = nil })
	t.Setenv("DOGGO_TEST_TIDY_MUTATE", "1")

	if err := TidyHostWorkspaceTree(tree, stagePath); err == nil {
		t.Fatal("TidyHostWorkspaceTree accepted a replaced workspace")
	}
	entries, err := os.ReadDir(toolchainRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "replacement" {
		t.Fatalf("replacement workspace was mutated: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(moved, "go.mod")); err != nil {
		t.Fatalf("owned workspace lost tidy output: %v", err)
	}
}

func TestTidyHostWorkspaceCleanupPreservesTempReplacement(t *testing.T) {
	toolchainRoot, depsRoot, _ := hostTestPaths(t)
	_ = depsRoot
	tree, stagePath := openHostTestTree(t, toolchainRoot)
	if err := MaterializeHostWorkspaceTree(tree, stagePath, nil); err != nil {
		t.Fatal(err)
	}
	var replacement, moved string
	beforeTidyTempCleanupForTest = func(path string) {
		replacement, moved = path, path+"-owned"
		if err := os.Rename(path, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		beforeTidyTempCleanupForTest = nil
		_ = os.RemoveAll(replacement)
		_ = os.RemoveAll(moved)
	})

	if err := TidyHostWorkspaceTree(tree, stagePath); err == nil {
		t.Fatal("TidyHostWorkspaceTree accepted uncertain temp cleanup")
	}
	entries, err := os.ReadDir(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "replacement" {
		t.Fatalf("temp replacement was mutated: %v", entries)
	}
}

func TestMaterializeHostWorkspaceIgnoresDoggoOnlyDependencies(t *testing.T) {
	toolchainRoot, depsRoot, _ := hostTestPaths(t)
	_ = depsRoot
	tree, stagePath := openHostTestTree(t, toolchainRoot)
	if err := MaterializeHostWorkspaceTree(tree, stagePath, []project.LockDep{{ModuleID: "doggo:example/dep", InstallDir: "deps/missing"}}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(toolchainRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "missing") {
		t.Fatalf("go.mod contains Doggo-only dependency: %q", content)
	}
}

func TestBuildBinaryAndHashUsesReadonlyTrimpathBuild(t *testing.T) {
	toolchainRoot, depsRoot, readLog := hostTestPaths(t)
	if err := os.MkdirAll(toolchainRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(depsRoot, 0755); err != nil {
		t.Fatal(err)
	}
	tree, _ := openHostTestTree(t, toolchainRoot)
	output := filepath.Join(filepath.Dir(toolchainRoot), "bin", "doggo")

	digest, err := BuildBinaryAndHashTree(tree, filepath.Base(output))
	if err != nil {
		t.Fatal(err)
	}
	if digest != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("binary SHA-256 = %q", digest)
	}
	log := readLog()
	if len(log) != 1 || !strings.HasPrefix(log[0], "build -mod=readonly -trimpath -o ") || !strings.HasSuffix(log[0], " ./cmd/doggo") {
		t.Fatalf("go command log = %q", log)
	}
	if strings.Contains(log[0], output) {
		t.Fatalf("go build wrote directly to requested output: %q", log)
	}
}

func TestMaterializeHostWorkspaceStopsWhenTidyFails(t *testing.T) {
	toolchainRoot, depsRoot, readLog := hostTestPaths(t)
	t.Setenv("DOGGO_TEST_TIDY_FAIL", "1")
	_ = depsRoot
	tree, stagePath := openHostTestTree(t, toolchainRoot)
	if err := MaterializeHostWorkspaceTree(tree, stagePath, nil); err != nil {
		t.Fatal(err)
	}

	err := TidyHostWorkspaceTree(tree, stagePath)
	if err == nil || !strings.Contains(err.Error(), "run go mod tidy") {
		t.Fatalf("TidyHostWorkspaceTree() error = %v, want tidy error", err)
	}
	if log := readLog(); len(log) != 1 || log[0] != "mod tidy" {
		t.Fatalf("go command log = %q, want [mod tidy]", log)
	}
}

func openHostTestTree(t *testing.T, toolchainRoot string) (*project.OwnedTree, string) {
	t.Helper()
	stagePath := filepath.Dir(toolchainRoot)
	root, err := os.OpenRoot(stagePath)
	if err != nil {
		t.Fatal(err)
	}
	tree := project.NewOwnedTree(root)
	if err := tree.CaptureExisting(); err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tree.Close(); err != nil {
			t.Error(err)
		}
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return tree, stagePath
}

func hostTestPaths(t *testing.T) (string, string, func() []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake go command uses POSIX shell")
	}
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "go.log")
	goPath := filepath.Join(binDir, "go")
	const fakeGo = `#!/bin/sh
[ "$GOWORK" = off ] || exit 3
printf '%s\n' "$*" >> "$DOGGO_TEST_GO_LOG"
if [ "$1 $2" = "mod tidy" ]; then
  [ "${DOGGO_TEST_TIDY_FAIL:-0}" = 0 ] || exit 1
  [ "${DOGGO_TEST_TIDY_MUTATE:-0}" = 0 ] || : > mutation
  exit 0
fi
if [ "$1" = build ]; then
  while [ "$1" != -o ]; do shift; done
  shift
  mkdir -p "$(dirname "$1")"
  : > "$1"
  exit 0
fi
exit 2
`
	if err := os.WriteFile(goPath, []byte(fakeGo), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOGGO_TEST_GO_LOG", logPath)
	t.Setenv("GOWORK", filepath.Join(t.TempDir(), "unrelated.go.work"))
	root := t.TempDir()
	return filepath.Join(root, "toolchain"), filepath.Join(root, "deps"), func() []string {
		content, err := os.ReadFile(logPath)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(content)), "\n")
	}
}
