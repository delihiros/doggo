package deps

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"doggo/internal/depsource"
	"doggo/internal/project"
	"doggo/internal/toolchain/gitdep"
	"doggo/internal/toolchain/scan"

	msemver "github.com/Masterminds/semver/v3"
	"golang.org/x/mod/module"
)

type auditedDependency struct {
	Source       string
	Kind         string
	Revision     string
	PackageName  string
	PackageVer   string
	ResolvedFrom string
}

func AuditResolvedLock(contentRoot, projectRoot string, mod *project.ModFile, lock *project.LockFile) ([]DependencyAuditFinding, error) {
	return auditDependencies(contentRoot, projectRoot, mod, lock)
}

func auditDependencies(contentRoot, projectRoot string, mod *project.ModFile, lock *project.LockFile) ([]DependencyAuditFinding, error) {
	findings := make([]DependencyAuditFinding, 0)
	deps := make([]auditedDependency, 0, len(lock.Deps))
	directPaths := make(map[project.LockDep]string)
	invalidDirectSources := make(map[string]bool)
	// Direct declarations must resolve to one lock entry independently of auditing
	// the selected graph, which also contains dependencies of dependencies.
	for _, source := range mod.Deps {
		parsed, err := depsource.Parse(source)
		if err != nil {
			findings = append(findings, DependencyAuditFinding{
				Source: source, Severity: "high", Code: "invalid-source", Message: err.Error(),
			})
			invalidDirectSources[source] = true
			continue
		}
		lockDep, ok, err := findDirectLockDep(projectRoot, lock.Deps, parsed.Declaration, parsed.Kind)
		if err != nil {
			code := lockLookupErrorAmbiguous
			if isLockLookupErrorCode(err, lockLookupErrorKindMismatch) {
				code = lockLookupErrorKindMismatch
			}
			if isLockLookupErrorCode(err, lockLookupErrorKindConflict) {
				code = lockLookupErrorKindConflict
			}
			findings = append(findings, DependencyAuditFinding{
				Source:   source,
				Kind:     parsed.Kind,
				Severity: "high",
				Code:     code,
				Message:  err.Error(),
			})
			invalidDirectSources[parsed.Declaration] = true
			continue
		}
		if !ok {
			findings = append(findings, DependencyAuditFinding{
				Source:   source,
				Kind:     parsed.Kind,
				Severity: "high",
				Code:     "unlocked-dependency",
				Message:  fmt.Sprintf("dependency is not locked in %s; run 'doggo mod sync'", project.LockFileName),
			})
			continue
		}
		if parsed.Kind == depsource.Path {
			directPaths[lockDep] = source
		}
	}

	for _, lockDep := range project.NormalizeLockDeps(lock.Deps) {
		source := lockDep.Source
		if invalidDirectSources[source] {
			continue
		}
		parsed, err := depsource.Parse(source)
		if err != nil {
			findings = append(findings, DependencyAuditFinding{
				Source: source, Severity: "high", Code: "invalid-source", Message: err.Error(),
			})
			continue
		}
		if parsed.Kind != lockDep.Kind {
			findings = append(findings, DependencyAuditFinding{
				Source: source, Kind: parsed.Kind, Severity: "high", Code: lockLookupErrorKindMismatch,
				Message: fmt.Sprintf("dependency kind mismatch for source %q (expected=%s)", source, parsed.Kind),
			})
			continue
		}
		deps = append(deps, auditedDependency{
			Source: source, Kind: parsed.Kind, Revision: strings.TrimSpace(lockDep.Revision),
		})

		switch parsed.Kind {
		case depsource.Path:
			if directSource, ok := directPaths[lockDep]; ok {
				findings = append(findings, auditPathDependency(projectRoot, directSource, lockDep)...)
			}
			// Transitive path spellings are relative to their declaring module.
			// Their installed content is checked by generation validation; resolving
			// those spellings against the project root would check a different path.
		case depsource.Git:
			findings = append(findings, auditGitDependency(source, lockDep)...)
		case depsource.Go:
			findings = append(findings, auditGoDependency(parsed, lockDep)...)
		default:
			findings = append(findings, DependencyAuditFinding{
				Source:   source,
				Kind:     parsed.Kind,
				Severity: "high",
				Code:     "unsupported-kind",
				Message:  fmt.Sprintf("unsupported dependency kind: %s", parsed.Kind),
			})
		}
	}

	toolchainRoot := filepath.Join(contentRoot, "toolchain")
	if _, err := os.Stat(filepath.Join(toolchainRoot, "go.mod")); err != nil {
		toolchainRoot = contentRoot
	}
	findings = append(findings, collectGovulncheckFindings(toolchainRoot)...)
	findings = append(findings, collectRegistryAdvisoryFindings(projectRoot, deps)...)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Source == findings[j].Source {
			if findings[i].Severity == findings[j].Severity {
				return findings[i].Code < findings[j].Code
			}
			return severityRank(findings[i].Severity) < severityRank(findings[j].Severity)
		}
		return findings[i].Source < findings[j].Source
	})
	return findings, nil
}

