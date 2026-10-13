package build

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"doggo/internal/project"
	"doggo/internal/testutil"
	toolchainsync "doggo/internal/toolchain/sync"
)

func TestServiceRunCommitsNewBinaryGeneration(t *testing.T) {
	root, prior := syncedProjectFixture(t)

	lock, err := (Service{}).Run(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Generation == prior.Generation || lock.Build.Binary == "" || lock.Build.SHA256 == "" {
		t.Fatalf("built lock = %#v, prior generation %q", lock, prior.Generation)
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
	want := errors.New("audit failed")
	var racer string
	service := Service{AuditPrepared: func(_, stage string, _ *project.LockFile) (int, error) {
		if !testutil.RenameRetained(t, root, moved) {
			racer = filepath.Join(stage, "toolchain", "main.go")
			testutil.ReplaceOwnedFile(t, racer)
			return 0, want
		}
		if err := os.Mkdir(root, 0755); err != nil {
			t.Fatal(err)
		}
		return 0, want
	}}

	_, err := service.Run(root, true)
	if !errors.Is(err, want) || !errors.Is(err, project.ErrOwnershipChanged) {
		t.Fatalf("build error = %v, want audit and abort failures", err)
	}
	if racer != "" {
		if content, err := os.ReadFile(racer); err != nil || string(content) != "replacement" {
			t.Fatalf("abort changed replacement: %q, %v", content, err)
		}
	}
}

func TestServiceRunWithSyncAuditsTentativeStageBeforeBuild(t *testing.T) {
	root, prior := builtProjectFixture(t)
	before := readCommittedState(t, root, prior)
	audited := false
	service := Service{
		AuditPrepared: func(projectRoot, stage string, lock *project.LockFile) (int, error) {
			audited = true
			if projectRoot != root {
				t.Fatalf("audit project root = %q", projectRoot)
			}
			if lock.Generation != "" || lock.Build != (project.LockBuild{}) {
				t.Fatalf("audit received sealed lock: %#v", lock)
			}
			if _, err := os.Stat(filepath.Join(stage, "toolchain", "go.mod")); err != nil {
				t.Fatalf("audit did not receive tentative stage: %v", err)
			}
			return 1, nil
		},
	}

	if _, err := service.Run(root, true); err == nil {
		t.Fatal("audit blocker unexpectedly allowed build")
	}
	if !audited {
		t.Fatal("tentative generation was not audited")
	}
	assertCommittedState(t, root, prior, before)
}

func TestServiceRunBuildFailurePreservesCommittedState(t *testing.T) {
	root, prior := builtProjectFixture(t)
	before := readCommittedState(t, root, prior)
	service := Service{
		AuditPrepared: func(_, stage string, _ *project.LockFile) (int, error) {
			if err := os.Remove(filepath.Join(stage, "toolchain", "cmd", "doggo", "main.go")); err != nil {
				t.Fatal(err)
			}
			return 0, nil
		},
	}

	if _, err := service.Run(root, true); err == nil {
		t.Fatal("broken tentative host unexpectedly built")
	}
	assertCommittedState(t, root, prior, before)
}

func TestServiceRunCommitFailurePreservesCommittedState(t *testing.T) {
	root, prior := builtProjectFixture(t)
	before := readCommittedState(t, root, prior)
	service := Service{
		AuditPrepared: func(_, _ string, lock *project.LockFile) (int, error) {
			lock.LockVersion++
			return 0, nil
		},
	}

	if _, err := service.Run(root, true); err == nil {
		t.Fatal("invalid tentative lock unexpectedly committed")
	}
	assertCommittedState(t, root, prior, before)
}

func builtProjectFixture(t *testing.T) (string, *project.LockFile) {
	t.Helper()
	root, _ := syncedProjectFixture(t)
	lock, err := (Service{}).Run(root, false)
	if err != nil {
		t.Fatal(err)
	}
	return root, lock
}

func syncedProjectFixture(t *testing.T) (string, *project.LockFile) {
	t.Helper()
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	lock, err := (toolchainsync.Service{}).Run(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, lock
}

type committedState struct {
	lock   []byte
	binary []byte
}

func readCommittedState(t *testing.T, root string, lock *project.LockFile) committedState {
	t.Helper()
	lockBytes, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(lock.Build.Binary)))
	if err != nil {
		t.Fatal(err)
	}
	return committedState{lock: lockBytes, binary: binary}
}

func assertCommittedState(t *testing.T, root string, prior *project.LockFile, before committedState) {
	t.Helper()
	afterLock, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterLock, before.lock) {
		t.Fatal("failed build changed doggo.lock")
	}
	afterBinary, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(prior.Build.Binary)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterBinary, before.binary) {
		t.Fatal("failed build changed prior binary")
	}
	if _, err := os.Stat(filepath.Join(root, ".doggo", "generations", prior.Generation)); err != nil {
		t.Fatalf("failed build removed prior generation: %v", err)
	}
}
