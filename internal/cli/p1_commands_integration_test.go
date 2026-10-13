package cli

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
)

func TestApp_Run_TestCommand_SuccessAndFailure(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	successTest := `(ns sample.tests)
(doggo.test/deftest ok-test
  (doggo.test/is (= 1 1)))
`
	if err := os.WriteFile(filepath.Join(projectRoot, "sample_test.dog"), []byte(successTest), 0644); err != nil {
		t.Fatalf("write sample_test.dog: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{"test"})
	if err := app.Run(); err != nil {
		t.Fatalf("test command should pass: %v", err)
	}

	failingTest := `(ns sample.tests.fail)
(doggo.test/deftest ng-test
  (doggo.test/is (= 1 2)))
`
	if err := os.WriteFile(filepath.Join(projectRoot, "failing_test.dog"), []byte(failingTest), 0644); err != nil {
		t.Fatalf("write failing_test.dog: %v", err)
	}

	app = NewApp("1.0.0", []string{"test"})
	if err := app.Run(); err == nil {
		t.Fatalf("test command should fail when failing tests exist")
	}
}

func TestApp_Run_TestCommand_ReportFormats(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	testA := `(ns alpha.tests)
(doggo.test/deftest ok-a
  (doggo.test/is (= 1 1)))
`
	testB := `(ns beta.tests)
(doggo.test/deftest ok-b
  (doggo.test/is (= (+ 1 1) 2)))
`
	if err := os.WriteFile(filepath.Join(projectRoot, "alpha_test.dog"), []byte(testA), 0644); err != nil {
		t.Fatalf("write alpha_test.dog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "beta_test.dog"), []byte(testB), 0644); err != nil {
		t.Fatalf("write beta_test.dog: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	jsonReport := filepath.Join(projectRoot, "reports", "tests.json")
	app := NewApp("1.0.0", []string{
		"test",
		"--report-format", "json",
		"--report-file", jsonReport,
	})
	if err := app.Run(); err != nil {
		t.Fatalf("test command with json report failed: %v", err)
	}

	body, err := os.ReadFile(jsonReport)
	if err != nil {
		t.Fatalf("read json report: %v", err)
	}
	var decoded struct {
		Summary struct {
			Test int `json:"test"`
			Pass int `json:"pass"`
			Fail int `json:"fail"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("invalid json report: %v\n%s", err, string(body))
	}
	if decoded.Summary.Test != 2 || decoded.Summary.Pass != 2 || decoded.Summary.Fail != 0 {
		t.Fatalf("unexpected summary in json report: %+v", decoded.Summary)
	}

	junitReport := filepath.Join(projectRoot, "reports", "tests.xml")
	app = NewApp("1.0.0", []string{
		"test",
		"--report-format", "junit",
		"--report-file", junitReport,
	})
	if err := app.Run(); err != nil {
		t.Fatalf("test command with junit report failed: %v", err)
	}
	xmlBody, err := os.ReadFile(junitReport)
	if err != nil {
		t.Fatalf("read junit report: %v", err)
	}
	if !strings.Contains(string(xmlBody), "<testsuite") {
		t.Fatalf("junit report does not contain testsuite: %s", string(xmlBody))
	}
}

func TestApp_Run_TestCommand_RejectsUnknownOption(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	testFile := `(ns alpha.tests
  (:require [doggo.test :as t]))
(t/deftest ok
  (t/is (= 1 1)))
`
	if err := os.WriteFile(filepath.Join(projectRoot, "alpha_test.dog"), []byte(testFile), 0644); err != nil {
		t.Fatalf("write alpha_test.dog: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{"test", "--jobs", "2"})
	err = app.Run()
	if err == nil || !strings.Contains(err.Error(), "unknown test option: --jobs") {
		t.Fatalf("expected unknown-option error, got %v", err)
	}
}

func TestApp_Run_TestCommand_DefaultDiscoveryPreservesExplicitFileOrderDependencies(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	alphaTest := `(ns alpha.tests
  (:require [doggo.test :as t]))
(def shared-value 42)
`
	betaTest := `(ns beta.tests
  (:require [doggo.test :as t]))
(t/deftest reads-definition-from-earlier-target-file
  (t/is (= 42 alpha.tests/shared-value)))
`
	if err := os.WriteFile(filepath.Join(projectRoot, "alpha_test.dog"), []byte(alphaTest), 0644); err != nil {
		t.Fatalf("write alpha_test.dog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "beta_test.dog"), []byte(betaTest), 0644); err != nil {
		t.Fatalf("write beta_test.dog: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{
		"test",
		"alpha_test.dog",
		"beta_test.dog",
	})
	if err := app.Run(); err != nil {
		t.Fatalf("default discovery should preserve explicit target-file load order: %v", err)
	}
}

func TestApp_Run_TestCommand_MultipleExplicitPureTargetsPass(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	alphaTest := `(ns alpha.tests
  (:require [doggo.test :as t]))
(t/deftest alpha-pass
  (t/is (= 3 (count [1 2 3]))))
`
	betaTest := `(ns beta.tests
  (:require [doggo.test :as t]))
(t/deftest beta-pass
  (t/is (= true (vector? []))))
`
	if err := os.WriteFile(filepath.Join(projectRoot, "alpha_test.dog"), []byte(alphaTest), 0644); err != nil {
		t.Fatalf("write alpha_test.dog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "beta_test.dog"), []byte(betaTest), 0644); err != nil {
		t.Fatalf("write beta_test.dog: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{
		"test",
		"alpha_test.dog",
		"beta_test.dog",
	})
	if err := app.Run(); err != nil {
		t.Fatalf("test command should run multiple explicit pure targets on the standard runtime: %v", err)
	}
}

func TestApp_Run_TestCommand_StdlibTestsCanRequireProjectStdlib(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	libPath := filepath.Join(projectRoot, "stdlib", "doggo", "sample.dog")
	if err := os.MkdirAll(filepath.Dir(libPath), 0755); err != nil {
		t.Fatalf("mkdir stdlib dir: %v", err)
	}
	libSource := `(ns doggo.sample)
(defn answer [] 42)
`
	if err := os.WriteFile(libPath, []byte(libSource), 0644); err != nil {
		t.Fatalf("write sample stdlib: %v", err)
	}

	testPath := filepath.Join(projectRoot, "stdlib", "doggo", "sample_test.dog")
	testSource := `(ns doggo.sample-test
  (:require [doggo.sample :as sample]
            [doggo.test :as t]))
(t/deftest stdlib-can-be-tested-with-doggo-test
  (t/is (= 42 (sample/answer))))
`
	if err := os.WriteFile(testPath, []byte(testSource), 0644); err != nil {
		t.Fatalf("write sample stdlib test: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{"test", "stdlib"})
	if err := app.Run(); err != nil {
		t.Fatalf("stdlib tests should run via doggo.test: %v", err)
	}
}

func TestApp_Run_TestCommand_StdlibTestsCannotSeePrivateBuiltins(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	testPath := filepath.Join(projectRoot, "stdlib", "doggo", "private_test.dog")
	if err := os.MkdirAll(filepath.Dir(testPath), 0755); err != nil {
		t.Fatalf("mkdir stdlib dir: %v", err)
	}
	testSource := `(ns doggo.private-test
  (:require [doggo.test :as t]))
(t/deftest stdlib-tests-stay-on-public-surface
  (t/is (= 2 (math-sqrt* 4))))
`
	if err := os.WriteFile(testPath, []byte(testSource), 0644); err != nil {
		t.Fatalf("write private stdlib test: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{"test", "stdlib"})
	if err := app.Run(); err == nil || !strings.Contains(err.Error(), "undefined variable: math-sqrt*") {
		t.Fatalf("expected stdlib tests to reject private builtin access, got %v", err)
	}
}

func TestApp_Run_TestCommand_RepoPureStdlibTestsPass(t *testing.T) {
	repoRoot := findRepoRootForCLITest(t)
	projectRoot := filepath.Join(t.TempDir(), "stdlib-project")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}
	writeModAndLock(t, projectRoot)

	if err := copyDir(filepath.Join(repoRoot, "stdlib"), filepath.Join(projectRoot, "stdlib")); err != nil {
		t.Fatalf("copy stdlib: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{
		"test",
		"stdlib/doggo/basis_test.dog",
	})
	if err := app.Run(); err != nil {
		t.Fatalf("pure repo stdlib doggo tests should pass on the standard runtime: %v", err)
	}
}

func TestApp_Run_TestCommand_ExplicitFileOrderPreservesTestNamespace(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	if err := os.WriteFile(filepath.Join(projectRoot, "alpha_test.dog"), []byte(`
(ns alpha.tests
  (:require [doggo.test :as t]))

(t/deftest alpha-pass
  (t/is (= 1 1)))
`), 0644); err != nil {
		t.Fatalf("write alpha_test.dog: %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectRoot, "beta_test.dog"), []byte(`
(ns beta.tests
  (:require [doggo.test :as t]))

(t/deftest beta-pass
  (t/is (= 42 (+ 40 2))))
`), 0644); err != nil {
		t.Fatalf("write beta_test.dog: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	reportPath := filepath.Join(projectRoot, "report.json")
	app := NewApp("1.0.0", []string{
		"test",
		"--ns", "beta.tests",
		"--report-format", "json",
		"--report-file", reportPath,
		"alpha_test.dog",
		"beta_test.dog",
	})
	if err := app.Run(); err != nil {
		t.Fatalf("explicit test files should preserve namespaces across load order: %v", err)
	}

	body, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var decoded struct {
		Summary struct {
			Test int `json:"test"`
			Pass int `json:"pass"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if decoded.Summary.Test != 1 || decoded.Summary.Pass != 1 {
		t.Fatalf("expected beta.tests summary to include exactly one passing test, got %+v", decoded.Summary)
	}
}

func TestApp_Run_TestCommand_DefaultInvocationUsesHostStdlibRuntime(t *testing.T) {
	repoRoot := findRepoRootForCLITest(t)
	projectRoot := filepath.Join(t.TempDir(), "stdlib-project")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}
	writeModAndLock(t, projectRoot)
	if err := copyDir(filepath.Join(repoRoot, "stdlib"), filepath.Join(projectRoot, "stdlib")); err != nil {
		t.Fatalf("copy stdlib: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{
		"test",
		"stdlib/doggo/process_test.dog",
	})
	if err := app.Run(); err != nil {
		t.Fatalf("default test command should run host stdlib tests through the CLI host runtime: %v", err)
	}
}

func TestRunFileWithProfilingUsesProjectStdlibCoreFromScriptProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	corePath := filepath.Join(projectRoot, "stdlib", "doggo", "core.dog")
	if err := os.MkdirAll(filepath.Dir(corePath), 0755); err != nil {
		t.Fatalf("mkdir core dir: %v", err)
	}
	coreSource := `(ns doggo.core)
(def project-core-marker :project)
`
	if err := os.WriteFile(corePath, []byte(coreSource), 0644); err != nil {
		t.Fatalf("write project core: %v", err)
	}

	scriptPath := filepath.Join(projectRoot, "scripts", "main.dog")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte("project-core-marker\n"), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	outsideDir := t.TempDir()
	if err := os.Chdir(outsideDir); err != nil {
		t.Fatalf("chdir outside project: %v", err)
	}

	if err := RunFileWithArgsAndVMFrameProfiling(scriptPath, nil, false); err != nil {
		t.Fatalf("script should resolve doggo.core from project root: %v", err)
	}
}

func TestRunFileWithProfilingResolvesRelativeLoadFileFromScriptDir(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	scriptPath := filepath.Join(projectRoot, "scripts", "main.dog")
	libPath := filepath.Join(projectRoot, "scripts", "support", "lib.dog")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(libPath), 0755); err != nil {
		t.Fatalf("mkdir lib dir: %v", err)
	}
	if err := os.WriteFile(libPath, []byte("(def loaded-from-script-dir 42)\n"), 0644); err != nil {
		t.Fatalf("write lib file: %v", err)
	}
	scriptSource := `(load-file "support/lib.dog")
loaded-from-script-dir
`
	if err := os.WriteFile(scriptPath, []byte(scriptSource), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	outsideDir := t.TempDir()
	if err := os.Chdir(outsideDir); err != nil {
		t.Fatalf("chdir outside project: %v", err)
	}

	if err := RunFileWithArgsAndVMFrameProfiling(scriptPath, nil, false); err != nil {
		t.Fatalf("script should resolve relative load-file from script dir: %v", err)
	}
}

func TestRunFileWithProfilingExecutesProjectStdlibImplementationFile(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	scriptPath := filepath.Join(projectRoot, "stdlib", "doggo", "math.dog")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		t.Fatalf("mkdir stdlib dir: %v", err)
	}
	source := `(ns doggo.math)
(def sqrt
  (fn [x]
    (math-sqrt* x)))
(sqrt 4)
`
	if err := os.WriteFile(scriptPath, []byte(source), 0644); err != nil {
		t.Fatalf("write stdlib file: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	outsideDir := t.TempDir()
	if err := os.Chdir(outsideDir); err != nil {
		t.Fatalf("chdir outside project: %v", err)
	}

	if err := RunFileWithArgsAndVMFrameProfiling(scriptPath, nil, false); err != nil {
		t.Fatalf("stdlib implementation file should run through trusted path: %v", err)
	}
}

func TestRunFileWithProfilingResolvesScriptPathRelativeToCallerCWD(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	scriptPath := filepath.Join(projectRoot, "scripts", "main.dog")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte("42\n"), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	callerDir := filepath.Join(projectRoot, "sub")
	if err := os.MkdirAll(callerDir, 0755); err != nil {
		t.Fatalf("mkdir caller dir: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(callerDir); err != nil {
		t.Fatalf("chdir caller dir: %v", err)
	}

	relativeScript := filepath.ToSlash(filepath.Join("..", "scripts", "main.dog"))
	if err := RunFileWithArgsAndVMFrameProfiling(relativeScript, nil, false); err != nil {
		t.Fatalf("script path should resolve relative to caller cwd: %v", err)
	}
}

func TestAppRunFilePassesScriptArgsIntoDoggoCLI(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	scriptPath := filepath.Join(projectRoot, "main.dog")
	source := `(ns sample.main
  (:require [doggo.cli :as cli]))
(when (not= cli/*command-line-args* ["alpha" "beta"])
  (nth [] 1))
`
	if err := os.WriteFile(scriptPath, []byte(source), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{filepath.ToSlash(scriptPath), "alpha", "beta"})
	if err := app.Run(); err != nil {
		t.Fatalf("app run failed: %v", err)
	}
}

func TestApp_Run_FmtCheckAndRewrite(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "sample.dog")
	original := "\t(foo)  \n(bar)"
	if err := os.WriteFile(target, []byte(original), 0644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{"fmt", "--check", "sample.dog"})
	if err := app.Run(); err == nil {
		t.Fatalf("fmt --check should fail for unformatted file")
	}

	app = NewApp("1.0.0", []string{"fmt", "sample.dog"})
	if err := app.Run(); err != nil {
		t.Fatalf("fmt rewrite failed: %v", err)
	}

	formatted, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read formatted: %v", err)
	}
	if got, want := string(formatted), "(foo)\n(bar)\n"; got != want {
		t.Fatalf("formatted content mismatch\nwant=%q\ngot=%q", want, got)
	}

	app = NewApp("1.0.0", []string{"fmt", "--check", "sample.dog"})
	if err := app.Run(); err != nil {
		t.Fatalf("fmt --check should pass after rewrite: %v", err)
	}
}

func writeModAndLock(t *testing.T, projectRoot string) {
	t.Helper()

	mod := &project.ModFile{Deps: []string{}}
	if err := project.WriteModFile(project.ModPath(projectRoot), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}

	writeCLIRuntimeGenerationAt(t, projectRoot, nil, false)
}

func findRepoRootForCLITest(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for i := 0; i < 10; i++ {
		if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("repo root not found from %s", wd)
	return ""
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		return os.Chmod(target, info.Mode())
	})
}
