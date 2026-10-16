package depsnapshot

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"doggo/internal/depsource"
	"doggo/internal/project"
	"doggo/internal/syntax/frontend"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
	"golang.org/x/mod/modfile"
)

var beforeSnapshotDirectoryOpenForTest func(string)

var afterManifestReadForTest func(string)
var afterSourceComponentOpenForTest func(string)

type sourceNode struct {
	path   string
	root   *os.Root
	parent *sourceNode
}

type resolvedSource struct {
	node   *sourceNode
	opened []*os.Root
}

func (source *resolvedSource) close() (err error) {
	for i := len(source.opened) - 1; i >= 0; i-- {
		err = errors.Join(err, source.opened[i].Close())
	}
	return err
}

type snapshotter struct {
	depsRoot               *os.Root
	tree                   *project.OwnedTree
	records                map[string]project.LockDep
	acquired               map[string]project.LockDep
	active                 map[string]bool
	modules                map[string]moduleSnapshot
	goModules              map[string]goModuleSnapshot
	retainedPaths          map[string]project.LockDep
	retainedPathIdentities []retainedPathIdentity
}

type retainedPathIdentity struct {
	dep  project.LockDep
	info fs.FileInfo
}

type moduleSnapshot struct {
	revision string
	digest   string
}

type goModuleSnapshot struct {
	moduleID string
	revision string
	digest   string
}

// SnapshotDependencies copies a complete dependency graph into destination.
func SnapshotDependencies(projectRoot, destination string, sources []string) (_ []project.LockDep, retErr error) {
	projectHandle, projectPath, err := project.OpenVerifiedRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, projectHandle.Close()) }()
	destinationRoot, err := openOrCreateDirectoryRoot(destination)
	if err != nil {
		return nil, err
	}
	destinationTree := project.NewOwnedTree(destinationRoot)
	defer func() { retErr = errors.Join(retErr, destinationTree.Close(), destinationRoot.Close()) }()
	if err := destinationTree.CaptureExisting(); err != nil {
		return nil, err
	}
	return SnapshotDependenciesTree(projectHandle, projectPath, destinationTree, sources)
}

// SnapshotDependenciesRoot writes through destination after verifying the
// retained project root against its diagnostic path.
func SnapshotDependenciesTree(projectRoot *os.Root, projectPath string, destination *project.OwnedTree, sources []string) (_ []project.LockDep, retErr error) {
	return SnapshotDependenciesTreeRetaining(projectRoot, projectPath, destination, sources, nil, nil)
}

// SnapshotDependenciesTreeRetaining resolves sources against copied retained
// snapshots. Shared identities must agree with the retained revision/digest.
func SnapshotDependenciesTreeRetaining(projectRoot *os.Root, projectPath string, destination *project.OwnedTree, sources []string, retained, retainedDirect []project.LockDep) (_ []project.LockDep, retErr error) {
	return SnapshotDependenciesTreeRetainingPaths(projectRoot, projectPath, destination, sources, retained, retainedDirect, nil)
}

// SnapshotDependenciesTreeRetainingPaths also rejects replacing a retained
// snapshot at a proven contextual path, even if its live module ID changed.
func SnapshotDependenciesTreeRetainingPaths(projectRoot *os.Root, projectPath string, destination *project.OwnedTree, sources []string, retained, retainedDirect []project.LockDep, retainedPaths map[string]project.LockDep) (_ []project.LockDep, retErr error) {
	projectSource, err := openSourceAncestry(projectPath)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, projectSource.close()) }()
	expected, expectedErr := projectRoot.Stat(".")
	opened, openedErr := projectSource.node.root.Stat(".")
	if expectedErr != nil || openedErr != nil || !os.SameFile(expected, opened) {
		return nil, fmt.Errorf("project root changed before dependency snapshot")
	}
	depsRoot, openErr := destination.OpenRoot("deps")
	if openErr != nil {
		depsRoot, err = destination.CreateRoot("deps", 0755, nil)
		if err != nil {
			return nil, errors.Join(openErr, err)
		}
	}
	deps, snapshotErr := snapshotDependenciesTree(projectSource, depsRoot, destination, sources, retained, retainedDirect, retainedPaths)
	return deps, errors.Join(snapshotErr, depsRoot.Close())
}

