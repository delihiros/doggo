package bench

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	benchdom "doggo/internal/bench"
	"doggo/internal/project"
	"doggo/internal/runtime/enginevm"
	runtimerunner "doggo/internal/runtime/runner"
	"doggo/internal/vm"
)

type RunOptions struct {
	ScenarioName      string
	RunAll            bool
	Warmup            int
	Iterations        int
	OutputDir         string
	Quiet             bool
	DisableLoopFusion bool
	CompileOnce       bool
	OnProgress        func(ProgressEvent)
}

type ProgressKind string

const (
	ProgressBatchStart    ProgressKind = "batch-start"
	ProgressScenarioStart ProgressKind = "scenario-start"
)

type ProgressEvent struct {
	Kind         ProgressKind
	Total        int
	ScenarioName string
	Warmup       int
	Iterations   int
	CompileOnce  bool
}

func DefaultRunOptions() RunOptions {
	return RunOptions{
		Warmup:            3,
		Iterations:        10,
		OutputDir:         benchdom.DefaultOutputDir(),
		Quiet:             false,
		DisableLoopFusion: false,
		CompileOnce:       false,
	}
}

type ScenarioResult struct {
	Scenario  benchdom.Scenario
	Result    *benchdom.Result
	SavedPath string
}

type ScenarioFailure struct {
	Name  string
	Error string
}

type RunReport struct {
	Completed []ScenarioResult
	Failures  []ScenarioFailure
	Total     int
}

type Service struct {
	OpenGenerationSession func(path string) (*project.GenerationSession, error)
	NewRunner             func(cfg runtimerunner.Config) (*vm.Runner, error)
	SaveResult            func(result *benchdom.Result, outputDir string) (string, error)
}

func (s Service) ListScenarios() []benchdom.Scenario {
	return benchdom.BuiltinScenarios()
}

func (s Service) Run(opts RunOptions) (_ *RunReport, err error) {
	var scenarios []benchdom.Scenario
	if opts.RunAll {
		scenarios = s.ListScenarios()
	} else {
		if strings.TrimSpace(opts.ScenarioName) == "" {
			return nil, fmt.Errorf("bench run requires a scenario name or --all flag")
		}
		scenario := benchdom.FindScenario(opts.ScenarioName)
		if scenario == nil {
			return nil, fmt.Errorf("unknown scenario: %s\nUse 'doggo bench list' to see available scenarios", opts.ScenarioName)
		}
		scenarios = []benchdom.Scenario{*scenario}
	}

	openSession := s.OpenGenerationSession
	if openSession == nil {
		openSession = project.OpenGenerationSession
	}
	session, err := openSession("")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()

	if opts.RunAll {
		opts.emitProgress(ProgressEvent{Kind: ProgressBatchStart, Total: len(scenarios)})
	}
	report := &RunReport{Completed: make([]ScenarioResult, 0, len(scenarios)), Failures: []ScenarioFailure{}, Total: len(scenarios)}
	for i := range scenarios {
		result, runErr := s.runScenario(&scenarios[i], session.Config, opts)
		if runErr != nil {
			if !opts.RunAll {
				return nil, runErr
			}
			report.Failures = append(report.Failures, ScenarioFailure{Name: scenarios[i].Name, Error: runErr.Error()})
			continue
		}
		report.Completed = append(report.Completed, *result)
	}
	return report, nil
}

func (s Service) runScenario(scenario *benchdom.Scenario, cfg project.RuntimeConfig, opts RunOptions) (*ScenarioResult, error) {
	opts.emitProgress(ProgressEvent{
		Kind:         ProgressScenarioStart,
		ScenarioName: scenario.Name,
		Warmup:       opts.Warmup,
		Iterations:   opts.Iterations,
		CompileOnce:  opts.CompileOnce,
	})

	collector := benchdom.NewStatsCollector(scenario.Name, opts.Warmup, opts.Iterations)
	collector.SetStartTime(time.Now())

	newRunner := s.NewRunner
	if newRunner == nil {
		newRunner = newStandardRunner
	}
	runner, err := newRunner(runtimerunner.Config{
		ProjectRoot:       cfg.ProjectRoot,
		LoadPaths:         append([]string(nil), cfg.LoadPaths...),
		GenerationSources: append([]*project.GenerationSource(nil), cfg.GenerationSources...),
		Lock:              cfg.Lock,
		BootstrapCore:     true,
	})
	if err != nil {
		return nil, err
	}
	if opts.DisableLoopFusion {
		runner.DisableLoopFusion()
	}
	if scenario.Setup != "" {
		if _, err := runner.Run(scenario.Setup); err != nil {
			return nil, fmt.Errorf("setup failed: %w", err)
		}
	}
	if opts.CompileOnce {
		if err := s.executeCompileOnce(runner, scenario, opts, collector); err != nil {
			return nil, err
		}
	} else {
		if err := s.executeNormal(runner, scenario, opts, collector); err != nil {
			return nil, err
		}
	}
	result, savedPath, err := s.saveBenchResult(collector, opts)
	if err != nil {
		return nil, err
	}
	return &ScenarioResult{
		Scenario:  *scenario,
		Result:    result,
		SavedPath: savedPath,
	}, nil
}

