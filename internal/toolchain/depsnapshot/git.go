package depsnapshot

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"doggo/internal/project"
	"doggo/internal/toolchain/gitdep"
)

var (
	newGitDependencyTempRoot = project.NewOwnedTempRoot
	cleanupGitDependencyTemp = func(root *project.OwnedTempRoot) error {
		return errors.Join(root.CaptureSubprocessOutput(), root.Close())
	}
)

func cloneGitDependency(source string) (string, string, func() error, error) {
	parsed, err := gitdep.ParseSource(source)
	if err != nil {
		return "", "", nil, err
	}
	temporary, err := newGitDependencyTempRoot("doggo-snapshot-git-")
	if err != nil {
		return "", "", nil, err
	}
	cleanup := func() error { return cleanupGitDependencyTemp(temporary) }
	repository := filepath.Join(temporary.Path(), "repository")
	if _, err := runGit("", "clone", "--quiet", parsed.Repo, repository); err != nil {
		return "", "", nil, errors.Join(err, cleanup())
	}
	if parsed.Ref != "" {
		resolved, err := resolveClonedGitRef(repository, parsed.Ref)
		if err != nil {
			return "", "", nil, errors.Join(err, cleanup())
		}
		revision := strings.TrimSpace(resolved)
		if _, err := runGit(repository, "checkout", "--detach", "--quiet", revision); err != nil {
			return "", "", nil, errors.Join(err, cleanup())
		}
		return repository, revision, cleanup, nil
	}
	revision, err := runGit(repository, "rev-parse", "HEAD")
	if err != nil {
		return "", "", nil, errors.Join(err, cleanup())
	}
	return repository, strings.TrimSpace(revision), cleanup, nil
}

func resolveClonedGitRef(repository, ref string) (string, error) {
	if len(ref) == 40 {
		if _, err := hex.DecodeString(ref); err == nil {
			// A full object ID is an immutable pin. Resolve from the object database,
			// never from a branch or tag whose name happens to contain that ID.
			objectID := strings.ToLower(ref)
			objects, err := runGit(repository, "rev-parse", "--disambiguate="+objectID)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(objects) != objectID {
				return "", fmt.Errorf("git object %s is not available", objectID)
			}
			return runGit(repository, "-c", "core.warnAmbiguousRefs=false", "rev-parse", "--verify", "--end-of-options", objectID+"^{commit}")
		}
	}
	candidates := []string{ref}
	switch {
	case strings.HasPrefix(ref, "refs/heads/"):
		// Clone creates only the default local branch; other branches are remote refs.
		candidates = []string{"refs/remotes/origin/" + strings.TrimPrefix(ref, "refs/heads/")}
	case !strings.HasPrefix(ref, "refs/") && ref != "HEAD":
		// Keep Git's tag precedence when a tag and branch have the same name.
		candidates = []string{"refs/tags/" + ref, "refs/remotes/origin/" + ref, ref}
	}
	var lastErr error
	for _, candidate := range candidates {
		resolved, err := runGit(repository, "rev-parse", "--verify", "--end-of-options", candidate+"^{commit}")
		if err == nil {
			return resolved, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func runGit(directory string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
