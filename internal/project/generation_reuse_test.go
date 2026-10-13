package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriterSnapshotCopiesOnlyDependenciesAndExpires(t *testing.T) {
	root, mod, lock := writeValidGeneration(t, true)
	owner, err := BeginGenerationOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	defer owner.RemoveStage()
	var retained *GenerationSnapshot
	if err := owner.WithPublishedSnapshot(mod, lock, func(snapshot *GenerationSnapshot) error {
		retained = snapshot
		content, err := snapshot.ReadFile(lock.Deps[0].InstallDir + "/lib/core.dog")
		if err != nil || string(content) != "(ns core)\n" {
			t.Fatalf("snapshot content = %q, %v", content, err)
		}
		tree, err := owner.StageTree()
		if err != nil {
			return err
		}
		return snapshot.CopyDependencies(tree, lock.Deps)
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bin", "toolchain", ".generation"} {
		if _, err := os.Stat(filepath.Join(owner.StagePath(), name)); !os.IsNotExist(err) {
			t.Fatalf("copied unrelated generation path %s: %v", name, err)
		}
	}
	if _, err := retained.ReadFile(lock.Deps[0].InstallDir + "/lib/core.dog"); err == nil {
		t.Fatal("expired writer snapshot remained readable")
	}
}

func TestWriterSnapshotKeepsExclusiveLeaseDuringAndAfterReuse(t *testing.T) {
	root, mod, lock := writeValidGeneration(t, false)
	owner, err := BeginGenerationOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	defer owner.RemoveStage()
	readers := make(chan *GenerationLease, 1)
	errs := make(chan error, 1)
	err = owner.WithPublishedSnapshot(mod, lock, func(*GenerationSnapshot) error {
		go func() {
			lease, err := AcquireGenerationLease(root, false)
			if err != nil {
				errs <- err
				return
			}
			readers <- lease
		}()
		select {
		case lease := <-readers:
			lease.Close()
			t.Fatal("snapshot reuse released its writer lease")
		case err := <-errs:
			t.Fatal(err)
		case <-time.After(150 * time.Millisecond):
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case lease := <-readers:
		lease.Close()
		t.Fatal("writer lease released after snapshot callback")
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := owner.RemoveStage(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case lease := <-readers:
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("reader remained blocked after writer close")
	}
}

func TestWriterSnapshotRejectsCorruptPreviousGeneration(t *testing.T) {
	root, mod, lock := writeValidGeneration(t, false)
	if err := os.WriteFile(filepath.Join(generationPath(root, lock.Generation), filepath.FromSlash(lock.Deps[0].InstallDir), "lib", "core.dog"), []byte("tampered\n"), 0644); err != nil {
		t.Fatal(err)
	}
	owner, err := BeginGenerationOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	defer owner.RemoveStage()
	called := false
	err = owner.WithPublishedSnapshot(mod, lock, func(*GenerationSnapshot) error { called = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") || called {
		t.Fatalf("corrupt snapshot access = %v, callback=%t", err, called)
	}
}

func TestWriterSnapshotRejectsContentChangedDuringReuse(t *testing.T) {
	root, mod, lock := writeValidGeneration(t, false)
	owner, err := BeginGenerationOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	defer owner.RemoveStage()
	err = owner.WithPublishedSnapshot(mod, lock, func(snapshot *GenerationSnapshot) error {
		name := lock.Deps[0].InstallDir + "/lib/core.dog"
		if err := os.WriteFile(filepath.Join(generationPath(root, lock.Generation), filepath.FromSlash(name)), []byte("tampered\n"), 0644); err != nil {
			return err
		}
		if _, err := snapshot.ReadFile(name); err == nil {
			t.Fatal("writer snapshot read changed content")
		}
		tree, err := owner.StageTree()
		if err != nil {
			return err
		}
		return snapshot.CopyDependencies(tree, lock.Deps)
	})
	if err == nil {
		t.Fatal("writer snapshot copied changed content")
	}
}

func TestWriterSnapshotRejectsSourceFileReplacement(t *testing.T) {
	root, mod, lock := writeValidGeneration(t, false)
	owner, err := BeginGenerationOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	defer owner.RemoveStage()
	name := lock.Deps[0].InstallDir + "/lib/core.dog"
	err = owner.WithPublishedSnapshot(mod, lock, func(snapshot *GenerationSnapshot) error {
		original := filepath.Join(generationPath(root, lock.Generation), filepath.FromSlash(name))
		if err := os.Rename(original, original+"-old"); err != nil {
			return err
		}
		if err := os.WriteFile(original, []byte("replacement"), 0644); err != nil {
			return err
		}
		if _, err := snapshot.ReadFile(name); !errors.Is(err, ErrOwnershipChanged) {
			t.Fatalf("replacement read error = %v", err)
		}
		tree, err := owner.StageTree()
		if err != nil {
			return err
		}
		return snapshot.CopyDependencies(tree, lock.Deps)
	})
	if !errors.Is(err, ErrOwnershipChanged) {
		t.Fatalf("replacement copy error = %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(owner.StagePath(), filepath.FromSlash(name))); err == nil && string(content) == "replacement" {
		t.Fatal("replacement source bytes copied")
	}
}
