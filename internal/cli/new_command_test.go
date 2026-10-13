package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/toolchain/newproject"
)

func TestParseNewCommandArgs(t *testing.T) {
	opts, err := parseNewCommandArgs([]string{"app", "my-app"})
	if err != nil {
		t.Fatalf("parseNewCommandArgs returned error: %v", err)
	}
	if opts.Template != "app" || opts.Name != "my-app" {
		t.Fatalf("unexpected opts: %#v", opts)
	}
	if opts.Module != "" || opts.DryRun {
		t.Fatalf("unexpected default flags: %#v", opts)
	}

	opts, err = parseNewCommandArgs([]string{"lib", "my-lib", "--module", "github.com/acme/my-lib", "--dry-run"})
	if err != nil {
		t.Fatalf("parseNewCommandArgs returned error: %v", err)
	}
	if opts.Template != "lib" || opts.Name != "my-lib" || opts.Module != "github.com/acme/my-lib" || !opts.DryRun {
		t.Fatalf("unexpected opts with flags: %#v", opts)
	}
}

func TestParseNewCommandArgsRejectsInvalidUsage(t *testing.T) {
	tests := [][]string{
		{},
		{"app"},
		{"app", "my-app", "extra"},
		{"app", "my-app", "--module"},
		{"app", "my-app", "--module", ""},
		{"app", "my-app", "--module", "   "},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			if _, err := parseNewCommandArgs(args); err == nil {
				t.Fatalf("expected error for args %#v", args)
			}
		})
	}
}

func TestRunNewCommandDelegatesToService(t *testing.T) {
	var got newproject.Options
	called := false
	app := NewApp("1.0.0", []string{"new", "app", "my-app", "--module", "github.com/acme/my-app", "--dry-run"})
	app.services.NewProject = func(opts newproject.Options) (*newproject.Result, error) {
		called = true
		got = opts
		return &newproject.Result{
			ProjectDir: filepath.Join(opts.BaseDir, opts.Name),
			DryRun:     opts.DryRun,
			Files: []newproject.PlannedFile{
				{Path: "doggo.mod", Content: ""},
			},
		}, nil
	}

	output := captureCLIStdout(t, func() {
		if err := app.runNewCommand(); err != nil {
			t.Fatalf("runNewCommand returned error: %v", err)
		}
	})

	if !called {
		t.Fatal("expected NewProject service to be called")
	}
	if got.Template != "app" || got.Name != "my-app" || got.Module != "github.com/acme/my-app" || !got.DryRun {
		t.Fatalf("unexpected delegated opts: %#v", got)
	}
	if got.BaseDir == "" {
		t.Fatal("expected BaseDir to be set")
	}
	if !strings.Contains(output, "Dry run") || !strings.Contains(output, "doggo.mod") {
		t.Fatalf("unexpected dry-run output:\n%s", output)
	}
}

func TestApp_Run_NewCreatesProject(t *testing.T) {
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{"new", "app", "my-app"})
	output := captureCLIStdout(t, func() {
		if err := app.Run(); err != nil {
			t.Fatalf("doggo new failed: %v", err)
		}
	})
	wantOutput := "Created " + filepath.ToSlash(filepath.Join(tmp, "my-app")) + "\n" +
		"  doggo.mod\n" +
		"  .gitignore\n" +
		"  README.md\n" +
		"  src/my_app/core.dog\n" +
		"  test/my_app/core_test.dog\n"
	if output != wantOutput {
		t.Fatalf("new output = %q, want %q", output, wantOutput)
	}

	for _, rel := range []string{
		"doggo.mod",
		"README.md",
		filepath.Join("src", "my_app", "core.dog"),
		filepath.Join("test", "my_app", "core_test.dog"),
	} {
		if _, err := os.Stat(filepath.Join(tmp, "my-app", rel)); err != nil {
			t.Fatalf("expected generated file %s: %v", rel, err)
		}
	}
}

