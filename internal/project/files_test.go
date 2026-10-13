package project

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInitModFileCreatesOnce(t *testing.T) {
	root := t.TempDir()
	if err := InitModFile(root); err != nil {
		t.Fatal(err)
	}
	mod, err := LoadModFile(ModPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(mod.Deps) != 0 {
		t.Fatalf("deps = %v, want empty", mod.Deps)
	}
	if err := InitModFile(root); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second init error = %v, want already exists", err)
	}
}

func TestInitModFileWaitsForGenerationReaders(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".doggo"), 0755); err != nil {
		t.Fatal(err)
	}
	owner, err := AcquireGenerationLease(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := AcquireGenerationLease(root, false)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- InitModFile(root) }()
	select {
	case err := <-done:
		t.Fatalf("init completed while reader held lease: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("init remained blocked after reader closed")
	}
}

func TestSharedGenerationLeaseRejectsModWrites(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		root := t.TempDir()
		creator, err := AcquireGenerationLease(root, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := creator.Close(); err != nil {
			t.Fatal(err)
		}
		lease, err := AcquireGenerationLease(root, false)
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Close()

		err = lease.CreateMod(&ModFile{Module: "example/created", Deps: []string{}})
		if err == nil || !strings.Contains(err.Error(), "not exclusive") {
			t.Fatalf("CreateMod error = %v, want shared lease rejection", err)
		}
		if _, err := os.Lstat(ModPath(root)); !os.IsNotExist(err) {
			t.Fatalf("CreateMod through shared lease mutated project: %v", err)
		}
	})

	t.Run("write", func(t *testing.T) {
		root := t.TempDir()
		creator, err := AcquireGenerationLease(root, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := creator.Close(); err != nil {
			t.Fatal(err)
		}
		if err := WriteModFile(ModPath(root), &ModFile{Module: "example/original", Deps: []string{}}); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(ModPath(root))
		if err != nil {
			t.Fatal(err)
		}
		lease, err := AcquireGenerationLease(root, false)
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Close()

		err = lease.WriteMod(&ModFile{Module: "example/replacement", Deps: []string{"./dep"}})
		if err == nil || !strings.Contains(err.Error(), "not exclusive") {
			t.Fatalf("WriteMod error = %v, want shared lease rejection", err)
		}
		after, err := os.ReadFile(ModPath(root))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Fatalf("WriteMod through shared lease changed file from %q to %q", before, after)
		}
	})
}

func TestInitModFileRejectsProjectDirectoryReplacement(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	moved := filepath.Join(base, "project-original")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	replaced := false
	afterInitModLeaseForTest = func() {
		replaced = renameRetainedForTest(t, root, moved)
		if replaced {
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { afterInitModLeaseForTest = nil })

	err := InitModFile(root)
	if !replaced {
		if err != nil {
			t.Fatalf("InitModFile after blocked replacement: %v", err)
		}
		if _, err := LoadModFile(ModPath(root)); err != nil {
			t.Fatalf("mod not created in protected project: %v", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), "root path changed") {
		t.Fatalf("InitModFile error = %v, want root replacement rejection", err)
	}
	for _, candidate := range []string{ModPath(root), ModPath(moved)} {
		if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
			t.Fatalf("replacement redirected mod mutation to %s: %v", candidate, err)
		}
	}
}

func TestWriteRegistryFileSortsPackagesAndCanonicalizesVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doggo.registry")
	registry := &RegistryFile{
		Packages: []RegistryPackage{
			{Name: "demo.b", Version: "1.0.0", Source: "pkg:demo.b@1.0.0"},
			{Name: "demo.a", Version: "v2.0.0", Source: "pkg:demo.a@v2.0.0"},
		},
	}
	if err := WriteRegistryFile(path, registry); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	text := string(content)
	idxA := strings.Index(text, `{:name "demo.a" :version "2.0.0" :source "pkg:demo.a@v2.0.0"}`)
	idxB := strings.Index(text, `{:name "demo.b" :version "1.0.0" :source "pkg:demo.b@1.0.0"}`)
	if idxA == -1 || idxB == -1 {
		t.Fatalf("serialized registry = %q", text)
	}
	if idxA > idxB {
		t.Fatalf("serialized registry packages are out of order: %q", text)
	}
	if strings.Contains(text, `:version "v2.0.0"`) {
		t.Fatalf("serialized registry did not canonicalize version: %q", text)
	}
}

func TestWriteRegistryFileRejectsNilRegistry(t *testing.T) {
	err := WriteRegistryFile(filepath.Join(t.TempDir(), "doggo.registry"), nil)
	if err == nil || !strings.Contains(err.Error(), "registry file is nil") {
		t.Fatalf("expected nil registry error, got %v", err)
	}
}

func TestLoadRegistryFileRejectsNonMapEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doggo.registry")
	content := `{:packages ["bad"] :advisories ["bad"]}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRegistryFile(path)
	if err == nil || !strings.Contains(err.Error(), ":packages[0] must be a map") {
		t.Fatalf("expected package entry map error, got %v", err)
	}

	content = `{:packages [] :advisories ["bad"]}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = LoadRegistryFile(path)
	if err == nil || !strings.Contains(err.Error(), ":advisories[0] must be a map") {
		t.Fatalf("expected advisory entry map error, got %v", err)
	}
}

func TestLoadLockFileRejectsNonMapDeps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doggo.lock")
	content := `{:lock-version 3 :generation "abc" :mod-lock-inputs-sha256 "abc" :deps ["bad"] :scan {:go-packages [] :reify-targets []} :build {:binary "doggo" :sha256 "def"}}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadLockFile(path)
	if err == nil || !strings.Contains(err.Error(), ":deps[0] must be a map") {
		t.Fatalf("expected deps entry map error, got %v", err)
	}
}

func TestLoadLockFilePreservesDepValidationOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doggo.lock")
	// This legacy top-level install path is intentional: the rejection fixture verifies
	// that the missing :kind error is reported before later invalid fields.
	content := `{:lock-version 3 :generation "abc" :mod-lock-inputs-sha256 "abc" :deps [{:source "github.com/acme/lib" :digest "sha256" :install-dir ".doggo/deps/lib"}] :scan {:go-packages [] :reify-targets []} :build {:binary "doggo" :sha256 "def"}}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadLockFile(path)
	if err == nil || !strings.Contains(err.Error(), ":deps[0].:kind") {
		t.Fatalf("expected kind validation error before later dep fields, got %v", err)
	}
}

func TestCanonicalLockV3RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doggo.lock")
	lock := &LockFile{
		LockVersion: LockVersion,
		Generation:  strings.Repeat("a", 64),
		Deps: []LockDep{{
			Source: "../shared", Kind: "path", ModuleID: "doggo:acme/shared",
			GoModule: "example.com/acme/shared", Revision: "",
			Digest: strings.Repeat("b", 64), InstallDir: "deps/" + strings.Repeat("c", 64),
		}},
		Scan: LockScan{ReifyTargets: []ReifyTarget{{
			Source: "example.com/facade.Public", Runtime: "example.com/origin.hidden",
		}}},
	}
	if err := WriteLockFile(path, lock); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{":generation", ":module-id", ":go-module", ":revision", ":reify-targets"} {
		if !strings.Contains(string(content), key) {
			t.Errorf("canonical lock missing %s: %s", key, content)
		}
	}
	for _, oldKey := range []string{":resolved-ref", ":reify-interfaces"} {
		if strings.Contains(string(content), oldKey) {
			t.Errorf("canonical lock contains removed %s: %s", oldKey, content)
		}
	}

	got, err := LoadLockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := *lock
	want.Scan.GoPackages = []string{}
	if !reflect.DeepEqual(got, &want) {
		t.Fatalf("round trip = %+v, want %+v", got, &want)
	}
}

func TestLoadLockFileRejectsV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doggo.lock")
	content := `{:lock-version 1 :generation "" :mod-lock-inputs-sha256 "" :deps [] :scan {:go-packages [] :reify-targets []} :build {:binary "" :sha256 ""}}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadLockFile(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported lock version: 1") {
		t.Fatalf("LoadLockFile() error = %v, want unsupported v1", err)
	}
}

func TestLoadLockFileRejectsV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doggo.lock")
	content := `{:lock-version 2 :generation "" :mod-lock-inputs-sha256 "" :deps [] :scan {:go-packages [] :reify-targets []} :build {:binary "" :sha256 ""}}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadLockFile(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported lock version: 2") {
		t.Fatalf("LoadLockFile() error = %v, want unsupported v2", err)
	}
}

func TestHashModLockInputsUsesOnlyResolutionFields(t *testing.T) {
	base := &ModFile{
		Module:  "example.com/demo",
		Name:    "Demo",
		Version: "1.0.0",
		License: "MIT",
		Compat:  "1",
		Deps:    []string{"example.com/lib"},
	}
	metadataOnly := &ModFile{
		Module:  base.Module,
		Name:    "Renamed",
		Version: "2.0.0",
		License: "Apache-2.0",
		Compat:  "2",
		Deps:    append([]string(nil), base.Deps...),
	}
	if got, want := HashModLockInputs(metadataOnly), HashModLockInputs(base); got != want {
		t.Fatalf("metadata-only change altered lock input hash: got %s, want %s", got, want)
	}

	moduleChanged := *base
	moduleChanged.Module = "example.com/renamed"
	if HashModLockInputs(&moduleChanged) == HashModLockInputs(base) {
		t.Fatal("module change did not alter lock input hash")
	}

	depsChanged := *base
	depsChanged.Deps = []string{"example.com/other"}
	if HashModLockInputs(&depsChanged) == HashModLockInputs(base) {
		t.Fatal("dependency change did not alter lock input hash")
	}
}
