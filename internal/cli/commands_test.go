package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	toolchaindeps "doggo/internal/toolchain/deps"
	"doggo/internal/toolchain/generation"
)

func TestBuildCommandSyncAuditBlockerDoesNotRewriteLockFileViaDefaultServices(t *testing.T) {
	rootPath := t.TempDir()
	mod := &project.ModFile{Module: "example.com/demo", Deps: []string{}}
	if err := project.WriteModFile(project.ModPath(rootPath), mod); err != nil {
		t.Fatalf("write mod: %v", err)
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		Generation:          strings.Repeat("a", 64),
		ModLockInputsSHA256: "stale-mod-hash",
		Deps:                []project.LockDep{},
		Scan:                project.LockScan{},
	}
	lockPath := project.LockPath(rootPath)
	if err := project.WriteLockFile(lockPath, lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock before: %v", err)
	}

	origResolveProjectRoot := defaultResolveProjectRoot
	origGenerations := defaultGenerationService
	origDepsLoadModFile := defaultDepsLoadModFile
	origDepsAuditResolvedLock := defaultDepsAuditResolvedLock
	t.Cleanup(func() {
		defaultResolveProjectRoot = origResolveProjectRoot
		defaultGenerationService = origGenerations
		defaultDepsLoadModFile = origDepsLoadModFile
		defaultDepsAuditResolvedLock = origDepsAuditResolvedLock
	})

	defaultResolveProjectRoot = func() (string, bool, error) {
		return rootPath, true, nil
	}
	prepared, err := (generation.Service{}).PrepareSync(rootPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defaultGenerationService = generationService{
		PrepareSync: func(string, []string) (*generation.PreparedGeneration, error) { return prepared, nil },
	}
	defaultDepsLoadModFile = func(path string) (*project.ModFile, error) {
		if path != project.ModPath(rootPath) {
			t.Fatalf("mod path = %q", path)
		}
		return mod, nil
	}
	defaultDepsAuditResolvedLock = func(contentRoot, sourceRoot string, modFile *project.ModFile, lock *project.LockFile) ([]toolchaindeps.DependencyAuditFinding, error) {
		if contentRoot != prepared.StageRoot() || sourceRoot != rootPath {
			t.Fatalf("audit roots = %q %q", contentRoot, sourceRoot)
		}
		if modFile == nil || modFile.Module != mod.Module {
			t.Fatalf("unexpected mod file: %+v", modFile)
		}
		if lock == nil {
			t.Fatal("expected prepared lock")
		}
		return []toolchaindeps.DependencyAuditFinding{
			{Source: "demo", Severity: "high", Code: "DOG-001", Message: "blocked"},
		}, nil
	}
	app := NewApp("dev", []string{"build", "--sync"})
	err = app.runBuildCommand()
	if err == nil || !strings.Contains(err.Error(), "dependency audit") {
		t.Fatalf("unexpected error: %v", err)
	}

	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("lock file changed under audit blocker\nbefore:\n%s\nafter:\n%s", string(before), string(after))
	}
}

