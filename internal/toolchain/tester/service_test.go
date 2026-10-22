package tester

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"doggo/internal/project"
	runtimerunner "doggo/internal/runtime/runner"
	"doggo/internal/toolchain/generation"
	"doggo/internal/types"
	typeatom "doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type fakeRunner struct {
	loadFile func(path string) (types.Value, error)
	run      func(expr string) (types.Value, error)
}

func TestServicePinsGenerationWithoutBlockingWritersAndClosesOnError(t *testing.T) {
	for _, failAt := range []string{"load", "execution"} {
		t.Run(failAt, func(t *testing.T) {
			root := t.TempDir()
			if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/tester", Deps: []string{}}); err != nil {
				t.Fatal(err)
			}
			prepared, err := (generation.Service{}).PrepareSync(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := prepared.Commit(); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "sample_test.dog")
			if err := os.WriteFile(target, []byte("(ns sample-test)\n"), 0644); err != nil {
				t.Fatal(err)
			}

			var session *project.GenerationSession
			openSession := func(path string) (*project.GenerationSession, error) {
				var err error
				session, err = project.OpenGenerationSession(path)
				if session != nil {
					t.Cleanup(func() { _ = session.Close() })
				}
				return session, err
			}
			assertReadableWithConcurrentWriter := func() {
				t.Helper()
				done := make(chan error, 1)
				go func() {
					lease, err := project.AcquireGenerationLease(root, true)
					if err == nil {
						err = lease.Close()
					}
					done <- err
				}()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("generation reader blocked a writer")
				}
				temporary, err := project.NewOwnedTempRoot("snapshot-check-")
				if err != nil {
					t.Fatal(err)
				}
				defer temporary.Close()
				if err := session.CopyAuditWorkspaceTo(temporary.Tree()); err != nil {
					t.Fatalf("active snapshot became unavailable: %v", err)
				}
			}

			want := errors.New(failAt + " failed")
			service := Service{OpenGenerationSession: openSession, NewRunner: func(runtimerunner.Config) (Runner, error) {
				return &fakeRunner{loadFile: func(string) (types.Value, error) {
					assertReadableWithConcurrentWriter()
					if failAt == "load" {
						return nil, want
					}
					return typeatom.NilValue, nil
				}, run: func(expr string) (types.Value, error) {
					if expr == "(require 'doggo.test)" {
						return typeatom.NilValue, nil
					}
					assertReadableWithConcurrentWriter()
					return nil, want
				}}, nil
			}}
			if _, err := service.Run(RunOptions{Paths: []string{target}, Namespaces: []string{"sample-test"}}); !errors.Is(err, want) {
				t.Fatalf("Run error = %v, want %v", err, want)
			}

			if session == nil {
				t.Fatal("session was not opened")
			}
			if _, err := session.BinarySnapshot(); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("service leaked its generation session: %v", err)
			}
		})
	}
}

func (f *fakeRunner) LoadFile(path string) (types.Value, error) {
	return f.loadFile(path)
}

func (f *fakeRunner) Run(expr string) (types.Value, error) {
	return f.run(expr)
}

func TestNewStandardRunnerUsesRuntimeOwnedHostSurface(t *testing.T) {
	runner, err := newStandardRunner(runtimerunner.Config{
		ProjectRoot:   t.TempDir(),
		LoadPaths:     []string{t.TempDir()},
		BootstrapCore: true,
	})
	if err != nil {
		t.Fatalf("newStandardRunner: %v", err)
	}

	got, err := runner.Run("(count [1 2 3])")
	if err != nil {
		t.Fatalf("count run: %v", err)
	}
	if got.String() != "3" {
		t.Fatalf("count result = %s, want 3", got.String())
	}

	if got, err := runner.Run(`(do (println "hi") 7)`); err != nil || got.String() != "7" {
		t.Fatalf("println should be available on the test runner host surface: got=%v err=%v", got, err)
	}

	if _, err := runner.Run(`(require '[doggo.io])`); err != nil {
		t.Fatalf("doggo.io should load on the test runner host surface: %v", err)
	}
	if got, err := runner.Run(`(doggo.io/basename "a/b.txt")`); err != nil || got.String() != `"b.txt"` {
		t.Fatalf("doggo.io should load on the test runner host surface: got=%v err=%v", got, err)
	}

	if _, err := runner.Run(`(io-file* "sample.txt")`); err == nil {
		t.Fatal("private host builtin io-file* should not be directly visible to tests")
	}
}

