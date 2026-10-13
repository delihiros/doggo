package deps

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"doggo/internal/project"
)

func auditGenerationDepPath(kind, moduleID, goModule, revision, digest string) string {
	return "deps/" + project.DependencyKey(project.LockDep{
		Kind: kind, ModuleID: moduleID, GoModule: goModule, Revision: revision, Digest: digest,
	})
}

func TestGovulncheckIgnoresAncestorGoWork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake govulncheck uses a POSIX shell")
	}
	parent := t.TempDir()
	projectRoot := filepath.Join(parent, "project")
	bin := filepath.Join(parent, "bin")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(bin, 0755); err != nil {
		t.Fatal(err)
	}
	goWork := filepath.Join(parent, "go.work")
	if err := os.WriteFile(goWork, []byte("go 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example/project\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(bin, "govulncheck")
	if err := os.WriteFile(command, []byte("#!/bin/sh\n[ \"$GOWORK\" = off ] || exit 7\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOWORK", goWork)
	original := govulncheckCommandOutput
	govulncheckCommandOutput = runCommandCombinedOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })

	if findings := collectGovulncheckFindings(projectRoot); len(findings) != 0 {
		t.Fatalf("govulncheck findings = %#v, want none", findings)
	}
}

func TestAuditDependencies_GitRiskFindings(t *testing.T) {
	projectRoot := t.TempDir()
	source := "http://example.com/org/repo.git"

	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "git",
				Revision:   "main",
				Digest:     "",
				InstallDir: auditGenerationDepPath("git", "", "", "main", ""),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}

	codes := map[string]bool{}
	for _, finding := range findings {
		codes[finding.Code] = true
	}
	for _, expected := range []string{"insecure-transport", "floating-ref", "non-commit-lock-ref"} {
		if !codes[expected] {
			t.Fatalf("expected finding code %q, got %+v", expected, findings)
		}
	}
}

func TestAuditDependenciesGoSourceSemantics(t *testing.T) {
	projectRoot := t.TempDir()
	contentRoot := t.TempDir()
	for _, tt := range []struct {
		name       string
		source     string
		lock       project.LockDep
		wantCode   string
		wantLevel  string
		wantAbsent bool
	}{
		{
			name: "exact match", source: "example.com/math@v1.2.3",
			lock:       project.LockDep{Source: "example.com/math@v1.2.3", Kind: "go", GoModule: "example.com/math", Revision: "v1.2.3"},
			wantAbsent: true,
		},
		{
			name: "omitted floats", source: "example.com/math",
			lock:     project.LockDep{Source: "example.com/math", Kind: "go", GoModule: "example.com/math", Revision: "v1.2.3"},
			wantCode: "floating-version", wantLevel: "medium",
		},
		{
			name: "latest floats", source: "example.com/math@latest",
			lock:     project.LockDep{Source: "example.com/math@latest", Kind: "go", GoModule: "example.com/math", Revision: "v1.2.3"},
			wantCode: "floating-version", wantLevel: "medium",
		},
		{
			name: "module mismatch", source: "example.com/math@v1.2.3",
			lock:     project.LockDep{Source: "example.com/math@v1.2.3", Kind: "go", GoModule: "example.com/other", Revision: "v1.2.3"},
			wantCode: "go-module-mismatch", wantLevel: "high",
		},
		{
			name: "invalid version", source: "example.com/math",
			lock:     project.LockDep{Source: "example.com/math", Kind: "go", GoModule: "example.com/math", Revision: "main"},
			wantCode: "invalid-go-version", wantLevel: "high",
		},
		{
			name: "exact version mismatch", source: "example.com/math@v1.2.3",
			lock:     project.LockDep{Source: "example.com/math@v1.2.3", Kind: "go", GoModule: "example.com/math", Revision: "v1.2.4"},
			wantCode: "go-version-mismatch", wantLevel: "high",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			findings, err := AuditResolvedLock(contentRoot, projectRoot, &project.ModFile{Deps: []string{tt.source}}, &project.LockFile{Deps: []project.LockDep{tt.lock}})
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantAbsent {
				if len(findings) != 0 {
					t.Fatalf("findings = %#v, want none", findings)
				}
				return
			}
			for _, finding := range findings {
				if finding.Code == tt.wantCode && finding.Severity == tt.wantLevel {
					return
				}
			}
			t.Fatalf("findings = %#v, want %s %s", findings, tt.wantLevel, tt.wantCode)
		})
	}
}

