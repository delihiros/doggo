package deps

import (
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
)

func TestAdditionalReviewAnnotatedTagOutdated(t *testing.T) {
	base := t.TempDir()
	source, commit := createGitUpdateDependency(t, base, "tagged")
	runUpdateGit(t, filepath.Join(base, "tagged"), "tag", "-a", "v1.0.0", "-m", "release")
	source = strings.TrimSuffix(source, "main") + "v1.0.0"
	status := outdatedDependencyStatus(t.TempDir(), source, []project.LockDep{{Source: source, Kind: "git", Revision: commit}})
	t.Logf("locked commit = %s; status = %+v", commit, status)
	if status.CheckError != "" || status.Outdated {
		t.Fatalf("unchanged annotated tag is reported outdated: %+v", status)
	}
}

func TestGitOutdatedRefSelection(t *testing.T) {
	base := t.TempDir()
	source, taggedCommit := createGitUpdateDependency(t, base, "refs")
	repository := filepath.Join(base, "refs")
	runUpdateGit(t, repository, "tag", "-a", "release", "-m", "release")
	runUpdateGit(t, repository, "tag", "lightweight")
	branchCommit := commitGitUpdateDependency(t, base, "refs", "2")
	runUpdateGit(t, repository, "branch", "release")
	for _, fixture := range []struct {
		ref, locked, want string
		outdated          bool
	}{
		{"release", taggedCommit, taggedCommit, false},
		{"refs/tags/release", taggedCommit, taggedCommit, false},
		{"refs/heads/release", taggedCommit, branchCommit, true},
		{"lightweight", taggedCommit, taggedCommit, false},
		{"main", branchCommit, branchCommit, false},
		{taggedCommit[:12], taggedCommit, taggedCommit, false},
	} {
		t.Run(fixture.ref, func(t *testing.T) {
			declaration := strings.TrimSuffix(source, "main") + fixture.ref
			status := outdatedDependencyStatus(t.TempDir(), declaration, []project.LockDep{{Source: declaration, Kind: "git", Revision: fixture.locked}})
			if status.CheckError != "" || status.Outdated != fixture.outdated || status.LatestRevision != fixture.want {
				t.Fatalf("status = %+v, want commit %s outdated=%t", status, fixture.want, fixture.outdated)
			}
		})
	}
	runUpdateGit(t, repository, "tag", "-f", "-a", "release", "-m", "moved")
	declaration := strings.TrimSuffix(source, "main") + "release"
	status := outdatedDependencyStatus(t.TempDir(), declaration, []project.LockDep{{Source: declaration, Kind: "git", Revision: taggedCommit}})
	if status.CheckError != "" || !status.Outdated || status.LatestRevision != branchCommit {
		t.Fatalf("moved annotated tag status = %+v", status)
	}
}

func TestGitOutdatedFullPinCannotBeShadowedByNamedRefs(t *testing.T) {
	base := t.TempDir()
	source, pinned := createGitUpdateDependency(t, base, "pins")
	other := commitGitUpdateDependency(t, base, "pins", "2")
	repository := filepath.Join(base, "pins")
	runUpdateGit(t, repository, "tag", pinned)
	runUpdateGit(t, repository, "tag", pinned[:12])
	runUpdateGit(t, repository, "branch", pinned)
	for _, fixture := range []struct {
		name, ref, locked, want string
		outdated                bool
	}{
		{"full pin", pinned, pinned, pinned, false},
		{"abbreviated named tag retains precedence", pinned[:12], other, other, false},
		{"qualified full hex tag", "refs/tags/" + pinned, pinned, other, true},
		{"qualified full hex branch", "refs/heads/" + pinned, pinned, other, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			declaration := strings.TrimSuffix(source, "main") + fixture.ref
			status := outdatedDependencyStatus(t.TempDir(), declaration, []project.LockDep{{Source: declaration, Kind: "git", Revision: fixture.locked}})
			if status.CheckError != "" || status.Outdated != fixture.outdated || status.LatestRevision != fixture.want {
				t.Fatalf("status = %+v, want commit %s outdated=%t", status, fixture.want, fixture.outdated)
			}
		})
	}
}
