package depsnapshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
)

func TestRetainedClosurePreservesGoLocalReplacementChain(t *testing.T) {
	root := t.TempDir()
	writeSnapshotFixture(t, root)
	mod, lock := publishRetainedFixture(t, root, []string{"./deps/dog-only", "./deps/both"})
	// Both the replacement content and its manifest may change independently.
	writeFile(t, filepath.Join(root, "deps", "middle", "go.mod"), "module changed.invalid/middle\n")
	writeFile(t, filepath.Join(root, "deps", "go-only", "only.go"), "changed")
	withRetainedFixture(t, root, mod, lock, func(owner *project.GenerationOwner, snapshot *project.GenerationSnapshot) error {
		retained, err := RetainedDependencyClosure(snapshot, lock.Deps, retainedDirectDeps(t, lock, mod.Deps), "./deps/dog-only")
		if err != nil {
			return err
		}
		if len(retained) != 3 {
			t.Fatalf("retained replacement closure = %#v", retained)
		}
		tree, err := owner.StageTree()
		if err != nil {
			return err
		}
		if err := snapshot.CopyDependencies(tree, retained); err != nil {
			return err
		}
		for _, dep := range retained {
			if dep.GoModule == "example.com/go-only" {
				content, err := tree.ReadFile(dep.InstallDir + "/only.go")
				if err != nil || string(content) != "package goonly\n" {
					t.Fatalf("retained replacement content = %q, %v", content, err)
				}
			}
		}
		return nil
	})
}

func TestRetainedClosureResolvesNestedSameSpellingIdentity(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "doggo.mod"), `{:module "example/a" :deps ["./child"]}`)
	writeFile(t, filepath.Join(root, "a", "child", "doggo.mod"), `{:module "example/nested-child" :deps []}`)
	writeFile(t, filepath.Join(root, "child", "doggo.mod"), `{:module "example/root-child" :deps []}`)
	mod, lock := publishRetainedFixture(t, root, []string{"./a", "./child"})
	withRetainedFixture(t, root, mod, lock, func(_ *project.GenerationOwner, snapshot *project.GenerationSnapshot) error {
		var direct []project.LockDep
		for _, dep := range lock.Deps {
			if dep.ModuleID == "doggo:example/a" || dep.ModuleID == "doggo:example/root-child" {
				direct = append(direct, dep)
			}
		}
		retained, err := RetainedDependencyClosure(snapshot, lock.Deps, direct, "./child")
		if err != nil {
			return err
		}
		if len(retained) != 2 || retained[1].ModuleID != "doggo:example/nested-child" {
			t.Fatalf("retained contextual closure = %#v", retained)
		}
		return nil
	})
}

func TestRetainedClosureResolvesSharedChildContextualAliases(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "doggo.mod"), `{:module "example/a" :deps ["./shared"]}`)
	writeFile(t, filepath.Join(root, "outer", "b", "doggo.mod"), `{:module "example/b" :deps ["../../a/shared"]}`)
	writeFile(t, filepath.Join(root, "a", "shared", "doggo.mod"), `{:module "example/shared" :deps []}`)
	mod, lock := publishRetainedFixture(t, root, []string{"./a", "./outer/b"})
	withRetainedFixture(t, root, mod, lock, func(_ *project.GenerationOwner, snapshot *project.GenerationSnapshot) error {
		retained, err := RetainedDependencyClosure(snapshot, lock.Deps, retainedDirectDeps(t, lock, mod.Deps), "./a")
		if err != nil {
			return err
		}
		if len(retained) != 2 || retained[1].ModuleID != "doggo:example/shared" {
			t.Fatalf("retained alias closure = %#v", retained)
		}
		return nil
	})
}

