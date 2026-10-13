package deps

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"doggo/internal/depsource"
	"doggo/internal/project"
)

const defaultRegistryFileName = "doggo.registry"

type packageSpec struct {
	Name    string
	Version string
}

func ResolvePackageSource(projectRoot, packageSource, registryOverride string) (string, project.RegistryPackage, error) {
	spec, err := parsePackageSpec(packageSource)
	if err != nil {
		return "", project.RegistryPackage{}, err
	}
	registryPath, err := ResolveRegistryPath(projectRoot, registryOverride, false)
	if err != nil {
		return "", project.RegistryPackage{}, err
	}
	registry, err := project.LoadRegistryFile(registryPath)
	if err != nil {
		return "", project.RegistryPackage{}, err
	}

	candidates := make([]project.RegistryPackage, 0, len(registry.Packages))
	for _, pkg := range registry.Packages {
		if pkg.Name != spec.Name {
			continue
		}
		if spec.Version != "" && !project.RegistryVersionsEqual(pkg.Version, spec.Version) {
			continue
		}
		candidates = append(candidates, pkg)
	}
	if len(candidates) == 0 {
		if spec.Version == "" {
			return "", project.RegistryPackage{}, fmt.Errorf("package not found in registry: %s", spec.Name)
		}
		return "", project.RegistryPackage{}, fmt.Errorf("package not found in registry: %s@%s", spec.Name, spec.Version)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Name != candidates[j].Name {
			return candidates[i].Name < candidates[j].Name
		}
		return project.CompareRegistryVersions(candidates[i].Version, candidates[j].Version) > 0
	})
	selected := candidates[0]
	parsed, err := depsource.Parse(selected.Source)
	if err != nil {
		return "", project.RegistryPackage{}, fmt.Errorf("registry entry %s@%s has invalid :source %q: %w", selected.Name, selected.Version, selected.Source, err)
	}
	return parsed.Declaration, selected, nil
}

func ResolveRegistryPath(projectRoot, override string, forWrite bool) (string, error) {
	if strings.TrimSpace(override) != "" {
		return resolvePathFromProject(projectRoot, override), nil
	}
	if envPath := strings.TrimSpace(os.Getenv("DOGGO_REGISTRY")); envPath != "" {
		return resolvePathFromProject(projectRoot, envPath), nil
	}

	projectRegistry := filepath.Join(projectRoot, defaultRegistryFileName)
	if forWrite {
		return projectRegistry, nil
	}
	if fileExists(projectRegistry) {
		return projectRegistry, nil
	}

	homeDir, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(homeDir) != "" {
		homeRegistry := filepath.Join(homeDir, ".doggo", "registry.edn")
		if fileExists(homeRegistry) {
			return homeRegistry, nil
		}
	}
	return "", fmt.Errorf("registry not found (set DOGGO_REGISTRY or pass --registry)")
}

func resolvePathFromProject(projectRoot, p string) string {
	trimmed := strings.TrimSpace(p)
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	return filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash(trimmed)))
}

func parsePackageSpec(source string) (packageSpec, error) {
	trimmed := strings.TrimSpace(source)
	if !strings.HasPrefix(trimmed, "pkg:") {
		return packageSpec{}, fmt.Errorf("package source must start with pkg: prefix")
	}
	body := strings.TrimPrefix(trimmed, "pkg:")
	if body == "" {
		return packageSpec{}, fmt.Errorf("package source is empty")
	}
	name := body
	version := ""
	if idx := strings.LastIndex(body, "@"); idx > 0 {
		name = body[:idx]
		version = body[idx+1:]
	}
	if strings.TrimSpace(name) == "" {
		return packageSpec{}, fmt.Errorf("package name is empty")
	}
	return packageSpec{Name: name, Version: strings.TrimSpace(version)}, nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
