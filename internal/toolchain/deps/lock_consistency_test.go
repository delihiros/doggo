package deps_test

import (
	"strings"
	"testing"

	"doggo/internal/project"
	toolchaindeps "doggo/internal/toolchain/deps"
)

func TestOutdatedDependenciesRejectsUnvalidatedLock(t *testing.T) {
	projectRoot := t.TempDir()

	mod := &project.ModFile{Deps: []string{}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		Generation:          strings.Repeat("a", 64),
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps:                []project.LockDep{},
		Scan:                project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	if _, err := toolchaindeps.OutdatedDependencies(projectRoot); err == nil {
		t.Fatal("outdated accepted a lock outside a validated generation")
	}
}