func TestRetainedClosureRejectsUnprovableSiblingIdentity(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		writeFile(t, filepath.Join(root, name, "parent", "doggo.mod"), `{:module "example/`+name+`" :deps ["../child"]}`)
		writeFile(t, filepath.Join(root, name, "child", "doggo.mod"), `{:module "example/`+name+`-child" :deps []}`)
	}
	mod, lock := publishRetainedFixture(t, root, []string{"./a/parent", "./b/parent"})
	withRetainedFixture(t, root, mod, lock, func(_ *project.GenerationOwner, snapshot *project.GenerationSnapshot) error {
		_, err := RetainedDependencyClosure(snapshot, lock.Deps, retainedDirectDeps(t, lock, mod.Deps), "./a/parent")
		if err == nil || !strings.Contains(err.Error(), "unproven retained path context") {
			t.Fatalf("ambiguous retained identity = %v", err)
		}
		return nil
	})
}

func TestSelectedSnapshotRejectsSharedChildRevisionConflict(t *testing.T) {
	repository := t.TempDir()
	writeFile(t, filepath.Join(repository, "doggo.mod"), `{:module "example/shared" :deps []}`)
	runTestGit(t, repository, "init")
	runTestGit(t, repository, "add", ".")
	runTestGit(t, repository, "-c", "user.name=Doggo", "-c", "user.email=doggo@example.invalid", "commit", "-m", "first")
	source := fileURLForTest(repository)
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := project.WriteModFile(project.ModPath(filepath.Join(root, name)), &project.ModFile{Module: "example/" + name, Deps: []string{source}}); err != nil {
			t.Fatal(err)
		}
	}
	mod, lock := publishRetainedFixture(t, root, []string{"./a", "./b"})
	// The tree is unchanged, but a different commit remains an incompatible pin.
	runTestGit(t, repository, "-c", "user.name=Doggo", "-c", "user.email=doggo@example.invalid", "commit", "--allow-empty", "-m", "second")
	withRetainedFixture(t, root, mod, lock, func(owner *project.GenerationOwner, snapshot *project.GenerationSnapshot) error {
		direct := retainedDirectDeps(t, lock, mod.Deps)
		retained, err := RetainedDependencyClosure(snapshot, lock.Deps, direct, "./a")
		if err != nil {
			return err
		}
		tree, err := owner.StageTree()
		if err != nil {
			return err
		}
		if err := snapshot.CopyDependencies(tree, retained); err != nil {
			return err
		}
		projectRoot, err := owner.OpenProjectRoot()
		if err != nil {
			return err
		}
		defer projectRoot.Close()
		var retainedDirect []project.LockDep
		for _, dep := range direct {
			if dep.Source == "./b" {
				retainedDirect = append(retainedDirect, dep)
			}
		}
		_, err = SnapshotDependenciesTreeRetaining(projectRoot, root, tree, []string{"./a"}, retained, retainedDirect)
		if err == nil || !strings.Contains(err.Error(), "module identity conflict") {
			t.Fatalf("shared revision conflict = %v", err)
		}
		return nil
	})
}

func TestSelectedSnapshotAllowsRenameAtDistinctPathWithSameOldIdentity(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		writeFile(t, filepath.Join(root, name, "doggo.mod"), `{:module "example/`+name+`" :deps ["./child"]}`)
		writeFile(t, filepath.Join(root, name, "child", "doggo.mod"), `{:module "example/old-child" :deps []}`)
	}
	mod, lock := publishRetainedFixture(t, root, []string{"./a", "./b"})
	writeFile(t, filepath.Join(root, "a", "child", "doggo.mod"), `{:module "example/new-child" :deps []}`)
	withRetainedFixture(t, root, mod, lock, func(owner *project.GenerationOwner, snapshot *project.GenerationSnapshot) error {
		direct := retainedDirectDeps(t, lock, mod.Deps)
		retained, paths, err := RetainedDependencyClosureWithPaths(snapshot, lock.Deps, direct, "./a")
		if err != nil {
			return err
		}
		tree, err := owner.StageTree()
		if err != nil {
			return err
		}
		if err := snapshot.CopyDependencies(tree, retained); err != nil {
			return err
		}
		projectRoot, err := owner.OpenProjectRoot()
		if err != nil {
			return err
		}
		defer projectRoot.Close()
		var retainedDirect []project.LockDep
		for _, dep := range direct {
			if dep.Source == "./b" {
				retainedDirect = append(retainedDirect, dep)
			}
		}
		deps, err := SnapshotDependenciesTreeRetainingPaths(projectRoot, root, tree, []string{"./a"}, retained, retainedDirect, paths)
		if err != nil {
			return err
		}
		var old, fresh bool
		for _, dep := range deps {
			old = old || dep.ModuleID == "doggo:example/old-child"
			fresh = fresh || dep.ModuleID == "doggo:example/new-child"
		}
		if !old || !fresh {
			t.Fatalf("distinct contextual rename lost a child: %#v", deps)
		}
		return nil
	})
}