func TestDefaultBuildServicesUseToolchainDepsAuditPreparedLock(t *testing.T) {
	rootPath := t.TempDir()
	if err := project.WriteModFile(project.ModPath(rootPath), &project.ModFile{Module: "example.com/demo"}); err != nil {
		t.Fatal(err)
	}

	calledAuditResolvedLock := false
	origResolveProjectRoot := defaultResolveProjectRoot
	origGenerations := defaultGenerationService
	origDepsAuditResolvedLock := defaultDepsAuditResolvedLock
	t.Cleanup(func() {
		defaultResolveProjectRoot = origResolveProjectRoot
		defaultGenerationService = origGenerations
		defaultDepsAuditResolvedLock = origDepsAuditResolvedLock
	})

	defaultResolveProjectRoot = func() (string, bool, error) { return rootPath, true, nil }
	prepared, err := (generation.Service{}).PrepareSync(rootPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defaultGenerationService = generationService{
		PrepareSync: func(string, []string) (*generation.PreparedGeneration, error) { return prepared, nil },
	}
	defaultDepsAuditResolvedLock = func(contentRoot, sourceRoot string, mod *project.ModFile, lock *project.LockFile) ([]toolchaindeps.DependencyAuditFinding, error) {
		calledAuditResolvedLock = true
		if contentRoot != prepared.StageRoot() || sourceRoot != rootPath {
			t.Fatalf("audit roots = %q %q", contentRoot, sourceRoot)
		}
		if mod == nil || mod.Module != "example.com/demo" {
			t.Fatalf("unexpected mod: %+v", mod)
		}
		if lock == nil {
			t.Fatal("expected prepared lock")
		}
		if err := os.Remove(filepath.Join(contentRoot, "toolchain", "cmd", "doggo", "main.go")); err != nil {
			t.Fatal(err)
		}
		return nil, nil
	}

	app := NewApp("dev", []string{"build", "--sync"})
	app.services.Build = defaultServices().Build
	if err := app.runBuildCommand(); err == nil {
		t.Fatal("expected build to fail later in pipeline, after audit seam was called")
	}
	if !calledAuditResolvedLock {
		t.Fatal("expected toolchain deps audit seams to be called")
	}
}

func TestAppParsesGlobalFlagsWithoutKnowingCommands(t *testing.T) {
	app := NewApp("dev", []string{"-vm-frame-profile", "fmt"})

	args, err := app.ParseGlobalFlags()
	if err != nil {
		t.Fatal(err)
	}
	if !app.enableVMFrameProfiling {
		t.Fatal("expected VM frame profile flag to be enabled")
	}
	if len(args) != 1 || args[0] != "fmt" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestModCommandUsesDedicatedHandler(t *testing.T) {
	app := NewApp("dev", []string{"mod"})

	err := app.Dispatch()
	if err == nil {
		t.Fatal("expected mod dispatch to require a subcommand")
	}
	if !strings.Contains(err.Error(), "requires a subcommand") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDepsCommandUsesDepsService(t *testing.T) {
	called := false
	app := NewApp("dev", []string{"deps", "list"})
	app.services.Deps.ListDependencies = func(projectRoot string) ([]string, error) {
		called = true
		return []string{"github.com/acme/math"}, nil
	}

	if err := app.runDepsListSubcommand(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected deps service to be called")
	}
}

func TestApp_Run_Version(t *testing.T) {
	app := NewApp("1.2.3", []string{"version"})

	// versionコマンドはエラーを返さない
	err := app.Run()
	if err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}
}

func TestApp_Run_Help(t *testing.T) {
	testCases := []string{"help", "-h", "--help", "-?", "-help"}

	for _, cmd := range testCases {
		t.Run(cmd, func(t *testing.T) {
			app := NewApp("1.0.0", []string{cmd})

			err := app.Run()
			if err != nil {
				t.Errorf("Run() with %q error = %v, want nil", cmd, err)
			}
		})
	}
}

func TestApp_Run_UnknownCommand(t *testing.T) {
	app := NewApp("1.0.0", []string{"-unknown"})

	err := app.Run()
	if err == nil {
		t.Error("Run() with unknown command should return error")
	}
}

func TestApp_Run_ModNoSubcommand(t *testing.T) {
	app := NewApp("1.0.0", []string{"mod"})

	err := app.Run()
	if err == nil {
		t.Error("Run() with 'mod' but no subcommand should return error")
	}
}

func TestApp_Run_ModUnknownSubcommand(t *testing.T) {
	app := NewApp("1.0.0", []string{"mod", "unknown"})

	err := app.Run()
	if err == nil {
		t.Error("Run() with unknown mod subcommand should return error")
	}
}

func TestApp_Run_DepsNoSubcommand(t *testing.T) {
	app := NewApp("1.0.0", []string{"deps"})

	err := app.Run()
	if err == nil {
		t.Error("Run() with 'deps' but no subcommand should return error")
	}
}

func TestApp_Run_DepsUnknownSubcommand(t *testing.T) {
	app := NewApp("1.0.0", []string{"deps", "unknown"})

	err := app.Run()
	if err == nil {
		t.Error("Run() with unknown deps subcommand should return error")
	}
}

func TestApp_Run_ReleaseNoSubcommand(t *testing.T) {
	app := NewApp("1.0.0", []string{"release"})

	err := app.Run()
	if err == nil {
		t.Error("Run() with 'release' but no subcommand should return error")
	}
}

func TestApp_Run_ReleaseUnknownSubcommand(t *testing.T) {
	app := NewApp("1.0.0", []string{"release", "unknown"})

	err := app.Run()
	if err == nil {
		t.Error("Run() with unknown release subcommand should return error")
	}
}

func TestApp_Run_LSPUnexpectedArgument(t *testing.T) {
	app := NewApp("1.0.0", []string{"lsp", "extra"})
	if err := app.Run(); err == nil {
		t.Fatal("Run() with lsp extra arg should fail")
	}
}

func TestFormatOutdatedStatusLine_PathOutdatedShowsDigest(t *testing.T) {
	status := toolchaindeps.DependencyOutdatedStatus{
		Source:       "./deps/lib",
		Kind:         "path",
		LockedDigest: "1234567890abcdef",
		LatestDigest: "abcdef1234567890",
		Outdated:     true,
	}

	got := formatOutdatedStatusLine(status)
	if !strings.Contains(got, "locked-digest=1234567890ab") {
		t.Fatalf("expected shortened locked digest in output, got: %s", got)
	}
	if !strings.Contains(got, "latest-digest=abcdef123456") {
		t.Fatalf("expected shortened latest digest in output, got: %s", got)
	}
}

func TestFormatOutdatedStatusLine_GitOutdatedShortensRefs(t *testing.T) {
	status := toolchaindeps.DependencyOutdatedStatus{
		Source:         "github.com/org/repo",
		Kind:           "git",
		LockedRevision: "1234567890abcdef1234567890abcdef12345678",
		LatestRevision: "abcdef1234567890abcdef1234567890abcdef12",
		Outdated:       true,
	}

	got := formatOutdatedStatusLine(status)
	if !strings.Contains(got, "locked=1234567890ab") {
		t.Fatalf("expected shortened locked ref in output, got: %s", got)
	}
	if !strings.Contains(got, "latest=abcdef123456") {
		t.Fatalf("expected shortened latest ref in output, got: %s", got)
	}
}

func TestGoRevisionRenderingPreservesPseudoVersions(t *testing.T) {
	const oldVersion = "v0.0.0-20260905123456-1234567890ab"
	const newVersion = "v0.0.0-20260906123456-abcdef123456"

	outdated := formatOutdatedStatusLine(toolchaindeps.DependencyOutdatedStatus{
		Source: "example.com/mod", Kind: "go", LockedRevision: oldVersion, LatestRevision: newVersion, Outdated: true,
	})
	if want := "example.com/mod [outdated] locked=" + oldVersion + " latest=" + newVersion; outdated != want {
		t.Fatalf("outdated line = %q, want %q", outdated, want)
	}

	updated := formatDependencyUpdateLine(toolchaindeps.DependencyUpdateResult{
		Source: "example.com/mod", Kind: "go", OldRevision: oldVersion, NewRevision: newVersion, Updated: true,
	})
	if want := "example.com/mod [updated] old-revision=" + oldVersion + " new-revision=" + newVersion; updated != want {
		t.Fatalf("update line = %q, want %q", updated, want)
	}
}

func TestFormatDependencyUpdateLine_GitUpdated(t *testing.T) {
	result := toolchaindeps.DependencyUpdateResult{
		Source:      "github.com/org/repo",
		Kind:        "git",
		OldRevision: "1234567890abcdef1234567890abcdef12345678",
		NewRevision: "abcdef1234567890abcdef1234567890abcdef12",
		Updated:     true,
	}

	got := formatDependencyUpdateLine(result)
	if !strings.Contains(got, "[updated]") {
		t.Fatalf("expected updated marker in output, got: %s", got)
	}
	if !strings.Contains(got, "old-revision=1234567890ab") {
		t.Fatalf("expected shortened old ref in output, got: %s", got)
	}
	if !strings.Contains(got, "new-revision=abcdef123456") {
		t.Fatalf("expected shortened new ref in output, got: %s", got)
	}
}

func TestFormatDependencyUpdateLine_PathUpToDate(t *testing.T) {
	result := toolchaindeps.DependencyUpdateResult{
		Source:    "./deps/local",
		Kind:      "path",
		NewDigest: "1234567890abcdef",
		Updated:   false,
	}

	got := formatDependencyUpdateLine(result)
	if !strings.Contains(got, "[up-to-date]") {
		t.Fatalf("expected up-to-date marker in output, got: %s", got)
	}
	if !strings.Contains(got, "digest=1234567890ab") {
		t.Fatalf("expected shortened digest in output, got: %s", got)
	}
}

func TestFormatDependencyAuditFindingLine(t *testing.T) {
	finding := toolchaindeps.DependencyAuditFinding{
		Source:   "github.com/org/repo",
		Kind:     "git",
		Severity: "high",
		Code:     "known-vulnerability",
		Message:  "GO-2024-0001: test vulnerability",
	}

	got := formatDependencyAuditFindingLine(finding)
	if !strings.Contains(got, "github.com/org/repo [high/git/known-vulnerability]") {
		t.Fatalf("unexpected audit finding prefix: %s", got)
	}
	if !strings.Contains(got, "GO-2024-0001") {
		t.Fatalf("unexpected audit finding message: %s", got)
	}
}