func snapshotDependenciesTree(projectSource *resolvedSource, depsRoot *os.Root, tree *project.OwnedTree, sources []string, retained, retainedDirect []project.LockDep, retainedPaths map[string]project.LockDep) (_ []project.LockDep, retErr error) {
	s := &snapshotter{
		depsRoot:      depsRoot,
		tree:          tree,
		records:       make(map[string]project.LockDep),
		acquired:      make(map[string]project.LockDep),
		active:        make(map[string]bool),
		modules:       make(map[string]moduleSnapshot),
		goModules:     make(map[string]goModuleSnapshot),
		retainedPaths: make(map[string]project.LockDep, len(retainedPaths)),
	}
	for context, dep := range retainedPaths {
		absolute := filepath.Join(projectSource.node.path, filepath.FromSlash(context))
		if err := s.rememberRetainedPath(filepath.Clean(absolute), dep); err != nil {
			return nil, err
		}
		canonical, info, exists, err := canonicalRetainedPath(absolute)
		if err != nil {
			return nil, err
		}
		if exists {
			if err := s.rememberRetainedPath(canonical, dep); err != nil {
				return nil, err
			}
			s.retainedPathIdentities = append(s.retainedPathIdentities, retainedPathIdentity{dep: dep, info: info})
		}
	}
	for _, dep := range retained {
		if err := s.checkIdentity(dep); err != nil {
			return nil, err
		}
		s.records[project.DependencyKey(dep)] = dep
	}
	declarations := make([]string, 0, len(sources))
	for _, source := range sources {
		parsed, err := depsource.Parse(source)
		if err != nil {
			return nil, err
		}
		declarations = append(declarations, parsed.Declaration)
	}
	sort.Strings(declarations)
	direct := make(map[string]string, len(declarations))
	for _, dep := range retainedDirect {
		if existing, ok := direct[dep.ModuleID]; ok && existing != dep.Source {
			return nil, fmt.Errorf("dependency declarations %q and %q share resolved identity %q", existing, dep.Source, dep.ModuleID)
		}
		direct[dep.ModuleID] = dep.Source
	}
	for _, source := range declarations {
		dep, err := s.snapshot(projectSource.node, source)
		if err != nil {
			return nil, err
		}
		if existing, ok := direct[dep.ModuleID]; ok && existing != source {
			return nil, fmt.Errorf("dependency declarations %q and %q share resolved identity %q", existing, source, dep.ModuleID)
		}
		direct[dep.ModuleID] = source
	}
	for key, dep := range s.records {
		if source, ok := direct[dep.ModuleID]; ok {
			dep.Source = source
			s.records[key] = dep
		}
	}

	deps := make([]project.LockDep, 0, len(s.records))
	for _, dep := range s.records {
		deps = append(deps, dep)
	}
	return project.NormalizeLockDeps(deps), nil
}

