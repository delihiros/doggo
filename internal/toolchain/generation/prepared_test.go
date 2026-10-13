package generation

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/testutil"
)

func TestPreparedGenerationCommitClosesPublishedGeneration(t *testing.T) {
	p := newPreparedFixture(t)
	if p.state != statePreparing {
		t.Fatalf("initial state = %v, want preparing", p.state)
	}

	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	if p.state != stateClosed {
		t.Fatalf("state = %v, want closed", p.state)
	}
	loaded, err := project.LoadLockFile(project.LockPath(p.projectRoot))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := project.LoadModFile(project.ModPath(p.projectRoot))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation != p.Lock.Generation || !project.IsLowerHexDigest(loaded.Generation) {
		t.Fatalf("committed generation = %q, prepared = %q", loaded.Generation, p.Lock.Generation)
	}
	if err := project.ValidateGeneration(p.projectRoot, mod, loaded); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit(); err != nil {
		t.Fatalf("repeated Commit = %v, want prior success", err)
	}
	if err := p.Abort(); err != nil {
		t.Fatalf("Abort after Commit = %v", err)
	}
}

func TestPreparedGenerationBuildOnlyWhilePreparing(t *testing.T) {
	p := newPreparedFixture(t)

	if err := p.Build(); err != nil {
		t.Fatal(err)
	}
	if p.Lock.Build.Binary != "" || !project.IsLowerHexDigest(p.Lock.Build.SHA256) {
		t.Fatalf("tentative build = %#v", p.Lock.Build)
	}
	if err := p.Abort(); err != nil {
		t.Fatal(err)
	}
	if err := p.Build(); err == nil {
		t.Fatal("Build after Abort succeeded")
	}
	if err := p.Commit(); err == nil {
		t.Fatal("Commit after Abort succeeded")
	}
	if err := p.Abort(); err != nil {
		t.Fatalf("repeated Abort = %v", err)
	}
}

func TestPreparedGenerationPreRenameLockFailureCanAbortPublication(t *testing.T) {
	p := newPreparedFixture(t)
	want := errors.New("lock temp write failed")
	original := writeGenerationLock
	writeGenerationLock = func(*project.GenerationOwner, *project.LockFile) error { return want }
	t.Cleanup(func() { writeGenerationLock = original })

	err := p.Commit()
	if !errors.Is(err, want) || p.state != statePublished {
		t.Fatalf("Commit = (%v, state %v), want published pre-rename failure", err, p.state)
	}
	published := filepath.Join(p.projectRoot, ".doggo", "generations", p.Lock.Generation)
	if _, err := os.Lstat(published); err != nil {
		t.Fatalf("published generation missing: %v", err)
	}
	if err := p.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(published); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Abort left unreferenced publication: %v", err)
	}
}

func TestPreparedGenerationPreRenameLockFailureCanRetryCommit(t *testing.T) {
	p := newPreparedFixture(t)
	want := errors.New("lock temp write failed")
	writes := 0
	publishes := 0
	original := writeGenerationLock
	originalPublish := publishGeneration
	publishGeneration = func(owner *project.GenerationOwner, generation string) (bool, error) {
		publishes++
		return owner.PublishStage(generation)
	}
	writeGenerationLock = func(owner *project.GenerationOwner, lock *project.LockFile) error {
		writes++
		if writes == 1 {
			return want
		}
		return owner.WriteLock(lock)
	}
	t.Cleanup(func() {
		writeGenerationLock = original
		publishGeneration = originalPublish
	})

	if err := p.Commit(); !errors.Is(err, want) || p.state != statePublished {
		t.Fatalf("first Commit = (%v, state %v), want published failure", err, p.state)
	}
	if err := p.Commit(); err != nil {
		t.Fatalf("retry Commit = %v", err)
	}
	if writes != 2 || publishes != 1 || p.state != stateClosed {
		t.Fatalf("retry writes = %d, publishes = %d, state = %v", writes, publishes, p.state)
	}
}

