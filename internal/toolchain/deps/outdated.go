package deps

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"doggo/internal/depsource"
	"doggo/internal/project"
	"doggo/internal/toolchain/depsnapshot"
)

var errGitRemoteRefNotFound = errors.New("git remote ref not found")

var (
	newOutdatedTempRoot              = project.NewOwnedTempRoot
	closeOutdatedTempRoot            = (*project.OwnedTempRoot).Close
	snapshotOutdatedDependenciesTree = depsnapshot.SnapshotDependenciesTree
)

func OutdatedDependencies(projectRoot string) (_ []DependencyOutdatedStatus, err error) {
	session, err := project.OpenGenerationSession(project.ModPath(projectRoot))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	mod, err := project.LoadModFile(project.ModPath(session.Config.ProjectRoot))
	if err != nil {
		return nil, err
	}
	projectRoot = session.Config.ProjectRoot
	lock := session.Config.Lock

	out := make([]DependencyOutdatedStatus, 0, len(mod.Deps))
	for _, source := range mod.Deps {
		out = append(out, outdatedDependencyStatus(projectRoot, source, lock.Deps))
	}
	return out, nil
}

func outdatedDependencyStatus(projectRoot, source string, lockDeps []project.LockDep) DependencyOutdatedStatus {
	status := DependencyOutdatedStatus{Source: source}
	parsed, err := depsource.Parse(source)
	if err != nil {
		status.Outdated = true
		status.CheckError = err.Error()
		return status
	}
	status.Kind = parsed.Kind
	lockDep, ok, err := findDirectLockDep(projectRoot, lockDeps, parsed.Declaration, parsed.Kind)
	if err != nil {
		status.Outdated = true
		status.CheckError = err.Error()
		return status
	}
	if !ok {
		status.Outdated = true
		status.CheckError = "dependency is not locked (run 'doggo mod sync')"
		return status
	}
	status.LockedDigest = lockDep.Digest

	switch parsed.Kind {
	case depsource.Path, depsource.Go:
		if parsed.Kind == depsource.Go {
			status.LockedRevision = lockDep.Revision
		}
		fillSnapshotOutdatedStatus(projectRoot, parsed, lockDep, &status)
	case depsource.Git:
		status.LockedRevision = lockDep.Revision
		fillGitOutdatedStatus(parsed, lockDep, &status)
	default:
		status.Outdated = true
		status.CheckError = fmt.Sprintf("unsupported dependency kind: %s", parsed.Kind)
	}
	return status
}

func fillSnapshotOutdatedStatus(projectRoot string, parsed depsource.Source, lockDep project.LockDep, status *DependencyOutdatedStatus) {
	stage, err := newOutdatedTempRoot("doggo-outdated-")
	if err != nil {
		status.Outdated = true
		status.CheckError = err.Error()
		return
	}
	projectHandle, projectPath, err := project.OpenVerifiedRoot(projectRoot)
	if err != nil {
		status.Outdated = true
		status.CheckError = errors.Join(err, closeOutdatedTempRoot(stage)).Error()
		return
	}
	deps, snapshotErr := snapshotOutdatedDependenciesTree(projectHandle, projectPath, stage.Tree(), []string{parsed.Declaration})
	if err := errors.Join(snapshotErr, projectHandle.Close(), closeOutdatedTempRoot(stage)); err != nil {
		status.Outdated = true
		status.CheckError = err.Error()
		return
	}
	dep, ok, err := findDirectLockDep(projectRoot, deps, parsed.Declaration, parsed.Kind)
	if err != nil {
		status.Outdated = true
		status.CheckError = err.Error()
		return
	}
	if ok {
		status.LatestDigest = dep.Digest
		status.Outdated = lockDep.Digest != dep.Digest
		if parsed.Kind == depsource.Go {
			status.LatestRevision = dep.Revision
			status.Outdated = status.Outdated || lockDep.Revision != dep.Revision
		}
		return
	}
	status.Outdated = true
	status.CheckError = "dependency snapshot is missing"
}

