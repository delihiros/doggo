package depsnapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"doggo/internal/depsource"
	"doggo/internal/project"
	"doggo/internal/toolchain/gitdep"
	"golang.org/x/mod/modfile"
)

// RetainedDependencyClosure reconstructs edges from validated snapshot
// manifests. Path spellings remain relative to their declaring dependency;
// they are never resolved against the live project.
func RetainedDependencyClosure(snapshot *project.GenerationSnapshot, locked, direct []project.LockDep, selected string) ([]project.LockDep, error) {
	retained, _, err := RetainedDependencyClosureWithPaths(snapshot, locked, direct, selected)
	return retained, err
}

// RetainedDependencyClosureWithPaths also preserves proven local contexts
// reached by unselected roots. Each path is relative to the project's origin,
// independently of the globally chosen Source spelling in its lock entry.
func RetainedDependencyClosureWithPaths(snapshot *project.GenerationSnapshot, locked, direct []project.LockDep, selected string) ([]project.LockDep, map[string]project.LockDep, error) {
	graph := newRetainedGraph(snapshot, locked)
	direct = append([]project.LockDep(nil), direct...)
	sort.Slice(direct, func(i, j int) bool { return direct[i].Source < direct[j].Source })
	for _, dep := range direct {
		parsed, err := depsource.Parse(dep.Source)
		if err != nil {
			return nil, nil, err
		}
		if parsed.Kind == depsource.Path {
			graph.rememberContext(dep, path.Clean(dep.Source))
		}
	}
	// Reconstruct the old graph in snapshot acquisition order first. A shared
	// path keeps its first acquired declaration in the lock; another parent may
	// spell the same contextual path differently. The selected root's old edges
	// establish those aliases but are not retained unless an unselected root
	// still reaches them.
	for _, dep := range direct {
		if err := graph.visit(dep, retainedDirectContext(dep), dep.Kind == depsource.Path); err != nil {
			return nil, nil, err
		}
	}
	graph.retained = make(map[string]project.LockDep)
	graph.retainedPaths = make(map[string]project.LockDep)
	graph.seen = make(map[string]bool)
	for _, dep := range direct {
		if dep.Source == selected {
			continue
		}
		if err := graph.visit(dep, retainedDirectContext(dep), dep.Kind == depsource.Path); err != nil {
			return nil, nil, err
		}
	}
	result := make([]project.LockDep, 0, len(graph.retained))
	for _, dep := range graph.retained {
		result = append(result, dep)
	}
	return project.NormalizeLockDeps(result), graph.retainedPaths, nil
}

func retainedDirectContext(dep project.LockDep) string {
	if dep.Kind == depsource.Path {
		// Local contexts are relative to the retained project's origin. An
		// absolute synthetic anchor would clamp leading .. components and
		// alias dependencies located at different ancestor depths.
		return path.Clean(dep.Source)
	}
	return path.Join("/remote", project.DependencyKey(dep))
}

type retainedGraph struct {
	snapshot      *project.GenerationSnapshot
	locked        []project.LockDep
	contexts      map[string]project.LockDep
	retained      map[string]project.LockDep
	seen          map[string]bool
	active        map[string]bool
	keyContexts   map[string]map[string]bool
	retainedPaths map[string]project.LockDep
}

func newRetainedGraph(snapshot *project.GenerationSnapshot, locked []project.LockDep) retainedGraph {
	return retainedGraph{snapshot: snapshot, locked: locked, contexts: make(map[string]project.LockDep), retained: make(map[string]project.LockDep), seen: make(map[string]bool), active: make(map[string]bool), keyContexts: make(map[string]map[string]bool)}
}

func (g *retainedGraph) rememberContext(dep project.LockDep, context string) {
	g.contexts[context] = dep
	key := project.DependencyKey(dep)
	if g.keyContexts[key] == nil {
		g.keyContexts[key] = make(map[string]bool)
	}
	g.keyContexts[key][context] = true
}