func auditGoDependency(parsed depsource.Source, dep project.LockDep) []DependencyAuditFinding {
	if dep.GoModule != parsed.ModulePath {
		return []DependencyAuditFinding{{
			Source: parsed.Declaration, Kind: depsource.Go, Severity: "high", Code: "go-module-mismatch",
			Message: fmt.Sprintf("lock Go module %q does not match source module %q", dep.GoModule, parsed.ModulePath),
		}}
	}
	if module.CanonicalVersion(dep.Revision) != dep.Revision || module.Check(dep.GoModule, dep.Revision) != nil {
		return []DependencyAuditFinding{{
			Source: parsed.Declaration, Kind: depsource.Go, Severity: "high", Code: "invalid-go-version",
			Message: fmt.Sprintf("lock revision %q is not a valid canonical Go module version", dep.Revision),
		}}
	}
	if !parsed.Floating() && parsed.Query != dep.Revision {
		return []DependencyAuditFinding{{
			Source: parsed.Declaration, Kind: depsource.Go, Severity: "high", Code: "go-version-mismatch",
			Message: fmt.Sprintf("lock revision %q does not match exact query %q", dep.Revision, parsed.Query),
		}}
	}
	if parsed.Floating() {
		return []DependencyAuditFinding{{
			Source: parsed.Declaration, Kind: depsource.Go, Severity: "medium", Code: "floating-version",
			Message: "Go module source does not pin an exact version",
		}}
	}
	return nil
}

func auditPathDependency(projectRoot, source string, dep project.LockDep) []DependencyAuditFinding {
	findings := make([]DependencyAuditFinding, 0)

	resolved := filepath.Join(projectRoot, filepath.FromSlash(source))
	st, err := os.Stat(resolved)
	if err != nil {
		findings = append(findings, DependencyAuditFinding{
			Source:   source,
			Kind:     "path",
			Severity: "high",
			Code:     "missing-path-target",
			Message:  fmt.Sprintf("resolved path does not exist: %s", resolved),
		})
		return findings
	}
	if !st.IsDir() {
		findings = append(findings, DependencyAuditFinding{
			Source:   source,
			Kind:     "path",
			Severity: "high",
			Code:     "invalid-path-target",
			Message:  fmt.Sprintf("resolved path is not a directory: %s", resolved),
		})
		return findings
	}

	projectAbs, err := filepath.Abs(projectRoot)
	if err != nil {
		return findings
	}
	resolvedAbs, err := filepath.Abs(resolved)
	if err != nil {
		return findings
	}
	if !isPathWithin(projectAbs, resolvedAbs) {
		findings = append(findings, DependencyAuditFinding{
			Source:   source,
			Kind:     "path",
			Severity: "medium",
			Code:     "external-path-dependency",
			Message:  "path dependency points outside project root",
		})
	}

	return findings
}

