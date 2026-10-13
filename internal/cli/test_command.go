package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (app *App) runTestCommand() error {
	opts, err := parseTestCommandArgs(app.args[1:])
	if err != nil {
		return err
	}

	if app.services.Test == nil {
		return fmt.Errorf("test service is not configured")
	}
	report, err := app.services.Test(opts.Paths, opts.Namespaces, opts.ReportFormat, opts.ReportFile)
	if err != nil {
		return err
	}
	cliReport := fromTesterReport(report)
	if cliReport == nil || (cliReport.Summary.Test == 0 && len(cliReport.Cases) == 0) {
		fmt.Println("No tests found")
		return nil
	}

	if err := outputTestReport(cliReport, opts); err != nil {
		return err
	}
	if cliReport.Summary.Fail+cliReport.Summary.Error > 0 {
		return fmt.Errorf("test failed")
	}
	return nil
}

func outputTestReport(report *TestRunReport, opts *testCommandOptions) error {
	if report == nil {
		return fmt.Errorf("test report is nil")
	}

	var body []byte
	var err error
	body, err = renderTestReportBody(report, opts.ReportFormat)
	if err != nil {
		return err
	}

	if opts.ReportFile != "" {
		if err := writeTestReportFile(opts.ReportFile, body); err != nil {
			return err
		}
		if opts.ReportFormat == testReportFormatHuman {
			fmt.Print(string(body))
		} else {
			fmt.Println(formatTestSummaryLine(report.Summary))
		}
		return nil
	}

	fmt.Print(string(body))
	if opts.ReportFormat != testReportFormatHuman && !strings.HasSuffix(string(body), "\n") {
		fmt.Println()
	}
	return nil
}

func writeTestReportFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0644)
}
