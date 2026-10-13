package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
)

func TestExecRejectsGenerationWritersBeforeOpeningSession(t *testing.T) {
	opened := 0
	wantOpenError := errors.New("generation session opened")
	oldOpen := defaultOpenGenerationSession
	defaultOpenGenerationSession = func(string) (*project.GenerationSession, error) {
		opened++
		return nil, wantOpenError
	}
	t.Cleanup(func() { defaultOpenGenerationSession = oldOpen })

	for _, args := range [][]string{
		{"deps", "add", "./a"},
		{"deps", "remove", "./a"},
		{"deps", "update"},
		{"deps", "update", "./a"},
		{"deps", "update", "--all"},
		{"mod", "init"},
		{"mod", "sync"},
		{"mod", "sync", "--help"}, // This handler ignores trailing arguments.
		{"build"},
		{"build", "--sync"},
		{"build", "-sync=false"},
		{"build", "ignored-target", "--help"}, // Flag parsing stops at the positional.
		{"-vm-frame-profile", "deps", "add", "./a"},
		{"--vm-frame-profile=false", "mod", "sync"},
		{"--cpu-profile", "cpu.prof", "deps", "remove", "./a"},
		{"-mem-profile=mem.prof", "deps", "update", "--all"},
		{"--", "build", "--sync"},
		{"exec", "--", "deps", "add", "./a"},
		{"exec", "--", "exec", "--", "build"},
		{"--vm-frame-profile", "exec", "--", "-cpu-profile=cpu.prof", "mod", "sync"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			opened = 0
			err := NewApp("test", append([]string{"exec", "--"}, args...)).Run()
			if err == nil || !strings.Contains(err.Error(), "generation-writing command") || !strings.Contains(err.Error(), "directly") {
				t.Fatalf("error = %v, want generation writer rejection with direct invocation hint", err)
			}
			if opened != 0 {
				t.Fatalf("opened %d generation sessions before rejecting writer", opened)
			}
		})
	}
}

func TestExecAllowsReadOnlyCommandsAndScriptPathsToOpenSession(t *testing.T) {
	want := errors.New("allowed command reached generation validation")
	oldOpen := defaultOpenGenerationSession
	defaultOpenGenerationSession = func(string) (*project.GenerationSession, error) { return nil, want }
	t.Cleanup(func() { defaultOpenGenerationSession = oldOpen })

	for _, args := range [][]string{
		{"deps", "list"},
		{"deps", "outdated"},
		{"deps", "audit"},
		{"version"},
		{"help"},
		{"-h"},
		{"--help"},
		{"-?"},
		{"-help"},
		{"repl"},
		{"test"},
		{"lsp"},
		{"fmt", "--check"},
		{"fmt"},
		{"publish"},
		{"release", "bundle"},
		{"build", "--help"},
		{"build", "--invalid"},
		{"script.dog", "deps", "add", "./a"},
		{"build.dog"},
		{"./build"},
		{"gc"}, // There is no gc command; dispatch treats this as a script path.
		{"deps", "--help", "add", "./a"},
		{"mod", "unknown"},
		{"exec", "--", "deps", "list"},
		{"exec", "--", "exec", "--", "script.dog"},
		{"exec", "build"},                       // Invalid nested exec cannot dispatch a writer.
		{"-cpu-profile", "build", "script.dog"}, // The writer name is an option value.
		{"--cpu-profile=cpu.prof", "version"},
		{"-vm-frame-profile=false", "./build"},
		{"--invalid", "build"}, // Invalid global flags cannot reach dispatch.
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stderr := captureCLIStream(t, &os.Stderr, func() {
				err := NewApp("test", append([]string{"exec", "--"}, args...)).Run()
				if !errors.Is(err, want) {
					t.Fatalf("error = %v, want read-only/script session validation", err)
				}
			})
			if stderr != "" {
				t.Fatalf("classification emitted child diagnostics before execution: %q", stderr)
			}
		})
	}
}

func TestExecWriterRejectionPrecedesInvalidGeneration(t *testing.T) {
	root := writeCLIRuntimeGeneration(t, []byte("invalid binary"), true)
	if err := os.WriteFile(project.LockPath(root), []byte("invalid lock"), 0644); err != nil {
		t.Fatal(err)
	}
	withWorkingDirectory(t, root)
	err := NewApp("test", []string{"exec", "--", "deps", "add", "./a"}).Run()
	if err == nil || !strings.Contains(err.Error(), "generation-writing command") {
		t.Fatalf("writer rejection = %v, want guard before generation validation", err)
	}
}

func TestExecReadOnlyCommandsStillValidateGeneration(t *testing.T) {
	root := writeCLIRuntimeGeneration(t, []byte("original binary"), true)
	lock, err := project.LoadLockFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(lock.Build.Binary)), []byte("corrupt binary"), 0755); err != nil {
		t.Fatal(err)
	}
	withWorkingDirectory(t, root)
	for _, args := range [][]string{{"version"}, {"deps", "list"}, {"script.dog"}, {"exec", "--", "version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := NewApp("test", append([]string{"exec", "--"}, args...)).Run()
			if err == nil || !strings.Contains(err.Error(), "stale or corrupt") {
				t.Fatalf("error = %v, want unchanged generation integrity validation", err)
			}
		})
	}
}