func (g *retainedGraph) visit(dep project.LockDep, context string, provenContext bool) error {
	key := project.DependencyKey(dep)
	if provenContext {
		g.rememberContext(dep, context)
		if g.retainedPaths != nil {
			g.retainedPaths[context] = dep
		}
	}
	g.retained[key] = dep
	visitKey := key + ":" + context
	if g.seen[visitKey] {
		return nil
	}
	if g.active[key] {
		return fmt.Errorf("retained dependency cycle at %s", dep.ModuleID)
	}
	g.active[key] = true
	defer delete(g.active, key)
	g.seen[visitKey] = true
	manifests, err := g.manifests(dep.InstallDir)
	if err != nil {
		return fmt.Errorf("retained dependency %s: %w", dep.ModuleID, err)
	}
	if manifests.moduleID != dep.ModuleID || manifests.goModule != dep.GoModule {
		return fmt.Errorf("retained dependency %s manifest identity mismatch", dep.ModuleID)
	}
	for _, source := range manifests.doggoDeps {
		child, childContext, err := g.child(dep, context, source, provenContext)
		if err != nil {
			return err
		}
		if err := g.visit(child, childContext, provenContext && child.Kind == depsource.Path); err != nil {
			return err
		}
	}
	if dep.Kind != depsource.Go && manifests.goMod != nil {
		for _, replacement := range manifests.goMod.Replace {
			if replacement.New.Version != "" {
				continue
			}
			// Snapshotting rewrites local Go replacements to exact install dirs.
			install := path.Clean(path.Join(dep.InstallDir, replacement.New.Path))
			var matches []project.LockDep
			for _, candidate := range g.locked {
				if candidate.InstallDir == install {
					matches = append(matches, candidate)
				}
			}
			child, err := uniqueRetainedChild(matches, replacement.New.Path)
			if err != nil {
				return err
			}
			// The rewritten replacement proves the child's snapshot identity,
			// but does not preserve its original relative path. Reuse contexts
			// already proved by direct declarations or Doggo edges; never rebase
			// the child's globally chosen Source spelling against this parent.
			contexts := make([]string, 0, len(g.keyContexts[project.DependencyKey(child)]))
			for known := range g.keyContexts[project.DependencyKey(child)] {
				contexts = append(contexts, known)
			}
			// Identical snapshots can have different escaping child closures at
			// different paths. The rewritten replacement cannot select one of
			// those paths, so retaining their union would keep unrelated children.
			if len(contexts) > 1 {
				return fmt.Errorf("ambiguous retained replacement context for %s", child.ModuleID)
			}
			sort.Strings(contexts)
			if len(contexts) == 0 {
				if err := g.visit(child, path.Join("/retained", project.DependencyKey(child)), false); err != nil {
					return err
				}
			}
			for _, known := range contexts {
				if err := g.visit(child, known, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (g *retainedGraph) child(parent project.LockDep, context, source string, provenContext bool) (project.LockDep, string, error) {
	parsed, err := depsource.Parse(source)
	if err != nil {
		return project.LockDep{}, "", err
	}
	childContext := path.Join("/remote", parsed.Declaration)
	var matches []project.LockDep
	if parsed.Kind == depsource.Path {
		childContext = path.Join(context, parsed.Declaration)
		if provenContext {
			if dep, ok := g.contexts[childContext]; ok {
				return dep, childContext, nil
			}
		}
		// A descendant manifest is part of the parent's retained content. It
		// supplies contextual identity even when several modules use ./child.
		relative := path.Clean(parsed.Declaration)
		if !provenContext && (relative == ".." || strings.HasPrefix(relative, "../")) {
			return project.LockDep{}, "", fmt.Errorf("unproven retained path context for %q in %s", source, parent.ModuleID)
		}
		if relative != ".." && !strings.HasPrefix(relative, "../") {
			manifests, readErr := g.manifests(path.Join(parent.InstallDir, relative))
			if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
				return project.LockDep{}, "", readErr
			}
			if readErr == nil {
				for _, candidate := range g.locked {
					if candidate.Kind == parsed.Kind && candidate.ModuleID == manifests.moduleID && candidate.GoModule == manifests.goModule {
						matches = append(matches, candidate)
					}
				}
				dep, err := uniqueRetainedChild(matches, source)
				if err != nil {
					return project.LockDep{}, "", err
				}
				if provenContext {
					g.rememberContext(dep, childContext)
				}
				return dep, childContext, nil
			}
		}
		// A known parent does not prove the identity at an escaping child
		// path. Source is a global alias, so even a unique spelling can name
		// another parent's child after an earlier acquisition used an alias.
		return project.LockDep{}, "", fmt.Errorf("unproven retained path context for %q in %s", source, parent.ModuleID)
	}
	for _, candidate := range g.locked {
		if candidate.Source == parsed.Declaration {
			if candidate.Kind != parsed.Kind {
				return project.LockDep{}, "", fmt.Errorf("retained dependency kind mismatch for %q", source)
			}
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 && parsed.Kind != depsource.Path {
		for _, candidate := range g.locked {
			if candidate.Kind != parsed.Kind {
				continue
			}
			if parsed.Kind == depsource.Go && candidate.GoModule == parsed.ModulePath && (parsed.Floating() || parsed.Query == candidate.Revision) || parsed.Kind == depsource.Git && gitdep.IdentityKey(candidate.Source) == gitdep.IdentityKey(source) {
				matches = append(matches, candidate)
			}
		}
	}
	dep, err := uniqueRetainedChild(matches, source)
	if err != nil {
		return project.LockDep{}, "", err
	}
	return dep, childContext, nil
}

func uniqueRetainedChild(matches []project.LockDep, source string) (project.LockDep, error) {
	if len(matches) == 0 {
		return project.LockDep{}, fmt.Errorf("retained dependency %q is not locked", source)
	}
	first := matches[0]
	for _, candidate := range matches[1:] {
		if candidate.Kind != first.Kind || candidate.ModuleID != first.ModuleID || candidate.GoModule != first.GoModule || candidate.Digest != first.Digest || candidate.Revision != first.Revision || candidate.InstallDir != first.InstallDir {
			return project.LockDep{}, fmt.Errorf("ambiguous retained dependency identity for %q", source)
		}
	}
	return first, nil
}

func (g *retainedGraph) manifests(prefix string) (moduleManifests, error) {
	result := moduleManifests{}
	doggo, doggoErr := g.snapshot.ReadFile(path.Join(prefix, project.ModFileName))
	goMod, goErr := g.snapshot.ReadFile(path.Join(prefix, "go.mod"))
	if doggoErr != nil && !errors.Is(doggoErr, fs.ErrNotExist) {
		return result, doggoErr
	}
	if goErr != nil && !errors.Is(goErr, fs.ErrNotExist) {
		return result, goErr
	}
	if doggoErr != nil && goErr != nil {
		return result, fs.ErrNotExist
	}
	if doggoErr == nil {
		module, deps, err := parseDoggoManifest(path.Join(prefix, project.ModFileName), doggo)
		if err != nil || module == "" {
			return result, errors.Join(fmt.Errorf("invalid retained Doggo manifest"), err)
		}
		result.moduleID, result.doggoDeps = "doggo:"+module, deps
	}
	if goErr == nil {
		parsed, err := modfile.Parse(path.Join(prefix, "go.mod"), goMod, nil)
		if err != nil {
			return result, err
		}
		if parsed.Module == nil || parsed.Module.Mod.Path == "" {
			return result, fmt.Errorf("invalid retained Go manifest")
		}
		result.goModule, result.goMod = parsed.Module.Mod.Path, parsed
		if result.moduleID == "" {
			result.moduleID = "go:" + result.goModule
		}
	}
	return result, nil
}