func TestPreparedGenerationParentSyncFailureRetriesWithoutRepublishing(t *testing.T) {
	p := newPreparedFixture(t)
	want := errors.New("generation parent sync failed")
	original := publishGeneration
	originalSync := syncPublished
	publishes := 0
	syncs := 0
	publishGeneration = func(owner *project.GenerationOwner, generation string) (bool, error) {
		publishes++
		renamed, err := owner.PublishStage(generation)
		if err != nil {
			return renamed, err
		}
		return true, &project.AtomicWriteError{Renamed: true, Err: want}
	}
	syncPublished = func(owner *project.GenerationOwner, generation string) error {
		syncs++
		return owner.SyncPublished(generation)
	}
	t.Cleanup(func() {
		publishGeneration = original
		syncPublished = originalSync
	})

	err := p.Commit()
	if !errors.Is(err, want) || p.state != statePublished {
		t.Fatalf("Commit = (%v, state %v), want published sync failure", err, p.state)
	}
	if err := p.Commit(); err != nil {
		t.Fatalf("retry Commit = %v", err)
	}
	if publishes != 1 || syncs != 1 || p.state != stateClosed {
		t.Fatalf("retry publishes = %d, syncs = %d, state = %v", publishes, syncs, p.state)
	}
}

func TestPreparedGenerationAbortCleansPublicationAfterPublisherCommitError(t *testing.T) {
	for _, borrowed := range []bool{false, true} {
		name := "owned"
		fixture := newPreparedFixture
		if borrowed {
			name = "borrowed"
			fixture = newBorrowedPreparedFixture
		}
		t.Run(name, func(t *testing.T) {
			p := fixture(t)
			want := errors.New("publisher failed after rename")
			original := publishGeneration
			publishGeneration = func(owner *project.GenerationOwner, generation string) (bool, error) {
				renamed, err := owner.PublishStage(generation)
				if err != nil {
					return renamed, err
				}
				return true, &project.AtomicWriteError{Renamed: true, Err: want}
			}
			t.Cleanup(func() { publishGeneration = original })

			if err := p.Commit(); !errors.Is(err, want) || p.state != statePublished {
				t.Fatalf("Commit = (%v, state %v), want published commit-point error", err, p.state)
			}
			published := filepath.Join(p.projectRoot, ".doggo", "generations", p.Lock.Generation)
			assertGenerationExists(t, p.projectRoot, p.Lock.Generation)
			if err := p.Abort(); err != nil {
				t.Fatalf("Abort after publisher commit error: %v", err)
			}
			if _, err := os.Lstat(published); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("Abort left publication: %v", err)
			}
		})
	}
}

func TestPreparedGenerationReusedPostRenameOrphanIsSyncedBeforeCommit(t *testing.T) {
	first := newPreparedFixture(t)
	want := errors.New("generation parent sync failed")
	originalPublish := publishGeneration
	publishGeneration = func(owner *project.GenerationOwner, generation string) (bool, error) {
		renamed, err := owner.PublishStage(generation)
		if err != nil {
			return renamed, err
		}
		return true, &project.AtomicWriteError{Renamed: true, Err: want}
	}
	if err := first.Commit(); !errors.Is(err, want) {
		t.Fatalf("orphan publication = %v, want %v", err, want)
	}
	orphan := first.Lock.Generation
	if err := first.close(); err != nil {
		t.Fatal(err)
	}
	publishGeneration = originalPublish

	originalSync := syncPublished
	syncs := 0
	syncPublished = func(owner *project.GenerationOwner, generation string) error {
		syncs++
		if generation != orphan {
			t.Fatalf("synced generation = %q, want orphan %q", generation, orphan)
		}
		return owner.SyncPublished(generation)
	}
	t.Cleanup(func() {
		publishGeneration = originalPublish
		syncPublished = originalSync
	})

	second := newPreparedFixtureAt(t, first.projectRoot)
	if err := second.Commit(); err != nil {
		t.Fatal(err)
	}
	if syncs != 1 {
		t.Fatalf("orphan reuse syncs = %d, want 1", syncs)
	}
}