func openOrCreateDirectoryRoot(path string) (*os.Root, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	anchor := absolute
	components := make([]string, 0, 4)
	if _, err := os.Lstat(absolute); err == nil {
		components = append(components, filepath.Base(absolute))
		anchor = filepath.Dir(absolute)
	} else if !os.IsNotExist(err) {
		return nil, err
	} else {
		for {
			components = append(components, filepath.Base(anchor))
			anchor = filepath.Dir(anchor)
			if _, err := os.Lstat(anchor); err == nil {
				break
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	root, err := os.OpenRoot(anchor)
	if err != nil {
		return nil, err
	}
	for i := len(components) - 1; i >= 0; i-- {
		component := components[i]
		next, err := openOrCreateRootChild(root, component)
		if err != nil {
			root.Close()
			return nil, err
		}
		root.Close()
		root = next
	}
	return root, nil
}

func openOrCreateRootChild(root *os.Root, name string) (*os.Root, error) {
	before, err := root.Lstat(name)
	if os.IsNotExist(err) {
		if err := root.Mkdir(name, 0755); err != nil {
			return nil, err
		}
		before, err = root.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("snapshot destination contains symlink component: %s", name)
	}
	if !before.IsDir() {
		return nil, fmt.Errorf("snapshot destination contains non-directory component: %s", name)
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil {
		child.Close()
		return nil, err
	}
	after, err := root.Lstat(name)
	if err != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		child.Close()
		return nil, fmt.Errorf("snapshot destination component changed while opening: %s", name)
	}
	return child, nil
}

func (s *snapshotter) snapshot(base *sourceNode, declaration string) (_ project.LockDep, retErr error) {
	parsed, err := depsource.Parse(declaration)
	if err != nil {
		return project.LockDep{}, err
	}
	normalized := parsed.Declaration
	kind := parsed.Kind
	revision := ""
	cleanup := func() error { return nil }
	var location *resolvedSource
	acquisitionKey := ""
	switch parsed.Kind {
	case depsource.Path:
		location, err = resolveRelativeSource(base, normalized)
		if err != nil {
			return project.LockDep{}, err
		}
		acquisitionKey = "path:" + filepath.Clean(location.node.path)
	case depsource.Git:
		sourceRoot := ""
		sourceRoot, revision, cleanup, err = cloneGitDependency(normalized)
		if err != nil {
			return project.LockDep{}, fmt.Errorf("resolve dependency %q: %w", normalized, err)
		}
		opened, err := openSourceAncestry(sourceRoot)
		if err != nil {
			return project.LockDep{}, errors.Join(fmt.Errorf("validate dependency %q: %w", normalized, err), cleanup())
		}
		location = opened
		acquisitionKey = "git:" + normalized
	case depsource.Go:
		sourceRoot, version, err := resolveGoDependency(parsed)
		if err != nil {
			return project.LockDep{}, err
		}
		location, err = openSourceAncestry(sourceRoot)
		if err != nil {
			return project.LockDep{}, fmt.Errorf("validate dependency %q: %w", normalized, err)
		}
		revision = version
		acquisitionKey = "go:" + parsed.ModulePath + "@" + version
	}
	defer func() { retErr = errors.Join(retErr, cleanup()) }()
	defer func() { retErr = errors.Join(retErr, location.close()) }()
	sourceRoot := location.node.path
	source := location.node.root

	if dep, ok := s.acquired[acquisitionKey]; ok {
		return dep, nil
	}
	if s.active[acquisitionKey] {
		return project.LockDep{}, fmt.Errorf("dependency replacement cycle at %q", normalized)
	}
	s.active[acquisitionKey] = true
	defer delete(s.active, acquisitionKey)

	manifests, err := readModuleManifests(source, sourceRoot)
	if err != nil {
		return project.LockDep{}, fmt.Errorf("read dependency %q identity: %w", normalized, err)
	}
	if kind == depsource.Go && manifests.goModule != parsed.ModulePath {
		return project.LockDep{}, fmt.Errorf("Go dependency %q declares module %q", normalized, manifests.goModule)
	}
	if afterManifestReadForTest != nil {
		afterManifestReadForTest(sourceRoot)
	}

	for _, child := range manifests.doggoDeps {
		if _, err := s.snapshot(location.node, child); err != nil {
			return project.LockDep{}, err
		}
	}
	if kind != depsource.Go && manifests.goMod != nil {
		rewritten := false
		replacements := append([]*modfile.Replace(nil), manifests.goMod.Replace...)
		for _, replacement := range replacements {
			if replacement.New.Version != "" {
				continue
			}
			if filepath.IsAbs(filepath.FromSlash(replacement.New.Path)) {
				return project.LockDep{}, fmt.Errorf("absolute replacement %q in %s", replacement.New.Path, filepath.Join(sourceRoot, "go.mod"))
			}
			child, err := s.snapshot(location.node, replacement.New.Path)
			if err != nil {
				return project.LockDep{}, err
			}
			// The host replaces snapshots by their declared Go module path, so a
			// different old-path alias could resolve to retained content instead.
			if child.GoModule == "" || child.GoModule != replacement.Old.Path {
				return project.LockDep{}, fmt.Errorf("local replacement in Go module %q: %q => %q declares Go module %q; replacement module paths must match", manifests.goModule, replacement.Old.Path, replacement.New.Path, child.GoModule)
			}
			from := filepath.Join("deps", "placeholder")
			to := filepath.FromSlash(child.InstallDir)
			relative, err := filepath.Rel(from, to)
			if err != nil {
				return project.LockDep{}, err
			}
			if err := manifests.goMod.AddReplace(replacement.Old.Path, replacement.Old.Version, filepath.ToSlash(relative), ""); err != nil {
				return project.LockDep{}, fmt.Errorf("rewrite replacement %s: %w", replacement.Old.Path, err)
			}
			rewritten = true
		}
		if rewritten {
			formatted, err := manifests.goMod.Format()
			if err != nil {
				return project.LockDep{}, fmt.Errorf("format %s: %w", filepath.Join(sourceRoot, "go.mod"), err)
			}
			override := manifests.overrides["go.mod"]
			override.content = formatted
			manifests.overrides["go.mod"] = override
		}
	}

	temporary, temporaryRoot, err := s.makeSnapshotDirectory()
	if err != nil {
		return project.LockDep{}, err
	}
	defer func() { retErr = errors.Join(retErr, temporaryRoot.Close()) }()
	published := false
	defer func() {
		if !published {
			retErr = errors.Join(retErr, s.tree.RemoveSubtree(filepath.ToSlash(filepath.Join("deps", temporary))))
		}
	}()
	digest, err := copyCanonicalTreeToOwnedTree(source, temporaryRoot, s.tree, filepath.ToSlash(filepath.Join("deps", temporary)), manifests.overrides)
	if err != nil {
		return project.LockDep{}, fmt.Errorf("copy dependency %q: %w", normalized, err)
	}
	dep := project.LockDep{
		Source: normalized, Kind: kind, ModuleID: manifests.moduleID, GoModule: manifests.goModule,
		Revision: revision, Digest: digest,
	}
	key := project.DependencyKey(dep)
	installDir := "deps/" + key
	dep.InstallDir = installDir
	if err := s.checkIdentity(dep); err != nil {
		return project.LockDep{}, err
	}
	if kind == depsource.Path && len(s.retainedPaths) != 0 {
		canonical, err := filepath.EvalSymlinks(sourceRoot)
		if err != nil {
			return project.LockDep{}, fmt.Errorf("canonicalize selected path %q: %w", normalized, err)
		}
		opened, openErr := source.Stat(".")
		canonicalInfo, statErr := os.Stat(canonical)
		if openErr != nil || statErr != nil || !os.SameFile(opened, canonicalInfo) {
			return project.LockDep{}, errors.Join(fmt.Errorf("selected path %q changed during canonicalization", normalized), openErr, statErr)
		}
		for _, path := range []string{filepath.Clean(sourceRoot), filepath.Clean(canonical)} {
			if retained, ok := s.retainedPaths[path]; ok && project.DependencyKey(retained) != key {
				return project.LockDep{}, retainedPathConflict(normalized, retained, dep)
			}
		}
		// Preserve directory identity too: a case-only rename after the retained
		// metadata check must not invalidate the canonical spelling just proved.
		for _, retained := range s.retainedPathIdentities {
			if os.SameFile(opened, retained.info) && project.DependencyKey(retained.dep) != key {
				return project.LockDep{}, retainedPathConflict(normalized, retained.dep, dep)
			}
		}
	}

	if _, err := s.depsRoot.Lstat(key); os.IsNotExist(err) {
		err = s.tree.RenameRoot(filepath.ToSlash(filepath.Join("deps", temporary)), filepath.ToSlash(filepath.Join("deps", key)))
		if err != nil {
			return project.LockDep{}, err
		}
		published = true
	} else if err != nil {
		return project.LockDep{}, err
	} else {
		existingRoot, err := s.tree.OpenRoot(filepath.ToSlash(filepath.Join("deps", key)))
		if err != nil {
			return project.LockDep{}, err
		}
		existingDigest, hashErr := project.CanonicalRootDigest(existingRoot)
		if hashErr != nil || existingDigest != digest {
			return project.LockDep{}, errors.Join(fmt.Errorf("existing dependency snapshot %s does not match digest %s", installDir, digest), hashErr, existingRoot.Close())
		}
		if err := existingRoot.Close(); err != nil {
			return project.LockDep{}, err
		}
		if err := s.tree.RemoveSubtree(filepath.ToSlash(filepath.Join("deps", temporary))); err != nil {
			return project.LockDep{}, err
		}
		published = true
	}
	if existing, ok := s.records[key]; ok && existing.Source < dep.Source {
		dep.Source = existing.Source
	}
	s.records[key] = dep
	s.acquired[acquisitionKey] = dep
	return dep, nil
}

func (s *snapshotter) rememberRetainedPath(path string, dep project.LockDep) error {
	if existing, ok := s.retainedPaths[path]; ok && project.DependencyKey(existing) != project.DependencyKey(dep) {
		return fmt.Errorf("ambiguous retained path identity for %q: %s and %s", path, existing.ModuleID, dep.ModuleID)
	}
	s.retainedPaths[path] = dep
	return nil
}

func retainedPathConflict(source string, retained, replacement project.LockDep) error {
	return fmt.Errorf("path identity conflict for %q: retained %s cannot be replaced with %s", source, retained.ModuleID, replacement.ModuleID)
}

func (s *snapshotter) makeSnapshotDirectory() (string, *os.Root, error) {
	for range 16 {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, err
		}
		name := ".snapshot-" + hex.EncodeToString(random[:])
		path := filepath.ToSlash(filepath.Join("deps", name))
		root, err := s.tree.CreateRoot(path, 0700, func() {
			if beforeSnapshotDirectoryOpenForTest != nil {
				beforeSnapshotDirectoryOpenForTest(name)
			}
		})
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		return name, root, nil
	}
	return "", nil, fmt.Errorf("create unique dependency snapshot directory")
}

func resolveRelativeSource(base *sourceNode, declaration string) (*resolvedSource, error) {
	resolved := &resolvedSource{node: base}
	components := strings.Split(filepath.Clean(filepath.FromSlash(declaration)), string(filepath.Separator))
	for len(components) > 0 && components[0] == ".." {
		components = components[1:]
		if resolved.node.parent != nil {
			resolved.node = resolved.node.parent
		}
	}
	for _, component := range components {
		switch component {
		case "", ".":
			continue
		}
		child, err := openExistingSourceChild(resolved.node.root, component)
		if err != nil {
			return nil, errors.Join(err, resolved.close())
		}
		resolved.opened = append(resolved.opened, child)
		path := filepath.Join(resolved.node.path, component)
		if afterSourceComponentOpenForTest != nil {
			afterSourceComponentOpenForTest(path)
		}
		resolved.node = &sourceNode{path: path, root: child, parent: resolved.node}
	}
	if err := validateSourceTree(resolved.node.root); err != nil {
		return nil, errors.Join(err, resolved.close())
	}
	return resolved, nil
}

func openSourceAncestry(path string) (*resolvedSource, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	selected, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	selectedInfo, err := selected.Stat(".")
	if err != nil || !selectedInfo.IsDir() {
		return nil, errors.Join(fmt.Errorf("source root is not a directory: %s", absolute), err, selected.Close())
	}
	physical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, errors.Join(err, selected.Close())
	}
	physical, err = filepath.Abs(physical)
	if err != nil {
		return nil, errors.Join(err, selected.Close())
	}
	volume := filepath.VolumeName(physical)
	anchorPath := volume + string(filepath.Separator)
	anchor, err := os.OpenRoot(anchorPath)
	if err != nil {
		return nil, errors.Join(err, selected.Close())
	}
	resolved := &resolvedSource{
		node:   &sourceNode{path: anchorPath, root: anchor},
		opened: []*os.Root{anchor},
	}
	relative, err := filepath.Rel(anchorPath, physical)
	if err != nil {
		return nil, errors.Join(err, selected.Close(), resolved.close())
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		child, err := openExistingSourceChild(resolved.node.root, component)
		if err != nil {
			return nil, errors.Join(err, selected.Close(), resolved.close())
		}
		resolved.opened = append(resolved.opened, child)
		resolved.node = &sourceNode{
			path:   filepath.Join(resolved.node.path, component),
			root:   child,
			parent: resolved.node,
		}
	}
	openedInfo, err := resolved.node.root.Stat(".")
	if err != nil || !os.SameFile(selectedInfo, openedInfo) {
		return nil, errors.Join(fmt.Errorf("source root changed while opening: %s", absolute), err, selected.Close(), resolved.close())
	}
	if err := selected.Close(); err != nil {
		return nil, errors.Join(err, resolved.close())
	}
	return resolved, nil
}

func openExistingSourceChild(root *os.Root, name string) (*os.Root, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("dependency declaration contains symlink component: %s", name)
	}
	if !before.IsDir() {
		return nil, fmt.Errorf("dependency declaration component is not a directory: %s", name)
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil {
		child.Close()
		return nil, err
	}
	after, err := root.Lstat(name)
	if err != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		child.Close()
		return nil, fmt.Errorf("dependency declaration component changed while opening: %s", name)
	}
	return child, nil
}