func auditGitDependency(source string, dep project.LockDep) []DependencyAuditFinding {
	findings := make([]DependencyAuditFinding, 0)

	parsed, err := gitdep.ParseSource(source)
	if err != nil {
		findings = append(findings, DependencyAuditFinding{
			Source:   source,
			Kind:     "git",
			Severity: "high",
			Code:     "invalid-git-source",
			Message:  err.Error(),
		})
		return findings
	}
	repo, ref := parsed.Repo, parsed.Ref

	normalizedRepo := strings.ToLower(strings.TrimSpace(repo))
	if strings.HasPrefix(normalizedRepo, "http://") {
		findings = append(findings, DependencyAuditFinding{
			Source:   source,
			Kind:     "git",
			Severity: "high",
			Code:     "insecure-transport",
			Message:  "git dependency uses plain HTTP transport",
		})
	}
	if strings.TrimSpace(ref) == "" {
		findings = append(findings, DependencyAuditFinding{
			Source:   source,
			Kind:     "git",
			Severity: "medium",
			Code:     "floating-ref",
			Message:  "git source does not pin a ref (floating HEAD)",
		})
	}
	if !isGitCommitHash(dep.Revision) {
		findings = append(findings, DependencyAuditFinding{
			Source:   source,
			Kind:     "git",
			Severity: "high",
			Code:     "non-commit-lock-ref",
			Message:  "lock revision must be a full 40-hex commit hash",
		})
	}
	return findings
}

func isPathWithin(baseDir, targetPath string) bool {
	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return false
	}
	return true
}

var govulncheckCommandOutput = runCommandCombinedOutput

func collectGovulncheckFindings(projectRoot string) []DependencyAuditFinding {
	goModPath := filepath.Join(projectRoot, "go.mod")
	if st, err := os.Stat(goModPath); err != nil || st.IsDir() {
		return nil
	}

	out, err := govulncheckCommandOutput(projectRoot, "govulncheck", "-json", "./...")
	if err != nil && isGovulncheckMissing(err) {
		return []DependencyAuditFinding{
			{
				Source:   "go-modules",
				Kind:     "go",
				Severity: "medium",
				Code:     "vuln-scan-unavailable",
				Message:  "govulncheck is not installed; install golang.org/x/vuln/cmd/govulncheck to enable known vulnerability checks",
			},
		}
	}

	findings, parseErr := parseGovulncheckFindings(out)
	if parseErr != nil {
		msg := "failed to parse govulncheck JSON output"
		if err != nil {
			msg = strings.TrimSpace(err.Error()) + "; " + msg
		}
		return []DependencyAuditFinding{
			{
				Source:   "go-modules",
				Kind:     "go",
				Severity: "high",
				Code:     "vuln-scan-failed",
				Message:  fmt.Sprintf("%s: %v", msg, parseErr),
			},
		}
	}
	if len(findings) > 0 {
		return findings
	}
	if err != nil {
		return []DependencyAuditFinding{
			{
				Source:   "go-modules",
				Kind:     "go",
				Severity: "high",
				Code:     "vuln-scan-failed",
				Message:  strings.TrimSpace(err.Error()),
			},
		}
	}
	return nil
}

func isGovulncheckMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "executable file not found") || strings.Contains(msg, "command not found")
}

type govulncheckJSON struct {
	OSV *struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	} `json:"osv"`
	Finding *struct {
		OSV   string `json:"osv"`
		Trace []struct {
			Function string `json:"function"`
		} `json:"trace"`
	} `json:"finding"`
}

