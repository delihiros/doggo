package publish

import (
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
)

func TestServiceRunPublishesThroughOwnedWorkflow(t *testing.T) {
	service := Service{
		LoadModFile: func(path string) (*project.ModFile, error) {
			return &project.ModFile{
				Module:  "github.com/acme/math",
				Name:    "acme/math",
				Version: "1.2.3",
				License: "MIT",
				Compat:  ">=0.1.0",
			}, nil
		},
		CreateArchive: func(projectRoot, name, version string) (string, error) {
			return "/tmp/acme-math-1.2.3.tgz", nil
		},
		LoadRegistry: func(path string) (*project.RegistryFile, error) {
			return &project.RegistryFile{}, nil
		},
		WriteRegistry: func(path string, registry *project.RegistryFile) error {
			if len(registry.Packages) != 1 {
				t.Fatalf("packages = %+v", registry.Packages)
			}
			return nil
		},
	}

	result, err := service.Run("/tmp/project", Options{
		RegistryPath: "/tmp/doggo.registry",
		Source:       "https://github.com/acme/math.git@v1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ArchivePath != "/tmp/acme-math-1.2.3.tgz" {
		t.Fatalf("archive = %q", result.ArchivePath)
	}
}

func TestServiceRunRequiresSourceBeforeSideEffects(t *testing.T) {
	sideEffect := false
	registryPath := filepath.Join(t.TempDir(), "doggo.registry")
	service := Service{
		LoadModFile: func(path string) (*project.ModFile, error) {
			return publishableMod(), nil
		},
		ResolveRegistry: func(projectRoot, override string, forWrite bool) (string, error) {
			sideEffect = true
			return registryPath, nil
		},
		CreateArchive: func(projectRoot, name, version string) (string, error) {
			sideEffect = true
			return "/tmp/acme-math-1.2.3.tgz", nil
		},
	}

	_, err := service.Run("/tmp/project", Options{})
	if err == nil || err.Error() != "publish requires --source" {
		t.Fatalf("error = %v", err)
	}
	if sideEffect {
		t.Fatal("registry or archive side effect occurred")
	}
}

func TestServiceRunRejectsInvalidSourceBeforeSideEffects(t *testing.T) {
	sideEffect := false
	registryPath := filepath.Join(t.TempDir(), "doggo.registry")
	service := Service{
		LoadModFile: func(path string) (*project.ModFile, error) {
			return publishableMod(), nil
		},
		ResolveRegistry: func(projectRoot, override string, forWrite bool) (string, error) {
			sideEffect = true
			return registryPath, nil
		},
		CreateArchive: func(projectRoot, name, version string) (string, error) {
			sideEffect = true
			return "/tmp/acme-math-1.2.3.tgz", nil
		},
	}

	_, err := service.Run("/tmp/project", Options{Source: "github.com/acme/math@main"})
	if err == nil || !strings.Contains(err.Error(), "invalid publish source") {
		t.Fatalf("error = %v", err)
	}
	if sideEffect {
		t.Fatal("registry or archive side effect occurred")
	}
}

func publishableMod() *project.ModFile {
	return &project.ModFile{
		Module:  "github.com/acme/math",
		Name:    "acme/math",
		Version: "1.2.3",
		License: "MIT",
		Compat:  ">=0.1.0",
	}
}