func (s *snapshotter) checkIdentity(dep project.LockDep) error {
	module := moduleSnapshot{revision: dep.Revision, digest: dep.Digest}
	if existing, ok := s.modules[dep.ModuleID]; ok && existing != module {
		return fmt.Errorf("module identity conflict for %s: (%s, %s) and (%s, %s)", dep.ModuleID, existing.revision, existing.digest, dep.Revision, dep.Digest)
	}
	s.modules[dep.ModuleID] = module
	if dep.GoModule == "" {
		return nil
	}
	goIdentity := goModuleSnapshot{moduleID: dep.ModuleID, revision: dep.Revision, digest: dep.Digest}
	if existing, ok := s.goModules[dep.GoModule]; ok && existing != goIdentity {
		return fmt.Errorf("Go module conflict for %s", dep.GoModule)
	}
	s.goModules[dep.GoModule] = goIdentity
	return nil
}

type moduleManifests struct {
	moduleID  string
	goModule  string
	doggoDeps []string
	goMod     *modfile.File
	overrides map[string]copyOverride
}

func readModuleManifests(root *os.Root, sourcePath string) (moduleManifests, error) {
	result := moduleManifests{overrides: make(map[string]copyOverride, 2)}
	doggoBytes, doggoMode, doggoExists, err := readManifestOnce(root, project.ModFileName)
	if err != nil {
		return moduleManifests{}, err
	}
	goBytes, goMode, goExists, err := readManifestOnce(root, "go.mod")
	if err != nil {
		return moduleManifests{}, err
	}
	if !doggoExists && !goExists {
		return moduleManifests{}, fmt.Errorf("dependency has neither doggo.mod nor go.mod manifest")
	}
	if doggoExists {
		module, deps, err := parseDoggoManifest(filepath.Join(sourcePath, project.ModFileName), doggoBytes)
		if err != nil {
			return moduleManifests{}, err
		}
		if module == "" {
			return moduleManifests{}, fmt.Errorf("%s has no module identity", filepath.Join(sourcePath, project.ModFileName))
		}
		result.moduleID = "doggo:" + module
		result.doggoDeps = deps
		result.overrides[project.ModFileName] = copyOverride{content: doggoBytes, mode: doggoMode}
	}
	if goExists {
		goPath := filepath.Join(sourcePath, "go.mod")
		parsed, err := modfile.Parse(goPath, goBytes, nil)
		if err != nil {
			return moduleManifests{}, err
		}
		if parsed.Module == nil || parsed.Module.Mod.Path == "" {
			return moduleManifests{}, fmt.Errorf("%s has no module declaration", goPath)
		}
		result.goModule = parsed.Module.Mod.Path
		result.goMod = parsed
		result.overrides["go.mod"] = copyOverride{content: goBytes, mode: goMode}
		if result.moduleID == "" {
			result.moduleID = "go:" + result.goModule
		}
	}
	return result, nil
}

