package deps

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/toolchain/depsnapshot"
	"doggo/internal/toolchain/generation"
)

func TestAdditionalReviewSameSpellingDistinctPathDependencies(t *testing.T) {
	root := t.TempDir()
	for _, fixture := range []struct {
		dir, module string
		deps        []string
	}{
		{"a", "example/a", []string{"./child"}},
		{"a/child", "example/a-child", []string{}},
		{"child", "example/root-child", []string{}},
	} {
		dir := filepath.Join(root, filepath.FromSlash(fixture.dir))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := project.WriteModFile(project.ModPath(dir), &project.ModFile{Module: fixture.module, Deps: fixture.deps}); err != nil {
			t.Fatal(err)
		}
	}
	mod := &project.ModFile{Module: "example/root", Deps: []string{"./a", "./child"}}
	if err := project.WriteModFile(project.ModPath(root), mod); err != nil {
		t.Fatal(err)
	}
	snapshots, err := depsnapshot.SnapshotDependencies(root, filepath.Join(t.TempDir(), "stage"), mod.Deps)
	if err != nil {
		t.Fatal(err)
	}
	status := outdatedDependencyStatus(root, "./child", snapshots)
	t.Logf("distinct path modules = %+v; direct child status = %+v", snapshots, status)
	if status.CheckError != "" || status.Outdated {
		t.Fatalf("valid unchanged direct dependency status: %+v", status)
	}
	findings, err := AuditResolvedLock(t.TempDir(), root, mod, &project.LockFile{Deps: snapshots})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("valid distinct contextual dependencies audited as invalid: %+v", findings)
	}

	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	service := Service{ResolveProjectRoot: func() (string, bool, error) { return root, true, nil }}
	before, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.UpdateDependencies("./child", false); err == nil || !strings.Contains(err.Error(), "ambiguous lock entries") {
		t.Fatalf("ambiguous old contextual update = %v", err)
	}
	after, err := os.ReadFile(project.LockPath(root))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("ambiguous contextual update changed prior lock: %v", err)
	}
	_, results, err := service.UpdateDependencies("", true)
	if err != nil || len(results) != 2 {
		t.Fatalf("full contextual update = %+v, %v", results, err)
	}
}

func TestContextualPathLookupStillRejectsConflicts(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "child"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "child")), &project.ModFile{Module: "example/root-child", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	direct := project.LockDep{Source: "./child", Kind: "path", ModuleID: "doggo:example/root-child", Digest: "one"}
	for _, fixture := range []struct {
		name, code string
		other      project.LockDep
	}{
		{"kind conflict", lockLookupErrorKindConflict, project.LockDep{Source: "./child", Kind: "git", ModuleID: "doggo:example/nested-child"}},
		{"same module different snapshot", lockLookupErrorAmbiguous, project.LockDep{Source: "./child", Kind: "path", ModuleID: direct.ModuleID, Digest: "two"}},
		{"identity changed", lockLookupErrorAmbiguous, project.LockDep{Source: "./child", Kind: "path", ModuleID: "doggo:example/nested-child", Digest: "two"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			entries := []project.LockDep{direct, fixture.other}
			if fixture.name == "identity changed" {
				entries[0].ModuleID = "doggo:example/old-root-child"
			}
			status := outdatedDependencyStatus(root, "./child", entries)
			if !status.Outdated || status.CheckError == "" {
				t.Fatalf("conflicting contextual lookup accepted: %+v", status)
			}
			findings, err := AuditResolvedLock(t.TempDir(), root, &project.ModFile{Deps: []string{"./child"}}, &project.LockFile{Deps: entries})
			if err != nil {
				t.Fatal(err)
			}
			for _, finding := range findings {
				if finding.Code == fixture.code {
					return
				}
			}
			t.Fatalf("conflict finding missing: %+v", findings)
		})
	}
}
