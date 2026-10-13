//go:build unix

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestExecForwardsTerminationAndReportsSignalStatus(t *testing.T) {
	root := writeCLIRuntimeGeneration(t, []byte(execTestBinary), true)
	withWorkingDirectory(t, root)
	ready := filepath.Join(root, "ready")

	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = stdin, stdout, stderr
	t.Cleanup(func() {
		os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
		for _, file := range []*os.File{stdin, stdout, stderr} {
			_ = file.Close()
		}
	})

	runErr := make(chan error, 1)
	go func() { runErr <- NewApp("test", []string{"exec", "--", "signal", ready}).Dispatch() }()
	waitForFile(t, ready)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-runErr:
		var childExit *ChildExitError
		if !errors.As(err, &childExit) {
			t.Fatalf("error = %T %v, want child exit", err, err)
		}
		if got, want := childExit.ExitCode(), 128+int(syscall.SIGTERM); got != want {
			t.Fatalf("signal exit status = %d, want %d", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exec did not return after forwarded termination")
	}
}
