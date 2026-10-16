package scan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"doggo/internal/project"
	"doggo/internal/toolchain/reifytarget"
	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

var defaultInteropPackages = []string{
	"encoding/json",
	"fmt",
	"io",
	"math",
	"net/http",
	"os",
	"reflect",
	"strings",
	"time",
}

var defaultReifyCandidates = []reifytarget.Candidate{
	{Source: "fmt.Stringer", Required: true},
	{Source: "net/http.Handler", Required: true},
}

var (
	beforeDependencyPackagesLoadForTest func(string)
	beforeScanTempCleanupForTest        func(string)
)

type Targets struct {
	GoPackages      []string
	ReifyCandidates []reifytarget.Candidate
}

type Service struct{}

func (s Service) DeriveTargetsTree(tree *project.OwnedTree, projectRoot string, deps []project.LockDep) (Targets, error) {
	depGoPackages, depReifySources, err := collectResolvedDependencyTargetsTree(tree, projectRoot, deps)
	if err != nil {
		return Targets{}, err
	}

	goPackages := project.NormalizeStringSlice(append(append([]string{}, defaultInteropPackages...), depGoPackages...))

	reifyCandidates := append([]reifytarget.Candidate(nil), defaultReifyCandidates...)
	for _, source := range depReifySources {
		reifyCandidates = append(reifyCandidates, reifytarget.Candidate{Source: source})
	}
	return Targets{
		GoPackages:      goPackages,
		ReifyCandidates: normalizeReifyCandidates(reifyCandidates),
	}, nil
}

func normalizeReifyCandidates(candidates []reifytarget.Candidate) []reifytarget.Candidate {
	required := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if candidate.Source != "" {
			required[candidate.Source] = required[candidate.Source] || candidate.Required
		}
	}
	out := make([]reifytarget.Candidate, 0, len(required))
	for source, isRequired := range required {
		out = append(out, reifytarget.Candidate{Source: source, Required: isRequired})
	}
	slices.SortFunc(out, func(a, b reifytarget.Candidate) int {
		return strings.Compare(a.Source, b.Source)
	})
	return out
}

func CollectDependencyGoPackages(resolvedDeps []project.LockDep) ([]string, error) {
	out := make([]string, 0, len(resolvedDeps))
	for _, dep := range resolvedDeps {
		if dep.GoModule != "" {
			out = append(out, dep.GoModule)
		}
	}
	return project.NormalizeStringSlice(out), nil
}

func CollectResolvedDependencyInterfacesTree(tree *project.OwnedTree, projectRoot string, deps []project.LockDep) ([]string, error) {
	_, interfaces, err := collectResolvedDependencyTargetsTree(tree, projectRoot, deps)
	return interfaces, err
}

func collectResolvedDependencyTargetsTree(tree *project.OwnedTree, projectRoot string, deps []project.LockDep) (_ []string, _ []string, err error) {
	if !slices.ContainsFunc(deps, func(dep project.LockDep) bool { return dep.GoModule != "" }) {
		return nil, nil, nil
	}
	isolated, err := copyRootForPackageLoad(tree)
	if err != nil {
		return nil, nil, err
	}
	defer func() { err = errors.Join(err, closeCopiedRoot(isolated)) }()
	interfaces := make(map[string]struct{})
	packagePaths := make(map[string]struct{})
	seen := make(map[string]struct{})
	for _, dep := range deps {
		if dep.GoModule == "" {
			continue
		}
		identity := dep.GoModule + "\x00" + dep.InstallDir
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		installRoot, err := tree.OpenRoot(dep.InstallDir)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve install directory for %q: %w", dep.Source, err)
		}
		installDir := ownedInstallDir{path: filepath.Join(projectRoot, filepath.FromSlash(dep.InstallDir)), root: installRoot}
		isolatedRoot, err := isolated.Tree().OpenRoot(dep.InstallDir)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve isolated install directory for %q: %w", dep.Source, errors.Join(err, installDir.root.Close()))
		}
		isolatedInstallDir := ownedInstallDir{path: filepath.Join(isolated.Path(), filepath.FromSlash(dep.InstallDir)), root: isolatedRoot}
		if beforeDependencyPackagesLoadForTest != nil {
			beforeDependencyPackagesLoadForTest(installDir.path)
		}
		preloadErr := project.VerifyRootPath(isolatedInstallDir.path, isolatedInstallDir.root)
		var found []string
		var selected []string
		var loadErr error
		if preloadErr == nil {
			selected, loadErr = selectDependencyPackages(isolatedInstallDir.path, dep.GoModule)
			if loadErr == nil {
				found, loadErr = collectExportedInterfaces(isolatedInstallDir.path, selected)
			}
		}
		verifyErr := tree.VerifyComplete()
		isolatedVerifyErr := project.VerifyRootPath(isolatedInstallDir.path, isolatedInstallDir.root)
		closeErr := errors.Join(isolatedInstallDir.root.Close(), installDir.root.Close())
		if err := errors.Join(preloadErr, loadErr, verifyErr, isolatedVerifyErr, closeErr); err != nil {
			return nil, nil, fmt.Errorf("load Go packages for %q: %w", dep.Source, err)
		}
		for _, path := range selected {
			packagePaths[path] = struct{}{}
		}
		for _, name := range found {
			interfaces[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(interfaces))
	for name := range interfaces {
		out = append(out, name)
	}
	paths := make([]string, 0, len(packagePaths))
	for path := range packagePaths {
		paths = append(paths, path)
	}
	return project.NormalizeStringSlice(paths), project.NormalizeStringSlice(out), nil
}

