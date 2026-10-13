package scan

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"doggo/internal/project"
	"doggo/internal/testutil"
	"doggo/internal/toolchain/reifytarget"
)

func TestScanServiceDerivesInteropPackagesAndReifyCandidates(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, filepath.Join(root, "deps", "math", "go.mod"), "module github.com/acme/math\n\ngo 1.24.0\n")
	writeScanFile(t, filepath.Join(root, "deps", "math", "math.go"), "package math\n\ntype Handler interface { Handle() }\n")
	t.Setenv("GOPROXY", "off")
	tree := openScanTestTree(t, root)

	targets, err := (Service{}).DeriveTargetsTree(tree, root, []project.LockDep{
		{Source: "github.com/acme/math", Kind: "git", GoModule: "github.com/acme/math", Revision: "abc", InstallDir: "deps/math"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(targets.GoPackages, "github.com/acme/math") {
		t.Fatalf("go packages = %v", targets.GoPackages)
	}
	wantCandidates := []reifytarget.Candidate{
		{Source: "fmt.Stringer", Required: true},
		{Source: "github.com/acme/math.Handler", Required: false},
		{Source: "net/http.Handler", Required: true},
	}
	if !reflect.DeepEqual(targets.ReifyCandidates, wantCandidates) {
		t.Fatalf("reify candidates = %+v, want %+v", targets.ReifyCandidates, wantCandidates)
	}
}

func TestCollectDependencyGoPackagesUsesLockedModuleIdentity(t *testing.T) {
	got, err := CollectDependencyGoPackages([]project.LockDep{
		{ModuleID: "doggo:example/dog-only"},
		{ModuleID: "doggo:example/both", GoModule: "example.com/both"},
		{ModuleID: "go:example.com/go-only", GoModule: "example.com/go-only"},
		{ModuleID: "go:example.com/go-only", GoModule: "example.com/go-only"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"example.com/both", "example.com/go-only"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectDependencyGoPackages() = %v, want %v", got, want)
	}
}

func TestCollectResolvedDependencyInterfacesUsesOwnedModuleRoot(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, filepath.Join(root, "deps", "module", "go.mod"), "module example.com/dep\n\ngo 1.24.0\n")
	writeScanFile(t, filepath.Join(root, "deps", "module", "dep.go"), "package dep\n\ntype Handler interface { Handle() }\n")
	tree := openScanTestTree(t, root)

	got, err := CollectResolvedDependencyInterfacesTree(tree, root, []project.LockDep{{
		ModuleID: "go:example.com/dep", GoModule: "example.com/dep", InstallDir: "deps/module",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"example.com/dep.Handler"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("interfaces = %v, want %v", got, want)
	}

	for _, installDir := range []string{"../escape", "/tmp/escape", "deps/../escape"} {
		_, err := CollectResolvedDependencyInterfacesTree(tree, root, []project.LockDep{{GoModule: "example.com/dep", InstallDir: installDir}})
		if err == nil {
			t.Fatalf("InstallDir %q accepted", installDir)
		}
	}
}

func TestCollectResolvedDependencyInterfacesDoesNotLoadReplacementAfterFinalVerification(t *testing.T) {
	root := t.TempDir()
	moduleRoot := filepath.Join(root, "deps", "module")
	writeScanFile(t, filepath.Join(moduleRoot, "go.mod"), "module example.com/dep\n\ngo 1.24.0\n")
	writeScanFile(t, filepath.Join(moduleRoot, "dep.go"), "package dep\n\ntype Handler interface { Handle() }\n")
	moved := moduleRoot + "-owned"
	beforeDependencyPackagesLoadForTest = func(path string) {
		if err := os.Rename(path, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeDependencyPackagesLoadForTest = nil })
	t.Setenv("GOPROXY", "off")
	tree := openScanTestTree(t, root)

	_, err := CollectResolvedDependencyInterfacesTree(tree, root, []project.LockDep{{
		ModuleID: "go:example.com/dep", GoModule: "example.com/dep", InstallDir: "deps/module",
	}})
	if err == nil {
		t.Fatal("CollectResolvedDependencyInterfacesTree accepted a replaced dependency")
	}
	entries, err := os.ReadDir(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "replacement" {
		t.Fatalf("replacement dependency was mutated: %v", entries)
	}
}

func TestCollectResolvedDependencyInterfacesPreservesSiblingReplacements(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, filepath.Join(root, "deps", "a", "go.mod"), "module example.com/a\n\ngo 1.24.0\n\nrequire example.com/b v0.0.0\nreplace example.com/b => ../b\n")
	writeScanFile(t, filepath.Join(root, "deps", "a", "a.go"), "package a\n\nimport \"example.com/b\"\n\ntype Handler interface { Handle(b.Value) }\n")
	writeScanFile(t, filepath.Join(root, "deps", "b", "go.mod"), "module example.com/b\n\ngo 1.24.0\n")
	writeScanFile(t, filepath.Join(root, "deps", "b", "b.go"), "package b\n\ntype Value struct{}\n")
	t.Setenv("GOPROXY", "off")
	tree := openScanTestTree(t, root)

	got, err := CollectResolvedDependencyInterfacesTree(tree, root, []project.LockDep{{
		ModuleID: "go:example.com/a", GoModule: "example.com/a", InstallDir: "deps/a",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"example.com/a.Handler"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("interfaces = %v, want %v", got, want)
	}
}

func TestCollectResolvedDependencyInterfacesCleanupPreservesTempReplacement(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, filepath.Join(root, "deps", "module", "go.mod"), "module example.com/dep\n\ngo 1.24.0\n")
	writeScanFile(t, filepath.Join(root, "deps", "module", "dep.go"), "package dep\n\ntype Handler interface { Handle() }\n")
	var replacement, moved string
	replaced := false
	beforeScanTempCleanupForTest = func(path string) {
		replacement, moved = path, path+"-owned"
		replaced = testutil.RenameRetained(t, path, moved)
		if !replaced {
			return
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "replacement"), []byte("racer"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		beforeScanTempCleanupForTest = nil
		_ = os.RemoveAll(replacement)
		_ = os.RemoveAll(moved)
	})
	tree := openScanTestTree(t, root)

	_, err := CollectResolvedDependencyInterfacesTree(tree, root, []project.LockDep{{
		ModuleID: "go:example.com/dep", GoModule: "example.com/dep", InstallDir: "deps/module",
	}})
	if replacement == "" {
		t.Fatal("temp-cleanup replacement hook was not called")
	}
	if !replaced {
		if err := errors.Join(err, tree.VerifyComplete()); err != nil {
			t.Fatalf("scan after blocked temp replacement: %v", err)
		}
		if _, err := os.Lstat(replacement); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned temporary root remains: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatal("CollectResolvedDependencyInterfacesTree accepted uncertain temp cleanup")
	}
	entries, err := os.ReadDir(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "replacement" {
		t.Fatalf("temp replacement was mutated: %v", entries)
	}
}

func openScanTestTree(t *testing.T, rootPath string) *project.OwnedTree {
	t.Helper()
	root, err := os.OpenRoot(rootPath)
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
	return tree
}

func writeScanFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCollectExportedInterfacesUsesRuntimeTypeIdentity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOPROXY", "off")

	for path, content := range map[string]string{
		"go.mod": "module example.com/aliases\n\ngo 1.24.0\n",
		"origin/origin.go": `package origin

type Reader interface { Read([]byte) (int, error) }
type Writer interface { Write([]byte) (int, error) }
type GenericReader[T any] interface { Read() T }
`,
		"facade/facade.go": `package facade

import "example.com/aliases/origin"

type Reader = origin.Reader
type Anonymous = interface { Close() error }
type Error = error
type IntReader = origin.GenericReader[int]
type RawGenericReader[T any] = origin.Writer
type Generic[T any] interface { Value() T }
type Constraint interface { ~int | ~int64 }
type hidden struct{}
type Sealed interface { sealed() }
type HiddenComposite interface { Use(map[string][]*hidden) }
type Local interface { Local() }
`,
	} {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", fullPath, err)
		}
	}

	got, err := collectExportedInterfaces(root, []string{"./facade"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"example.com/aliases/facade.HiddenComposite",
		"example.com/aliases/facade.IntReader",
		"example.com/aliases/facade.Local",
		"example.com/aliases/facade.Reader",
		"example.com/aliases/facade.Sealed",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectExportedInterfaces() = %v, want %v", got, want)
	}
}
