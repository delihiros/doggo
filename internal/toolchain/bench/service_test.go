package bench

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	benchdom "doggo/internal/bench"
	"doggo/internal/project"
	runtimerunner "doggo/internal/runtime/runner"
	"doggo/internal/toolchain/generation"
	"doggo/internal/vm"
)

func TestNewStandardRunnerUsesRuntimeOwnedStandardSurface(t *testing.T) {
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

	if _, err := runner.Run(`(slurp "sample.txt")`); err == nil {
		t.Fatal("expected slurp to stay unavailable on the default standard surface")
	}
}

func TestServicePinsGenerationWithoutBlockingWritersAndClosesOnError(t *testing.T) {
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/bench", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

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

	want := errors.New("save failed")
	service := Service{OpenGenerationSession: openSession, SaveResult: func(*benchdom.Result, string) (string, error) {
		assertReadableWithConcurrentWriter()
		return "", want
	}}
	opts := RunOptions{ScenarioName: "baseline/reduce-only-100", Warmup: 0, Iterations: 1, OnProgress: func(event ProgressEvent) {
		if event.Kind == ProgressScenarioStart {
			assertReadableWithConcurrentWriter()
		}
	}}
	if _, err := service.Run(opts); !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want %v", err, want)
	}

	if session == nil {
		t.Fatal("session was not opened")
	}
	if _, err := session.BinarySnapshot(); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("service leaked its generation session: %v", err)
	}
}

func TestServiceRunReturnsUnknownScenarioError(t *testing.T) {
	_, err := (Service{}).Run(RunOptions{ScenarioName: "missing-scenario"})
	if err == nil || !strings.Contains(err.Error(), "unknown scenario") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceRunExecutesScenarioAndSavesResult(t *testing.T) {
	projectRoot := t.TempDir()
	generationSource := &project.GenerationSource{}
	sessionLoadPaths := []string{projectRoot}
	sessionSources := []*project.GenerationSource{generationSource}
	var savedResult *benchdom.Result
	service := Service{
		OpenGenerationSession: func(path string) (*project.GenerationSession, error) {
			return &project.GenerationSession{Config: project.RuntimeConfig{
				ProjectRoot: projectRoot, LoadPaths: sessionLoadPaths, GenerationSources: sessionSources,
			}}, nil
		},
		NewRunner: func(cfg runtimerunner.Config) (*vm.Runner, error) {
			if len(cfg.LoadPaths) != 1 || cfg.LoadPaths[0] != projectRoot {
				t.Fatalf("runner load paths = %#v", cfg.LoadPaths)
			}
			if len(cfg.GenerationSources) != 1 || cfg.GenerationSources[0] != generationSource {
				t.Fatalf("runner generation sources = %#v", cfg.GenerationSources)
			}
			cfg.LoadPaths[0] = "replacement"
			cfg.GenerationSources[0] = &project.GenerationSource{}
			return newStandardRunner(cfg)
		},
		SaveResult: func(result *benchdom.Result, outputDir string) (string, error) {
			savedResult = result
			return filepath.Join(outputDir, "result.json"), nil
		},
	}

	report, err := service.Run(RunOptions{
		ScenarioName: "baseline/reduce-only-100",
		Warmup:       0,
		Iterations:   1,
		OutputDir:    filepath.Join(projectRoot, "bench", "results"),
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.Total != 1 || len(report.Completed) != 1 || len(report.Failures) != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if savedResult == nil || savedResult.Scenario != "baseline/reduce-only-100" {
		t.Fatalf("unexpected saved result: %+v", savedResult)
	}
	if report.Completed[0].SavedPath == "" {
		t.Fatal("expected saved path")
	}
	if sessionSources[0] != generationSource {
		t.Fatal("runner config mutation changed session generation sources")
	}
	if sessionLoadPaths[0] != projectRoot {
		t.Fatalf("runner config mutation changed session load paths: %#v", sessionLoadPaths)
	}
}

func TestServiceRunAllAggregatesFailures(t *testing.T) {
	service := Service{
		OpenGenerationSession: func(path string) (*project.GenerationSession, error) {
			return &project.GenerationSession{Config: project.RuntimeConfig{ProjectRoot: t.TempDir(), LoadPaths: []string{}}}, nil
		},
		NewRunner: func(cfg runtimerunner.Config) (*vm.Runner, error) {
			return nil, errors.New("boom")
		},
	}

	report, err := service.Run(RunOptions{
		RunAll:     true,
		Warmup:     0,
		Iterations: 0,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.Total == 0 {
		t.Fatal("expected scenarios")
	}
	if len(report.Completed) != 0 {
		t.Fatalf("unexpected completed runs: %+v", report.Completed)
	}
	if len(report.Failures) != report.Total {
		t.Fatalf("failures = %d, total = %d", len(report.Failures), report.Total)
	}
}
