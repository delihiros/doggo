package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	benchdom "doggo/internal/bench"
	releasebundle "doggo/internal/release"
	toolchainbench "doggo/internal/toolchain/bench"
	toolchainnewproject "doggo/internal/toolchain/newproject"
	toolchainpublish "doggo/internal/toolchain/publish"
	toolchainrelease "doggo/internal/toolchain/release"
)

func renderNewProjectResult(result *toolchainnewproject.Result) string {
	if result == nil {
		return ""
	}
	var builder strings.Builder
	if result.DryRun {
		fmt.Fprintf(&builder, "Dry run: would create %s\n", filepath.ToSlash(result.ProjectDir))
	} else {
		fmt.Fprintf(&builder, "Created %s\n", filepath.ToSlash(result.ProjectDir))
	}
	for _, file := range result.Files {
		fmt.Fprintf(&builder, "  %s\n", filepath.ToSlash(file.Path))
	}
	return builder.String()
}

func renderPublishResult(result *toolchainpublish.Result) string {
	return fmt.Sprintf(
		"Published %s@%s\n  source: %s\n  archive: %s\n  registry: %s\n",
		result.Package.Name,
		result.Package.Version,
		result.Package.Source,
		result.ArchivePath,
		result.RegistryPath,
	)
}

func renderReleaseBundleSuccess(outputs *releasebundle.ReleaseBundleOutputs, allowUnsigned bool) string {
	msg := fmt.Sprintf("release bundle: wrote %s\n", filepath.ToSlash(outputs.ManifestPath))
	if outputs.SignaturePath == "" && allowUnsigned {
		msg += "release bundle: unsigned bundle generated (--allow-unsigned)\n"
	}
	return msg
}

func renderReleaseVerifySuccess() string {
	return "release verify: artifact checksums verified; SBOM files present\n"
}

func renderReleaseGateSuccess() string {
	return "release gate: passed\n"
}

func renderReleaseKeygenSuccess(outputs *toolchainrelease.KeyPairPaths) string {
	return fmt.Sprintf(
		"release keygen: wrote %s and %s\n",
		filepath.ToSlash(outputs.PrivateKeyPath),
		filepath.ToSlash(outputs.PublicKeyPath),
	)
}

func renderTestReportBody(report *TestRunReport, format string) ([]byte, error) {
	switch format {
	case testReportFormatHuman:
		return []byte(renderHumanTestReport(report)), nil
	case testReportFormatJSON:
		return renderJSONTestReport(report)
	case testReportFormatJUnit:
		return renderJUnitTestReport(report)
	default:
		return nil, fmt.Errorf("unsupported report format: %s", format)
	}
}

func renderBenchHelp() string {
	var builder strings.Builder
	builder.WriteString("Usage: doggo bench <subcommand> [options]\n\n")
	builder.WriteString(renderSubcommandSchemaSection("Subcommands", "bench", benchSubcommandSchemas))
	return builder.String()
}

func renderScenarioList(scenarios []benchdom.Scenario) string {
	var builder strings.Builder
	builder.WriteString("Available benchmark scenarios:\n\n")
	maxLen := 0
	for _, scenario := range scenarios {
		if len(scenario.Name) > maxLen {
			maxLen = len(scenario.Name)
		}
	}
	for _, scenario := range scenarios {
		fmt.Fprintf(&builder, "  %-*s %s\n", maxLen, scenario.Name, scenario.Description)
	}
	fmt.Fprintf(&builder, "\nTotal: %d scenarios\n", len(scenarios))
	return builder.String()
}

func renderBenchReport(report *toolchainbench.RunReport) string {
	if report == nil {
		return ""
	}
	var builder strings.Builder
	for _, completed := range report.Completed {
		if report.Total > 1 {
			fmt.Fprintf(&builder, "Result: %s\n", completed.Scenario.Name)
		}
		renderBenchScenarioResult(&builder, completed)
	}
	if report.Total > 1 {
		fmt.Fprintf(&builder, "\nCompleted: %d/%d scenarios\n", len(report.Completed), report.Total)
		if len(report.Failures) > 0 {
			builder.WriteString("Failed scenarios:\n")
			for _, failure := range report.Failures {
				fmt.Fprintf(&builder, "  - %s: %s\n", failure.Name, failure.Error)
			}
		}
	}
	return builder.String()
}

func renderBenchScenarioResult(builder *strings.Builder, completed toolchainbench.ScenarioResult) {
	if completed.Result == nil {
		return
	}
	result := completed.Result
	fmt.Fprintf(builder, "  Mean:   %.2f ms/iteration\n", result.Stats.MeanNs/1e6)
	fmt.Fprintf(builder, "  Median: %.2f ms\n", float64(result.Stats.MedianNs)/1e6)
	fmt.Fprintf(builder, "  Min:    %.2f ms\n", float64(result.Stats.MinNs)/1e6)
	fmt.Fprintf(builder, "  Max:    %.2f ms\n", float64(result.Stats.MaxNs)/1e6)
	fmt.Fprintf(builder, "  P95:    %.2f ms\n", float64(result.Stats.P95Ns)/1e6)
	if result.Stats.CompileTimeNs > 0 {
		fmt.Fprintf(builder, "  Compile: %.2f ms\n", float64(result.Stats.CompileTimeNs)/1e6)
	}
	if result.Stats.ExecTimeNs > 0 {
		fmt.Fprintf(builder, "  Exec (total): %.2f ms\n", float64(result.Stats.ExecTimeNs)/1e6)
	}
	fmt.Fprintf(builder, "  Alloc: %d bytes, %d objects\n", result.Stats.AllocBytes, result.Stats.AllocObjects)
	fmt.Fprintf(builder, "  Saved: %s\n\n", filepath.ToSlash(completed.SavedPath))
}

func renderBenchProgress(event toolchainbench.ProgressEvent) string {
	var builder strings.Builder
	switch event.Kind {
	case toolchainbench.ProgressBatchStart:
		fmt.Fprintf(&builder, "Running %d scenarios...\n\n", event.Total)
	case toolchainbench.ProgressScenarioStart:
		fmt.Fprintf(&builder, "Running: %s\n", event.ScenarioName)
		fmt.Fprintf(&builder, "  Warmup: %d iterations\n", event.Warmup)
		fmt.Fprintf(&builder, "  Measure: %d iterations\n", event.Iterations)
		if event.CompileOnce {
			builder.WriteString("  Mode: compile-once/run-many\n")
		}
	}
	return builder.String()
}
