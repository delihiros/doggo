package deps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/toolchain/depsnapshot"
)

func TestAdditionalReviewAuditTransitiveRegistryAdvisory(t *testing.T) {
	t.Setenv("DOGGO_REGISTRY", "")
	base := t.TempDir()
	childSource, _ := createGitUpdateDependency(t, base, "child")
	runUpdateGit(t, filepath.Join(base, "child"), "tag", "v1.0.0")
	childSource = strings.TrimSuffix(childSource, "main") + "v1.0.0"
	parentSource, _ := createGitUpdateDependency(t, base, "parent")
	if err := project.WriteModFile(project.ModPath(filepath.Join(base, "parent")), &project.ModFile{Module: "example/parent", Deps: []string{childSource}}); err != nil {
		t.Fatal(err)
	}
	commitGitUpdateDependency(t, base, "parent", "2")
	root := filepath.Join(base, "project")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	mod := &project.ModFile{Module: "example/project", Deps: []string{parentSource}}
	if err := project.WriteModFile(project.ModPath(root), mod); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(base, "stage")
	snapshots, err := depsnapshot.SnapshotDependencies(root, stage, mod.Deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshots = %+v", snapshots)
	}
	if err := project.WriteRegistryFile(filepath.Join(root, "doggo.registry"), &project.RegistryFile{
		Packages:   []project.RegistryPackage{{Name: "child", Version: "1.0.0", Source: childSource}},
		Advisories: []project.RegistryAdvisory{{Name: "child", Range: "=1.0.0", Severity: "high", ID: "TRANSITIVE-1"}},
	}); err != nil {
		t.Fatal(err)
	}
	findings, err := AuditResolvedLock(stage, root, mod, &project.LockFile{Deps: snapshots})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("snapshots = %+v; audit findings = %+v", snapshots, findings)
	control, err := AuditResolvedLock(stage, root, &project.ModFile{Module: mod.Module, Deps: []string{parentSource, childSource}}, &project.LockFile{Deps: snapshots})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("same child source and registry with child declared directly: %+v", control)
	controlFound := false
	for _, finding := range control {
		if finding.Code == "registry-advisory" && strings.Contains(finding.Message, "TRANSITIVE-1") {
			controlFound = true
		}
	}
	if !controlFound {
		t.Fatal("direct control failed to report registry advisory")
	}
	for _, finding := range findings {
		if finding.Code == "registry-advisory" && strings.Contains(finding.Message, "TRANSITIVE-1") {
			return
		}
	}
	t.Fatal("known high-severity advisory on snapshotted transitive dependency was not reported")
}

func TestAuditIncludesTransitiveGitTransportRisk(t *testing.T) {
	root := t.TempDir()
	parent := "https://example.com/parent.git@v1.0.0"
	child := "http://example.com/child.git@v1.0.0"
	findings, err := AuditResolvedLock(t.TempDir(), root, &project.ModFile{Deps: []string{parent}}, &project.LockFile{Deps: []project.LockDep{
		{Source: parent, Kind: "git", Revision: strings.Repeat("a", 40)},
		{Source: child, Kind: "git", Revision: strings.Repeat("b", 40)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Source == child && finding.Code == "insecure-transport" && finding.Severity == "high" {
			return
		}
	}
	t.Fatalf("transitive insecure transport absent: %+v", findings)
}

func TestAuditTransitivePathUsesSnapshotContext(t *testing.T) {
	root := t.TempDir()
	for _, fixture := range []struct {
		dir, module string
		deps        []string
	}{
		{"parent", "example/parent", []string{"./child"}},
		{"parent/child", "example/child", []string{}},
	} {
		dir := filepath.Join(root, filepath.FromSlash(fixture.dir))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := project.WriteModFile(project.ModPath(dir), &project.ModFile{Module: fixture.module, Deps: fixture.deps}); err != nil {
			t.Fatal(err)
		}
	}
	mod := &project.ModFile{Module: "example/root", Deps: []string{"./parent"}}
	stage := filepath.Join(t.TempDir(), "stage")
	snapshots, err := depsnapshot.SnapshotDependencies(root, stage, mod.Deps)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := AuditResolvedLock(stage, root, mod, &project.LockFile{Deps: snapshots})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("valid nested path audited against wrong root: %+v", findings)
	}
}