func TestPreparedGenerationPostLockRenameUncertaintyIsTerminal(t *testing.T) {
	p := newPreparedFixture(t)
	want := errors.New("project directory sync failed")
	original := writeGenerationLock
	writeGenerationLock = func(owner *project.GenerationOwner, lock *project.LockFile) error {
		if err := owner.WriteLock(lock); err != nil {
			return err
		}
		return &project.CommitUncertainError{Generation: lock.Generation, Cause: want}
	}
	t.Cleanup(func() { writeGenerationLock = original })

	err := p.Commit()
	var uncertain *project.CommitUncertainError
	if !errors.As(err, &uncertain) || !errors.Is(err, want) || p.state != stateClosed {
		t.Fatalf("Commit = (%T %v, state %v), want closed uncertainty", err, err, p.state)
	}
	if retry := p.Commit(); retry != err {
		t.Fatalf("repeated Commit = %T %v, want stored result %T %v", retry, retry, err, err)
	}
	published := filepath.Join(p.projectRoot, ".doggo", "generations", p.Lock.Generation)
	if err := p.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(published); err != nil {
		t.Fatalf("Abort removed uncertain committed generation: %v", err)
	}
}

func TestPreparedGenerationCommitRetryAcceptsExactMarker(t *testing.T) {
	p := newPreparedFixture(t)
	want := errors.New("tree sync failed")
	original := syncGenerationTree
	calls := 0
	syncGenerationTree = func(owner *project.GenerationOwner) error {
		calls++
		if calls == 1 {
			return want
		}
		return owner.SyncStage()
	}
	t.Cleanup(func() { syncGenerationTree = original })

	if err := p.Commit(); !errors.Is(err, want) {
		t.Fatalf("first Commit = %v, want %v", err, want)
	}
	if err := p.Commit(); err != nil {
		t.Fatalf("retry Commit with exact marker = %v", err)
	}
}

func TestPreparedGenerationCommitRetryRejectsMismatchedMarker(t *testing.T) {
	p := newPreparedFixture(t)
	want := errors.New("tree sync failed")
	original := syncGenerationTree
	syncGenerationTree = func(*project.GenerationOwner) error { return want }
	t.Cleanup(func() { syncGenerationTree = original })

	if err := p.Commit(); !errors.Is(err, want) {
		t.Fatalf("first Commit = %v, want %v", err, want)
	}
	if err := os.WriteFile(filepath.Join(p.stage, ".generation"), []byte("wrong\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit(); err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("retry Commit error = %v, want marker mismatch", err)
	}
}

func TestPreparedGenerationCommitRetryRejectsSymlinkMarker(t *testing.T) {
	p := newPreparedFixture(t)
	want := errors.New("tree sync failed")
	original := syncGenerationTree
	syncGenerationTree = func(*project.GenerationOwner) error { return want }
	t.Cleanup(func() { syncGenerationTree = original })

	if err := p.Commit(); !errors.Is(err, want) {
		t.Fatalf("first Commit = %v, want %v", err, want)
	}
	marker := filepath.Join(p.stage, ".generation")
	content, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(target, content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, marker); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := p.Commit(); !errors.Is(err, project.ErrOwnershipChanged) {
		t.Fatalf("retry Commit error = %v, want marker ownership rejection", err)
	}
}

func TestPreparedGenerationBuildRejectsNilLock(t *testing.T) {
	p := newPreparedFixture(t)
	p.Lock = nil
	if err := p.Build(); err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("Build error = %v, want nil lock rejection", err)
	}
}