func parseGovulncheckFindings(output string) ([]DependencyAuditFinding, error) {
	decoder := json.NewDecoder(strings.NewReader(output))
	osvByID := map[string]string{}
	affectedIDs := map[string]struct{}{}
	for {
		var event govulncheckJSON
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if event.OSV != nil && strings.TrimSpace(event.OSV.ID) != "" {
			osvByID[event.OSV.ID] = strings.TrimSpace(event.OSV.Summary)
		}
		if event.Finding == nil || strings.TrimSpace(event.Finding.OSV) == "" {
			continue
		}
		for _, frame := range event.Finding.Trace {
			if strings.TrimSpace(frame.Function) != "" {
				affectedIDs[event.Finding.OSV] = struct{}{}
				break
			}
		}
	}

	if len(affectedIDs) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(affectedIDs))
	for id := range affectedIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	findings := make([]DependencyAuditFinding, 0, len(ids))
	for _, id := range ids {
		msg := id
		if summary := osvByID[id]; summary != "" {
			msg = fmt.Sprintf("%s: %s", id, summary)
		}
		findings = append(findings, DependencyAuditFinding{
			Source:   "go-modules",
			Kind:     "go",
			Severity: "high",
			Code:     "known-vulnerability",
			Message:  msg,
		})
	}
	return findings, nil
}

func runCommandCombinedOutput(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(out), nil
}

func severityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	default:
		return 3
	}
}

func collectRegistryAdvisoryFindings(projectRoot string, deps []auditedDependency) []DependencyAuditFinding {
	registryPath, err := ResolveRegistryPath(projectRoot, "", false)
	if err != nil {
		return nil
	}
	registry, err := project.LoadRegistryFile(registryPath)
	if err != nil {
		return []DependencyAuditFinding{
			{
				Source:   "registry",
				Kind:     "registry",
				Severity: "high",
				Code:     "registry-load-failed",
				Message:  fmt.Sprintf("failed to load registry %s: %v", registryPath, err),
			},
		}
	}
	if len(registry.Advisories) == 0 || len(registry.Packages) == 0 || len(deps) == 0 {
		return nil
	}

	findings := make([]DependencyAuditFinding, 0)
	for i := range deps {
		dep := &deps[i]
		pkg, ok := resolveDependencyRegistryPackage(*dep, registry.Packages)
		if !ok {
			if needsRegistryVersionPinFinding(*dep, registry.Packages, registry.Advisories) {
				findings = append(findings, DependencyAuditFinding{
					Source:   dep.Source,
					Kind:     dep.Kind,
					Severity: "high",
					Code:     "registry-advisory-unpinned",
					Message:  "registry advisory matching skipped for floating dependency; pin a semver tag (e.g. @v1.2.3)",
				})
			} else if needsRegistryAdvisoryMappingFinding(*dep, registry.Packages, registry.Advisories) {
				findings = append(findings, DependencyAuditFinding{
					Source:   dep.Source,
					Kind:     dep.Kind,
					Severity: "high",
					Code:     "registry-advisory-unmapped",
					Message:  "registry advisory matching skipped for pinned dependency; repository has advisories but source/ref is not mapped in registry packages",
				})
			}
			continue
		}
		dep.PackageName = pkg.Name
		dep.PackageVer = project.NormalizeRegistryVersion(pkg.Version)
		dep.ResolvedFrom = pkg.Source

		for _, advisory := range registry.Advisories {
			applies, advisoryErr := advisoryAppliesToDependency(advisory, *dep)
			if advisoryErr != nil {
				msgParts := make([]string, 0, 4)
				if strings.TrimSpace(advisory.ID) != "" {
					msgParts = append(msgParts, strings.TrimSpace(advisory.ID))
				}
				if strings.TrimSpace(advisory.Message) != "" {
					msgParts = append(msgParts, strings.TrimSpace(advisory.Message))
				}
				if strings.TrimSpace(advisory.Range) != "" {
					msgParts = append(msgParts, "range="+strings.TrimSpace(advisory.Range))
				}
				msgParts = append(msgParts, advisoryErr.Error())
				findings = append(findings, DependencyAuditFinding{
					Source:   dep.Source,
					Kind:     dep.Kind,
					Severity: "high",
					Code:     "registry-advisory-invalid-range",
					Message:  strings.Join(msgParts, " | "),
				})
				continue
			}
			if !applies {
				continue
			}
			severity := strings.ToLower(strings.TrimSpace(advisory.Severity))
			if severity == "" {
				severity = "medium"
			}
			msgParts := make([]string, 0, 4)
			if strings.TrimSpace(advisory.ID) != "" {
				msgParts = append(msgParts, strings.TrimSpace(advisory.ID))
			}
			if strings.TrimSpace(advisory.Message) != "" {
				msgParts = append(msgParts, strings.TrimSpace(advisory.Message))
			}
			if strings.TrimSpace(advisory.Range) != "" {
				msgParts = append(msgParts, "range="+strings.TrimSpace(advisory.Range))
			}
			if len(msgParts) == 0 {
				msgParts = append(msgParts, "registry advisory matched")
			}
			findings = append(findings, DependencyAuditFinding{
				Source:   dep.Source,
				Kind:     dep.Kind,
				Severity: severity,
				Code:     "registry-advisory",
				Message:  strings.Join(msgParts, " | "),
			})
		}
	}
	return dedupeAuditFindings(findings)
}

