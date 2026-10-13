package deps

import (
	"doggo/internal/project"
	"testing"
)

func TestFindLockDepBySource_AmbiguousEntries(t *testing.T) {
	deps := []project.LockDep{
		{Source: "https://github.com/org/repo.git", Kind: "git", Revision: "a", InstallDir: "deps/a"},
		{Source: "https://github.com/org/repo.git", Kind: "git", Revision: "b", InstallDir: "deps/b"},
	}

	_, ok, err := findLockDepBySource(deps, "https://github.com/org/repo.git", "git")
	if err == nil {
		t.Fatalf("expected ambiguous lock entry error")
	}
	if ok {
		t.Fatalf("expected lookup to fail with ok=false when ambiguous")
	}
}

func TestFindLockDepBySource_AllowsDuplicateIdenticalEntries(t *testing.T) {
	deps := []project.LockDep{
		{Source: "https://github.com/org/repo.git", Kind: "git", Revision: "a", InstallDir: "deps/a"},
		{Source: "https://github.com/org/repo.git", Kind: "git", Revision: "a", InstallDir: "deps/a"},
	}

	dep, ok, err := findLockDepBySource(deps, "https://github.com/org/repo.git", "git")
	if err != nil {
		t.Fatalf("unexpected lookup error: %v", err)
	}
	if !ok {
		t.Fatalf("expected matching lock entry")
	}
	if dep.Revision != "a" {
		t.Fatalf("unexpected revision: %s", dep.Revision)
	}
}

func TestFindLockDepBySource_KindMismatch(t *testing.T) {
	deps := []project.LockDep{
		{Source: "https://github.com/org/repo.git", Kind: "path", InstallDir: "deps/p"},
	}

	_, ok, err := findLockDepBySource(deps, "https://github.com/org/repo.git", "git")
	if err == nil {
		t.Fatalf("expected kind mismatch error")
	}
	if ok {
		t.Fatalf("expected lookup to fail with ok=false on kind mismatch")
	}
	if !isLockLookupErrorCode(err, lockLookupErrorKindMismatch) {
		t.Fatalf("expected kind mismatch error code, got: %v", err)
	}
}

func TestFindLockDepBySource_ConflictingKinds(t *testing.T) {
	deps := []project.LockDep{
		{Source: "https://github.com/org/repo.git", Kind: "git", Revision: "a", InstallDir: "deps/g"},
		{Source: "https://github.com/org/repo.git", Kind: "path", InstallDir: "deps/p"},
	}

	_, ok, err := findLockDepBySource(deps, "https://github.com/org/repo.git", "git")
	if err == nil {
		t.Fatalf("expected conflicting lock kinds error")
	}
	if ok {
		t.Fatalf("expected lookup to fail with ok=false on conflicting kinds")
	}
	if !isLockLookupErrorCode(err, lockLookupErrorKindConflict) {
		t.Fatalf("expected conflicting kind error code, got: %v", err)
	}
}
