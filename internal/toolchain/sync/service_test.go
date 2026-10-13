package sync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/testutil"
	"doggo/internal/toolchain/generation"
)

func TestServiceRunCommitsSyncOnlyGeneration(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}

	lock, err := (Service{}).Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Generation == "" || lock.Build != (project.LockBuild{}) {
		t.Fatalf("sync lock = %#v", lock)
	}
	mod, err := project.LoadModFile(project.ModPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := project.ValidateGeneration(root, mod, lock); err != nil {
		t.Fatal(err)
	}
}

func TestServiceRunJoinsAbortFailure(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	moved := filepath.Join(base, "project-original")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	var racer string
	service := Service{PrepareSync: func(string, []string) (*generation.PreparedGeneration, error) {
		prepared, err := (generation.Service{}).PrepareSync(root, nil)
		if err != nil {
			return nil, err
		}
		prepared.Lock.LockVersion++
		if !testutil.RenameRetained(t, root, moved) {
			racer = filepath.Join(prepared.StageRoot(), "toolchain", "main.go")
			testutil.ReplaceOwnedFile(t, racer)
			return prepared, nil
		}
		if err := os.Mkdir(root, 0755); err != nil {
			t.Fatal(err)
		}
		return prepared, nil
	}}

	_, err := service.Run(root)
	if err == nil || !strings.Contains(err.Error(), "unsupported lock version") || !errors.Is(err, project.ErrOwnershipChanged) {
		t.Fatalf("sync error = %v, want commit and abort failures", err)
	}
	if racer != "" {
		if content, err := os.ReadFile(racer); err != nil || string(content) != "replacement" {
			t.Fatalf("abort changed replacement: %q, %v", content, err)
		}
	}
}

func TestServiceRunSnapshotsCurrentModDependencies(t *testing.T) {
	root := t.TempDir()
	dep := filepath.Join(root, "dep")
	if err := os.Mkdir(dep, 0755); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(dep), &project.ModFile{Module: "example/dep", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{"./dep"}}); err != nil {
		t.Fatal(err)
	}

	lock, err := (Service{}).Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Deps) != 1 || lock.Deps[0].ModuleID != "doggo:example/dep" {
		t.Fatalf("sync deps = %#v", lock.Deps)
	}
}

func TestServiceRunFailurePreservesCommittedGeneration(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Service{}).Run(root); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareCurrentBuild(root)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort()
	if err := prepared.Build(); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	prior := prepared.Lock
	priorBinary, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(prior.Build.Binary)))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{"./missing"}}); err != nil {
		t.Fatal(err)
	}

	if _, err := (Service{}).Run(root); err == nil {
		t.Fatal("sync unexpectedly succeeded")
	}
	after, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("failed sync changed doggo.lock")
	}
	if _, err := os.Stat(filepath.Join(root, ".doggo", "generations", prior.Generation)); err != nil {
		t.Fatalf("failed sync removed prior generation: %v", err)
	}
	afterBinary, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(prior.Build.Binary)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterBinary, priorBinary) {
		t.Fatal("failed sync changed prior binary")
	}
}