func TestPreparedGenerationRejectsInvalidTentativeLockBeforePublication(t *testing.T) {
	p := newPreparedFixture(t)
	p.Lock.Build.Binary = "arbitrary/binary"

	if err := p.Commit(); err == nil || !strings.Contains(err.Error(), "tentative") {
		t.Fatalf("Commit error = %v, want tentative lock rejection", err)
	}
	if p.state != statePreparing {
		t.Fatalf("state = %v, want preparing", p.state)
	}
	assertNoDigestGenerations(t, p.projectRoot)
	if err := p.Abort(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedGenerationRejectsPublicMutationAfterSeal(t *testing.T) {
	p := newPreparedFixture(t)
	want := errors.New("tree sync failed")
	original := syncGenerationTree
	calls := 0
	syncGenerationTree = func(owner *project.GenerationOwner) error {
		calls++
		if calls == 1 {
			return want
		}
		return owner.SyncStage()
	}
	t.Cleanup(func() { syncGenerationTree = original })

	if err := p.Commit(); !errors.Is(err, want) {
		t.Fatalf("first Commit = %v, want %v", err, want)
	}
	p.Lock.Scan.GoPackages = []string{"example.com/switched"}
	if err := p.Commit(); err == nil || !strings.Contains(err.Error(), "changed after seal") {
		t.Fatalf("retry Commit = %v, want sealed lock divergence", err)
	}
	assertNoDigestGenerations(t, p.projectRoot)
}

func TestPreparedGenerationAbortRejectsReplacedGenerationsRoot(t *testing.T) {
	p := newPreparedFixture(t)
	generations := filepath.Join(p.projectRoot, ".doggo", "generations")
	original := filepath.Join(p.projectRoot, ".doggo", "owned-generations")
	outside := t.TempDir()
	if !testutil.RenameRetained(t, generations, original) {
		if err := p.Abort(); err != nil {
			t.Fatalf("abort after blocked generations replacement: %v", err)
		}
		if _, err := os.Lstat(p.stage); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("owned stage remains after abort: %v", err)
		}
		return
	}
	if err := os.Symlink(outside, generations); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := p.Abort()
	if err == nil || !strings.Contains(err.Error(), "generations") {
		t.Fatalf("Abort error = %v, want replaced-root rejection", err)
	}
	outsideEntries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(outsideEntries) != 0 {
		t.Fatalf("cleanup escaped to replacement root: %v", outsideEntries)
	}
	if _, err := os.Lstat(filepath.Join(original, filepath.Base(p.stage))); err != nil {
		t.Fatalf("owned original stage was not safely retained: %v", err)
	}
	if retry := p.Abort(); retry != err {
		t.Fatalf("repeated Abort = %T %v, want stored %T %v", retry, retry, err, err)
	}
}

func TestPreparedGenerationCommitRejectsOrdinaryIdentityReplacement(t *testing.T) {
	for _, target := range []string{"project", "generations", "stage"} {
		t.Run(target, func(t *testing.T) {
			p := newPreparedFixture(t)
			replacementStage, cleanup := replacePreparedIdentity(t, p, target)
			t.Cleanup(cleanup)
			if replacementStage == "" {
				if err := p.Commit(); err != nil {
					t.Fatalf("commit after blocked %s replacement: %v", target, err)
				}
				assertGenerationExists(t, p.projectRoot, p.Lock.Generation)
				return
			}

			if err := p.Commit(); err == nil {
				t.Fatalf("Commit accepted replaced %s identity", target)
			}
			if content, err := os.ReadFile(filepath.Join(replacementStage, "sentinel")); err != nil || string(content) != "replacement" {
				t.Fatalf("replacement stage changed after Commit: %q, %v", content, err)
			}
		})
	}
}

func TestPreparedGenerationAbortNeverDeletesSameNamedReplacement(t *testing.T) {
	for _, target := range []string{"project", "generations", "stage"} {
		t.Run(target, func(t *testing.T) {
			p := newPreparedFixture(t)
			replacementStage, cleanup := replacePreparedIdentity(t, p, target)
			t.Cleanup(cleanup)
			if replacementStage == "" {
				if err := p.Abort(); err != nil {
					t.Fatalf("abort after blocked %s replacement: %v", target, err)
				}
				if _, err := os.Lstat(p.stage); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("owned stage remains after abort: %v", err)
				}
				return
			}

			if err := p.Abort(); err == nil {
				t.Fatalf("Abort accepted replaced %s identity", target)
			}
			if content, err := os.ReadFile(filepath.Join(replacementStage, "sentinel")); err != nil || string(content) != "replacement" {
				t.Fatalf("Abort deleted same-named replacement: %q, %v", content, err)
			}
		})
	}
}