func resolveDependencyRegistryPackage(dep auditedDependency, packages []project.RegistryPackage) (project.RegistryPackage, bool) {
	// 1) exact source match
	for _, pkg := range packages {
		if strings.TrimSpace(pkg.Source) == strings.TrimSpace(dep.Source) {
			return pkg, true
		}
	}
	if dep.Kind != depsource.Git {
		return project.RegistryPackage{}, false
	}

	parsed, err := gitdep.ParseSource(dep.Source)
	repo, ref := parsed.Repo, parsed.Ref
	if err != nil || strings.TrimSpace(repo) == "" {
		return project.RegistryPackage{}, false
	}
	depVersion := semverVersionOrEmpty(ref)
	if depVersion == "" {
		depVersion = semverVersionOrEmpty(dep.Revision)
	}
	repoCanonical := scan.NormalizeGitRepositoryPath(repo)
	if repoCanonical == "" {
		repoCanonical = strings.TrimSpace(repo)
	}

	// 2) repo+version match
	for _, pkg := range packages {
		pkgParsed, pkgErr := gitdep.ParseSource(pkg.Source)
		pkgRepo := pkgParsed.Repo
		if pkgErr != nil {
			continue
		}
		pkgRepoCanonical := scan.NormalizeGitRepositoryPath(pkgRepo)
		if pkgRepoCanonical == "" {
			pkgRepoCanonical = strings.TrimSpace(pkgRepo)
		}
		if pkgRepoCanonical != repoCanonical {
			continue
		}
		if depVersion != "" && project.RegistryVersionsEqual(pkg.Version, depVersion) {
			return pkg, true
		}
		if strings.TrimSpace(dep.Revision) != "" && strings.TrimSpace(refFromSource(pkg.Source)) == strings.TrimSpace(dep.Revision) {
			return pkg, true
		}
	}
	return project.RegistryPackage{}, false
}

func semverVersionOrEmpty(value string) string {
	canonical := project.NormalizeRegistryVersion(value)
	if canonical == "" || !project.IsSemver(canonical) {
		return ""
	}
	return canonical
}

func needsRegistryVersionPinFinding(dep auditedDependency, packages []project.RegistryPackage, advisories []project.RegistryAdvisory) bool {
	if dep.Kind != "git" {
		return false
	}
	parsed, err := gitdep.ParseSource(dep.Source)
	if err != nil {
		return false
	}
	if strings.TrimSpace(parsed.Ref) != "" {
		return false
	}
	packageNames := registryPackageNamesForDependencyRepo(dep, packages)
	if len(packageNames) == 0 {
		return false
	}
	return hasMatchingAdvisoryForPackageNames(packageNames, advisories)
}

func needsRegistryAdvisoryMappingFinding(dep auditedDependency, packages []project.RegistryPackage, advisories []project.RegistryAdvisory) bool {
	if dep.Kind != "git" {
		return false
	}
	parsed, err := gitdep.ParseSource(dep.Source)
	if err != nil {
		return false
	}
	if strings.TrimSpace(parsed.Ref) == "" {
		return false
	}
	packageNames := registryPackageNamesForDependencyRepo(dep, packages)
	if len(packageNames) == 0 {
		return false
	}
	return hasMatchingAdvisoryForPackageNames(packageNames, advisories)
}

