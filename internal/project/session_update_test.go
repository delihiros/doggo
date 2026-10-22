package project

import (
	"path/filepath"
	"testing"
	"time"
)

// Readers must pin their immutable generation, not the publication lock used
// by every later build/sync transaction.
func TestSessionAllowsRepeatedGenerationOwners(t *testing.T) {
	root, _, lock := writeValidGeneration(t, true)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for range 3 {
		owner := ownerWhileReading(t, root, session)
		defer owner.Close()
		if err := owner.SyncPublished(lock.Generation); err != nil {
			t.Fatal(err)
		}
		if err := owner.RemoveStage(); err != nil {
			t.Fatal(err)
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := session.BinarySnapshot(); err != nil {
			t.Fatal(err)
		}
		if _, err := session.Config.GenerationSources[0].ReadFile("lib/core.dog"); err != nil {
			t.Fatal(err)
		}
	}
}

func ownerWhileReading(t *testing.T, root string, sessions ...*GenerationSession) *GenerationOwner {
	t.Helper()
	type result struct {
		owner *GenerationOwner
		err   error
	}
	done := make(chan result, 1)
	go func() { o, err := BeginGenerationOwner(root); done <- result{o, err} }()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.owner
	case <-time.After(3 * time.Second):
		for _, s := range sessions {
			_ = s.Close()
		}
		r := <-done
		if r.owner != nil {
			_ = r.owner.RemoveStage()
			_ = r.owner.Close()
		}
		t.Fatal("generation reader blocked an independent writer")
		return nil
	}
}

func TestGenerationCleanupWaitsForEveryPinnedReader(t *testing.T) {
	root, _, lock := writeValidGeneration(t, true)
	first, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	owner := ownerWhileReading(t, root, first, second)
	defer owner.Close()
	if err := owner.AdoptPublishedGeneration(lock.Generation); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- owner.RemoveStage() }()
	assertCleanupBlocked := func() {
		t.Helper()
		select {
		case err := <-done:
			t.Fatalf("removed a pinned generation: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	assertCleanupBlocked()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	assertCleanupBlocked()
	if _, err := second.Config.GenerationSources[0].ReadFile("lib/core.dog"); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not resume after the final reader closed")
	}
}