func TestPreparedGenerationReusesOnlyValidGeneration(t *testing.T) {
	first := newPreparedFixture(t)
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	generation := first.Lock.Generation

	second := newPreparedFixtureAt(t, first.projectRoot)
	stage := second.stage
	if err := second.Commit(); err != nil {
		t.Fatalf("valid reuse: %v", err)
	}
	if second.Lock.Generation != generation {
		t.Fatalf("reused generation = %q, want %q", second.Lock.Generation, generation)
	}
	if _, err := os.Lstat(stage); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("valid reuse left stage: %v", err)
	}

	if err := os.WriteFile(filepath.Join(first.projectRoot, ".doggo", "generations", generation, "toolchain", "main.go"), []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}
	third := newPreparedFixtureAt(t, first.projectRoot)
	if err := third.Commit(); err == nil || !strings.Contains(err.Error(), "stale or corrupt") {
		t.Fatalf("corrupt reuse error = %v", err)
	}
	if third.state != statePreparing {
		t.Fatalf("corrupt reuse state = %v, want preparing", third.state)
	}
	if err := third.Abort(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedGenerationAbortNeverDeletesReusedGeneration(t *testing.T) {
	first := newPreparedFixture(t)
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	published := filepath.Join(first.projectRoot, ".doggo", "generations", first.Lock.Generation)
	if err := os.Remove(project.LockPath(first.projectRoot)); err != nil {
		t.Fatal(err)
	}

	second := newPreparedFixtureAt(t, first.projectRoot)
	want := errors.New("lock write failed")
	original := writeGenerationLock
	writeGenerationLock = func(*project.GenerationOwner, *project.LockFile) error { return want }
	t.Cleanup(func() { writeGenerationLock = original })
	if err := second.Commit(); !errors.Is(err, want) {
		t.Fatalf("Commit error = %v, want %v", err, want)
	}
	if err := second.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(published); err != nil {
		t.Fatalf("Abort deleted reused generation: %v", err)
	}
}

func TestPreparedGenerationAbortUsesPrivatePublishedOwnership(t *testing.T) {
	p, generationA, lockB, _, _ := publishedAWithUnreferencedB(t)
	p.Lock = cloneLock(lockB)

	if err := p.Abort(); err != nil {
		t.Fatal(err)
	}
	assertGenerationExists(t, p.projectRoot, lockB.Generation)
	assertGenerationMissing(t, p.projectRoot, generationA)
}

func TestPreparedGenerationRetryRejectsPublicGenerationSwitch(t *testing.T) {
	p, generationA, lockB, allowLock, publishCount := publishedAWithUnreferencedB(t)
	p.Lock.Generation = lockB.Generation
	allowLock()

	err := p.Commit()
	if err == nil || !strings.Contains(err.Error(), "changed after publication") {
		t.Fatalf("Commit error = %v, want public lock divergence rejection", err)
	}
	if publishCount() != 1 {
		t.Fatalf("Commit republished after identity switch: %d publications", publishCount())
	}
	if _, err := os.Lstat(project.LockPath(p.projectRoot)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Commit wrote switched lock: %v", err)
	}
	assertGenerationExists(t, p.projectRoot, generationA)
	assertGenerationExists(t, p.projectRoot, lockB.Generation)

	if err := p.Abort(); err != nil {
		t.Fatal(err)
	}
	assertGenerationMissing(t, p.projectRoot, generationA)
	assertGenerationExists(t, p.projectRoot, lockB.Generation)
}

func newPreparedFixture(t *testing.T) *PreparedGeneration {
	t.Helper()
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	return newPreparedFixtureAt(t, root)
}

func newPreparedFixtureAt(t *testing.T, root string) *PreparedGeneration {
	t.Helper()
	owner, err := project.BeginGenerationOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	return newPreparedFixtureWithOwner(t, root, owner)
}

func newBorrowedPreparedFixture(t *testing.T) *PreparedGeneration {
	t.Helper()
	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	tree := project.NewOwnedTree(root)
	t.Cleanup(func() { _ = tree.Close() })
	if err := tree.WriteFile(project.ModFileName, []byte(`{:module "example/project" :deps []}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	owner, err := project.BeginGenerationOwnerTree(root, rootPath, tree)
	if err != nil {
		t.Fatal(err)
	}
	return newPreparedFixtureWithOwner(t, rootPath, owner)
}

func newPreparedFixtureWithOwner(t *testing.T, root string, owner *project.GenerationOwner) *PreparedGeneration {
	t.Helper()
	t.Cleanup(func() { _ = owner.Close() })
	stage := owner.StagePath()
	tree, err := owner.StageTree()
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"deps", "toolchain"} {
		root, err := tree.CreateRoot(directory, 0755, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := root.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := tree.WriteFile("toolchain/main.go", []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := tree.WriteFile("toolchain/go.mod", []byte("module example.invalid/runtime\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := tree.CreateAll("toolchain/cmd/doggo", 0755); err != nil {
		t.Fatal(err)
	}
	if err := tree.WriteFile("toolchain/cmd/doggo/main.go", []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mod, err := owner.LoadMod()
	if err != nil {
		t.Fatal(err)
	}
	return &PreparedGeneration{
		Lock: &project.LockFile{
			LockVersion:         project.LockVersion,
			ModLockInputsSHA256: project.HashModLockInputs(mod),
			Deps:                []project.LockDep{},
			Scan: project.LockScan{
				GoPackages:   []string{},
				ReifyTargets: []project.ReifyTarget{},
			},
		},
		projectRoot: root,
		stage:       stage,
		owner:       owner,
		mod:         mod,
		state:       statePreparing,
	}
}

func publishedAWithUnreferencedB(t *testing.T) (*PreparedGeneration, string, *project.LockFile, func(), func() int) {
	t.Helper()
	first := newPreparedFixture(t)
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	lockB := cloneLock(first.Lock)
	if err := os.Remove(project.LockPath(first.projectRoot)); err != nil {
		t.Fatal(err)
	}

	p := newPreparedFixtureAt(t, first.projectRoot)
	if err := os.WriteFile(filepath.Join(p.stage, "toolchain", "main.go"), []byte("package main\n// generation A\n"), 0644); err != nil {
		t.Fatal(err)
	}
	failLock := true
	publishes := 0
	want := errors.New("lock write failed")
	originalWrite := writeGenerationLock
	originalPublish := publishGeneration
	writeGenerationLock = func(owner *project.GenerationOwner, lock *project.LockFile) error {
		if failLock {
			return want
		}
		return owner.WriteLock(lock)
	}
	publishGeneration = func(owner *project.GenerationOwner, generation string) (bool, error) {
		publishes++
		return owner.PublishStage(generation)
	}
	t.Cleanup(func() {
		writeGenerationLock = originalWrite
		publishGeneration = originalPublish
	})
	if err := p.Commit(); !errors.Is(err, want) || p.state != statePublished {
		t.Fatalf("publish generation A = (%v, state %v)", err, p.state)
	}
	return p, p.Lock.Generation, lockB, func() { failLock = false }, func() int { return publishes }
}

func assertGenerationExists(t *testing.T, root, generation string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, ".doggo", "generations", generation)); err != nil {
		t.Fatalf("generation %s missing: %v", generation, err)
	}
}

func assertGenerationMissing(t *testing.T, root, generation string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root, ".doggo", "generations", generation)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("generation %s remains: %v", generation, err)
	}
}

func assertNoDigestGenerations(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".doggo", "generations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if project.IsLowerHexDigest(entry.Name()) {
			t.Fatalf("unexpected published generation %s", entry.Name())
		}
	}
}

func replacePreparedIdentity(t *testing.T, p *PreparedGeneration, target string) (string, func()) {
	t.Helper()
	root := p.projectRoot
	generations := filepath.Join(root, ".doggo", "generations")
	stageName := filepath.Base(p.stage)
	cleanupPaths := []string{}
	switch target {
	case "project":
		owned := root + "-owned"
		if !testutil.RenameRetained(t, root, owned) {
			return "", func() {}
		}
		cleanupPaths = append(cleanupPaths, owned)
		generations = filepath.Join(root, ".doggo", "generations")
	case "generations":
		owned := filepath.Join(root, ".doggo", "owned-generations")
		if !testutil.RenameRetained(t, generations, owned) {
			return "", func() {}
		}
		cleanupPaths = append(cleanupPaths, owned)
	case "stage":
		owned := p.stage + "-owned"
		if !testutil.RenameRetained(t, p.stage, owned) {
			return "", func() {}
		}
		cleanupPaths = append(cleanupPaths, owned)
	default:
		t.Fatalf("unknown replacement target %q", target)
	}
	replacement := filepath.Join(generations, stageName)
	if err := os.MkdirAll(replacement, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacement, "sentinel"), []byte("replacement"), 0644); err != nil {
		t.Fatal(err)
	}
	return replacement, func() {
		for _, path := range cleanupPaths {
			_ = os.RemoveAll(path)
		}
	}
}