func registryPackageNamesForDependencyRepo(dep auditedDependency, packages []project.RegistryPackage) map[string]struct{} {
	names := make(map[string]struct{})
	parsed, err := gitdep.ParseSource(dep.Source)
	repo := parsed.Repo
	if err != nil || strings.TrimSpace(repo) == "" {
		return names
	}

	repoCanonical := scan.NormalizeGitRepositoryPath(repo)
	if repoCanonical == "" {
		repoCanonical = strings.TrimSpace(repo)
	}
	for _, pkg := range packages {
		pkgParsed, pkgErr := gitdep.ParseSource(pkg.Source)
		pkgRepo := pkgParsed.Repo
		if pkgErr != nil || strings.TrimSpace(pkgRepo) == "" {
			continue
		}
		pkgRepoCanonical := scan.NormalizeGitRepositoryPath(pkgRepo)
		if pkgRepoCanonical == "" {
			pkgRepoCanonical = strings.TrimSpace(pkgRepo)
		}
		if pkgRepoCanonical == repoCanonical {
			name := strings.TrimSpace(pkg.Name)
			if name != "" {
				names[name] = struct{}{}
			}
		}
	}
	return names
}

func hasMatchingAdvisoryForPackageNames(packageNames map[string]struct{}, advisories []project.RegistryAdvisory) bool {
	for _, advisory := range advisories {
		name := strings.TrimSpace(advisory.Name)
		if name == "" {
			continue
		}
		if _, ok := packageNames[name]; ok {
			return true
		}
	}
	return false
}

func refFromSource(source string) string {
	parsed, err := gitdep.ParseSource(source)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Ref)
}

func advisoryAppliesToDependency(advisory project.RegistryAdvisory, dep auditedDependency) (bool, error) {
	if strings.TrimSpace(advisory.Name) == "" || strings.TrimSpace(dep.PackageName) == "" {
		return false, nil
	}
	if strings.TrimSpace(advisory.Name) != strings.TrimSpace(dep.PackageName) {
		return false, nil
	}
	rangeExpr := strings.TrimSpace(advisory.Range)
	if rangeExpr == "" {
		return true, nil
	}
	if strings.TrimSpace(dep.PackageVer) == "" || !project.IsSemver(dep.PackageVer) {
		return false, nil
	}
	ok, err := matchesSemverRange(dep.PackageVer, rangeExpr)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func matchesSemverRange(version, rangeExpr string) (bool, error) {
	normalizedVersion := project.NormalizeRegistryVersion(version)
	if !project.IsSemver(normalizedVersion) {
		return false, fmt.Errorf("invalid version: %s", version)
	}
	trimmedRange := strings.TrimSpace(rangeExpr)
	if trimmedRange == "" {
		return true, nil
	}
	semverVersion, err := msemver.NewVersion(normalizedVersion)
	if err != nil {
		return false, fmt.Errorf("invalid semver version: %w", err)
	}
	constraints, err := msemver.NewConstraint(trimmedRange)
	if err != nil {
		return false, fmt.Errorf("invalid advisory range: %w", err)
	}
	return constraints.Check(semverVersion), nil
}

func dedupeAuditFindings(findings []DependencyAuditFinding) []DependencyAuditFinding {
	if len(findings) == 0 {
		return findings
	}
	seen := make(map[string]struct{}, len(findings))
	out := make([]DependencyAuditFinding, 0, len(findings))
	for _, finding := range findings {
		key := strings.Join([]string{
			strings.TrimSpace(finding.Source),
			strings.TrimSpace(finding.Kind),
			strings.TrimSpace(strings.ToLower(finding.Severity)),
			strings.TrimSpace(finding.Code),
			strings.TrimSpace(finding.Message),
		}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, finding)
	}
	return out
}