func readManifestOnce(root *os.Root, name string) ([]byte, os.FileMode, bool, error) {
	before, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	if !before.Mode().IsRegular() {
		return nil, 0, false, fmt.Errorf("manifest is not a regular file: %s", name)
	}
	content, opened, err := readRootRegularFile(root, name, before)
	if err != nil {
		return nil, 0, false, fmt.Errorf("read manifest %s: %w", name, err)
	}
	mode := os.FileMode(0644)
	if opened.Mode()&0111 != 0 {
		mode = 0755
	}
	return content, mode, true, nil
}

func parseDoggoManifest(path string, content []byte) (string, []string, error) {
	form, err := frontend.ReadSingleForm(path, string(content))
	if err != nil {
		return "", nil, err
	}
	manifest, ok := form.(*coll.Map)
	if !ok {
		return "", nil, fmt.Errorf("%s: root must be a map", path)
	}
	fields := make(map[string]string, 5)
	for _, name := range []string{"module", "name", "version", "license", "compat"} {
		value := manifest.Get(atom.NewKeyword(name))
		if value == nil {
			continue
		}
		text, ok := value.(atom.String)
		if !ok {
			return "", nil, fmt.Errorf("%s: :%s must be a string", path, name)
		}
		fields[name] = string(text)
	}
	depsValue := manifest.Get(atom.NewKeyword("deps"))
	depsVector, ok := depsValue.(*coll.Vector)
	if !ok {
		return "", nil, fmt.Errorf("%s: :deps must be a vector of strings", path)
	}
	deps := make([]string, 0, len(depsVector.Items))
	for _, value := range depsVector.Items {
		text, ok := value.(atom.String)
		if !ok {
			return "", nil, fmt.Errorf("%s: :deps entries must be strings", path)
		}
		deps = append(deps, string(text))
	}
	return fields["module"], project.NormalizeStringSlice(deps), nil
}
