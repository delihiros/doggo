package cli

import (
	"os"
	"path/filepath"
	"testing"

	toolchaintester "doggo/internal/toolchain/tester"
)

func TestRunTestCommandUsesTesterService(t *testing.T) {
	called := false
	app := NewApp("dev", []string{"test", "--ns", "demo.test"})
	app.services.Test = func(paths []string, namespaces []string, reportFormat, reportFile string) (*toolchaintester.Report, error) {
		called = true
		return &toolchaintester.Report{
			Summary: toolchaintester.Summary{Test: 1, Pass: 1},
		}, nil
	}

	if err := app.runTestCommand(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected test service to be called")
	}
}

func TestRunTestCommandPassesParsedArguments(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "reports", "test.json")
	app := NewApp("dev", []string{
		"test",
		"--ns", "demo.test",
		"--report-format", "json",
		"--report-file", reportPath,
		"tests",
		"tests",
	})
	app.services.Test = func(paths []string, namespaces []string, reportFormat, reportFile string) (*toolchaintester.Report, error) {
		if got := len(paths); got != 1 || paths[0] != "tests" {
			t.Fatalf("paths = %#v", paths)
		}
		if got := len(namespaces); got != 1 || namespaces[0] != "demo.test" {
			t.Fatalf("namespaces = %#v", namespaces)
		}
		if reportFormat != testReportFormatJSON {
			t.Fatalf("report format = %q", reportFormat)
		}
		if reportFile != reportPath {
			t.Fatalf("report file = %q", reportFile)
		}
		return &toolchaintester.Report{Summary: toolchaintester.Summary{Test: 1, Pass: 1}}, nil
	}

	if err := app.runTestCommand(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected report file to be written: %v", err)
	}
}