func TestApp_Run_NewDryRunReportsOnlyPlannedFiles(t *testing.T) {
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{"new", "app", "demo", "--dry-run"})
	output := captureCLIStdout(t, func() {
		if err := app.Run(); err != nil {
			t.Fatalf("doggo new --dry-run failed: %v", err)
		}
	})

	want := "Dry run: would create " + filepath.ToSlash(filepath.Join(tmp, "demo")) + "\n" +
		"  doggo.mod\n" +
		"  .gitignore\n" +
		"  README.md\n" +
		"  src/demo/core.dog\n" +
		"  test/demo/core_test.dog\n"
	if output != want {
		t.Fatalf("dry-run output = %q, want %q", output, want)
	}
	if _, err := os.Stat(filepath.Join(tmp, "demo")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create project directory, stat err = %v", err)
	}
}

func TestApp_Run_NewGenerationFailureRemovesTemporaryProject(t *testing.T) {
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Setenv("PATH", filepath.Join(tmp, "missing-path"))

	err = NewApp("1.0.0", []string{"new", "app", "demo"}).Run()
	if err == nil {
		t.Fatal("doggo new accepted unavailable Go toolchain")
	}
	if !strings.Contains(err.Error(), "executable file not found") {
		t.Fatalf("doggo new lost primary generation error: %v", err)
	}
	if strings.Contains(err.Error(), "owned tree contains unrecorded entry") {
		t.Fatalf("cleanup lost generation ownership: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(tmp, "demo")); !os.IsNotExist(statErr) {
		t.Fatalf("target exists after generation failure: %v", statErr)
	}
	entries, readErr := os.ReadDir(tmp)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".doggo-new-") {
			t.Fatalf("temporary project leaked after generation failure: %s", entry.Name())
		}
	}
}

func TestApp_Run_NewCliDirectZeroArgsRunsMain(t *testing.T) {
	projectRoot := createGeneratedProject(t, "cli", "my-tool")
	mainPath := filepath.Join(projectRoot, "src", "my_tool", "main.dog")

	output := captureCLIStdout(t, func() {
		if err := RunFileWithArgsAndVMFrameProfiling(mainPath, []string{}, false); err != nil {
			t.Fatalf("run generated cli failed: %v", err)
		}
	})

	if got, want := strings.TrimSpace(output), `"args=0"`; got != want {
		t.Fatalf("unexpected generated cli output: want %s, got %s", want, got)
	}
}

func TestApp_Run_NewCliRequireDoesNotAutoRunMain(t *testing.T) {
	projectRoot := createGeneratedProject(t, "cli", "my-tool")
	consumerPath := filepath.Join(projectRoot, "consumer.dog")
	source := `(ns consumer
  (:require [my_tool.main :as tool]))

(println (tool/run ["one"]))
`
	if err := os.WriteFile(consumerPath, []byte(source), 0644); err != nil {
		t.Fatalf("write consumer: %v", err)
	}

	output := captureCLIStdout(t, func() {
		if err := RunFileWithArgsAndVMFrameProfiling(consumerPath, []string{}, false); err != nil {
			t.Fatalf("run consumer failed: %v", err)
		}
	})

	if strings.Contains(output, `"args=0"`) {
		t.Fatalf("generated cli main should not run during require:\n%s", output)
	}
	if got, want := strings.TrimSpace(output), `"args=1"`; got != want {
		t.Fatalf("unexpected consumer output: want %s, got %s", want, got)
	}
}

func TestCommandRegistryIncludesNew(t *testing.T) {
	command := DefaultCLIRegistry().Lookup("new")
	if command == nil {
		t.Fatal("expected new command to be registered")
	}
	if command.Usage != "new <template> <name> [options]" {
		t.Fatalf("unexpected usage: %q", command.Usage)
	}
	help := renderCLIHelp()
	if !strings.Contains(help, "doggo new app my-app") {
		t.Fatalf("help does not mention doggo new:\n%s", help)
	}
}

func createGeneratedProject(t *testing.T, template, name string) string {
	t.Helper()
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	app := NewApp("1.0.0", []string{"new", template, name})
	captureCLIStdout(t, func() {
		if err := app.Run(); err != nil {
			t.Fatalf("doggo new failed: %v", err)
		}
	})
	projectRoot := filepath.Join(tmp, name)
	writeCLIRuntimeGenerationAt(t, projectRoot, nil, false)
	return projectRoot
}

