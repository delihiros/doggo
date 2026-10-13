package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	toolchaintester "doggo/internal/toolchain/tester"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestParseTestCommandArgs(t *testing.T) {
	opts, err := parseTestCommandArgs([]string{
		"tests", "--ns", "foo.bar", "pkg", "--ns", "baz.qux",
	})
	if err != nil {
		t.Fatalf("parseTestCommandArgs failed: %v", err)
	}
	if got, want := strings.Join(opts.Paths, ","), "tests,pkg"; got != want {
		t.Fatalf("paths mismatch: got=%q want=%q", got, want)
	}
	if got, want := strings.Join(opts.Namespaces, ","), "foo.bar,baz.qux"; got != want {
		t.Fatalf("namespaces mismatch: got=%q want=%q", got, want)
	}
	if opts.ReportFormat != testReportFormatHuman {
		t.Fatalf("expected default report format %q, got %q", testReportFormatHuman, opts.ReportFormat)
	}
	if opts.ReportFile != "" {
		t.Fatalf("expected empty report file by default, got %q", opts.ReportFile)
	}
}

func TestParseTestCommandArgs_MissingNamespace(t *testing.T) {
	if _, err := parseTestCommandArgs([]string{"--ns"}); err == nil {
		t.Fatalf("expected error for missing namespace")
	}
}

func TestParseTestCommandArgs_WithReportOptions(t *testing.T) {
	opts, err := parseTestCommandArgs([]string{
		"--report-format", "json",
		"--report-file", "reports/test.json",
		"--ns", "my.tests",
		"tests",
	})
	if err != nil {
		t.Fatalf("parseTestCommandArgs failed: %v", err)
	}

	if opts.ReportFormat != testReportFormatJSON {
		t.Fatalf("expected report format json, got %s", opts.ReportFormat)
	}
	if opts.ReportFile != "reports/test.json" {
		t.Fatalf("expected report file path to be preserved, got %q", opts.ReportFile)
	}
	if len(opts.Namespaces) != 1 || opts.Namespaces[0] != "my.tests" {
		t.Fatalf("unexpected namespaces: %v", opts.Namespaces)
	}
	if len(opts.Paths) != 1 || opts.Paths[0] != "tests" {
		t.Fatalf("unexpected paths: %v", opts.Paths)
	}
}

func TestParseTestCommandArgs_NormalizesDuplicates(t *testing.T) {
	opts, err := parseTestCommandArgs([]string{
		"tests",
		"tests",
		"--ns", " demo.test ",
		"--ns", "demo.test",
	})
	if err != nil {
		t.Fatalf("parseTestCommandArgs failed: %v", err)
	}
	if got, want := strings.Join(opts.Paths, ","), "tests"; got != want {
		t.Fatalf("paths mismatch: got=%q want=%q", got, want)
	}
	if got, want := strings.Join(opts.Namespaces, ","), "demo.test"; got != want {
		t.Fatalf("namespaces mismatch: got=%q want=%q", got, want)
	}
}

func TestParseTestCommandArgs_RejectsUnknownOption(t *testing.T) {
	cases := [][]string{
		{"--jobs"},
		{"--unknown"},
	}
	for _, args := range cases {
		if _, err := parseTestCommandArgs(args); err == nil || !strings.Contains(err.Error(), "unknown test option: "+args[0]) {
			t.Fatalf("expected unknown-option error for args %v, got %v", args, err)
		}
	}
}

func TestParseTestCommandArgs_InvalidReportFormat(t *testing.T) {
	if _, err := parseTestCommandArgs([]string{"--report-format", "yaml"}); err == nil {
		t.Fatalf("expected error for invalid report format")
	}
	if _, err := parseTestCommandArgs([]string{"--report-format"}); err == nil {
		t.Fatalf("expected error for missing report format value")
	}
}

func TestParseTestCommandArgs_MissingReportFile(t *testing.T) {
	if _, err := parseTestCommandArgs([]string{"--report-file"}); err == nil {
		t.Fatalf("expected error for missing report file")
	}
}

