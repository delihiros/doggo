package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"doggo/internal/project"
	"golang.org/x/tools/go/packages"
)

// Ordinary modules retain their root-only policy. A module with no buildable
// root instead contributes its actual public packages to both generated lists.
func selectDependencyPackages(installDir, modulePath string) ([]string, error) {
	cfg := &packages.Config{
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedModule,
		Dir:        installDir,
		Env:        append(os.Environ(), "GOWORK=off"),
		Tests:      false,
		BuildFlags: []string{"-mod=mod"},
	}
	roots, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("probe module root %q: %w", modulePath, err)
	}
	if len(roots) != 1 {
		return nil, fmt.Errorf("probe module root %q: expected one package, got %d", modulePath, len(roots))
	}
	root := roots[0]
	if len(root.GoFiles) > 0 || len(root.CompiledGoFiles) > 0 {
		if err := dependencyMetadataError(root); err != nil {
			return nil, err
		}
		if !belongsToDependency(root, installDir, modulePath) || root.PkgPath != modulePath || root.Name == "" {
			return nil, fmt.Errorf("module root %q has unexpected package identity %q", modulePath, root.PkgPath)
		}
		return []string{root.PkgPath}, nil
	}
	if !noBuildableRoot(root, installDir) {
		if err := dependencyMetadataError(root); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("module root %q has no buildable source and an unexpected loader result", modulePath)
	}
	// The no-files loader result has no Module metadata. All other successful
	// root probes must still identify this exact module and install directory.
	if len(root.Errors) == 0 && !belongsToDependency(root, installDir, modulePath) {
		return nil, fmt.Errorf("module root %q has unexpected package identity %q", modulePath, root.PkgPath)
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("discover packages in %q: %w", modulePath, err)
	}
	var selected []string
	for _, pkg := range pkgs {
		// Only positive metadata evidence permits ignoring an excluded package's
		// errors. In particular, a missing name is not evidence of exclusion.
		if excludedDependencyPackage(pkg, installDir, modulePath) {
			continue
		}
		if err := dependencyMetadataError(pkg); err != nil {
			return nil, err
		}
		if !belongsToDependency(pkg, installDir, modulePath) || pkg.Name == "" {
			return nil, fmt.Errorf("discovered package %q has unexpected module or source identity", pkg.PkgPath)
		}
		if len(pkg.GoFiles) > 0 {
			selected = append(selected, pkg.PkgPath)
		}
	}
	selected = project.NormalizeStringSlice(selected)
	if len(selected) == 0 {
		return nil, fmt.Errorf("module %q has no usable Go packages", modulePath)
	}
	return selected, nil
}

func dependencyMetadataError(pkg *packages.Package) error {
	if len(pkg.Errors) > 0 {
		return fmt.Errorf("package %q has errors: %v", pkg.PkgPath, pkg.Errors)
	}
	if pkg.Module != nil && pkg.Module.Error != nil {
		return fmt.Errorf("package %q has module error: %s", pkg.PkgPath, pkg.Module.Error.Err)
	}
	return nil
}

func noBuildableRoot(pkg *packages.Package, installDir string) bool {
	if len(pkg.GoFiles) > 0 || len(pkg.CompiledGoFiles) > 0 || pkg.Module != nil && pkg.Module.Error != nil {
		return false
	}
	for _, err := range pkg.Errors {
		if err.Kind != packages.ListError || err.Pos != "" && err.Pos != "-" {
			return false
		}
		matched := false
		for _, prefix := range []string{"no Go files in ", "no non-test Go files in ", "build constraints exclude all Go files in "} {
			if strings.HasPrefix(err.Msg, prefix) && filepath.Clean(strings.TrimPrefix(err.Msg, prefix)) == filepath.Clean(installDir) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func belongsToDependency(pkg *packages.Package, installDir, modulePath string) bool {
	if pkg.Module == nil || pkg.Module.Path != modulePath || filepath.Clean(pkg.Module.Dir) != filepath.Clean(installDir) {
		return false
	}
	if pkg.PkgPath != modulePath && !strings.HasPrefix(pkg.PkgPath, modulePath+"/") {
		return false
	}
	for _, file := range append(append([]string{}, pkg.GoFiles...), pkg.CompiledGoFiles...) {
		if _, ok := dependencyRelativePath(installDir, file); !ok {
			return false
		}
	}
	return true
}

func excludedDependencyPackage(pkg *packages.Package, installDir, modulePath string) bool {
	if pkg.Name == "main" || pkg.ID != pkg.PkgPath {
		return true
	}
	for _, component := range strings.Split(pkg.PkgPath, "/") {
		if component == "internal" {
			return true
		}
	}
	if pkg.Module != nil && (pkg.Module.Path != modulePath || filepath.Clean(pkg.Module.Dir) != filepath.Clean(installDir)) {
		return true
	}
	for _, file := range pkg.GoFiles {
		rel, ok := dependencyRelativePath(installDir, file)
		if !ok {
			return true
		}
		for _, component := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
			if component == "vendor" || component == "testdata" || component != "." && (strings.HasPrefix(component, ".") || strings.HasPrefix(component, "_")) {
				return true
			}
		}
	}
	return false
}

func dependencyRelativePath(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	return rel, err == nil && filepath.IsLocal(rel)
}
