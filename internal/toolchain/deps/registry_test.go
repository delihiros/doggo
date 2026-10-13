package deps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
)

func TestResolvePackageSource(t *testing.T) {
	projectRoot := t.TempDir()
	registryPath := filepath.Join(projectRoot, "doggo.registry")
	registry := &project.RegistryFile{
		Packages: []project.RegistryPackage{
			{
				Name:    "acme/math",
				Version: "1.0.0",
				Source:  "https://github.com/acme/math.git@v1.0.0",
			},
			{
				Name:    "acme/math",
				Version: "1.2.0",
				Source:  "https://github.com/acme/math.git@v1.2.0",
			},
		},
	}
	if err := project.WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	source, resolved, err := ResolvePackageSource(projectRoot, "pkg:acme/math", registryPath)
	if err != nil {
		t.Fatalf("resolve latest package failed: %v", err)
	}
	if source != "https://github.com/acme/math.git@v1.2.0" {
		t.Fatalf("unexpected latest source: %s", source)
	}
	if resolved.Version != "1.2.0" {
		t.Fatalf("unexpected latest version: %s", resolved.Version)
	}

	source, resolved, err = ResolvePackageSource(projectRoot, "pkg:acme/math@1.0.0", registryPath)
	if err != nil {
		t.Fatalf("resolve exact package failed: %v", err)
	}
	if source != "https://github.com/acme/math.git@v1.0.0" {
		t.Fatalf("unexpected exact source: %s", source)
	}
	if resolved.Version != "1.0.0" {
		t.Fatalf("unexpected exact version: %s", resolved.Version)
	}
}

func TestResolvePackageSource_ExactVersionMatchesOptionalVPrefix(t *testing.T) {
	projectRoot := t.TempDir()
	registryPath := filepath.Join(projectRoot, "doggo.registry")
	registry := &project.RegistryFile{
		Packages: []project.RegistryPackage{
			{
				Name:    "acme/math",
				Version: "v1.2.3",
				Source:  "https://github.com/acme/math.git@v1.2.3",
			},
		},
	}
	if err := project.WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	source, resolved, err := ResolvePackageSource(projectRoot, "pkg:acme/math@1.2.3", registryPath)
	if err != nil {
		t.Fatalf("resolve exact package with optional v prefix failed: %v", err)
	}
	if source != "https://github.com/acme/math.git@v1.2.3" || resolved.Version != "1.2.3" {
		t.Fatalf("unexpected resolved package: source=%s resolved=%+v", source, resolved)
	}
}

func TestResolvePackageSource_PrefersSemverPrereleaseOrder(t *testing.T) {
	projectRoot := t.TempDir()
	registryPath := filepath.Join(projectRoot, "doggo.registry")
	registry := &project.RegistryFile{
		Packages: []project.RegistryPackage{
			{
				Name:    "acme/math",
				Version: "1.0.0-2",
				Source:  "https://github.com/acme/math.git@v1.0.0-2",
			},
			{
				Name:    "acme/math",
				Version: "1.0.0-10",
				Source:  "https://github.com/acme/math.git@v1.0.0-10",
			},
		},
	}
	if err := project.WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	source, resolved, err := ResolvePackageSource(projectRoot, "pkg:acme/math", registryPath)
	if err != nil {
		t.Fatalf("resolve latest prerelease failed: %v", err)
	}
	if source != "https://github.com/acme/math.git@v1.0.0-10" || resolved.Version != "1.0.0-10" {
		t.Fatalf("unexpected prerelease winner: source=%s resolved=%+v", source, resolved)
	}
}

func TestResolvePackageSourceAcceptsGoModuleSource(t *testing.T) {
	projectRoot := t.TempDir()
	registryPath := filepath.Join(projectRoot, "doggo.registry")
	if err := project.WriteRegistryFile(registryPath, &project.RegistryFile{
		Packages: []project.RegistryPackage{{
			Name:    "acme/math",
			Version: "1.2.3",
			Source:  "github.com/acme/math@v1.2.3",
		}},
	}); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	source, _, err := ResolvePackageSource(projectRoot, "pkg:acme/math@1.2.3", registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if source != "github.com/acme/math@v1.2.3" {
		t.Fatalf("source = %q", source)
	}
}

func TestResolvePackageSourceRejectsInvalidRegisteredSource(t *testing.T) {
	projectRoot := t.TempDir()
	registryPath := filepath.Join(projectRoot, "doggo.registry")
	if err := project.WriteRegistryFile(registryPath, &project.RegistryFile{
		Packages: []project.RegistryPackage{{
			Name:    "acme/math",
			Version: "1.2.3",
			Source:  "github.com/acme/math@main",
		}},
	}); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	_, _, err := ResolvePackageSource(projectRoot, "pkg:acme/math@1.2.3", registryPath)
	if err == nil {
		t.Fatal("expected invalid registry source to fail")
	}
	for _, part := range []string{"acme/math", "1.2.3", "invalid"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error %q does not contain %q", err, part)
		}
	}
}

func TestResolveRegistryPathFallsBackToProjectFileForWrites(t *testing.T) {
	projectRoot := t.TempDir()
	got, err := ResolveRegistryPath(projectRoot, "", true)
	if err != nil {
		t.Fatalf("resolve registry path: %v", err)
	}
	want := filepath.Join(projectRoot, defaultRegistryFileName)
	if got != want {
		t.Fatalf("registry path = %q want %q", got, want)
	}
}

func TestResolveRegistryPathUsesExplicitOverride(t *testing.T) {
	projectRoot := t.TempDir()
	override := "custom/registry.edn"
	got, err := ResolveRegistryPath(projectRoot, override, false)
	if err != nil {
		t.Fatalf("resolve registry path: %v", err)
	}
	want := filepath.Clean(filepath.Join(projectRoot, override))
	if got != want {
		t.Fatalf("registry path = %q want %q", got, want)
	}
}

func TestResolveRegistryPathUsesEnvOverride(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.Setenv("DOGGO_REGISTRY", "env.registry"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	defer os.Unsetenv("DOGGO_REGISTRY")

	got, err := ResolveRegistryPath(projectRoot, "", false)
	if err != nil {
		t.Fatalf("resolve registry path: %v", err)
	}
	want := filepath.Clean(filepath.Join(projectRoot, "env.registry"))
	if got != want {
		t.Fatalf("registry path = %q want %q", got, want)
	}
}
