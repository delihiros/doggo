package cli

import (
	"doggo/internal/project"
	toolchaindeps "doggo/internal/toolchain/deps"
	"doggo/internal/toolchain/generation"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModSyncSubcommandUsesSyncService(t *testing.T) {
	called := false
	app := NewApp("1.0.0", []string{"mod", "sync"})
	app.services.Sync = func() error {
		called = true
		return nil
	}

	if err := app.runModSyncSubcommand(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected sync service to be called")
	}
}

func TestModInitSubcommandUsesInitProjectService(t *testing.T) {
	called := false
	app := NewApp("1.0.0", []string{"mod", "init"})
	app.services.InitProject = func() error {
		called = true
		return nil
	}

	if err := app.runModInitSubcommand(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected init service to be called")
	}
}

func TestApp_Run_ModInitAndDepsLifecycle(t *testing.T) {
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	app := NewApp("1.0.0", []string{"mod", "init"})
	if err := app.Run(); err != nil {
		t.Fatalf("mod init failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmp, project.ModFileName)); err != nil {
		t.Fatalf("expected %s to exist: %v", project.ModFileName, err)
	}

	app = NewApp("1.0.0", []string{"deps", "add", "../sample-lib"})
	if err := app.Run(); err != nil {
		t.Fatalf("deps add failed: %v", err)
	}

	mod, err := project.LoadModFile(project.ModPath(tmp))
	if err != nil {
		t.Fatalf("load mod: %v", err)
	}
	if len(mod.Deps) != 1 || mod.Deps[0] != "../sample-lib" {
		t.Fatalf("unexpected deps after add: %v", mod.Deps)
	}

	app = NewApp("1.0.0", []string{"deps", "remove", "../sample-lib"})
	if err := app.Run(); err != nil {
		t.Fatalf("deps remove failed: %v", err)
	}

	mod, err = project.LoadModFile(project.ModPath(tmp))
	if err != nil {
		t.Fatalf("load mod: %v", err)
	}
	if len(mod.Deps) != 0 {
		t.Fatalf("unexpected deps after remove: %v", mod.Deps)
	}

	app = NewApp("1.0.0", []string{"mod", "sync"})
	if err := app.Run(); err != nil {
		t.Fatalf("mod sync failed: %v", err)
	}

	app = NewApp("1.0.0", []string{"deps", "outdated"})
	if err := app.Run(); err != nil {
		t.Fatalf("deps outdated failed: %v", err)
	}

	app = NewApp("1.0.0", []string{"deps", "update", "--all"})
	if err := app.Run(); err != nil {
		t.Fatalf("deps update --all failed: %v", err)
	}

	app = NewApp("1.0.0", []string{"deps", "audit"})
	if err := app.Run(); err != nil {
		t.Fatalf("deps audit failed: %v", err)
	}

}

func TestApp_Run_DepsAuditFindingsReturnError(t *testing.T) {
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	mod := &project.ModFile{Module: "example/project", Deps: []string{}}
	if err := project.WriteModFile(project.ModPath(tmp), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	prepared, err := (generation.Service{}).PrepareSync(tmp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}

	app := NewApp("1.0.0", []string{"deps", "audit"})
	app.services.Deps = &toolchaindeps.Service{
		ResolveProjectRoot: func() (string, bool, error) { return tmp, true, nil },
		Auditor: toolchaindeps.Auditor{AuditLock: func(string, string, *project.ModFile, *project.LockFile) ([]toolchaindeps.DependencyAuditFinding, error) {
			return []toolchaindeps.DependencyAuditFinding{{Severity: "high", Code: "blocked"}}, nil
		}},
	}
	if err := app.Run(); err == nil {
		t.Fatalf("deps audit should fail when findings exist")
	}
}

func TestApp_Run_DepsAuditMediumFindingsDoNotFail(t *testing.T) {
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	mod := &project.ModFile{Module: "example/project", Deps: []string{}}
	if err := project.WriteModFile(project.ModPath(tmp), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	prepared, err := (generation.Service{}).PrepareSync(tmp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}

	app := NewApp("1.0.0", []string{"deps", "audit"})
	app.services.Deps = &toolchaindeps.Service{
		ResolveProjectRoot: func() (string, bool, error) { return tmp, true, nil },
		Auditor: toolchaindeps.Auditor{AuditLock: func(string, string, *project.ModFile, *project.LockFile) ([]toolchaindeps.DependencyAuditFinding, error) {
			return []toolchaindeps.DependencyAuditFinding{{Severity: "medium", Code: "warning"}}, nil
		}},
	}
	if err := app.Run(); err != nil {
		t.Fatalf("deps audit should pass for medium-only findings: %v", err)
	}
}

func TestRunFileWithProfilingRequiresModInit(t *testing.T) {
	tmp := t.TempDir()
	script := filepath.Join(tmp, "main.dog")
	if err := os.WriteFile(script, []byte(`(println "hi")`), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	err = RunFileWithArgsAndVMFrameProfiling(script, nil, false)
	if err == nil {
		t.Fatalf("expected error when %s is missing", project.ModFileName)
	}
	if !strings.Contains(err.Error(), "mod init") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFileWithProfilingRequiresLockSync(t *testing.T) {
	tmp := t.TempDir()
	script := filepath.Join(tmp, "main.dog")
	if err := os.WriteFile(script, []byte(`(println "hi")`), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := project.WriteModFile(project.ModPath(tmp), &project.ModFile{Deps: []string{}}); err != nil {
		t.Fatalf("write mod: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	err = RunFileWithArgsAndVMFrameProfiling(script, nil, false)
	if err == nil {
		t.Fatalf("expected error when %s is missing", project.LockFileName)
	}
	if !strings.Contains(err.Error(), "mod sync") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApp_Run_PublishAndAddPackageDependency(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "doggo.registry")

	publisherRoot := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)

	if err := os.Chdir(publisherRoot); err != nil {
		t.Fatalf("chdir publisher root: %v", err)
	}
	if err := project.WriteModFile(project.ModPath(publisherRoot), &project.ModFile{
		Module:  "github.com/acme/math",
		Name:    "acme/math",
		Version: "1.2.3",
		License: "MIT",
		Compat:  ">=0.1.0",
		Deps:    []string{},
	}); err != nil {
		t.Fatalf("write publisher mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(publisherRoot, "math.dog"), []byte("(ns acme.math)\n"), 0644); err != nil {
		t.Fatalf("write publisher source: %v", err)
	}

	app := NewApp("1.0.0", []string{
		"publish",
		"--registry", registryPath,
		"--source", "https://github.com/acme/math.git@v1.2.3",
	})
	if err := app.Run(); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	consumerRoot := t.TempDir()
	if err := os.Chdir(consumerRoot); err != nil {
		t.Fatalf("chdir consumer root: %v", err)
	}
	if err := os.Setenv("DOGGO_REGISTRY", registryPath); err != nil {
		t.Fatalf("set DOGGO_REGISTRY: %v", err)
	}
	defer os.Unsetenv("DOGGO_REGISTRY")

	app = NewApp("1.0.0", []string{"mod", "init"})
	if err := app.Run(); err != nil {
		t.Fatalf("mod init failed: %v", err)
	}

	app = NewApp("1.0.0", []string{"deps", "add", "pkg:acme/math@1.2.3"})
	if err := app.Run(); err != nil {
		t.Fatalf("deps add package failed: %v", err)
	}

	mod, err := project.LoadModFile(project.ModPath(consumerRoot))
	if err != nil {
		t.Fatalf("load consumer mod: %v", err)
	}
	if len(mod.Deps) != 1 || mod.Deps[0] != "https://github.com/acme/math.git@v1.2.3" {
		t.Fatalf("unexpected consumer deps: %+v", mod.Deps)
	}
}
