package cli

import (
	"fmt"

	benchdom "doggo/internal/bench"
	toolchainbench "doggo/internal/toolchain/bench"
)

func (app *App) runBenchCommand() error {
	if len(app.args) < 2 {
		return app.printBenchHelp()
	}
	return app.runNamedSubcommand(defaultBenchSubcommandRegistry())
}

func (app *App) printBenchHelp() error {
	fmt.Print(renderBenchHelp())
	return nil
}

func (app *App) benchList() error {
	if app.services.BenchList == nil {
		return fmt.Errorf("bench list service is not configured")
	}
	scenarios, err := app.services.BenchList()
	if err != nil {
		return err
	}
	fmt.Print(renderBenchScenarioList(scenarios))
	return nil
}

func (app *App) benchRun() error {
	opts, err := parseBenchRunArgs(app.args[2:])
	if err != nil {
		return err
	}
	if !opts.Quiet {
		opts.OnProgress = func(event toolchainbench.ProgressEvent) {
			fmt.Print(renderBenchProgress(event))
		}
	}
	if app.services.BenchRun == nil {
		return fmt.Errorf("bench run service is not configured")
	}
	report, err := app.services.BenchRun(*opts)
	if err != nil {
		return err
	}
	fmt.Print(renderBenchRunReport(report, *opts))
	return nil
}

func renderBenchScenarioList(scenarios []benchdom.Scenario) string {
	return renderScenarioList(scenarios)
}

func renderBenchRunReport(report *toolchainbench.RunReport, opts toolchainbench.RunOptions) string {
	return renderBenchReport(report)
}