func TestServiceLoadsTargetFilesOnceAndRunsRequestedNamespaces(t *testing.T) {
	projectRoot := t.TempDir()
	generationSource := &project.GenerationSource{}
	sessionLoadPaths := []string{projectRoot}
	sessionSources := []*project.GenerationSource{generationSource}
	targetFile := filepath.Join(projectRoot, "test", "example_test.dog")
	if err := os.MkdirAll(filepath.Dir(targetFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetFile, []byte("(ns demo.test)"), 0644); err != nil {
		t.Fatal(err)
	}

	var loaded []string
	service := Service{
		OpenGenerationSession: func(scriptPath string) (*project.GenerationSession, error) {
			return &project.GenerationSession{Config: project.RuntimeConfig{
				ProjectRoot: projectRoot, LoadPaths: sessionLoadPaths, GenerationSources: sessionSources,
			}}, nil
		},
		NewRunner: func(cfg runtimerunner.Config) (Runner, error) {
			if len(cfg.LoadPaths) != 1 || cfg.LoadPaths[0] != projectRoot {
				t.Fatalf("runner load paths = %#v", cfg.LoadPaths)
			}
			if len(cfg.GenerationSources) != 1 || cfg.GenerationSources[0] != generationSource {
				t.Fatalf("runner generation sources = %#v", cfg.GenerationSources)
			}
			cfg.LoadPaths[0] = "replacement"
			cfg.GenerationSources[0] = &project.GenerationSource{}
			return &fakeRunner{
				loadFile: func(path string) (types.Value, error) {
					loaded = append(loaded, path)
					return typeatom.NilValue, nil
				},
				run: func(expr string) (types.Value, error) {
					switch expr {
					case "(require 'doggo.test)":
						return typeatom.NilValue, nil
					case "(doggo.test/run-tests 'demo.test)":
						return fakeReportValue(), nil
					default:
						t.Fatalf("unexpected expr: %s", expr)
						return nil, nil
					}
				},
			}, nil
		},
	}

	report, err := service.Run(RunOptions{
		Paths:      []string{targetFile},
		Namespaces: []string{"demo.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Test != 1 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	if !reflect.DeepEqual(loaded, []string{targetFile}) {
		t.Fatalf("loaded files = %v", loaded)
	}
	if sessionSources[0] != generationSource {
		t.Fatal("runner config mutation changed session generation sources")
	}
	if sessionLoadPaths[0] != projectRoot {
		t.Fatalf("runner config mutation changed session load paths: %#v", sessionLoadPaths)
	}
}

func fakeReportValue() types.Value {
	return coll.NewMapFromPairs([]types.Value{
		typeatom.NewKeyword("summary"), coll.NewMapFromPairs([]types.Value{
			typeatom.NewKeyword("test"), typeatom.NewNumberInt(1),
			typeatom.NewKeyword("pass"), typeatom.NewNumberInt(1),
			typeatom.NewKeyword("fail"), typeatom.NewNumberInt(0),
			typeatom.NewKeyword("error"), typeatom.NewNumberInt(0),
			typeatom.NewKeyword("duration-ms"), typeatom.NewNumberFloat(1.5),
		}),
		typeatom.NewKeyword("cases"), coll.NewVector(
			coll.NewMapFromPairs([]types.Value{
				typeatom.NewKeyword("ns"), types.NewSymbol("demo.test"),
				typeatom.NewKeyword("name"), types.NewSymbol("ok"),
				typeatom.NewKeyword("status"), typeatom.NewKeyword("pass"),
				typeatom.NewKeyword("duration-ms"), typeatom.NewNumberFloat(1.5),
				typeatom.NewKeyword("contexts"), coll.NewVector(),
			}),
		),
	})
}
