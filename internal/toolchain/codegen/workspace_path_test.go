package codegen

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectWorkspaceModuleDirPreservesUsePathSpelling(t *testing.T) {
	base := t.TempDir()
	logical := filepath.Join(base, "logical")
	physical := filepath.Join(base, "physical")
	moduleRoot := filepath.Join(physical, "app")
	normalize := func(path string) (string, error) {
		if strings.HasPrefix(path, logical+string(filepath.Separator)) {
			return physical + strings.TrimPrefix(path, logical), nil
		}
		return path, nil
	}
	for _, tt := range []struct {
		name, workFile, use, want string
	}{
		{"relative logical", filepath.Join(logical, "go.work"), "./app", filepath.Join(logical, "app")},
		{"relative physical", filepath.Join(physical, "go.work"), "./app", moduleRoot},
		{"absolute logical", filepath.Join(physical, "go.work"), filepath.Join(logical, "app"), filepath.Join(logical, "app")},
		{"absolute physical", filepath.Join(logical, "go.work"), moduleRoot, moduleRoot},
		{"different module", filepath.Join(logical, "go.work"), "./other", moduleRoot},
		{"similar prefix", filepath.Join(logical, "go.work"), "./app-other", moduleRoot},
		{"suffix alias", filepath.Join(logical, "go.work"), "./app-link", moduleRoot},
		{"absolute suffix traversal", filepath.Join(logical, "go.work"), logical + string(filepath.Separator) + "app-link" + string(filepath.Separator) + ".." + string(filepath.Separator) + "app", moduleRoot},
	} {
		t.Run(tt.name, func(t *testing.T) {
			contents := []byte(fmt.Sprintf("go 1.24.0\nuse %q\n", tt.use))
			got, err := selectWorkspaceModuleDir(moduleRoot, tt.workFile, contents, normalize)
			if err != nil || got != tt.want {
				t.Fatalf("selected module directory = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestSelectWorkspaceModuleDirReportsInvalidWorkspace(t *testing.T) {
	moduleRoot := t.TempDir()
	identity := func(path string) (string, error) { return path, nil }
	if _, err := selectWorkspaceModuleDir(moduleRoot, filepath.Join(moduleRoot, "go.work"), []byte("use ("), identity); err == nil {
		t.Fatal("accepted invalid go.work")
	}
	wantErr := errors.New("system alias changed")
	fail := func(string) (string, error) { return "", wantErr }
	if _, err := selectWorkspaceModuleDir(moduleRoot, filepath.Join(moduleRoot, "go.work"), []byte("go 1.24.0\nuse ./app\n"), fail); !errors.Is(err, wantErr) {
		t.Fatalf("normalization error = %v, want %v", err, wantErr)
	}
}

func TestSelectWorkspaceModuleDirMatchesLogicalModuleRoot(t *testing.T) {
	base := t.TempDir()
	logical := filepath.Join(base, "logical", "app")
	physical := filepath.Join(base, "physical", "app")
	normalize := func(path string) (string, error) {
		if path == logical {
			return physical, nil
		}
		return path, nil
	}
	contents := []byte(fmt.Sprintf("go 1.24.0\nuse %q\n", physical))
	got, err := selectWorkspaceModuleDir(logical, filepath.Join(base, "go.work"), contents, normalize)
	if err != nil || got != physical {
		t.Fatalf("selected module directory = %q, %v; want %q", got, err, physical)
	}
}

func TestGoWorkspaceFileHonorsGoEnvironment(t *testing.T) {
	root := t.TempDir()
	moduleRoot := filepath.Join(root, "app")
	if err := os.MkdirAll(moduleRoot, 0755); err != nil {
		t.Fatal(err)
	}
	workFile := filepath.Join(root, "go.work")
	if err := os.WriteFile(workFile, []byte("go 1.24.0\nuse ./app\n"), 0644); err != nil {
		t.Fatal(err)
	}
	otherWorkFile := filepath.Join(root, "other.work")
	if err := os.WriteFile(otherWorkFile, []byte("go 1.24.0\nuse ./app\n"), 0644); err != nil {
		t.Fatal(err)
	}
	goenv := filepath.Join(root, "go.env")
	if err := os.WriteFile(goenv, []byte("GOWORK="+otherWorkFile+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, gowork, goenv, want string
	}{
		{"default search", "", "off", workFile},
		{"auto search", "auto", goenv, workFile},
		{"explicit path", workFile, goenv, workFile},
		{"persisted default", "", goenv, otherWorkFile},
		{"disabled", "off", goenv, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOWORK", tt.gowork)
			t.Setenv("GOENV", tt.goenv)
			got, err := goWorkspaceFile(moduleRoot, os.Environ())
			if err != nil || got != tt.want {
				t.Fatalf("effective workspace = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestValidateGeneratedPackageIsolatedDisablesCallerWorkspace(t *testing.T) {
	moduleRoot := t.TempDir()
	writeTestFiles(t, moduleRoot, map[string]string{
		"go.mod":  "module example.com/app\n\ngo 1.24.0\n",
		"base.go": "package app\n",
	})
	t.Setenv("GOWORK", filepath.Join(t.TempDir(), "missing.work"))
	output := filepath.Join(moduleRoot, "generated.go")
	source := []byte("package app\nconst Generated = true\n")
	if err := validateGeneratedPackage(moduleRoot, output, source, false); err == nil {
		t.Fatal("nonisolated validation ignored the caller's invalid workspace")
	}
	if err := validateGeneratedPackage(moduleRoot, output, source, true); err != nil {
		t.Fatalf("isolated validation did not disable the caller workspace: %v", err)
	}
}
