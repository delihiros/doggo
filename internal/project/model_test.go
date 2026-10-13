package project

import (
	"reflect"
	"testing"
)

func TestNormalizeRegistryVersion(t *testing.T) {
	for input, want := range map[string]string{
		" v1.2.3+build ": "1.2.3",
		"not-semver":     "not-semver",
	} {
		if got := NormalizeRegistryVersion(input); got != want {
			t.Errorf("NormalizeRegistryVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRegistryVersionComparison(t *testing.T) {
	if CompareRegistryVersions("1.10.0", "1.2.0") <= 0 {
		t.Fatal("expected semantic version comparison")
	}
	if CompareRegistryVersions(" zeta ", "alpha") <= 0 {
		t.Fatal("expected trimmed lexical fallback")
	}
	if !RegistryVersionsEqual(" v1.2.3+build ", "1.2.3") {
		t.Fatal("expected canonical semantic versions to be equal")
	}
	if !RegistryVersionsEqual(" custom ", "custom") {
		t.Fatal("expected trimmed invalid versions to be equal")
	}
}

func TestNormalizeLockDepsDedupesAndSortsByIdentity(t *testing.T) {
	deps := []LockDep{
		{Source: "b", Kind: "git", ModuleID: "go:b", GoModule: "b", Revision: "2", Digest: "sha256:2", InstallDir: "deps/b"},
		{Source: "a", Kind: "path", ModuleID: "doggo:a", Revision: "1", Digest: "sha256:1", InstallDir: "deps/a"},
		{Source: "a", Kind: "path", ModuleID: "doggo:a", Revision: "1", Digest: "sha256:1", InstallDir: "deps/a"},
		{Source: "a", Kind: "path", ModuleID: "doggo:a", Revision: "2", Digest: "sha256:2", InstallDir: "deps/a-v2"},
		{Source: "a", Kind: "git", ModuleID: "go:a", GoModule: "a", Revision: "3", Digest: "sha256:3", InstallDir: "deps/a-git"},
	}

	got := NormalizeLockDeps(deps)
	want := []LockDep{
		{Source: "a", Kind: "path", ModuleID: "doggo:a", Revision: "1", Digest: "sha256:1", InstallDir: "deps/a"},
		{Source: "a", Kind: "path", ModuleID: "doggo:a", Revision: "2", Digest: "sha256:2", InstallDir: "deps/a-v2"},
		{Source: "a", Kind: "git", ModuleID: "go:a", GoModule: "a", Revision: "3", Digest: "sha256:3", InstallDir: "deps/a-git"},
		{Source: "b", Kind: "git", ModuleID: "go:b", GoModule: "b", Revision: "2", Digest: "sha256:2", InstallDir: "deps/b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeLockDeps() = %+v, want %+v", got, want)
	}
}

func TestNormalizeLockDepsReturnsEmptySliceForEmptyInput(t *testing.T) {
	got := NormalizeLockDeps(nil)
	if got == nil {
		t.Fatal("NormalizeLockDeps(nil) returned nil, want empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("NormalizeLockDeps(nil) length = %d, want 0", len(got))
	}
}

func TestNormalizeReifyTargetsSortsAndDedupesIdenticalPairs(t *testing.T) {
	targets := []ReifyTarget{
		{Source: "example.com/z.Public", Runtime: "example.com/origin.B"},
		{Source: "example.com/b.Public", Runtime: "example.com/origin.A"},
		{Source: "example.com/a.Public", Runtime: "example.com/origin.A"},
		{Source: "example.com/a.Public", Runtime: "example.com/origin.A"},
	}

	got := NormalizeReifyTargets(targets)
	want := []ReifyTarget{
		{Source: "example.com/a.Public", Runtime: "example.com/origin.A"},
		{Source: "example.com/b.Public", Runtime: "example.com/origin.A"},
		{Source: "example.com/z.Public", Runtime: "example.com/origin.B"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeReifyTargets() = %+v, want %+v", got, want)
	}
}
