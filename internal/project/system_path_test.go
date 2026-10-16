package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeSystemAliasKeepsSuffixUnresolved(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "private", "var")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "var")
	if err := os.Symlink(filepath.Join("private", "var"), alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, suffix := range []string{"", "directory", filepath.Join("untrusted-link", "file")} {
		got, err := normalizeSystemAlias(filepath.Join(alias, suffix), alias, target)
		if err != nil || got != filepath.Join(target, suffix) {
			t.Fatalf("suffix %q: got %q, %v; want %q", suffix, got, err, filepath.Join(target, suffix))
		}
	}
	for _, candidate := range []string{alias + "iable", filepath.Join(root, "elsewhere", "var", "file")} {
		got, err := normalizeSystemAlias(candidate, alias, target)
		if err != nil || got != candidate {
			t.Fatalf("unrelated path %q: got %q, %v", candidate, got, err)
		}
	}
}

func TestNormalizeSystemAliasRejectsUnexpectedTarget(t *testing.T) {
	root := t.TempDir()
	expected := filepath.Join(root, "private", "var")
	other := filepath.Join(root, "other")
	if err := os.MkdirAll(expected, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "var")
	if err := os.Symlink(other, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := normalizeSystemAlias(filepath.Join(alias, "key"), alias, expected); err == nil {
		t.Fatal("accepted an unexpected alias target")
	}
}

func TestNormalizeSystemAliasRejectsSymlinkTargetParent(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	if err := os.MkdirAll(filepath.Join(actual, "var"), 0700); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(root, "private")
	if err := os.Symlink(actual, private); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	alias := filepath.Join(root, "var")
	if err := os.Symlink(filepath.Join("private", "var"), alias); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeSystemAlias(filepath.Join(alias, "key"), alias, filepath.Join(private, "var")); err == nil {
		t.Fatal("accepted a symlink in the trusted alias target")
	}
}