func TestSelectedSnapshotRetainsAbsentUnselectedLivePaths(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "doggo.mod"), `{:module "example/a" :deps []}`)
	writeFile(t, filepath.Join(root, "b", "doggo.mod"), `{:module "example/b" :deps ["./child"]}`)
	writeFile(t, filepath.Join(root, "b", "child", "doggo.mod"), `{:module "example/b-child" :deps []}`)
	mod, lock := publishRetainedFixture(t, root, []string{"./a", "./b"})
	if err := os.Rename(filepath.Join(root, "b"), filepath.Join(root, "absent-b")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "a", "value.dog"), "(def value 2)\n")
	deps, err := prepareRetainedTestSnapshot(t, root, root, mod, lock, "./a")
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range lock.Deps {
		if old.ModuleID == "doggo:example/a" {
			continue
		}
		found := false
		for _, dep := range deps {
			found = found || dep == old
		}
		if !found {
			t.Fatalf("absent unselected live path changed retained snapshot: old=%#v new=%#v", old, deps)
		}
	}
}

func TestSelectedSnapshotRejectsUnverifiableRetainedLivePath(t *testing.T) {
	for _, kind := range []string{"non-directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "a", "doggo.mod"), `{:module "example/a" :deps []}`)
			writeFile(t, filepath.Join(root, "b", "doggo.mod"), `{:module "example/b" :deps []}`)
			mod, lock := publishRetainedFixture(t, root, []string{"./a", "./b"})
			if err := os.Rename(filepath.Join(root, "b"), filepath.Join(root, "old-b")); err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				if err := os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "b")); err != nil {
					t.Skipf("directory symlink unavailable: %v", err)
				}
			} else {
				writeFile(t, filepath.Join(root, "b"), "not a directory\n")
			}
			_, err := prepareRetainedTestSnapshot(t, root, root, mod, lock, "./a")
			if err == nil || !strings.Contains(err.Error(), "retained path contains "+kind+" component") {
				t.Fatalf("unverifiable current path established retained alias: %v", err)
			}
		})
	}
}

func TestRetainedGoReplacementPreservesProvenChildContext(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "doggo.mod"), `{:module "example/a" :deps ["./shared"]}`)
	writeFile(t, filepath.Join(root, "outer", "b", "doggo.mod"), `{:module "example/b" :deps []}`)
	writeFile(t, filepath.Join(root, "outer", "b", "go.mod"), "module example.com/b\n\ngo 1.26\n\nreplace example.com/shared => ../../a/shared\n")
	writeFile(t, filepath.Join(root, "a", "shared", "doggo.mod"), `{:module "example/shared" :deps ["./other"]}`)
	writeFile(t, filepath.Join(root, "a", "shared", "go.mod"), "module example.com/shared\n\ngo 1.26\n")
	writeFile(t, filepath.Join(root, "a", "shared", "other", "doggo.mod"), `{:module "example/required-child" :deps []}`)
	writeFile(t, filepath.Join(root, "outer", "other", "doggo.mod"), `{:module "example/unrelated-direct" :deps []}`)
	mod, lock := publishRetainedFixture(t, root, []string{"./a", "./outer/b", "./outer/other"})
	withRetainedFixture(t, root, mod, lock, func(_ *project.GenerationOwner, snapshot *project.GenerationSnapshot) error {
		retained, err := RetainedDependencyClosure(snapshot, lock.Deps, retainedDirectDeps(t, lock, mod.Deps), "./a")
		if err != nil {
			return err
		}
		for _, dep := range retained {
			if dep.ModuleID == "doggo:example/required-child" {
				return nil
			}
		}
		t.Fatalf("retained Go replacement lost required child: %#v", retained)
		return nil
	})
}