func TestAuditDependenciesInvalidDeclaration(t *testing.T) {
	const source = "github.com/acme/math@main"
	findings, err := AuditResolvedLock(t.TempDir(), t.TempDir(), &project.ModFile{Deps: []string{source}}, &project.LockFile{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Source != source || findings[0].Severity != "high" || findings[0].Code != "invalid-source" {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestAuditDependencies_PathOutsideProject(t *testing.T) {
	parent := t.TempDir()
	projectRoot := filepath.Join(parent, "project")
	externalDep := filepath.Join(parent, "external")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(externalDep, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalDep, "lib.dog"), []byte("(ns ext)\n"), 0644); err != nil {
		t.Fatalf("write external dependency file: %v", err)
	}

	source := "../external"
	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "path",
				Digest:     "abc",
				InstallDir: auditGenerationDepPath("path", "", "", "", "abc"),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}

	found := false
	for _, finding := range findings {
		if finding.Code == "external-path-dependency" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected external-path-dependency finding, got %+v", findings)
	}
}

func TestAuditDependencies_ParsesGovulncheckJSON(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/test\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	mod := &project.ModFile{Deps: []string{}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps:                []project.LockDep{},
		Scan:                project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		if dir != projectRoot {
			t.Fatalf("unexpected govulncheck dir: %s", dir)
		}
		if name != "govulncheck" {
			t.Fatalf("unexpected command name: %s", name)
		}
		return strings.Join([]string{
			"{\n  \"osv\": {\"id\": \"GO-2024-0001\", \"summary\": \"first vuln\"}\n}",
			"{\n  \"osv\": {\"id\": \"GO-2024-0002\", \"summary\": \"second vuln\"}\n}",
			"{\n  \"osv\": {\"id\": \"GO-2024-0003\", \"summary\": \"not reachable\"}\n}",
			"{\n  \"finding\": {\"osv\": \"GO-2024-0001\", \"trace\": [{\"function\": \"VulnerableOne\"}]}\n}",
			"{\n  \"finding\": {\"osv\": \"GO-2024-0001\", \"trace\": [{\"function\": \"VulnerableOne\"}]}\n}",
			"{\n  \"finding\": {\"osv\": \"GO-2024-0002\", \"trace\": [{\"function\": \"VulnerableTwo\"}]}\n}",
			"{\n  \"finding\": {\"osv\": \"GO-2024-0003\", \"trace\": [{\"package\": \"example.com/unreachable\"}]}\n}",
		}, "\n"), nil
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("expected 2 vulnerability findings, got %d: %+v", len(findings), findings)
	}
	if findings[0].Code != "known-vulnerability" || findings[1].Code != "known-vulnerability" {
		t.Fatalf("unexpected finding codes: %+v", findings)
	}
	if !strings.Contains(findings[0].Message, "GO-2024-0001") || !strings.Contains(findings[1].Message, "GO-2024-0002") {
		t.Fatalf("unexpected vulnerability messages: %+v", findings)
	}
}

func TestAuditDependencies_ReportsGovulncheckMissing(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/test\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	mod := &project.ModFile{Deps: []string{}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps:                []project.LockDep{},
		Scan:                project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return "", fmt.Errorf("govulncheck -json ./...: exec: \"govulncheck\": executable file not found in $PATH")
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Code != "vuln-scan-unavailable" {
		t.Fatalf("unexpected finding: %+v", findings[0])
	}
}

func TestAuditDependencies_UsesVulnerabilityOutputEvenOnCommandError(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/test\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	mod := &project.ModFile{Deps: []string{}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps:                []project.LockDep{},
		Scan:                project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return `{"osv":{"id":"GO-2024-9999","summary":"critical vuln"}}{"finding":{"osv":"GO-2024-9999","trace":[{"function":"Vulnerable"}]}}`, fmt.Errorf("govulncheck exited with status 3")
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 vulnerability finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Code != "known-vulnerability" {
		t.Fatalf("unexpected finding code: %+v", findings[0])
	}
}

func TestAuditDependencies_AmbiguousLockEntries(t *testing.T) {
	projectRoot := t.TempDir()
	source := "https://github.com/org/repo.git"

	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "git",
				Revision:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Digest:     "",
				InstallDir: auditGenerationDepPath("git", "", "", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""),
			},
			{
				Source:     source,
				Kind:       "git",
				Revision:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Digest:     "",
				InstallDir: auditGenerationDepPath("git", "", "", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ""),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return "", nil
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected one finding for ambiguous lock entry, got %d: %+v", len(findings), findings)
	}
	if findings[0].Code != "ambiguous-lock-entry" {
		t.Fatalf("unexpected finding: %+v", findings[0])
	}
}

func TestAuditDependencies_DependencyKindMismatch(t *testing.T) {
	projectRoot := t.TempDir()
	source := "https://github.com/org/repo.git"

	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "path",
				Digest:     "",
				InstallDir: auditGenerationDepPath("path", "", "", "", ""),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return "", nil
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected one finding for dependency kind mismatch, got %d: %+v", len(findings), findings)
	}
	if findings[0].Code != "dependency-kind-mismatch" {
		t.Fatalf("unexpected finding: %+v", findings[0])
	}
}

func TestAuditDependencies_RegistryAdvisoryMatched(t *testing.T) {
	projectRoot := t.TempDir()
	source := "https://github.com/acme/math.git@v1.2.3"

	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "git",
				Revision:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Digest:     "",
				InstallDir: auditGenerationDepPath("git", "", "", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	registryPath := filepath.Join(projectRoot, defaultRegistryFileName)
	registry := &project.RegistryFile{
		Packages: []project.RegistryPackage{
			{
				Name:    "acme/math",
				Version: "1.2.3",
				Source:  source,
			},
		},
		Advisories: []project.RegistryAdvisory{
			{
				Name:     "acme/math",
				Range:    ">=1.0.0 <2.0.0",
				Severity: "high",
				ID:       "DOG-2026-0001",
				Message:  "test advisory",
			},
		},
	}
	if err := project.WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return "", nil
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}

	found := false
	for _, finding := range findings {
		if finding.Code != "registry-advisory" {
			continue
		}
		if !strings.Contains(finding.Message, "DOG-2026-0001") {
			t.Fatalf("expected advisory ID in message: %+v", finding)
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("expected registry advisory finding, got %+v", findings)
	}
}

func TestAuditDependencies_GoExactSourceRegistryAdvisoryMatched(t *testing.T) {
	projectRoot := t.TempDir()
	const source = "example.com/acme/math@v1.2.3"
	if err := project.WriteRegistryFile(filepath.Join(projectRoot, defaultRegistryFileName), &project.RegistryFile{
		Packages: []project.RegistryPackage{{Name: "acme/math", Version: "1.2.3", Source: source}},
		Advisories: []project.RegistryAdvisory{{
			Name: "acme/math", Range: ">=1.0.0 <2.0.0", Severity: "high", ID: "DOG-GO-0001",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	findings := collectRegistryAdvisoryFindings(projectRoot, []auditedDependency{{
		Source: source, Kind: "go", Revision: "v1.2.3",
	}})
	if len(findings) != 1 || findings[0].Code != "registry-advisory" || !strings.Contains(findings[0].Message, "DOG-GO-0001") {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestAuditDependencies_RegistryAdvisoryOutOfRange(t *testing.T) {
	projectRoot := t.TempDir()
	source := "https://github.com/acme/math.git@v1.2.3"

	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "git",
				Revision:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Digest:     "",
				InstallDir: auditGenerationDepPath("git", "", "", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	registryPath := filepath.Join(projectRoot, defaultRegistryFileName)
	registry := &project.RegistryFile{
		Packages: []project.RegistryPackage{
			{
				Name:    "acme/math",
				Version: "1.2.3",
				Source:  source,
			},
		},
		Advisories: []project.RegistryAdvisory{
			{
				Name:     "acme/math",
				Range:    ">=2.0.0",
				Severity: "high",
				ID:       "DOG-2026-0002",
				Message:  "out of range",
			},
		},
	}
	if err := project.WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return "", nil
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}

	for _, finding := range findings {
		if finding.Code == "registry-advisory" {
			t.Fatalf("did not expect registry advisory finding: %+v", findings)
		}
	}
}

func TestMatchesSemverRange_LogicalOR(t *testing.T) {
	match, err := matchesSemverRange("1.2.3", "<1.0.0 || >=1.2.0")
	if err != nil {
		t.Fatalf("matchesSemverRange returned error: %v", err)
	}
	if !match {
		t.Fatalf("expected OR range to match 1.2.3")
	}

	noMatch, err := matchesSemverRange("1.1.0", "<1.0.0 || >=1.2.0")
	if err != nil {
		t.Fatalf("matchesSemverRange returned error: %v", err)
	}
	if noMatch {
		t.Fatalf("expected OR range not to match 1.1.0")
	}
}

func TestMatchesSemverRange_CaretRange(t *testing.T) {
	match, err := matchesSemverRange("1.2.3", "^1.2.0")
	if err != nil {
		t.Fatalf("matchesSemverRange returned error: %v", err)
	}
	if !match {
		t.Fatalf("expected caret range to match 1.2.3")
	}

	noMatch, err := matchesSemverRange("2.0.0", "^1.2.0")
	if err != nil {
		t.Fatalf("matchesSemverRange returned error: %v", err)
	}
	if noMatch {
		t.Fatalf("expected caret range not to match 2.0.0")
	}
}

func TestAuditDependencies_RegistryAdvisoryLogicalORMatched(t *testing.T) {
	projectRoot := t.TempDir()
	source := "https://github.com/acme/math.git@v1.2.3"

	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "git",
				Revision:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Digest:     "",
				InstallDir: auditGenerationDepPath("git", "", "", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	registryPath := filepath.Join(projectRoot, defaultRegistryFileName)
	registry := &project.RegistryFile{
		Packages: []project.RegistryPackage{
			{
				Name:    "acme/math",
				Version: "1.2.3",
				Source:  source,
			},
		},
		Advisories: []project.RegistryAdvisory{
			{
				Name:     "acme/math",
				Range:    "<1.0.0 || >=1.2.0",
				Severity: "high",
				ID:       "DOG-2026-0003",
				Message:  "or-range advisory",
			},
		},
	}
	if err := project.WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return "", nil
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}

	found := false
	for _, finding := range findings {
		if finding.Code == "registry-advisory" && strings.Contains(finding.Message, "DOG-2026-0003") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected OR advisory finding, got %+v", findings)
	}
}

func TestAuditDependencies_RegistryAdvisoryInvalidRange(t *testing.T) {
	projectRoot := t.TempDir()
	source := "https://github.com/acme/math.git@v1.2.3"

	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "git",
				Revision:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Digest:     "",
				InstallDir: auditGenerationDepPath("git", "", "", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	registryPath := filepath.Join(projectRoot, defaultRegistryFileName)
	registry := &project.RegistryFile{
		Packages: []project.RegistryPackage{
			{
				Name:    "acme/math",
				Version: "1.2.3",
				Source:  source,
			},
		},
		Advisories: []project.RegistryAdvisory{
			{
				Name:     "acme/math",
				Range:    "not-a-range",
				Severity: "high",
				ID:       "DOG-2026-0099",
				Message:  "invalid range expression",
			},
		},
	}
	if err := project.WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return "", nil
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}

	found := false
	for _, finding := range findings {
		if finding.Code != "registry-advisory-invalid-range" {
			continue
		}
		if !strings.Contains(finding.Message, "DOG-2026-0099") {
			t.Fatalf("expected advisory id in invalid-range message: %+v", finding)
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("expected registry-advisory-invalid-range finding, got %+v", findings)
	}
}

func TestAuditDependencies_RegistryAdvisoryUnpinnedDependency(t *testing.T) {
	projectRoot := t.TempDir()
	source := "https://github.com/acme/math.git"

	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "git",
				Revision:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Digest:     "",
				InstallDir: auditGenerationDepPath("git", "", "", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	registryPath := filepath.Join(projectRoot, defaultRegistryFileName)
	registry := &project.RegistryFile{
		Packages: []project.RegistryPackage{
			{
				Name:    "acme/math",
				Version: "1.2.3",
				Source:  "https://github.com/acme/math.git@v1.2.3",
			},
		},
		Advisories: []project.RegistryAdvisory{
			{
				Name:     "acme/math",
				Range:    ">=1.0.0",
				Severity: "high",
				ID:       "DOG-2026-0004",
				Message:  "advisory requires pinned version",
			},
		},
	}
	if err := project.WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return "", nil
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}

	found := false
	for _, finding := range findings {
		if finding.Code == "registry-advisory-unpinned" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected registry-advisory-unpinned finding, got %+v", findings)
	}
}

func TestAuditDependencies_RegistryAdvisoryUnpinnedDependency_NoRelevantAdvisory(t *testing.T) {
	projectRoot := t.TempDir()
	source := "https://github.com/acme/math.git"

	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "git",
				Revision:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Digest:     "",
				InstallDir: auditGenerationDepPath("git", "", "", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	registryPath := filepath.Join(projectRoot, defaultRegistryFileName)
	registry := &project.RegistryFile{
		Packages: []project.RegistryPackage{
			{
				Name:    "acme/math",
				Version: "1.2.3",
				Source:  "https://github.com/acme/math.git@v1.2.3",
			},
		},
		Advisories: []project.RegistryAdvisory{
			{
				Name:     "other/pkg",
				Range:    ">=1.0.0",
				Severity: "high",
				ID:       "DOG-2026-0010",
				Message:  "unrelated advisory",
			},
		},
	}
	if err := project.WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return "", nil
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}

	for _, finding := range findings {
		if finding.Code == "registry-advisory-unpinned" {
			t.Fatalf("did not expect registry-advisory-unpinned for unrelated advisories, got %+v", findings)
		}
	}
}

func TestAuditDependencies_RegistryAdvisoryResolvedByLockCommitRef(t *testing.T) {
	projectRoot := t.TempDir()
	source := "https://github.com/acme/math.git"
	lockCommit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "git",
				Revision:   lockCommit,
				Digest:     "",
				InstallDir: auditGenerationDepPath("git", "", "", lockCommit, ""),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	registryPath := filepath.Join(projectRoot, defaultRegistryFileName)
	registry := &project.RegistryFile{
		Packages: []project.RegistryPackage{
			{
				Name:    "acme/math",
				Version: "1.2.3",
				Source:  "https://github.com/acme/math.git@" + lockCommit,
			},
		},
		Advisories: []project.RegistryAdvisory{
			{
				Name:     "acme/math",
				Range:    ">=1.0.0",
				Severity: "high",
				ID:       "DOG-2026-0011",
				Message:  "commit pinned advisory",
			},
		},
	}
	if err := project.WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return "", nil
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}

	foundAdvisory := false
	foundUnpinned := false
	for _, finding := range findings {
		if finding.Code == "registry-advisory" && strings.Contains(finding.Message, "DOG-2026-0011") {
			foundAdvisory = true
		}
		if finding.Code == "registry-advisory-unpinned" {
			foundUnpinned = true
		}
	}
	if !foundAdvisory {
		t.Fatalf("expected registry advisory matched by lock commit ref, got %+v", findings)
	}
	if foundUnpinned {
		t.Fatalf("did not expect registry-advisory-unpinned when commit fallback resolved package, got %+v", findings)
	}
}

func TestAuditDependencies_RegistryAdvisoryPinnedCommitUnmapped(t *testing.T) {
	projectRoot := t.TempDir()
	commit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	source := "https://github.com/acme/math.git@" + commit

	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "git",
				Revision:   commit,
				Digest:     "",
				InstallDir: auditGenerationDepPath("git", "", "", commit, ""),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	registryPath := filepath.Join(projectRoot, defaultRegistryFileName)
	registry := &project.RegistryFile{
		Packages: []project.RegistryPackage{
			{
				Name:    "acme/math",
				Version: "1.2.3",
				Source:  "https://github.com/acme/math.git@v1.2.3",
			},
		},
		Advisories: []project.RegistryAdvisory{
			{
				Name:     "acme/math",
				Range:    ">=1.0.0",
				Severity: "high",
				ID:       "DOG-2026-0012",
				Message:  "package has advisory but commit source is unmapped",
			},
		},
	}
	if err := project.WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return "", nil
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}

	found := false
	for _, finding := range findings {
		if finding.Code == "registry-advisory-unmapped" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected registry-advisory-unmapped finding, got %+v", findings)
	}
}

func TestAuditDependencies_RegistryLoadFailure(t *testing.T) {
	projectRoot := t.TempDir()
	source := "https://github.com/acme/math.git@v1.2.3"

	mod := &project.ModFile{Deps: []string{source}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps: []project.LockDep{
			{
				Source:     source,
				Kind:       "git",
				Revision:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Digest:     "",
				InstallDir: auditGenerationDepPath("git", "", "", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""),
			},
		},
		Scan: project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if err := project.WriteLockFile(project.LockPath(projectRoot), lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	invalidRegistry := filepath.Join(projectRoot, defaultRegistryFileName)
	if err := os.WriteFile(invalidRegistry, []byte("not-a-map"), 0644); err != nil {
		t.Fatalf("write invalid registry: %v", err)
	}

	original := govulncheckCommandOutput
	t.Cleanup(func() { govulncheckCommandOutput = original })
	govulncheckCommandOutput = func(dir, name string, args ...string) (string, error) {
		return "", nil
	}

	findings, err := AuditResolvedLock(projectRoot, projectRoot, mod, lock)
	if err != nil {
		t.Fatalf("audit dependencies failed: %v", err)
	}

	found := false
	for _, finding := range findings {
		if finding.Code == "registry-load-failed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected registry-load-failed finding, got %+v", findings)
	}
}