func TestTesterResolveTargetFiles_AutoDiscovery(t *testing.T) {
	root := t.TempDir()

	mustWrite := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	mustWrite("a_test.dog", "(ns a)")
	mustWrite("b.dog", "(ns b)")
	mustWrite("pkg/c_test.dog", "(ns c)")
	mustWrite("docs/d_test.dog", "(ns d)")
	mustWrite("examples/e_test.dog", "(ns e)")
	mustWrite(".doggo/f_test.dog", "(ns f)")

	files, err := toolchaintester.ResolveTargetFiles(root, nil)
	if err != nil {
		t.Fatalf("ResolveTargetFiles failed: %v", err)
	}
	paths := filepath.ToSlash(strings.Join(files, "\n"))
	if !strings.Contains(paths, filepath.ToSlash(filepath.Join(root, "a_test.dog"))) {
		t.Fatalf("a_test.dog not found: %v", files)
	}
	if !strings.Contains(paths, filepath.ToSlash(filepath.Join(root, "pkg", "c_test.dog"))) {
		t.Fatalf("pkg/c_test.dog not found: %v", files)
	}
	if strings.Contains(paths, "docs/d_test.dog") {
		t.Fatalf("docs test should be excluded: %v", files)
	}
	if strings.Contains(paths, "examples/e_test.dog") {
		t.Fatalf("examples test should be excluded: %v", files)
	}
	if strings.Contains(paths, ".doggo/f_test.dog") {
		t.Fatalf(".doggo test should be excluded: %v", files)
	}
}

func TestTesterResolveTargetFiles_ExplicitFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "manual_suite.dog")
	if err := os.WriteFile(target, []byte("(ns manual)"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	files, err := toolchaintester.ResolveTargetFiles(root, []string{target})
	if err != nil {
		t.Fatalf("ResolveTargetFiles failed: %v", err)
	}
	if len(files) != 1 || filepath.Clean(files[0]) != filepath.Clean(target) {
		t.Fatalf("unexpected files: %v", files)
	}
}

func TestTesterDecodeReportSummaryConversion(t *testing.T) {
	result := coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("summary"), coll.NewMapFromPairs([]types.Value{
			atom.NewKeyword("test"), atom.NewNumberInt(3),
			atom.NewKeyword("pass"), atom.NewNumberInt(1),
			atom.NewKeyword("fail"), atom.NewNumberInt(1),
			atom.NewKeyword("error"), atom.NewNumberInt(2),
			atom.NewKeyword("duration-ms"), atom.NewNumberInt(7),
		}),
		atom.NewKeyword("cases"), coll.NewVector(),
	})
	testerReport, err := toolchaintester.DecodeReport(result)
	if err != nil {
		t.Fatalf("DecodeReport failed: %v", err)
	}
	report := fromTesterReport(testerReport)
	failures := report.Summary.Fail + report.Summary.Error
	if failures != 3 {
		t.Fatalf("failures=%d, want 3", failures)
	}
}

func TestCheckFormatSource(t *testing.T) {
	issues := checkFormatSource("sample.dog", "\t(println \"x\")  \n(no-newline)")
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d (%v)", len(issues), issues)
	}
	if issues[0].Message != "source is not formatted" {
		t.Fatalf("unexpected issue: %+v", issues[0])
	}
}

func TestCheckFormatSource_AlreadyFormatted(t *testing.T) {
	issues := checkFormatSource("sample.dog", "(println \"x\")\n")
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestParseFmtCommandArgs(t *testing.T) {
	opts, err := parseFmtCommandArgs([]string{"--check", "examples"})
	if err != nil {
		t.Fatalf("parseFmtCommandArgs failed: %v", err)
	}
	if !opts.Check {
		t.Fatalf("expected check mode")
	}
	if len(opts.Paths) != 1 || opts.Paths[0] != "examples" {
		t.Fatalf("unexpected paths: %v", opts.Paths)
	}
}

func TestFormatSource(t *testing.T) {
	got := formatSource("\t(foo)  \n(bar)")
	want := "(foo)\n(bar)\n"
	if got != want {
		t.Fatalf("formatSource mismatch\nwant=%q\ngot=%q", want, got)
	}
}

func TestLintSourceRules(t *testing.T) {
	source := `
(require (+ 1 2))
(import 123)
(import "fmt")
(import "fmt")
`
	issues := lintSource("lint.dog", source)
	if len(issues) < 3 {
		t.Fatalf("expected at least 3 issues, got %d (%v)", len(issues), issues)
	}

	foundRequire := false
	foundImportLiteral := false
	foundDuplicateImport := false
	for _, issue := range issues {
		switch {
		case strings.Contains(issue.Message, "require argument must be"):
			foundRequire = true
		case strings.Contains(issue.Message, "import argument must be string literal"):
			foundImportLiteral = true
		case strings.Contains(issue.Message, "duplicate import"):
			foundDuplicateImport = true
		}
	}
	if !foundRequire || !foundImportLiteral || !foundDuplicateImport {
		t.Fatalf("unexpected lint issues: %+v", issues)
	}
}

func TestLintSource_AllowsStaticRequireForms(t *testing.T) {
	source := `
(require 'foo.bar)
(require :foo.bar)
(require '[foo.bar :as fb])
`
	issues := lintSource("lint-ok.dog", source)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}
}
