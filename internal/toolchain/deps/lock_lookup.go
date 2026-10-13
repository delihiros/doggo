package deps

import (
	"doggo/internal/depsource"
	"doggo/internal/project"
	"doggo/internal/toolchain/depsnapshot"
	"fmt"
)

const (
	lockLookupErrorAmbiguous    = "ambiguous-lock-entry"
	lockLookupErrorKindMismatch = "dependency-kind-mismatch"
	lockLookupErrorKindConflict = "conflicting-lock-kinds"
)

type lockLookupError struct {
	code string
	msg  string
}

func (e *lockLookupError) Error() string { return e.msg }

func isLockLookupErrorCode(err error, code string) bool {
	lookupErr, ok := err.(*lockLookupError)
	return ok && lookupErr.code == code
}

func findDirectLockDep(projectRoot string, lockDeps []project.LockDep, source, expectedKind string) (project.LockDep, bool, error) {
	dep, ok, err := findLockDepBySource(lockDeps, source, expectedKind)
	if expectedKind != depsource.Path || !isLockLookupErrorCode(err, lockLookupErrorAmbiguous) {
		return dep, ok, err
	}
	moduleID, identityErr := depsnapshot.PathDependencyModuleID(projectRoot, source)
	if identityErr != nil {
		return project.LockDep{}, false, &lockLookupError{
			code: lockLookupErrorAmbiguous,
			msg:  fmt.Sprintf("ambiguous lock entries for source %q: cannot resolve module identity: %v", source, identityErr),
		}
	}
	contextMatches := make([]project.LockDep, 0, 1)
	for _, candidate := range lockDeps {
		if candidate.Source == source && candidate.ModuleID == moduleID {
			contextMatches = append(contextMatches, candidate)
		}
	}
	if len(contextMatches) == 0 {
		// A changed identity cannot establish which previous lock entry was direct.
		return project.LockDep{}, false, err
	}
	return findLockDepBySource(contextMatches, source, expectedKind)
}

func findLockDepBySource(lockDeps []project.LockDep, source string, expectedKind string) (project.LockDep, bool, error) {
	allSourceMatches := make([]project.LockDep, 0, 2)
	for _, dep := range lockDeps {
		if dep.Source != source {
			continue
		}
		allSourceMatches = append(allSourceMatches, dep)
	}
	if len(allSourceMatches) == 0 {
		return project.LockDep{}, false, nil
	}

	kindSet := make(map[string]struct{}, 2)
	for _, dep := range allSourceMatches {
		kindSet[dep.Kind] = struct{}{}
	}
	if len(kindSet) > 1 {
		return project.LockDep{}, false, &lockLookupError{
			code: lockLookupErrorKindConflict,
			msg:  fmt.Sprintf("conflicting lock kinds for source %q", source),
		}
	}

	matches := allSourceMatches
	if expectedKind != "" {
		matches = matches[:0]
		for _, dep := range allSourceMatches {
			if dep.Kind == expectedKind {
				matches = append(matches, dep)
			}
		}
		if len(matches) == 0 {
			return project.LockDep{}, false, &lockLookupError{
				code: lockLookupErrorKindMismatch,
				msg:  fmt.Sprintf("dependency kind mismatch for source %q (expected=%s)", source, expectedKind),
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}

	first := matches[0]
	for i := 1; i < len(matches); i++ {
		current := matches[i]
		if first.Kind != current.Kind || first.ModuleID != current.ModuleID || first.GoModule != current.GoModule || first.Revision != current.Revision || first.Digest != current.Digest || first.InstallDir != current.InstallDir {
			return project.LockDep{}, false, &lockLookupError{
				code: lockLookupErrorAmbiguous,
				msg:  fmt.Sprintf("ambiguous lock entries for source %q", source),
			}
		}
	}
	return first, true, nil
}