func TestRetainedGoReplacementRejectsUnprovenEscapingChild(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "outer", "a", "doggo.mod"), `{:module "example/a" :deps ["../../other"]}`)
	writeFile(t, filepath.Join(root, "outer", "b", "doggo.mod"), `{:module "example/b" :deps []}`)
	writeFile(t, filepath.Join(root, "outer", "b", "go.mod"), "module example.com/b\n\ngo 1.26\n\nreplace example.com/shared => ../../shared\n")
	writeFile(t, filepath.Join(root, "shared", "doggo.mod"), `{:module "example/shared" :deps ["../other"]}`)
	writeFile(t, filepath.Join(root, "shared", "go.mod"), "module example.com/shared\n\ngo 1.26\n")
	writeFile(t, filepath.Join(root, "other", "doggo.mod"), `{:module "example/required-child" :deps []}`)
	writeFile(t, filepath.Join(root, "outer", "c", "doggo.mod"), `{:module "example/c" :deps ["../other"]}`)
	writeFile(t, filepath.Join(root, "outer", "other", "doggo.mod"), `{:module "example/unrelated-child" :deps []}`)
	mod, lock := publishRetainedFixture(t, root, []string{"./outer/a", "./outer/b", "./outer/c"})
	withRetainedFixture(t, root, mod, lock, func(_ *project.GenerationOwner, snapshot *project.GenerationSnapshot) error {
		retained, err := RetainedDependencyClosure(snapshot, lock.Deps, retainedDirectDeps(t, lock, mod.Deps), "./outer/a")
		if err != nil {
			if !strings.Contains(err.Error(), "unproven retained path context") {
				t.Fatalf("unproven path error = %v", err)
			}
			return nil
		}
		for _, dep := range retained {
			if dep.ModuleID == "doggo:example/required-child" {
				return nil
			}
		}
		t.Fatalf("unproven replacement context lost required child: %#v", retained)
		return nil
	})
}

func TestRetainedClosureKeepsDistinctAncestorContexts(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "outer", "project")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(base, "a", "doggo.mod"), `{:module "example/far-a" :deps ["../b"]}`)
	writeFile(t, filepath.Join(base, "outer", "a", "doggo.mod"), `{:module "example/near-a" :deps ["../b"]}`)
	writeFile(t, filepath.Join(base, "b", "doggo.mod"), `{:module "example/far-b" :deps []}`)
	writeFile(t, filepath.Join(base, "outer", "b", "doggo.mod"), `{:module "example/near-b" :deps []}`)
	mod, lock := publishRetainedFixture(t, root, []string{"../a", "../../a", "../b", "../../b"})
	withRetainedFixture(t, root, mod, lock, func(_ *project.GenerationOwner, snapshot *project.GenerationSnapshot) error {
		retained, err := RetainedDependencyClosure(snapshot, lock.Deps, retainedDirectDeps(t, lock, mod.Deps), "../../b")
		if err != nil {
			return err
		}
		for _, dep := range retained {
			if dep.ModuleID == "doggo:example/far-b" {
				return nil
			}
		}
		t.Fatalf("ancestor contexts lost retained far child: %#v", retained)
		return nil
	})
}

