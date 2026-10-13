package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"testing"

	"doggo/internal/cli"
)

func TestChildExitCodeOnlyRecognizesDoggoExecExit(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestChildExitCodeHelper$")
	command.Env = append(os.Environ(), "DOGGO_CHILD_EXIT_CODE_HELPER=1")
	unrelated := fmt.Errorf("external tool failed: %w", command.Run())
	if code, ok := childExitCode(unrelated); ok {
		t.Fatalf("unrelated exec.ExitError forwarded as %d", code)
	}
	if code, ok := childExitCode(&cli.ChildExitError{}); !ok || code != 1 {
		t.Fatalf("dedicated child exit = (%d, %v), want (1, true)", code, ok)
	}
}

func TestChildExitCodeHelper(t *testing.T) {
	if os.Getenv("DOGGO_CHILD_EXIT_CODE_HELPER") == "1" {
		os.Exit(23)
	}
}

func TestResolveVersionPrefersLinkerValue(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}
	if got := resolveVersion("v2.0.0-rc.1", info, true); got != "v2.0.0-rc.1" {
		t.Fatalf("resolveVersion() = %q, want linker version", got)
	}
}

func TestResolveVersionUsesModuleVersionForGoInstall(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}
	if got := resolveVersion("", info, true); got != "v1.2.3" {
		t.Fatalf("resolveVersion() = %q, want module version", got)
	}
}

func TestResolveVersionReportsDevelopmentBuild(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	if got := resolveVersion("", info, true); got != "dev" {
		t.Fatalf("resolveVersion() = %q, want dev", got)
	}
}