func copyRootForPackageLoad(source *project.OwnedTree) (*project.OwnedTempRoot, error) {
	root, err := project.NewOwnedTempRoot(".doggo-scan-")
	if err != nil {
		return nil, err
	}
	if err := source.CopyTo(root.Tree(), nil); err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return root, nil
}

func closeCopiedRoot(root *project.OwnedTempRoot) error {
	if beforeScanTempCleanupForTest != nil {
		beforeScanTempCleanupForTest(root.Path())
	}
	return root.Close()
}

type ownedInstallDir struct {
	path string
	root *os.Root
}

func ModulePathFromGoMod(root string) (string, error) {
	goModPath := filepath.Join(root, "go.mod")
	content, err := os.ReadFile(goModPath)
	if err != nil {
		return "", err
	}
	parsed, err := modfile.Parse(goModPath, content, nil)
	if err != nil {
		return "", err
	}
	if parsed.Module == nil {
		return "", fmt.Errorf("%s does not contain module declaration", goModPath)
	}
	return parsed.Module.Mod.Path, nil
}

func NormalizeGitRepositoryPath(source string) string {
	s := strings.TrimSpace(source)
	s = strings.TrimPrefix(s, "git+")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "ssh://")
	s = strings.TrimPrefix(s, "git://")

	if strings.HasPrefix(s, "git@") {
		rest := strings.TrimPrefix(s, "git@")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) == 2 {
			s = parts[0] + "/" + parts[1]
		} else {
			s = rest
		}
	}

	s = strings.TrimSuffix(s, ".git")
	s = strings.Trim(s, "/")
	return s
}

func collectExportedInterfaces(projectRoot string, targets []string) ([]string, error) {
	cfg := &packages.Config{
		Mode:       packages.NeedName | packages.NeedTypes | packages.NeedTypesSizes,
		Dir:        projectRoot,
		Env:        append(os.Environ(), "GOWORK=off"),
		Tests:      false,
		BuildFlags: []string{"-mod=mod"},
	}

	interfaces := make(map[string]struct{})
	for _, pkgPath := range targets {
		loadedPkgs, err := packages.Load(cfg, pkgPath)
		if err != nil {
			return nil, fmt.Errorf("load package %q: %w", pkgPath, err)
		}
		if len(loadedPkgs) == 0 {
			return nil, fmt.Errorf("package %q not found", pkgPath)
		}

		foundValidPkg := false
		for _, pkg := range loadedPkgs {
			if len(pkg.Errors) > 0 {
				return nil, fmt.Errorf("package %q has errors: %v", pkgPath, pkg.Errors)
			}
			if pkg.IllTyped {
				return nil, fmt.Errorf("package %q or dependency is ill-typed", pkgPath)
			}
			if pkg.Types == nil {
				continue
			}
			foundValidPkg = true

			scope := pkg.Types.Scope()
			for _, name := range scope.Names() {
				obj := scope.Lookup(name)
				if obj == nil || !obj.Exported() {
					continue
				}
				target, err := reifytarget.Resolve(pkg.Types, name)
				if err != nil {
					continue
				}
				interfaces[target.Target.Source] = struct{}{}
			}
		}
		if !foundValidPkg {
			return nil, fmt.Errorf("package %q has no type information", pkgPath)
		}
	}

	out := make([]string, 0, len(interfaces))
	for iface := range interfaces {
		out = append(out, iface)
	}
	return project.NormalizeStringSlice(out), nil
}
