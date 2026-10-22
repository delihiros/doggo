package generation

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"doggo/internal/project"
)

func TestRepeatedCommitsPreserveActiveGenerationReaders(t *testing.T) {
	first := newPreparedFixture(t)
	root := first.projectRoot
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	var readers []*project.GenerationSession
	t.Cleanup(func() {
		for _, r := range readers {
			_ = r.Close()
		}
	})
	for i := range 3 {
		reader, err := project.OpenGenerationSession(project.ModPath(root))
		if err != nil {
			t.Fatal(err)
		}
		readers = append(readers, reader)
		type result struct {
			owner *project.GenerationOwner
			err   error
		}
		done := make(chan result, 1)
		go func() { o, err := project.BeginGenerationOwner(root); done <- result{o, err} }()
		var owner *project.GenerationOwner
		select {
		case r := <-done:
			if r.err != nil {
				t.Fatal(r.err)
			}
			owner = r.owner
		case <-time.After(3 * time.Second):
			for _, r := range readers {
				_ = r.Close()
			}
			r := <-done
			if r.owner != nil {
				_ = r.owner.RemoveStage()
				_ = r.owner.Close()
			}
			t.Fatal("active generation readers blocked another commit")
		}
		next := newPreparedFixtureWithOwner(t, root, owner)
		tree, err := owner.StageTree()
		if err != nil {
			t.Fatal(err)
		}
		if err := tree.WriteOwnedFile("toolchain/main.go", []byte(fmt.Sprintf("package main\n// generation %d\n", i)), 0644); err != nil {
			t.Fatal(err)
		}
		if err := next.Commit(); err != nil {
			t.Fatal(err)
		}
		if next.Lock.Generation == reader.Config.Lock.Generation {
			t.Fatal("commit did not select a new generation")
		}
		for _, r := range readers {
			// Copying validates retained directory/file identities and hashes,
			// including generations no longer selected by doggo.lock.
			destination, err := project.NewOwnedTempRoot("reader-update-")
			if err != nil {
				t.Fatal(err)
			}
			copyErr := r.CopyAuditWorkspaceTo(destination.Tree())
			closeErr := destination.Close()
			if copyErr != nil || closeErr != nil {
				t.Fatalf("old snapshot after update: %v / %v", copyErr, closeErr)
			}
		}
		if stages, err := filepath.Glob(filepath.Join(root, ".doggo", "generations", ".stage-*")); err != nil || len(stages) != 0 {
			t.Fatalf("stages remain: %v / %v", stages, err)
		}
	}
}
