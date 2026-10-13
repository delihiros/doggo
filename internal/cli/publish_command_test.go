package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	toolchainpublish "doggo/internal/toolchain/publish"
)

func TestRenderPublishResult(t *testing.T) {
	out := renderPublishResult(&toolchainpublish.Result{
		Package:      project.RegistryPackage{Name: "demo.core", Version: "1.0.0", Source: "pkg:demo.core@1.0.0"},
		ArchivePath:  "dist/demo.core-1.0.0.tgz",
		RegistryPath: "doggo.registry",
	})
	if !strings.Contains(out, "Published demo.core@1.0.0") {
		t.Fatalf("unexpected output: %s", out)
	}
	if !strings.Contains(out, "  archive: dist/demo.core-1.0.0.tgz") {
		t.Fatalf("archive line missing: %s", out)
	}
}

func TestPublishCommandUsesPublishServiceInsteadOfModmgrManager(t *testing.T) {
	called := false
	app := NewApp("dev", []string{"publish"})
	app.services.Publish = func(registryPath, source string, allowOverwrite bool) (*toolchainpublish.Result, error) {
		called = true
		return &toolchainpublish.Result{
			Package: project.RegistryPackage{Name: "demo.core", Version: "1.0.0", Source: "pkg:demo.core@1.0.0"},
		}, nil
	}

	if err := app.runPublishCommand(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected publish service to be called")
	}
}

func TestApp_Run_PublishRequiresProject(t *testing.T) {
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{"publish"})
	if err := app.Run(); err == nil {
		t.Fatalf("expected publish to fail without %s", project.ModFileName)
	}
}

func TestApp_Run_PublishWritesRegistry(t *testing.T) {
	tmp := t.TempDir()
	registryPath := filepath.Join(tmp, "registry.edn")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if err := project.WriteModFile(project.ModPath(tmp), &project.ModFile{
		Module:  "github.com/acme/math",
		Name:    "acme/math",
		Version: "1.2.3",
		License: "MIT",
		Compat:  ">=0.1.0",
		Deps:    []string{},
	}); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "math.dog"), []byte("(ns acme.math)\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	app := NewApp("1.0.0", []string{
		"publish",
		"--registry", registryPath,
		"--source", "https://github.com/acme/math.git@v1.2.3",
	})
	if err := app.Run(); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	if _, err := os.Stat(registryPath); err != nil {
		t.Fatalf("expected registry file to be written: %v", err)
	}
}
