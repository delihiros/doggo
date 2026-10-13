package cli

import (
	"strings"
	"testing"

	benchdom "doggo/internal/bench"
	toolchainbench "doggo/internal/toolchain/bench"
)

func TestBenchRunUsesBenchService(t *testing.T) {
	called := false
	app := NewApp("dev", []string{"bench", "run", "--all"})
	app.services.BenchRun = func(opts toolchainbench.RunOptions) (*toolchainbench.RunReport, error) {
		called = true
		if !opts.RunAll {
			t.Fatal("expected run-all option")
		}
		return &toolchainbench.RunReport{Total: 1}, nil
	}

	if err := app.benchRun(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected bench service to be called")
	}
}

func TestBenchListUsesBenchService(t *testing.T) {
	called := false
	app := NewApp("dev", []string{"bench", "list"})
	app.services.BenchList = func() ([]benchdom.Scenario, error) {
		called = true
		return []benchdom.Scenario{{Name: "loop-fusion/map-reduce", Description: "demo"}}, nil
	}

	if err := app.benchList(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected bench list service to be called")
	}
}

func TestPrintBenchHelpIncludesDefaultValues(t *testing.T) {
	output := renderBenchHelp()
	for _, want := range []string{"default: 3", "default: 10", "bench/results", "--no-loop-fusion"} {
		if !strings.Contains(output, want) {
			t.Fatalf("bench help missing %q\n%s", want, output)
		}
	}
}

func TestBenchRunPrintsLiveProgressFromService(t *testing.T) {
	output := captureCLIStdout(t, func() {
		app := NewApp("dev", []string{"bench", "run", "--all"})
		app.services.BenchRun = func(opts toolchainbench.RunOptions) (*toolchainbench.RunReport, error) {
			if opts.OnProgress == nil {
				t.Fatal("expected progress callback")
			}
			opts.OnProgress(toolchainbench.ProgressEvent{Kind: toolchainbench.ProgressBatchStart, Total: 2})
			opts.OnProgress(toolchainbench.ProgressEvent{
				Kind:         toolchainbench.ProgressScenarioStart,
				ScenarioName: "loop-fusion/map-reduce",
				Warmup:       3,
				Iterations:   10,
			})
			return &toolchainbench.RunReport{Total: 2}, nil
		}
		if err := app.benchRun(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "Running 2 scenarios...") {
		t.Fatalf("missing batch progress\n%s", output)
	}
	if !strings.Contains(output, "Running: loop-fusion/map-reduce") {
		t.Fatalf("missing scenario progress\n%s", output)
	}
}

func TestBenchRunQuietSuppressesProgressButKeepsFinalReport(t *testing.T) {
	output := captureCLIStdout(t, func() {
		app := NewApp("dev", []string{"bench", "run", "--all", "--quiet"})
		app.services.BenchRun = func(opts toolchainbench.RunOptions) (*toolchainbench.RunReport, error) {
			if opts.OnProgress != nil {
				t.Fatal("expected quiet mode to leave progress callback unset")
			}
			return &toolchainbench.RunReport{
				Total: 1,
				Completed: []toolchainbench.ScenarioResult{{
					Scenario: benchdom.Scenario{Name: "loop-fusion/map-reduce"},
					Result: &benchdom.Result{
						Stats: benchdom.Stats{MeanNs: 2_500_000, MedianNs: 2_000_000, MinNs: 1_000_000, MaxNs: 4_000_000, P95Ns: 3_000_000},
					},
					SavedPath: "bench/results/demo.json",
				}},
			}, nil
		}
		if err := app.benchRun(); err != nil {
			t.Fatal(err)
		}
	})

	if strings.Contains(output, "Running:") {
		t.Fatalf("quiet mode should suppress live progress\n%s", output)
	}
	if !strings.Contains(output, "Mean:") || !strings.Contains(output, "Saved: bench/results/demo.json") {
		t.Fatalf("quiet mode should keep final report\n%s", output)
	}
}
