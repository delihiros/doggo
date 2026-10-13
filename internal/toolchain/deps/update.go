package deps

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"doggo/internal/depsource"
	"doggo/internal/project"
	"doggo/internal/toolchain/generation"
	"doggo/internal/toolchain/gitdep"
	"golang.org/x/mod/module"
)

type updateConfig struct {
	targetSource        string
	updateAll           bool
	loadState           func(projectRoot string) (*project.ModFile, *project.LockFile, error)
	prepareSync         func(projectRoot string, sources []string) (*generation.PreparedGeneration, error)
	prepareSelectedSync func(projectRoot, selected string, previous *project.LockFile, direct []project.LockDep) (*generation.PreparedGeneration, error)
	audit               func(projectRoot, stagedRoot string, lock *project.LockFile) ([]DependencyAuditFinding, error)
}

var filepathAbs = filepath.Abs

func runUpdateWorkflow(projectRoot string, cfg updateConfig) (_ *project.LockFile, _ []DependencyUpdateResult, retErr error) {
	loadState := cfg.loadState
	if loadState == nil {
		loadState = loadModAndMatchingLock
	}
	prepareSync := cfg.prepareSync

	mod, oldLock, err := loadState(projectRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("update stage load-current-generation: %w", err)
	}
	effective, aliases, err := selectUpdateSources(projectRoot, mod.Deps, oldLock, cfg.targetSource, cfg.updateAll)
	if err != nil {
		return nil, nil, fmt.Errorf("update stage select-dependencies: %w", err)
	}

	var prepared *generation.PreparedGeneration
	if cfg.updateAll {
		if prepareSync == nil {
			prepareSync = (generation.Service{}).PrepareSync
		}
		prepared, err = prepareSync(projectRoot, effective)
	} else {
		parsedTarget, parseErr := depsource.Parse(cfg.targetSource)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		direct := make([]project.LockDep, 0, len(mod.Deps))
		for _, source := range mod.Deps {
			parsed, parseErr := depsource.Parse(source)
			if parseErr != nil {
				return nil, nil, parseErr
			}
			// Old identity must come entirely from the previous lock.
			// A live manifest may have changed to another module that happens
			// to share this relative spelling in a nested declaring context.
			dep, ok, lookupErr := findLockDepBySource(oldLock.Deps, parsed.Declaration, parsed.Kind)
			if lookupErr != nil {
				return nil, nil, fmt.Errorf("update stage select-dependencies: %w", lookupErr)
			}
			if !ok {
				return nil, nil, fmt.Errorf("dependency %q is not locked. Run 'doggo mod sync'", source)
			}
			dep.Source = parsed.Declaration
			direct = append(direct, dep)
		}
		prepareSelected := cfg.prepareSelectedSync
		if prepareSelected == nil {
			prepareSelected = (generation.Service{}).PrepareSelectedSync
		}
		prepared, err = prepareSelected(projectRoot, parsedTarget.Declaration, oldLock, direct)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("update stage prepare-generation: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, prepared.Abort()) }()
	if err := prepared.RequireCurrentGeneration(oldLock.Generation); err != nil {
		return nil, nil, fmt.Errorf("update stage verify-current-generation: %w", err)
	}
	if prepared.Lock.ModLockInputsSHA256 != oldLock.ModLockInputsSHA256 {
		return nil, nil, fmt.Errorf("update stage prepare-generation: %s changed before writer lease", project.ModFileName)
	}
	prepared.Lock.Deps, err = restoreDeclaredSources(projectRoot, prepared.Lock.Deps, aliases)
	if err != nil {
		return nil, nil, fmt.Errorf("update stage restore-declarations: %w", err)
	}
	if cfg.audit != nil {
		findings, err := cfg.audit(projectRoot, prepared.StageRoot(), prepared.Lock)
		if err != nil {
			return nil, nil, fmt.Errorf("update stage audit-generation: %w", err)
		}
		if blockers := countBlockingFindings(findings); blockers > 0 {
			return nil, nil, fmt.Errorf("update blocked by dependency audit: %d high-severity issue(s). Run 'doggo deps audit'", blockers)
		}
	}
	resultSources := mod.Deps
	if !cfg.updateAll {
		parsed, err := depsource.Parse(cfg.targetSource)
		if err != nil {
			return nil, nil, fmt.Errorf("update stage build-update-results: %w", err)
		}
		resultSources = []string{parsed.Declaration}
	}
	results, err := buildDependencyUpdateResults(projectRoot, resultSources, oldLock.Deps, prepared.Lock.Deps, cfg.updateAll)
	if err != nil {
		return nil, nil, fmt.Errorf("update stage build-update-results: %w", err)
	}
	selectedResults := results
	if !cfg.updateAll {
		parsed, err := depsource.Parse(cfg.targetSource)
		if err != nil {
			return nil, nil, fmt.Errorf("update stage select-update-result: %w", err)
		}
		if _, ok, err := findDirectLockDep(projectRoot, prepared.Lock.Deps, parsed.Declaration, parsed.Kind); err != nil {
			return nil, nil, fmt.Errorf("update stage select-update-result: %w", err)
		} else if !ok {
			return nil, nil, fmt.Errorf("updated lock does not contain dependency source: %s", cfg.targetSource)
		}
		selectedResults = nil
		for _, result := range results {
			if result.Source == parsed.Declaration {
				selectedResults = []DependencyUpdateResult{result}
				break
			}
		}
		if selectedResults == nil {
			return nil, nil, fmt.Errorf("updated lock does not contain dependency source: %s", cfg.targetSource)
		}
	}
	if err := prepared.Commit(); err != nil {
		return nil, nil, fmt.Errorf("update stage commit-generation: %w", err)
	}
	return prepared.Lock, selectedResults, nil
}

func selectUpdateSources(projectRoot string, direct []string, lock *project.LockFile, target string, all bool) ([]string, map[string]string, error) {
	effective := append([]string(nil), direct...)
	aliases := make(map[string]string, len(direct))
	parsedDirect := make([]depsource.Source, len(direct))
	for i, dep := range direct {
		parsed, err := depsource.Parse(dep)
		if err != nil {
			return nil, nil, err
		}
		parsedDirect[i] = parsed
		key, err := sourceIdentityKey(projectRoot, dep)
		if err != nil {
			return nil, nil, err
		}
		if original, ok := aliases[key]; ok && original != dep {
			return nil, nil, fmt.Errorf("dependency declarations %q and %q share identity %q", original, dep, key)
		}
		aliases[key] = dep
	}
	if all {
		return effective, aliases, nil
	}
	if strings.TrimSpace(target) == "" {
		return nil, nil, fmt.Errorf("update command requires --all or a dependency source")
	}
	parsedTarget, err := depsource.Parse(target)
	if err != nil {
		return nil, nil, err
	}
	target = parsedTarget.Declaration
	if !containsString(direct, target) {
		return nil, nil, fmt.Errorf("dependency not found in %s: %s", project.ModFileName, target)
	}
	for i, dep := range direct {
		parsed := parsedDirect[i]
		if dep == target || parsed.Kind == depsource.Path || parsed.Kind == depsource.Go && !parsed.Floating() {
			continue
		}
		locked, ok, err := findLockDepBySource(lock.Deps, parsed.Declaration, parsed.Kind)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, fmt.Errorf("dependency %q is not locked. Run 'doggo mod sync'", dep)
		}
		var pinned string
		switch parsed.Kind {
		case depsource.Git:
			pinned = pinGitSourceToCommit(parsed.Repository, locked.Revision)
		case depsource.Go:
			if module.CanonicalVersion(locked.Revision) != locked.Revision || module.Check(parsed.ModulePath, locked.Revision) != nil {
				return nil, nil, fmt.Errorf("invalid Go module revision %q for %s", locked.Revision, parsed.ModulePath)
			}
			pinned = parsed.ModulePath + "@" + locked.Revision
		default:
			continue
		}
		effective[i] = pinned
		key, err := sourceIdentityKey(projectRoot, pinned)
		if err != nil {
			return nil, nil, err
		}
		aliases[key] = dep
	}
	return effective, aliases, nil
}

func buildDependencyUpdateResults(projectRoot string, directSources []string, oldDeps, newDeps []project.LockDep, allowUnknownOld bool) ([]DependencyUpdateResult, error) {
	results := make([]DependencyUpdateResult, 0, len(directSources))
	for _, source := range directSources {
		parsed, err := depsource.Parse(source)
		if err != nil {
			return nil, err
		}
		oldDep, oldOK, err := findLockDepBySource(oldDeps, parsed.Declaration, parsed.Kind)
		unknownOld := allowUnknownOld && isLockLookupErrorCode(err, lockLookupErrorAmbiguous)
		if err != nil && !unknownOld {
			return nil, err
		}
		if unknownOld {
			oldDep, oldOK = project.LockDep{}, false
		}
		newDep, newOK, err := findDirectLockDep(projectRoot, newDeps, parsed.Declaration, parsed.Kind)
		if err != nil {
			return nil, err
		}
		if !oldOK && !newOK {
			continue
		}
		result := DependencyUpdateResult{Source: source}
		if oldOK {
			result.Kind = oldDep.Kind
			result.OldRevision = oldDep.Revision
			result.OldDigest = oldDep.Digest
		}
		if newOK {
			if result.Kind == "" {
				result.Kind = newDep.Kind
			}
			result.NewRevision = newDep.Revision
			result.NewDigest = newDep.Digest
		}
		result.Updated = unknownOld || result.OldRevision != result.NewRevision || result.OldDigest != result.NewDigest
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Source < results[j].Source })
	return results, nil
}

