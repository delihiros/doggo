package depsnapshot

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"doggo/internal/project"
)

func TestSnapshotDependenciesResolvesGoModuleWithoutFollowingReplace(t *testing.T) {
	moduleRoot := t.TempDir()
	goMod := "module github.com/acme/math\n\ngo 1.25.0\n\nreplace example.com/missing => ../missing\n"
	writeFile(t, filepath.Join(moduleRoot, "go.mod"), goMod)
	writeFile(t, filepath.Join(moduleRoot, "math.go"), "package math\n")

	var query string
	stubGoModuleDownload(t, func(got string) ([]byte, string, error) {
		query = got
		return []byte(fmt.Sprintf(`{"Path":"github.com/acme/math","Version":"v1.2.3","Dir":%q}`, moduleRoot)), "", nil
	})
	destination := filepath.Join(t.TempDir(), "stage")
	deps, err := SnapshotDependencies(t.TempDir(), destination, []string{"github.com/acme/math@latest"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "github.com/acme/math@latest" {
		t.Fatalf("download query = %q", query)
	}
	if len(deps) != 1 {
		t.Fatalf("snapshot count = %d, want 1: %+v", len(deps), deps)
	}
	dep := deps[0]
	if dep.Kind != "go" || dep.Source != "github.com/acme/math@latest" || dep.Revision != "v1.2.3" || dep.ModuleID != "go:github.com/acme/math" || dep.GoModule != "github.com/acme/math" {
		t.Fatalf("Go lock entry = %+v", dep)
	}
	copied, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(dep.InstallDir), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(copied) != goMod {
		t.Fatalf("copied go.mod = %q, want original %q", copied, goMod)
	}
}

func TestSnapshotDependenciesRejectsDifferingDirectDeclarationsWithSameResolvedIdentity(t *testing.T) {
	const modulePath = "example.com/mod"
	proxy := t.TempDir()
	writeGoProxyVersion(t, proxy, modulePath, "v1.2.3")
	writeFile(t, filepath.Join(proxy, filepath.FromSlash(modulePath), "@v", "list"), "v1.2.3\n")
	t.Setenv("GOPROXY", fileURLForTest(proxy))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GONOPROXY", "none")
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOPATH", t.TempDir())
	t.Setenv("GOMODCACHE", filepath.Join(t.TempDir(), "modcache"))

	omitted := modulePath
	latest := modulePath + "@latest"
	_, err := SnapshotDependencies(t.TempDir(), filepath.Join(t.TempDir(), "stage"), []string{omitted, latest})
	if err == nil || !strings.Contains(err.Error(), omitted) || !strings.Contains(err.Error(), latest) || !strings.Contains(err.Error(), "go:"+modulePath) {
		t.Fatalf("error = %v, want both declarations and resolved identity", err)
	}
}

func TestSnapshotDependenciesOwnsGitCloneAndReturnsCleanupError(t *testing.T) {
	repository := t.TempDir()
	writeFile(t, filepath.Join(repository, "doggo.mod"), `{:module "example/repo" :deps []}`)
	runTestGit(t, repository, "init")
	runTestGit(t, repository, "add", "doggo.mod")
	runTestGit(t, repository, "-c", "user.name=Doggo", "-c", "user.email=doggo@example.com", "commit", "-m", "initial")

	originalNew := newGitDependencyTempRoot
	originalCleanup := cleanupGitDependencyTemp
	want := errors.New("cleanup failed")
	var owned *project.OwnedTempRoot
	newGitDependencyTempRoot = func(prefix string) (*project.OwnedTempRoot, error) {
		var err error
		owned, err = originalNew(prefix)
		return owned, err
	}
	cleanupGitDependencyTemp = func(root *project.OwnedTempRoot) error {
		return errors.Join(originalCleanup(root), want)
	}
	t.Cleanup(func() {
		newGitDependencyTempRoot = originalNew
		cleanupGitDependencyTemp = originalCleanup
	})

	source := fileURLForTest(repository)
	_, err := SnapshotDependencies(t.TempDir(), filepath.Join(t.TempDir(), "stage"), []string{source})
	if !errors.Is(err, want) {
		t.Fatalf("SnapshotDependencies error = %v, want cleanup failure", err)
	}
	if owned == nil {
		t.Fatal("Git clone did not allocate an OwnedTempRoot")
	}
	if _, err := os.Lstat(owned.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Git temporary root remains: %v", err)
	}
}

func TestCloneGitDependencyJoinsCommandAndCleanupErrors(t *testing.T) {
	originalCleanup := cleanupGitDependencyTemp
	want := errors.New("cleanup failed")
	cleanupGitDependencyTemp = func(root *project.OwnedTempRoot) error {
		return errors.Join(originalCleanup(root), want)
	}
	t.Cleanup(func() { cleanupGitDependencyTemp = originalCleanup })

	_, _, _, err := cloneGitDependency("file:///doggo-missing-repository")
	if err == nil || !errors.Is(err, want) || !strings.Contains(err.Error(), "git clone") {
		t.Fatalf("cloneGitDependency error = %v, want command and cleanup failures", err)
	}
}

func hashCanonicalTree(root string) (string, error) {
	opened, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer opened.Close()
	return project.CanonicalRootDigest(opened)
}

func TestHashCanonicalTreeCompatibility(t *testing.T) {
	// Fixed digests pin the persisted format independently of the shared hasher.
	for _, tt := range []struct {
		name string
		mode os.FileMode
		want string
	}{
		{"empty", 0, "af5570f5a1810b7af78caf4bc70a660f0df51e42baf91d4de5b2328de0e83dfc"},
		{"nested", 0600, "d7f12a9f40ec643ebf26bd2d4967275dc764991b8a59e766d153c6ae7e2586cf"},
		{"executable", 0700, "932a7b249c1dfe927a86691b643e903edd6a1bc7ec50641cb8c005ca91e4d05a"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mode&0111 != 0 && runtime.GOOS == "windows" {
				t.Skip("Windows does not expose Unix executable permission bits")
			}
			root := t.TempDir()
			if tt.mode != 0 {
				// Native Windows ordering reverses dep/core.dog and dep0.txt.
				writeFile(t, filepath.Join(root, "dep0.txt"), "top\n")
				file := filepath.Join(root, "dep", "core.dog")
				writeFile(t, file, "first\x00second\n")
				if err := os.Chmod(file, tt.mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(root, "empty"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			got, err := hashCanonicalTree(root)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("tree digest = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestHashCanonicalTreeFramesEntriesUnambiguously(t *testing.T) {
	oneEntry := t.TempDir()
	twoEntries := t.TempDir()
	writeFile(t, filepath.Join(oneEntry, "a"), "first\x00fb\x000644\x00second")
	writeFile(t, filepath.Join(twoEntries, "a"), "first")
	writeFile(t, filepath.Join(twoEntries, "b"), "second")

	if left, right := oldCanonicalEncoding(t, oneEntry), oldCanonicalEncoding(t, twoEntries); !bytes.Equal(left, right) {
		t.Fatalf("regression fixture does not collide under old encoding:\n%x\n%x", left, right)
	}
	left, err := hashCanonicalTree(oneEntry)
	if err != nil {
		t.Fatal(err)
	}
	right, err := hashCanonicalTree(twoEntries)
	if err != nil {
		t.Fatal(err)
	}
	if left == right {
		t.Fatalf("distinct trees have ambiguous canonical digest %s", left)
	}
}

func TestMakeSnapshotDirectoryRejectsReplacementBeforeOpen(t *testing.T) {
	destination := t.TempDir()
	root, err := os.OpenRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tree := project.NewOwnedTree(root)
	defer tree.Close()
	depsRoot, err := tree.CreateRoot("deps", 0755, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer depsRoot.Close()
	var replacement, moved string
	beforeSnapshotDirectoryOpenForTest = func(name string) {
		replacement = filepath.Join(destination, "deps", name)
		moved = replacement + "-owned"
		if err := os.Rename(replacement, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(replacement, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(replacement, "replacement"), []byte("racer"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		beforeSnapshotDirectoryOpenForTest = nil
		_ = os.RemoveAll(replacement)
		_ = os.RemoveAll(moved)
	})

	name, opened, err := (&snapshotter{depsRoot: depsRoot, tree: tree}).makeSnapshotDirectory()
	if opened != nil {
		_ = opened.Close()
	}
	if err == nil {
		t.Fatalf("makeSnapshotDirectory accepted replacement %s", name)
	}
	content, readErr := os.ReadFile(filepath.Join(replacement, "replacement"))
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("replacement changed: content=%q err=%v", content, readErr)
	}
	if _, statErr := os.Lstat(moved); !os.IsNotExist(statErr) {
		t.Fatalf("created snapshot stage was not cleaned: %v", statErr)
	}
}

func TestSnapshotDependenciesCapturesModulesReplacementsAndStableRoots(t *testing.T) {
	firstRoot := filepath.Join(t.TempDir(), "project")
	secondRoot := filepath.Join(t.TempDir(), "project")
	writeSnapshotFixture(t, firstRoot)
	writeSnapshotFixture(t, secondRoot)
	for _, root := range []string{firstRoot, secondRoot} {
		generatedRoot := filepath.Join(root, "deps", "dog-only")
		generatedMod, err := project.LoadModFile(project.ModPath(generatedRoot))
		if err != nil {
			t.Fatal(err)
		}
		if generatedMod.Module != "example/dog-only" || generatedMod.Name != "dog-only" || generatedMod.Version != "0.1.0" || generatedMod.License != "MIT" || generatedMod.Compat != ">=0.1.0" {
			t.Fatalf("generated library manifest = %#v", generatedMod)
		}
		if _, err := os.Stat(filepath.Join(generatedRoot, "go.mod")); !os.IsNotExist(err) {
			t.Fatalf("generated Doggo-only library has go.mod: %v", err)
		}
		generatedSource, err := os.ReadFile(filepath.Join(generatedRoot, "src", "dog_only", "core.dog"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(generatedSource), "(ns dog_only.core)") || !strings.Contains(string(generatedSource), "(def add") {
			t.Fatalf("generated library source = %q", generatedSource)
		}
	}

	firstDestination := filepath.Join(t.TempDir(), "stage")
	secondDestination := filepath.Join(t.TempDir(), "stage")
	sources := []string{"./deps/dog-only", "./deps/alias/../dog-only", "./deps/both"}
	first, err := SnapshotDependencies(firstRoot, firstDestination, sources)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SnapshotDependencies(secondRoot, secondDestination, sources)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("snapshots differ by project root:\nfirst:  %+v\nsecond: %+v", first, second)
	}
	if len(first) != 4 {
		t.Fatalf("snapshot count = %d, want 4: %+v", len(first), first)
	}

	byModule := make(map[string]project.LockDep, len(first))
	for _, dep := range first {
		byModule[dep.ModuleID] = dep
		if !regexp.MustCompile(`^deps/[0-9a-f]{64}$`).MatchString(dep.InstallDir) {
			t.Fatalf("install-dir = %q, want deps/<64-hex>", dep.InstallDir)
		}
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(dep.Digest) {
			t.Fatalf("digest = %q, want 64-hex", dep.Digest)
		}
		actualDigest, err := hashCanonicalTree(filepath.Join(firstDestination, filepath.FromSlash(dep.InstallDir)))
		if err != nil {
			t.Fatal(err)
		}
		if actualDigest != dep.Digest {
			t.Fatalf("tree digest = %q, lock digest = %q", actualDigest, dep.Digest)
		}
		if want := project.DependencyKey(dep); filepath.Base(dep.InstallDir) != want {
			t.Fatalf("install key = %q, want %q", filepath.Base(dep.InstallDir), want)
		}
	}
	if got := byModule["doggo:example/dog-only"].GoModule; got != "" {
		t.Fatalf("Doggo-only GoModule = %q, want empty", got)
	}
	if got := byModule["doggo:example/both"].GoModule; got != "example.com/both" {
		t.Fatalf("both-manifest GoModule = %q", got)
	}
	if got := byModule["go:example.com/middle"].GoModule; got != "example.com/middle" {
		t.Fatalf("middle GoModule = %q", got)
	}
	if got := byModule["go:example.com/go-only"].GoModule; got != "example.com/go-only" {
		t.Fatalf("Go-only GoModule = %q", got)
	}

	both := byModule["doggo:example/both"]
	middle := byModule["go:example.com/middle"]
	goOnly := byModule["go:example.com/go-only"]
	rootMode, err := os.Stat(filepath.Join(firstDestination, filepath.FromSlash(both.InstallDir)))
	if err != nil {
		t.Fatal(err)
	}
	wantMode := os.FileMode(0755)
	if runtime.GOOS == "windows" {
		wantMode = 0777 // Windows directories do not expose Unix permission bits.
	}
	if got := rootMode.Mode().Perm(); got != wantMode {
		t.Fatalf("snapshot root mode = %#o, want %#o", got, wantMode)
	}
	rewritten, err := os.ReadFile(filepath.Join(firstDestination, filepath.FromSlash(both.InstallDir), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	wantReplacement := "../" + filepath.Base(middle.InstallDir)
	if !strings.Contains(string(rewritten), "=> "+wantReplacement) {
		t.Fatalf("rewritten go.mod = %q, want replacement %q", rewritten, wantReplacement)
	}
	if strings.Contains(string(rewritten), firstRoot) {
		t.Fatalf("rewritten go.mod leaked source root: %q", rewritten)
	}
	middleMod, err := os.ReadFile(filepath.Join(firstDestination, filepath.FromSlash(middle.InstallDir), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	wantNestedReplacement := "../" + filepath.Base(goOnly.InstallDir)
	if !strings.Contains(string(middleMod), "=> "+wantNestedReplacement) {
		t.Fatalf("nested rewritten go.mod = %q, want replacement %q", middleMod, wantNestedReplacement)
	}
	goOnlyMod, err := os.ReadFile(filepath.Join(firstDestination, filepath.FromSlash(goOnly.InstallDir), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "module   example.com/go-only\n\ngo 1.24.0\n"; string(goOnlyMod) != want {
		t.Fatalf("go.mod without local replacements changed: got %q, want %q", goOnlyMod, want)
	}
	if _, err := os.Stat(filepath.Join(firstDestination, filepath.FromSlash(both.InstallDir), ".git")); !os.IsNotExist(err) {
		t.Fatalf(".git was copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(firstDestination, filepath.FromSlash(both.InstallDir), ".doggo")); !os.IsNotExist(err) {
		t.Fatalf(".doggo was copied: %v", err)
	}
	doggoOnly := byModule["doggo:example/dog-only"]
	if _, err := os.Stat(filepath.Join(firstDestination, filepath.FromSlash(doggoOnly.InstallDir), ".git")); !os.IsNotExist(err) {
		t.Fatalf(".git metadata file was copied: %v", err)
	}
	mode, err := os.Stat(filepath.Join(firstDestination, filepath.FromSlash(both.InstallDir), "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	wantFileMode := os.FileMode(0755)
	if runtime.GOOS == "windows" {
		wantFileMode = 0666 // Windows files do not expose Unix executable bits.
	}
	if got := mode.Mode().Perm(); got != wantFileMode {
		t.Fatalf("executable mode = %#o, want %#o", got, wantFileMode)
	}
}

func TestSnapshotDependenciesRejectsAbsolutePathDeclaration(t *testing.T) {
	root := t.TempDir()
	dep := filepath.Join(t.TempDir(), "dep")
	writeFile(t, filepath.Join(dep, "doggo.mod"), `{:module "example/dep" :deps []}`)
	_, err := SnapshotDependencies(root, filepath.Join(t.TempDir(), "stage"), []string{dep})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error = %v, want absolute declaration rejection", err)
	}
}

func TestSnapshotDependenciesRejectsSymlinkedDestinationDeps(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "dep", "doggo.mod"), `{:module "example/dep" :deps []}`)
	destination := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(destination, "deps")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := SnapshotDependencies(root, destination, []string{"./dep"})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v, want destination symlink rejection", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("snapshot escaped destination: %v", entries)
	}
}

func TestSnapshotDependenciesAllowsTrustedSymlinkedDestinationAncestor(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "dep", "doggo.mod"), `{:module "example/dep" :deps []}`)
	realParent := t.TempDir()
	trustedLink := filepath.Join(t.TempDir(), "trusted-parent")
	if err := os.Symlink(realParent, trustedLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	destination := filepath.Join(trustedLink, "stage")

	deps, err := SnapshotDependencies(root, destination, []string{"./dep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 {
		t.Fatalf("snapshot count = %d, want 1", len(deps))
	}
	if _, err := os.Stat(filepath.Join(realParent, "stage", filepath.FromSlash(deps[0].InstallDir), "doggo.mod")); err != nil {
		t.Fatalf("snapshot missing beneath trusted destination anchor: %v", err)
	}
}

func TestSnapshotDependenciesRejectsAbsoluteReplacement(t *testing.T) {
	root := t.TempDir()
	dep := filepath.Join(root, "dep")
	writeFile(t, filepath.Join(dep, "go.mod"), "module example.com/dep\n\nreplace example.com/other => "+filepath.Join(root, "other")+"\n")
	_, err := SnapshotDependencies(root, filepath.Join(t.TempDir(), "stage"), []string{"./dep"})
	if err == nil || !strings.Contains(err.Error(), "absolute replacement") {
		t.Fatalf("error = %v, want absolute replacement rejection", err)
	}
}

func TestSnapshotDependenciesRejectsReplacementCycle(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "go.mod"), "module example.com/a\n\nreplace example.com/b => ../b\n")
	writeFile(t, filepath.Join(root, "b", "go.mod"), "module example.com/b\n\nreplace example.com/a => ../a\n")
	_, err := SnapshotDependencies(root, filepath.Join(t.TempDir(), "stage"), []string{"./a"})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %v, want replacement cycle", err)
	}
}

func TestSnapshotDependenciesRejectsSymlinkedSourceAncestor(t *testing.T) {
	for _, nested := range []bool{false, true} {
		name := "direct"
		if nested {
			name = "replacement"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			writeFile(t, filepath.Join(outside, "dep", "go.mod"), "module example.com/dep\n")
			if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			sources := []string{"./link/dep"}
			if nested {
				writeFile(t, filepath.Join(root, "parent", "go.mod"), "module example.com/parent\n\nreplace example.com/dep => ../link/dep\n")
				sources = []string{"./parent"}
			}
			_, err := SnapshotDependencies(root, filepath.Join(t.TempDir(), "stage"), sources)
			if err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("error = %v, want source ancestor symlink rejection", err)
			}
		})
	}
}

func TestSnapshotDependenciesKeepsOpenedSourceAncestryDuringReplacement(t *testing.T) {
	root := t.TempDir()
	originalOuter := filepath.Join(root, "outer")
	writeFile(t, filepath.Join(originalOuter, "inner", "dep", "go.mod"), "module example.com/original\n\nreplace example.com/original-child => ../child\n")
	writeFile(t, filepath.Join(originalOuter, "inner", "child", "go.mod"), "module example.com/original-child\n")
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "inner", "dep", "go.mod"), "module example.com/outside\n\nreplace example.com/outside-child => ../child\n")
	writeFile(t, filepath.Join(outside, "inner", "child", "go.mod"), "module example.com/outside-child\n")

	swapped := false
	afterSourceComponentOpenForTest = func(path string) {
		if swapped || path != originalOuter {
			return
		}
		swapped = true
		if err := os.Rename(originalOuter, filepath.Join(root, "opened-outer")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, originalOuter); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { afterSourceComponentOpenForTest = nil }()

	deps, err := SnapshotDependencies(root, filepath.Join(t.TempDir(), "stage"), []string{"./outer/inner/dep"})
	if err != nil {
		t.Fatal(err)
	}
	modules := make(map[string]bool, len(deps))
	for _, dep := range deps {
		modules[dep.ModuleID] = true
	}
	if !modules["go:example.com/original"] || !modules["go:example.com/original-child"] || modules["go:example.com/outside"] || modules["go:example.com/outside-child"] {
		t.Fatalf("snapshotted modules = %v, want only opened source ancestry", modules)
	}
}

func TestSnapshotDependenciesSupportsSiblingSourceDeclaration(t *testing.T) {
	container := t.TempDir()
	projectRoot := filepath.Join(container, "project")
	if err := os.Mkdir(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(container, "sibling", "doggo.mod"), `{:module "example/sibling" :deps []}`)

	deps, err := SnapshotDependencies(projectRoot, filepath.Join(t.TempDir(), "stage"), []string{"../sibling"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0].ModuleID != "doggo:example/sibling" {
		t.Fatalf("snapshotted dependencies = %+v, want sibling module", deps)
	}
}

func TestSnapshotDependenciesRejectsManifestlessModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "dep", "source.dog"), "{:ok true}")
	_, err := SnapshotDependencies(root, filepath.Join(t.TempDir(), "stage"), []string{"./dep"})
	if err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("error = %v, want manifest rejection", err)
	}
}

func TestSnapshotDependenciesUsesOneManifestSnapshotForIdentityGraphAndCopy(t *testing.T) {
	root := t.TempDir()
	depRoot := filepath.Join(root, "dep")
	if err := os.MkdirAll(depRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(depRoot), &project.ModFile{
		Module: "example/original", Deps: []string{"../dog-child"},
	}); err != nil {
		t.Fatal(err)
	}
	originalDoggoMod, err := os.ReadFile(project.ModPath(depRoot))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/original\n\nreplace example.com/go-child => ../go-child\n")
	writeFile(t, filepath.Join(root, "dog-child", "doggo.mod"), `{:module "example/dog-child" :deps []}`)
	writeFile(t, filepath.Join(root, "go-child", "go.mod"), "module example.com/go-child\n")

	afterManifestReadForTest = func(acquiredRoot string) {
		if acquiredRoot != depRoot {
			return
		}
		if err := project.WriteModFile(project.ModPath(depRoot), &project.ModFile{Module: "example/mutated", Deps: []string{}}); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/mutated\n")
	}
	defer func() { afterManifestReadForTest = nil }()

	destination := filepath.Join(t.TempDir(), "stage")
	deps, err := SnapshotDependencies(root, destination, []string{"./dep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 3 {
		t.Fatalf("snapshot count = %d, want original three-node graph: %+v", len(deps), deps)
	}
	byModule := make(map[string]project.LockDep, len(deps))
	for _, dep := range deps {
		byModule[dep.ModuleID] = dep
	}
	parent, ok := byModule["doggo:example/original"]
	if !ok || parent.GoModule != "example.com/original" {
		t.Fatalf("original manifest identity missing: %+v", deps)
	}
	if _, ok := byModule["doggo:example/dog-child"]; !ok {
		t.Fatalf("original Doggo dependency missing: %+v", deps)
	}
	goChild, ok := byModule["go:example.com/go-child"]
	if !ok {
		t.Fatalf("original Go replacement missing: %+v", deps)
	}
	stagedDoggoMod, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(parent.InstallDir), "doggo.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stagedDoggoMod, originalDoggoMod) {
		t.Fatalf("staged doggo.mod = %q, want originally parsed bytes %q", stagedDoggoMod, originalDoggoMod)
	}
	stagedGoMod, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(parent.InstallDir), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stagedGoMod), "module example.com/original") || !strings.Contains(string(stagedGoMod), "../"+filepath.Base(goChild.InstallDir)) || strings.Contains(string(stagedGoMod), "mutated") {
		t.Fatalf("staged go.mod did not use originally parsed bytes: %q", stagedGoMod)
	}
}

func TestSnapshotDependenciesRejectsSymlinkEntry(t *testing.T) {
	root := t.TempDir()
	dep := filepath.Join(root, "dep")
	writeFile(t, filepath.Join(dep, "doggo.mod"), `{:module "example/dep" :deps []}`)
	if err := os.Symlink("doggo.mod", filepath.Join(dep, "bad-entry")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := SnapshotDependencies(root, filepath.Join(t.TempDir(), "stage"), []string{"./dep"})
	if err == nil || !strings.Contains(err.Error(), "unsupported filesystem entry") {
		t.Fatalf("error = %v, want symlink rejection", err)
	}
}

func TestSnapshotDependenciesRejectsTwoRevisionsForOneModule(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "doggo.mod"), `{:module "example/shared" :deps []}`)
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "add", "doggo.mod")
	runTestGit(t, repo, "-c", "user.name=Doggo", "-c", "user.email=doggo@example.com", "commit", "-m", "first")
	runTestGit(t, repo, "tag", "first")
	runTestGit(t, repo, "-c", "user.name=Doggo", "-c", "user.email=doggo@example.com", "commit", "--allow-empty", "-m", "second")
	runTestGit(t, repo, "tag", "second")

	sources := []string{fileURLForTest(repo) + "?ref=first", fileURLForTest(repo) + "?ref=second"}
	_, err := SnapshotDependencies(t.TempDir(), filepath.Join(t.TempDir(), "stage"), sources)
	if err == nil || !strings.Contains(err.Error(), "module identity conflict") {
		t.Fatalf("error = %v, want revision conflict", err)
	}
}

func TestSnapshotDependenciesRejectsFilenameShapedMissingGitRef(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "doggo.mod"), `{:module "example/repo" :deps []}`)
	writeFile(t, filepath.Join(repo, "README.md"), "tracked path\n")
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "add", "doggo.mod", "README.md")
	runTestGit(t, repo, "-c", "user.name=Doggo", "-c", "user.email=doggo@example.com", "commit", "-m", "initial")

	_, err := SnapshotDependencies(t.TempDir(), filepath.Join(t.TempDir(), "stage"), []string{fileURLForTest(repo) + "?ref=README.md"})
	if err == nil {
		t.Fatal("filename-shaped missing Git ref was accepted")
	}
}

