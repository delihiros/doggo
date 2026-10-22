package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"doggo/internal/project"
)

const execTestBinary = `#!/bin/sh
case "$1" in
  args) shift; printf '%s' "$*" ;;
  io) IFS= read -r input; printf 'out:%s:%s' "$DOGGO_EXEC_TEST" "$input"; printf 'err:%s' "$DOGGO_EXEC_TEST" >&2 ;;
  exit) exit "$2" ;;
  wait) printf ready > "$2"; cat >/dev/null ;;
  signal) printf ready > "$2"; exec sleep 30 ;;
esac
`

func TestExecRunsGenerationBinaryWithArguments(t *testing.T) {
	requireExecShell(t)
	root := writeCLIRuntimeGeneration(t, []byte(execTestBinary), true)
	if err := os.WriteFile(filepath.Join(root, "script.dog"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	fixed := filepath.Join(root, "bin", project.GenerationBinaryName())
	if err := os.MkdirAll(filepath.Dir(fixed), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixed, []byte("#!/bin/sh\nprintf wrong\n"), 0755); err != nil {
		t.Fatal(err)
	}

	withWorkingDirectory(t, root)
	stdout, stderr, err := runExecApp(t, []string{"exec", "--", "args", "script.dog"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "script.dog" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestExecRejectsSyncOnlyGenerationWithBuildHint(t *testing.T) {
	root := writeCLIRuntimeGeneration(t, nil, false)
	withWorkingDirectory(t, root)
	_, _, err := runExecApp(t, []string{"exec", "--", "script.dog"}, "")
	if err == nil || !strings.Contains(err.Error(), "doggo build") {
		t.Fatalf("error = %v, want doggo build hint", err)
	}
}

func TestExecValidatesGenerationDigestBeforeStarting(t *testing.T) {
	requireExecShell(t)
	root := writeCLIRuntimeGeneration(t, []byte(execTestBinary), true)
	lock, err := project.LoadLockFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, filepath.FromSlash(lock.Build.Binary))
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf corrupt\n"), 0755); err != nil {
		t.Fatal(err)
	}
	withWorkingDirectory(t, root)
	_, _, err = runExecApp(t, []string{"exec", "--", "args"}, "")
	if err == nil || !strings.Contains(err.Error(), "stale or corrupt") {
		t.Fatalf("error = %v, want digest validation failure", err)
	}
}

func TestExecStartsRetainedBinaryAfterPathReplacement(t *testing.T) {
	requireExecShell(t)
	original, err := os.ReadFile("/bin/echo")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := os.ReadFile("/usr/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	root := writeCLIRuntimeGeneration(t, original, true)
	lock, err := project.LoadLockFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, filepath.FromSlash(lock.Build.Binary))
	beforeExecChildStartForTest = func() {
		if err := os.Rename(binary, binary+".verified"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(binary, replacement, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeExecChildStartForTest = nil })

	withWorkingDirectory(t, root)
	stdout, stderr, err := runExecApp(t, []string{"exec", "--", "retained"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "retained\n" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestExecInheritsStandardIOAndEnvironment(t *testing.T) {
	requireExecShell(t)
	root := writeCLIRuntimeGeneration(t, []byte(execTestBinary), true)
	withWorkingDirectory(t, root)
	t.Setenv("DOGGO_EXEC_TEST", "inherited")
	stdout, stderr, err := runExecApp(t, []string{"exec", "--", "io"}, "stdin\n")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "out:inherited:stdin" || stderr != "err:inherited" {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestExecReturnsChildExitStatus(t *testing.T) {
	requireExecShell(t)
	root := writeCLIRuntimeGeneration(t, []byte(execTestBinary), true)
	withWorkingDirectory(t, root)
	_, _, err := runExecApp(t, []string{"exec", "--", "exit", "23"}, "")
	var exitError interface{ ExitCode() int }
	if !errors.As(err, &exitError) || exitError.ExitCode() != 23 {
		t.Fatalf("error = %T %v, want exit status 23", err, err)
	}
	var childExitError *ChildExitError
	if !errors.As(err, &childExitError) {
		t.Fatalf("error type = %T, want dedicated child exit error", err)
	}
}

func TestExecPinsGenerationWithoutBlockingWriter(t *testing.T) {
	requireExecShell(t)
	root := writeCLIRuntimeGeneration(t, []byte(execTestBinary), true)
	withWorkingDirectory(t, root)
	ready := filepath.Join(root, "ready")

	stdin, stdinWriter, err := os.Pipe()
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
		_ = stdin.Close()
		_ = stdinWriter.Close()
		_ = stdout.Close()
		_ = stderr.Close()
	})

	runErr := make(chan error, 1)
	go func() { runErr <- NewApp("test", []string{"exec", "--", "wait", ready}).Dispatch() }()
	waitForFile(t, ready)

	writer := make(chan *project.GenerationLease, 1)
	writerErr := make(chan error, 1)
	go func() {
		lease, err := project.AcquireGenerationLease(root, true)
		if err != nil {
			writerErr <- err
			return
		}
		writer <- lease
	}()
	closeWriter(t, writer, writerErr)
	if err := stdinWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-runErr; err != nil {
		t.Fatal(err)
	}
}

func TestExecClosesSessionWhenStartFails(t *testing.T) {
	requireExecShell(t)
	root := writeCLIRuntimeGeneration(t, []byte("#!/definitely/missing\n"), true)
	withWorkingDirectory(t, root)
	_, _, err := runExecApp(t, []string{"exec", "--", "anything"}, "")
	if err == nil {
		t.Fatal("expected child start failure")
	}
	lease, leaseErr := project.AcquireGenerationLease(root, true)
	if leaseErr != nil {
		t.Fatalf("session remained open after start failure: %v", leaseErr)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeCLIRuntimeGeneration(t *testing.T, binary []byte, built bool) string {
	t.Helper()
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/cli", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	writeCLIRuntimeGenerationAt(t, root, binary, built)
	return root
}

func writeCLIRuntimeGenerationAt(t *testing.T, root string, binary []byte, built bool) {
	t.Helper()
	mod, err := project.LoadModFile(project.ModPath(root))
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(root, ".doggo", "generations", "stage")
	for _, dir := range []string{"deps", "toolchain"} {
		if err := os.MkdirAll(filepath.Join(stage, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	lock := &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(mod),
		Deps:                []project.LockDep{},
		Scan:                project.LockScan{GoPackages: []string{}, ReifyTargets: []project.ReifyTarget{}},
	}
	if built {
		path := filepath.Join(stage, "bin", project.GenerationBinaryName())
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, binary, 0755); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(binary)
		lock.Build.SHA256 = hex.EncodeToString(sum[:])
	}
	generation, err := project.GenerationDigest(stage, lock)
	if err != nil {
		t.Fatal(err)
	}
	lock.Generation = generation
	if built {
		lock.Build.Binary = project.GenerationBinaryPath(generation)
	}
	if err := os.WriteFile(filepath.Join(stage, ".generation"), []byte(generation+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stage, filepath.Join(root, ".doggo", "generations", generation)); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteLockFile(project.LockPath(root), lock); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".doggo", "generation.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func runExecApp(t *testing.T, args []string, input string) (string, string, error) {
	t.Helper()
	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(stdin, input); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Seek(0, io.SeekStart); err != nil {
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
	err = NewApp("test", args).Dispatch()
	os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
	read := func(file *os.File) string {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(file)
		if err != nil {
			t.Fatal(err)
		}
		return string(content)
	}
	gotStdout, gotStderr := read(stdout), read(stderr)
	for _, file := range []*os.File{stdin, stdout, stderr} {
		if closeErr := file.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	return gotStdout, gotStderr, err
}

func withWorkingDirectory(t *testing.T, path string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func requireExecShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test generation uses a POSIX script binary")
	}
}