func TestRetainedProvenParentRejectsEarlierReplacementAlias(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "outer", "a", "doggo.mod"), `{:module "example/a" :deps []}`)
	writeFile(t, filepath.Join(root, "outer", "a", "go.mod"), "module example.com/a\n\ngo 1.26\n\nreplace example.com/other => ../../other\n")
	writeFile(t, filepath.Join(root, "other", "doggo.mod"), `{:module "example/required-child" :deps []}`)
	writeFile(t, filepath.Join(root, "other", "go.mod"), "module example.com/other\n\ngo 1.26\n")
	writeFile(t, filepath.Join(root, "shared", "doggo.mod"), `{:module "example/shared" :deps ["../other"]}`)
	writeFile(t, filepath.Join(root, "outer", "c", "doggo.mod"), `{:module "example/c" :deps ["../other"]}`)
	writeFile(t, filepath.Join(root, "outer", "other", "doggo.mod"), `{:module "example/unrelated-child" :deps []}`)
	mod, lock := publishRetainedFixture(t, root, []string{"./outer/a", "./shared", "./outer/c"})
	withRetainedFixture(t, root, mod, lock, func(_ *project.GenerationOwner, snapshot *project.GenerationSnapshot) error {
		retained, err := RetainedDependencyClosure(snapshot, lock.Deps, retainedDirectDeps(t, lock, mod.Deps), "./outer/a")
		if err != nil {
			if !strings.Contains(err.Error(), "unproven retained path context") {
				t.Fatalf("unproven path error = %v", err)
			}
			return nil
		}
		for _, dep := range retained {
			if dep.ModuleID == "doggo:example/required-child" {
				return nil
			}
		}
		t.Fatalf("proven parent guessed unrelated child: %#v", retained)
		return nil
	})
}

func TestRetainedGoReplacementRejectsDistinctProvenClosures(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		writeFile(t, filepath.Join(root, name, "doggo.mod"), `{:module "example/`+name+`" :deps ["./other" "./shared"]}`)
		writeFile(t, filepath.Join(root, name, "other", "doggo.mod"), `{:module "example/`+name+`-other" :deps []}`)
		writeFile(t, filepath.Join(root, name, "shared", "doggo.mod"), `{:module "example/shared" :deps ["../other"]}`)
		writeFile(t, filepath.Join(root, name, "shared", "go.mod"), "module example.com/shared\n\ngo 1.26\n")
	}
	writeFile(t, filepath.Join(root, "c", "doggo.mod"), `{:module "example/c" :deps []}`)
	writeFile(t, filepath.Join(root, "c", "go.mod"), "module example.com/c\n\ngo 1.26\n\nreplace example.com/shared => ../b/shared\n")
	mod, lock := publishRetainedFixture(t, root, []string{"./a", "./b", "./c"})
	withRetainedFixture(t, root, mod, lock, func(_ *project.GenerationOwner, snapshot *project.GenerationSnapshot) error {
		retained, err := RetainedDependencyClosure(snapshot, lock.Deps, retainedDirectDeps(t, lock, mod.Deps), "./a")
		if err == nil || !strings.Contains(err.Error(), "ambiguous retained replacement context") {
			t.Fatalf("replacement with distinct physical closures = %#v, %v", retained, err)
		}
		return nil
	})
}

func TestRetainedGoReplacementReusesCanonicalContext(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "doggo.mod"), `{:module "example/a" :deps ["./shared"]}`)
	writeFile(t, filepath.Join(root, "b", "doggo.mod"), `{:module "example/b" :deps ["../a/./shared"]}`)
	writeFile(t, filepath.Join(root, "a", "shared", "doggo.mod"), `{:module "example/shared" :deps ["./other"]}`)
	writeFile(t, filepath.Join(root, "a", "shared", "go.mod"), "module example.com/shared\n\ngo 1.26\n")
	writeFile(t, filepath.Join(root, "a", "shared", "other", "doggo.mod"), `{:module "example/required-child" :deps []}`)
	writeFile(t, filepath.Join(root, "c", "doggo.mod"), `{:module "example/c" :deps []}`)
	writeFile(t, filepath.Join(root, "c", "go.mod"), "module example.com/c\n\ngo 1.26\n\nreplace example.com/shared => ../a/shared\n")
	mod, lock := publishRetainedFixture(t, root, []string{"./a", "./b", "./c"})
	withRetainedFixture(t, root, mod, lock, func(_ *project.GenerationOwner, snapshot *project.GenerationSnapshot) error {
		retained, err := RetainedDependencyClosure(snapshot, lock.Deps, retainedDirectDeps(t, lock, mod.Deps), "./a")
		if err != nil {
			return err
		}
		if len(retained) != 4 {
			t.Fatalf("canonical replacement closure = %#v", retained)
		}
		for _, dep := range retained {
			if dep.ModuleID == "doggo:example/required-child" {
				return nil
			}
		}
		t.Fatalf("canonical replacement lost required child: %#v", retained)
		return nil
	})
}