func restoreDeclaredSources(projectRoot string, resolved []project.LockDep, aliases map[string]string) ([]project.LockDep, error) {
	out := append([]project.LockDep(nil), resolved...)
	for i := range out {
		key, err := sourceIdentityKey(projectRoot, out[i].Source)
		if err != nil {
			return nil, err
		}
		if original, ok := aliases[key]; ok {
			out[i].Source = original
		}
	}
	return project.NormalizeLockDeps(out), nil
}

func countBlockingFindings(findings []DependencyAuditFinding) int {
	count := 0
	for _, finding := range findings {
		if isHighSeverity(finding) {
			count++
		}
	}
	return count
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func pinGitSourceToCommit(repo, commit string) string {
	parsed, err := url.Parse(repo)
	if err != nil || parsed.Scheme == "" {
		return repo + "@" + commit
	}
	query := parsed.Query()
	query.Set("ref", commit)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func sourceIdentityKey(projectRoot, source string) (string, error) {
	parsed, err := depsource.Parse(source)
	if err != nil {
		return "", err
	}
	switch parsed.Kind {
	case depsource.Path:
		candidate := parsed.Declaration
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(projectRoot, candidate)
		}
		absolute, err := filepathAbs(candidate)
		if err != nil {
			return "", err
		}
		return filepath.Clean(absolute), nil
	case depsource.Git:
		return gitdep.IdentityKey(parsed.Declaration), nil
	case depsource.Go:
		return parsed.ModulePath, nil
	}
	return "", fmt.Errorf("unsupported dependency kind: %s", parsed.Kind)
}
