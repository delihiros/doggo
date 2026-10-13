package depsnapshot

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAdditionalReviewNondefaultGitBranch(t *testing.T) {
	repository := t.TempDir()
	writeFile(t, filepath.Join(repository, "doggo.mod"), `{:module "example/branch" :deps []}`)
	runTestGit(t, repository, "init")
	runTestGit(t, repository, "symbolic-ref", "HEAD", "refs/heads/main")
	runTestGit(t, repository, "config", "user.email", "doggo@example.invalid")
	runTestGit(t, repository, "config", "user.name", "Doggo Test")
	runTestGit(t, repository, "add", ".")
	runTestGit(t, repository, "commit", "-m", "main")
	revisionOfHEAD := func() string {
		out, err := runGit(repository, "rev-parse", "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	mainRevision := revisionOfHEAD()
	runTestGit(t, repository, "tag", "-a", "release", "-m", "release")
	runTestGit(t, repository, "tag", "lightweight")
	runTestGit(t, repository, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(repository, "feature.dog"), "(def feature true)\n")
	runTestGit(t, repository, "add", ".")
	runTestGit(t, repository, "commit", "-m", "feature")
	featureRevision := revisionOfHEAD()
	runTestGit(t, repository, "branch", "release")
	runTestGit(t, repository, "checkout", "main")
	for _, fixture := range []struct{ ref, want string }{
		{"feature", featureRevision},
		{"refs/heads/feature", featureRevision},
		{"release", mainRevision}, // Unqualified names retain Git's tag precedence.
		{"refs/tags/release", mainRevision},
		{"refs/heads/release", featureRevision},
		{"lightweight", mainRevision},
		{mainRevision, mainRevision},
		{featureRevision[:12], featureRevision},
	} {
		t.Run(fixture.ref, func(t *testing.T) {
			source := fileURLForTest(repository) + "?ref=" + fixture.ref
			_, revision, cleanup, err := cloneGitDependency(source)
			if cleanup != nil {
				t.Cleanup(func() {
					if err := cleanup(); err != nil {
						t.Fatal(err)
					}
				})
			}
			if err != nil {
				t.Fatalf("valid Git ref cannot be acquired: %v", err)
			}
			if revision != fixture.want {
				t.Fatalf("revision = %s, want %s", revision, fixture.want)
			}
		})
	}
}

func TestGitFullCommitPinCannotBeShadowedByNamedRefs(t *testing.T) {
	repository := t.TempDir()
	writeFile(t, filepath.Join(repository, "doggo.mod"), `{:module "example/pinned" :deps []}`)
	runTestGit(t, repository, "init")
	runTestGit(t, repository, "symbolic-ref", "HEAD", "refs/heads/main")
	runTestGit(t, repository, "config", "user.email", "doggo@example.invalid")
	runTestGit(t, repository, "config", "user.name", "Doggo Test")
	runTestGit(t, repository, "add", ".")
	runTestGit(t, repository, "commit", "-m", "pinned")
	commit, err := runGit(repository, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	pinned := strings.TrimSpace(commit)
	writeFile(t, filepath.Join(repository, "value.dog"), "(def value 2)\n")
	runTestGit(t, repository, "add", ".")
	runTestGit(t, repository, "commit", "-m", "other")
	commit, err = runGit(repository, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	other := strings.TrimSpace(commit)
	runTestGit(t, repository, "tag", pinned)
	runTestGit(t, repository, "tag", pinned[:12])
	runTestGit(t, repository, "branch", pinned)
	missingObject := strings.Repeat("f", 40)
	runTestGit(t, repository, "tag", missingObject)
	for _, fixture := range []struct {
		name, ref, want string
		wantError       bool
	}{
		{"full pin", pinned, pinned, false},
		{"abbreviated named tag retains precedence", pinned[:12], other, false},
		{"qualified full hex tag", "refs/tags/" + pinned, other, false},
		{"qualified full hex branch", "refs/heads/" + pinned, other, false},
		{"missing full object cannot select tag", missingObject, "", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			cloned, revision, cleanup, err := cloneGitDependency(fileURLForTest(repository) + "?ref=" + fixture.ref)
			if cleanup != nil {
				t.Cleanup(func() {
					if err := cleanup(); err != nil {
						t.Fatal(err)
					}
				})
			}
			if fixture.wantError {
				if err == nil {
					t.Fatalf("missing immutable object acquired named tag commit %s", revision)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if revision != fixture.want {
				t.Fatalf("revision = %s, want %s (other commit %s)", revision, fixture.want, other)
			}
			head, err := runGit(cloned, "rev-parse", "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(head) != fixture.want {
				t.Fatalf("checked-out HEAD = %s, want %s", head, fixture.want)
			}
		})
	}
}