func fillGitOutdatedStatus(parsed depsource.Source, lockDep project.LockDep, status *DependencyOutdatedStatus) {
	latestRef, err := resolveLatestGitRef(parsed)
	if err != nil {
		status.CheckError = err.Error()
		return
	}
	status.LatestRevision = latestRef
	if isGitCommitPrefix(latestRef) {
		status.Outdated = !hasCommitPrefix(lockDep.Revision, latestRef)
		if !status.Outdated {
			// lock stores canonical commit hash; keep that in display for matched prefixes.
			status.LatestRevision = lockDep.Revision
		}
		return
	}
	status.Outdated = !strings.EqualFold(lockDep.Revision, latestRef)
}

func resolveLatestGitRef(parsed depsource.Source) (string, error) {
	repo, ref := parsed.Repository, parsed.Ref

	commit, err := queryGitRemoteRef(repo, ref)
	if err == nil {
		return commit, nil
	}

	// If remote does not expose this value as branch/tag but the ref looks like a
	// commit prefix, treat it as a pinned commit expression.
	// This fallback is allowed only when at least one remote target was reachable
	// and returned "not found".
	if isGitCommitPrefix(ref) && errors.Is(err, errGitRemoteRefNotFound) {
		return strings.ToLower(ref), nil
	}
	return "", fmt.Errorf("failed to resolve remote ref for %q: %w", parsed.Declaration, err)
}

func queryGitRemoteRef(target, ref string) (string, error) {
	if ref == "" {
		return queryGitRemoteCommit(target, "HEAD")
	}
	candidates := []string{ref}
	switch {
	case strings.HasPrefix(ref, "refs/tags/"):
		candidates = []string{ref + "^{}", ref}
	case !strings.HasPrefix(ref, "refs/") && ref != "HEAD":
		// Match clone's tag precedence and compare an annotated tag's peeled commit.
		candidates = []string{"refs/tags/" + ref + "^{}", "refs/tags/" + ref, "refs/heads/" + ref}
	}
	out, err := runGitOutput("", append([]string{"ls-remote", target}, candidates...)...)
	if err != nil {
		return "", err
	}
	if isGitCommitHash(ref) {
		// Full object IDs retain immutable pin semantics even when a remote ref
		// has the same name. The successful query still checks remote reachability.
		return strings.ToLower(ref), nil
	}
	refs := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && isGitCommitHash(fields[0]) {
			refs[fields[1]] = fields[0]
		}
	}
	for _, candidate := range candidates {
		if commit, ok := refs[candidate]; ok {
			return commit, nil
		}
	}
	return "", fmt.Errorf("%w: ref %q", errGitRemoteRefNotFound, ref)
}

func queryGitRemoteCommit(target, query string) (string, error) {
	out, err := runGitOutput("", "ls-remote", target, query)
	if err != nil {
		return "", err
	}
	commit := parseLSRemoteCommit(out)
	if commit == "" {
		return "", fmt.Errorf("%w: %s", errGitRemoteRefNotFound, query)
	}
	return commit, nil
}

func runGitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func parseLSRemoteCommit(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		if isGitCommitHash(fields[0]) {
			return fields[0]
		}
	}
	return ""
}

func isGitCommitHash(v string) bool {
	if len(v) != 40 {
		return false
	}
	for _, ch := range v {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			continue
		}
		return false
	}
	return true
}

func isGitCommitPrefix(v string) bool {
	if len(v) < 7 || len(v) > 40 {
		return false
	}
	for _, ch := range v {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			continue
		}
		return false
	}
	return true
}

func hasCommitPrefix(fullCommit, prefix string) bool {
	full := strings.ToLower(strings.TrimSpace(fullCommit))
	refPrefix := strings.ToLower(strings.TrimSpace(prefix))
	if full == "" || refPrefix == "" {
		return false
	}
	return strings.HasPrefix(full, refPrefix)
}
