package tester

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"doggo/internal/project"
	runtimerunner "doggo/internal/runtime/runner"
	"doggo/internal/toolchain/generation"
	"doggo/internal/types"
	typeatom "doggo/internal/types/atom"
)

func TestServiceResolvesDirectoryTargetsInOwnProject(t *testing.T) {
	ancestor := t.TempDir()
	nested := filepath.Join(ancestor, "nested")
	for _, root := range []string{ancestor, nested} {
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/" + filepath.Base(root), Deps: []string{}}); err != nil {
			t.Fatal(err)
		}
		prepared, err := (generation.Service{}).PrepareSync(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := prepared.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(nested, "test", "sample_test.dog")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("(ns sample-test)\n"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		cwd   string
		paths []string
	}{
		{"absolute_project_root", ancestor, []string{nested}},
		{"relative_project_root", ancestor, []string{"nested"}},
		{"current_project_root", nested, []string{"."}},
		{"test_subdirectory", ancestor, []string{filepath.Dir(target)}},
		{"test_file", ancestor, []string{target}},
		{"no_paths", nested, nil},
		{"empty_path", nested, []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(tc.cwd)
			var loaded []string
			service := Service{NewRunner: func(cfg runtimerunner.Config) (Runner, error) {
				if cfg.ProjectRoot != nested {
					t.Errorf("runner project = %q, want nested project %q (ancestor also has a valid generation)", cfg.ProjectRoot, nested)
				}
				return &fakeRunner{
					loadFile: func(path string) (types.Value, error) {
						loaded = append(loaded, path)
						return typeatom.NilValue, nil
					},
					run: func(expr string) (types.Value, error) {
						if expr == "(require 'doggo.test)" {
							return typeatom.NilValue, nil
						}
						return fakeReportValue(), nil
					},
				}, nil
			}}
			report, err := service.Run(RunOptions{Paths: tc.paths, Namespaces: []string{"sample-test"}})
			if err != nil {
				t.Fatal(err)
			}
			if report.Summary.Test != 1 || !reflect.DeepEqual(loaded, []string{target}) {
				t.Fatalf("report = %+v, loaded = %#v, want one test from %q", report, loaded, target)
			}
		})
	}
}

func TestServicePreservesMissingTargetError(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	service := Service{
		OpenGenerationSession: func(string) (*project.GenerationSession, error) {
			return &project.GenerationSession{Config: project.RuntimeConfig{ProjectRoot: root}}, nil
		},
		NewRunner: func(runtimerunner.Config) (Runner, error) {
			t.Fatal("runner created before missing target validation")
			return nil, nil
		},
	}
	if _, err := service.Run(RunOptions{Paths: []string{missing}}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing target error = %v, want filesystem not-exist error", err)
	}
}