func TestSnapshotDependenciesRejectsDistinctDoggoModulesWithOneGoModule(t *testing.T) {
	root := t.TempDir()
	for _, module := range []string{"one", "two"} {
		writeFile(t, filepath.Join(root, module, "doggo.mod"), `{:module "example/`+module+`" :deps []}`)
		writeFile(t, filepath.Join(root, module, "go.mod"), "module example.com/shared\n")
	}
	_, err := SnapshotDependencies(root, filepath.Join(t.TempDir(), "stage"), []string{"./one", "./two"})
	if err == nil || !strings.Contains(err.Error(), "Go module conflict") {
		t.Fatalf("error = %v, want Go module conflict", err)
	}
}

func writeSnapshotFixture(t *testing.T, root string) {
	t.Helper()
	generateDoggoLibraryFixture(t, filepath.Join(root, "deps", "dog-only"))
	writeFile(t, filepath.Join(root, "deps", "dog-only", ".git"), "gitdir: elsewhere\n")
	writeFile(t, filepath.Join(root, "deps", "both", "doggo.mod"), `{:module "example/both" :deps []}`)
	writeFile(t, filepath.Join(root, "deps", "both", "go.mod"), "module example.com/both\n\ngo 1.24.0\n\nreplace example.com/middle => ../middle\n")
	writeFile(t, filepath.Join(root, "deps", "both", "both.go"), "package both\n")
	writeFile(t, filepath.Join(root, "deps", "both", ".git", "ignored"), "ignored")
	writeFile(t, filepath.Join(root, "deps", "both", ".doggo", "ignored"), "ignored")
	writeFile(t, filepath.Join(root, "deps", "both", "run.sh"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(root, "deps", "both", "run.sh"), 0711); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "deps", "middle", "go.mod"), "module example.com/middle\n\ngo 1.24.0\n\nreplace example.com/go-only => ../go-only\n")
	writeFile(t, filepath.Join(root, "deps", "middle", "middle.go"), "package middle\n")
	writeFile(t, filepath.Join(root, "deps", "go-only", "go.mod"), "module   example.com/go-only\n\ngo 1.24.0\n")
	writeFile(t, filepath.Join(root, "deps", "go-only", "only.go"), "package goonly\n")
}

func generateDoggoLibraryFixture(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{
		Module: "example/dog-only", Name: "dog-only", Version: "0.1.0", License: "MIT", Compat: ">=0.1.0", Deps: []string{},
	}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "src", "dog_only", "core.dog"), `(ns dog_only.core)

(def add
  (fn [x y]
    (+ x y)))
`)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func runTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func oldCanonicalEncoding(t *testing.T, root string) []byte {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	var encoded []byte
	for _, name := range names {
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		encoded = append(encoded, 'f')
		encoded = append(encoded, name...)
		encoded = append(encoded, 0)
		encoded = append(encoded, "0644"...)
		encoded = append(encoded, 0)
		encoded = append(encoded, content...)
		encoded = append(encoded, 0)
	}
	return encoded
}