func publishRetainedFixture(t *testing.T, root string, sources []string) (*project.ModFile, *project.LockFile) {
	t.Helper()
	mod := &project.ModFile{Module: "example/root", Deps: sources}
	if err := project.WriteModFile(project.ModPath(root), mod); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(root, ".doggo", "generations", "fixture")
	deps, err := SnapshotDependencies(root, stage, sources)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(stage, "toolchain"), 0755); err != nil {
		t.Fatal(err)
	}
	lock := &project.LockFile{LockVersion: project.LockVersion, ModLockInputsSHA256: project.HashModLockInputs(mod), Deps: deps}
	lock.Generation, err = project.GenerationDigest(stage, lock)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(stage, ".generation"), lock.Generation+"\n")
	if err := os.Rename(stage, filepath.Join(filepath.Dir(stage), lock.Generation)); err != nil {
		t.Fatal(err)
	}
	return mod, lock
}

func retainedDirectDeps(t *testing.T, lock *project.LockFile, sources []string) []project.LockDep {
	t.Helper()
	var direct []project.LockDep
	for _, source := range sources {
		var matches []project.LockDep
		for _, dep := range lock.Deps {
			if dep.Source == source {
				matches = append(matches, dep)
			}
		}
		dep, err := uniqueRetainedChild(matches, source)
		if err != nil {
			t.Fatal(err)
		}
		direct = append(direct, dep)
	}
	return direct
}

func withRetainedFixture(t *testing.T, root string, mod *project.ModFile, lock *project.LockFile, use func(*project.GenerationOwner, *project.GenerationSnapshot) error) {
	t.Helper()
	owner, err := project.BeginGenerationOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	defer owner.RemoveStage()
	if err := owner.WithPublishedSnapshot(mod, lock, func(snapshot *project.GenerationSnapshot) error { return use(owner, snapshot) }); err != nil {
		t.Fatal(err)
	}
}

func prepareRetainedTestSnapshot(t *testing.T, root, projectAlias string, mod *project.ModFile, lock *project.LockFile, selected string) ([]project.LockDep, error) {
	t.Helper()
	owner, err := project.BeginGenerationOwner(root)
	if err != nil {
		return nil, err
	}
	defer owner.Close()
	defer owner.RemoveStage()
	var deps []project.LockDep
	err = owner.WithPublishedSnapshot(mod, lock, func(snapshot *project.GenerationSnapshot) error {
		direct := retainedDirectDeps(t, lock, mod.Deps)
		retained, paths, err := RetainedDependencyClosureWithPaths(snapshot, lock.Deps, direct, selected)
		if err != nil {
			return err
		}
		tree, err := owner.StageTree()
		if err != nil {
			return err
		}
		if err := snapshot.CopyDependencies(tree, retained); err != nil {
			return err
		}
		projectRoot, err := owner.OpenProjectRoot()
		if err != nil {
			return err
		}
		defer projectRoot.Close()
		var retainedDirect []project.LockDep
		for _, dep := range direct {
			if dep.Source != selected {
				retainedDirect = append(retainedDirect, dep)
			}
		}
		deps, err = SnapshotDependenciesTreeRetainingPaths(projectRoot, projectAlias, tree, []string{selected}, retained, retainedDirect, paths)
		return err
	})
	return deps, err
}
