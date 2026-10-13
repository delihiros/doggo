package publish

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"doggo/internal/project"
)

func TestServiceRunWritesRegistryAndArchive(t *testing.T) {
	projectRoot := t.TempDir()
	registryPath := filepath.Join(projectRoot, "doggo.registry")

	mod := &project.ModFile{
		Module:  "github.com/acme/math",
		Name:    "acme/math",
		Version: "1.2.3",
		License: "MIT",
		Compat:  ">=0.1.0",
		Deps:    []string{},
	}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "math.dog"), []byte("(ns acme.math)\n(defn add [a b] (+ a b))\n"), 0644); err != nil {
		t.Fatalf("write dog source: %v", err)
	}

	result, err := Service{}.Run(projectRoot, Options{
		RegistryPath: registryPath,
		Source:       "https://github.com/acme/math.git@v1.2.3",
	})
	if err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	if result.Package.Name != "acme/math" {
		t.Fatalf("unexpected package name: %s", result.Package.Name)
	}
	if result.Package.Version != "1.2.3" {
		t.Fatalf("unexpected package version: %s", result.Package.Version)
	}
	if result.Package.Source != "https://github.com/acme/math.git@v1.2.3" {
		t.Fatalf("unexpected package source: %s", result.Package.Source)
	}
	if _, err := os.Stat(result.ArchivePath); err != nil {
		t.Fatalf("expected archive to exist: %v", err)
	}

	registry, err := project.LoadRegistryFile(registryPath)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(registry.Packages) != 1 {
		t.Fatalf("expected one registry package, got %d", len(registry.Packages))
	}
	if registry.Packages[0].Name != "acme/math" || registry.Packages[0].Version != "1.2.3" {
		t.Fatalf("unexpected registry package entry: %+v", registry.Packages[0])
	}
}

func TestServiceRunRequiresMetadata(t *testing.T) {
	projectRoot := t.TempDir()
	mod := &project.ModFile{
		Module: "github.com/acme/math",
		Deps:   []string{},
	}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "math.dog"), []byte("(ns acme.math)\n"), 0644); err != nil {
		t.Fatalf("write dog source: %v", err)
	}

	if _, err := (Service{}).Run(projectRoot, Options{Source: "https://github.com/acme/math.git@v1.2.3"}); err == nil {
		t.Fatalf("expected publish to fail without required metadata")
	}
}

func TestServiceRunDuplicateWithoutOverwriteDoesNotCreateArchive(t *testing.T) {
	projectRoot := t.TempDir()
	registryPath := filepath.Join(projectRoot, "doggo.registry")
	mod := &project.ModFile{
		Module:  "github.com/acme/math",
		Name:    "acme/math",
		Version: "1.2.3",
		License: "MIT",
		Compat:  ">=0.1.0",
		Deps:    []string{},
	}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "math.dog"), []byte("(ns acme.math)\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := project.WriteRegistryFile(registryPath, &project.RegistryFile{
		Packages: []project.RegistryPackage{
			{
				Name:    "acme/math",
				Version: "1.2.3",
				Source:  "https://github.com/acme/math.git@v1.2.3",
				License: "MIT",
				Compat:  ">=0.1.0",
			},
		},
		Advisories: []project.RegistryAdvisory{},
	}); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	_, err := (Service{}).Run(projectRoot, Options{
		RegistryPath: registryPath,
		Source:       "https://github.com/acme/math.git@v1.2.3",
	})
	if err == nil {
		t.Fatalf("expected duplicate publish to fail without --allow-overwrite")
	}

	distPublishDir := filepath.Join(projectRoot, "dist", "publish")
	entries, readErr := os.ReadDir(distPublishDir)
	if readErr == nil && len(entries) > 0 {
		t.Fatalf("expected no archive side-effects on duplicate publish, found %d entries", len(entries))
	}
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read publish dir: %v", readErr)
	}
}

func TestServiceRunArchiveSkipsSymlinkEntries(t *testing.T) {
	projectRoot := t.TempDir()
	registryPath := filepath.Join(projectRoot, "doggo.registry")
	mod := &project.ModFile{
		Module:  "github.com/acme/math",
		Name:    "acme/math",
		Version: "1.2.3",
		License: "MIT",
		Compat:  ">=0.1.0",
		Deps:    []string{},
	}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "main.dog"), []byte("(ns acme.main)\n"), 0644); err != nil {
		t.Fatalf("write main source: %v", err)
	}

	outsideDir := t.TempDir()
	outsideDog := filepath.Join(outsideDir, "outside.dog")
	if err := os.WriteFile(outsideDog, []byte("(ns outside.secret)\n"), 0644); err != nil {
		t.Fatalf("write outside source: %v", err)
	}
	symlinkPath := filepath.Join(projectRoot, "linked_outside.dog")
	if err := os.Symlink(outsideDog, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	result, err := (Service{}).Run(projectRoot, Options{
		RegistryPath: registryPath,
		Source:       "https://github.com/acme/math.git@v1.2.3",
	})
	if err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	f, err := os.Open(result.ArchivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	gzReader, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("open gzip reader: %v", err)
	}
	defer gzReader.Close()
	tarReader := tar.NewReader(gzReader)

	containsSymlinkEntry := false
	for {
		hdr, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read tar entry: %v", err)
		}
		if hdr.Name == "linked_outside.dog" {
			containsSymlinkEntry = true
			break
		}
	}
	if containsSymlinkEntry {
		t.Fatalf("archive must not include symlinked .dog entries")
	}
}

func TestServiceRunDuplicateWithLegacyVPrefixVersionFailsWithoutOverwrite(t *testing.T) {
	projectRoot := t.TempDir()
	registryPath := filepath.Join(projectRoot, "doggo.registry")
	mod := &project.ModFile{
		Module:  "github.com/acme/math",
		Name:    "acme/math",
		Version: "1.2.3",
		License: "MIT",
		Compat:  ">=0.1.0",
		Deps:    []string{},
	}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "math.dog"), []byte("(ns acme.math)\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	legacyRegistry := `{:packages [{:name "acme/math" :version "v1.2.3" :source "https://github.com/acme/math.git@v1.2.3"}] :advisories []}`
	if err := os.WriteFile(registryPath, []byte(legacyRegistry+"\n"), 0644); err != nil {
		t.Fatalf("write legacy registry: %v", err)
	}

	if _, err := (Service{}).Run(projectRoot, Options{
		RegistryPath: registryPath,
		Source:       "https://github.com/acme/math.git@v1.2.3",
	}); err == nil {
		t.Fatalf("expected publish duplicate check to fail for semver-equivalent legacy version")
	}
}