func (s Service) executeNormal(runner *vm.Runner, scenario *benchdom.Scenario, opts RunOptions, collector *benchdom.StatsCollector) error {
	for i := 0; i < opts.Warmup; i++ {
		if _, err := runner.Run(scenario.Code); err != nil {
			return fmt.Errorf("warmup failed: %w", err)
		}
	}
	collector.CaptureMemoryBaseline()
	for i := 0; i < opts.Iterations; i++ {
		timer := benchdom.NewTimer()
		if _, err := runner.Run(scenario.Code); err != nil {
			return fmt.Errorf("iteration %d failed: %w", i, err)
		}
		collector.RecordDuration(timer.Elapsed())
	}
	collector.CaptureMemoryEndpoint()
	return nil
}

func (s Service) executeCompileOnce(runner *vm.Runner, scenario *benchdom.Scenario, opts RunOptions, collector *benchdom.StatsCollector) error {
	compileTimer := benchdom.NewTimer()
	precompiled, err := runner.Compile(scenario.Code)
	if err != nil {
		return fmt.Errorf("compile failed: %w", err)
	}
	collector.SetCompileTime(compileTimer.Elapsed())
	for i := 0; i < opts.Warmup; i++ {
		if _, err := runner.RunPrecompiled(precompiled); err != nil {
			return fmt.Errorf("warmup failed: %w", err)
		}
	}
	collector.CaptureMemoryBaseline()
	var lastStats *vm.ExecStats
	for i := 0; i < opts.Iterations; i++ {
		timer := benchdom.NewTimer()
		if i == opts.Iterations-1 {
			_, lastStats, err = runner.RunPrecompiledWithStats(precompiled)
		} else {
			_, err = runner.RunPrecompiled(precompiled)
		}
		if err != nil {
			return fmt.Errorf("iteration %d failed: %w", i, err)
		}
		elapsed := timer.Elapsed()
		collector.RecordDuration(elapsed)
		collector.RecordExecTime(elapsed)
	}
	if lastStats != nil {
		collector.SetLastIterationInstructionStats(lastStats.TotalInstructions, lastStats.OpcodeStats)
	}
	collector.CaptureMemoryEndpoint()
	return nil
}

func (s Service) saveBenchResult(collector *benchdom.StatsCollector, opts RunOptions) (*benchdom.Result, string, error) {
	result := collector.ToResult()
	if sha := getGitSHA(); sha != "" {
		result.SetGitCommit(sha)
	}
	result.SetOptConfig(!opts.DisableLoopFusion)
	saveResult := s.SaveResult
	if saveResult == nil {
		saveResult = benchdom.SaveResult
	}
	savedPath, err := saveResult(result, opts.OutputDir)
	if err != nil {
		return nil, "", fmt.Errorf("failed to save result: %w", err)
	}
	return result, savedPath, nil
}

func newStandardRunner(cfg runtimerunner.Config) (*vm.Runner, error) {
	runner, err := runtimerunner.New(cfg)
	if err != nil {
		return nil, err
	}
	vmRunner, ok := enginevm.AsVMRunner(runner)
	if !ok || vmRunner == nil {
		return nil, fmt.Errorf("runtime runner did not expose a vm runner")
	}
	return vmRunner, nil
}

func getGitSHA() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (opts RunOptions) emitProgress(event ProgressEvent) {
	if opts.OnProgress != nil {
		opts.OnProgress(event)
	}
}