func TestGeneratedAppCompletesToolchainWorkflow(t *testing.T) {
	projectRoot := createGeneratedProject(t, "app", "practical-app")
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir generated project: %v", err)
	}

	for _, args := range [][]string{
		{"fmt", "--check", "."},
		{"lint", "."},
		{"test"},
		{"mod", "sync"},
		{"build"},
	} {
		if err := NewApp("1.0.0", args).Run(); err != nil {
			t.Fatalf("doggo %s failed: %v", strings.Join(args, " "), err)
		}
	}

	lock, err := project.LoadLockFile(project.LockPath(projectRoot))
	if err != nil {
		t.Fatal(err)
	}
	firstBuild := lock
	if err := NewApp("1.0.0", []string{"build"}).Run(); err != nil {
		t.Fatalf("second doggo build failed: %v", err)
	}
	lock, err = project.LoadLockFile(project.LockPath(projectRoot))
	if err != nil {
		t.Fatal(err)
	}
	if lock.Build.Binary == "" || lock.Build.SHA256 == "" {
		t.Fatalf("second doggo build did not publish a binary: first=%#v second=%#v", firstBuild, lock)
	}
	mod, err := project.LoadModFile(project.ModPath(projectRoot))
	if err != nil {
		t.Fatal(err)
	}
	if err := project.ValidateGeneration(projectRoot, mod, lock); err != nil {
		t.Fatalf("second doggo build generation is invalid: %v", err)
	}
	session, err := project.OpenGenerationSession(projectRoot)
	if err != nil {
		t.Fatalf("open second build session: %v", err)
	}
	if !session.HasBinary() {
		t.Fatal("second build session has no retained binary")
	}
	snapshot, err := session.BinarySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ProjectPath != lock.Build.Binary || snapshot.SHA256 != lock.Build.SHA256 || len(snapshot.Content) == 0 {
		t.Fatalf("second build session snapshot = %#v", snapshot)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	unchangedBuild := lock
	binary := filepath.Join(projectRoot, filepath.FromSlash(lock.Build.Binary))
	output, err := exec.Command(binary, "src/practical_app/core.dog", "Codex").CombinedOutput()
	if err != nil {
		t.Fatalf("built runtime failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Hello, Codex!") {
		t.Fatalf("unexpected built runtime output: %s", output)
	}

	dependencyRoot := filepath.Join(filepath.Dir(projectRoot), "localdep")
	if err := os.MkdirAll(dependencyRoot, 0755); err != nil {
		t.Fatalf("mkdir local dependency: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dependencyRoot, "go.mod"), []byte("module example.com/localdep\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatalf("write dependency go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dependencyRoot, "localdep.go"), []byte("package localdep\n\ntype Greeter interface { Greet(string) string }\n\nfunc Answer() int { return 42 }\n"), 0644); err != nil {
		t.Fatalf("write dependency source: %v", err)
	}
	if err := NewApp("1.0.0", []string{"deps", "add", "../localdep"}).Run(); err != nil {
		t.Fatalf("doggo deps add failed: %v", err)
	}
	if err := NewApp("1.0.0", []string{"mod", "sync"}).Run(); err != nil {
		t.Fatalf("doggo mod sync with local dependency failed: %v", err)
	}
	if err := NewApp("1.0.0", []string{"build"}).Run(); err != nil {
		t.Fatalf("doggo build with local dependency failed: %v", err)
	}
	lock, err = project.LoadLockFile(project.LockPath(projectRoot))
	if err != nil {
		t.Fatal(err)
	}
	if lock.Generation == unchangedBuild.Generation {
		t.Fatalf("dependency change reused generation %s", unchangedBuild.Generation)
	}
	mod, err = project.LoadModFile(project.ModPath(projectRoot))
	if err != nil {
		t.Fatal(err)
	}
	if err := project.ValidateGeneration(projectRoot, mod, lock); err != nil {
		t.Fatalf("dependency build generation is invalid: %v", err)
	}
	binary = filepath.Join(projectRoot, filepath.FromSlash(lock.Build.Binary))

	interopScript := filepath.Join(projectRoot, "localdep.dog")
	if err := os.WriteFile(interopScript, []byte("(import \"example.com/localdep\")\n(println (localdep/Answer))\n"), 0644); err != nil {
		t.Fatalf("write interop script: %v", err)
	}
	output, err = exec.Command(binary, interopScript).CombinedOutput()
	if err != nil {
		t.Fatalf("built runtime failed with local dependency: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "42" {
		t.Fatalf("unexpected local dependency result: %s", output)
	}
}
