package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"doggo/internal/project"
	"doggo/internal/toolchain/generation"
	"doggo/internal/vm"
)

func TestFileExecutionUsesGenerationSourcesFromSession(t *testing.T) {
	root, script := writeGenerationSourceCLIProject(t)
	withWorkingDirectory(t, root)

	if err := RunFileWithArgsAndVMFrameProfiling(script, nil, false); err != nil {
		t.Fatalf("run file with generation dependency: %v", err)
	}
}

func TestREPLUsesGenerationSourcesFromSession(t *testing.T) {
	root, _ := writeGenerationSourceCLIProject(t)
	withWorkingDirectory(t, root)
	old := runREPLWithRunner
	runREPLWithRunner = func(runner *vm.Runner) error {
		if _, err := runner.Run(`(require '[dep.core :as dep])`); err != nil {
			return err
		}
		got, err := runner.Run(`dep/value`)
		if err != nil {
			return err
		}
		if got.String() != "42" {
			t.Fatalf("dependency value = %s, want 42", got.String())
		}
		return nil
	}
	t.Cleanup(func() { runREPLWithRunner = old })

	if err := NewApp("test", []string{"repl"}).Dispatch(); err != nil {
		t.Fatalf("run REPL with generation dependency: %v", err)
	}
}

func TestFileExecutionDoesNotBlockGenerationWriter(t *testing.T) {
	root := t.TempDir()
	writeModAndLock(t, root)
	ready := filepath.Join(root, "ready")
	script := filepath.Join(root, "script.dog")
	source := "(require '[doggo.io :as io])\n" +
		"(require '[doggo.time :as time])\n" +
		"(io/spit " + strconv.Quote(ready) + " \"ready\")\n" +
		"(time/sleep 300)\n"
	if err := os.WriteFile(script, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	runErr := make(chan error, 1)
	go func() { runErr <- RunFileWithArgsAndVMFrameProfiling(script, nil, false) }()
	waitForFile(t, ready)
	writer, writerErr := startExclusiveWriter(root)
	closeWriter(t, writer, writerErr)
	if err := <-runErr; err != nil {
		t.Fatal(err)
	}
}

func TestFileExecutionClosesGenerationSessionOnLoadError(t *testing.T) {
	root := t.TempDir()
	writeModAndLock(t, root)
	script := filepath.Join(root, "broken.dog")
	if err := os.WriteFile(script, []byte("("), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RunFileWithArgsAndVMFrameProfiling(script, nil, false); err == nil {
		t.Fatal("expected load error")
	}
	lease, err := project.AcquireGenerationLease(root, true)
	if err != nil {
		t.Fatalf("session remained open after load error: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestREPLAllowsGenerationWriterAndClosesOnError(t *testing.T) {
	root := t.TempDir()
	writeModAndLock(t, root)
	withWorkingDirectory(t, root)
	want := errors.New("repl stopped")
	old := runREPLWithRunner
	var writer <-chan *project.GenerationLease
	var writerErr <-chan error
	runREPLWithRunner = func(*vm.Runner) error {
		writer, writerErr = startExclusiveWriter(root)
		closeWriter(t, writer, writerErr)
		return want
	}
	t.Cleanup(func() { runREPLWithRunner = old })

	if err := NewApp("test", []string{"repl"}).Dispatch(); !errors.Is(err, want) {
		t.Fatalf("REPL error = %v", err)
	}
}

func TestLSPAllowsGenerationWriterBeforeServerExit(t *testing.T) {
	root := t.TempDir()
	writeModAndLock(t, root)
	withWorkingDirectory(t, t.TempDir())
	input, output, runErr := startLSPApp(t)
	rootURIPath := filepath.ToSlash(root)
	if !strings.HasPrefix(rootURIPath, "/") {
		rootURIPath = "/" + rootURIPath
	}
	writeLSPMessage(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"rootUri": (&url.URL{Scheme: "file", Path: rootURIPath}).String(),
		},
	})
	waitForLSPOutput(t, output, `"capabilities"`, runErr)

	writer, writerErr := startExclusiveWriter(root)
	closeWriter(t, writer, writerErr)
	writeLSPMessage(t, input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	if err := <-runErr; err != nil {
		t.Fatal(err)
	}
}

func TestLSPRejectsDuplicateInitializeWithoutBlockingWriters(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeModAndLock(t, rootA)
	writeModAndLock(t, rootB)
	withWorkingDirectory(t, t.TempDir())
	input, output, runErr := startLSPApp(t)
	writeLSPMessage(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{"rootPath": rootA},
	})
	waitForLSPOutput(t, output, `"capabilities"`, runErr)

	writerA, writerAErr := startExclusiveWriter(rootA)
	closeWriter(t, writerA, writerAErr)
	writeLSPMessage(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "initialize",
		"params":  map[string]any{"rootPath": rootB},
	})
	waitForLSPOutput(t, output, `"id":2`, runErr)
	content, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"error"`) {
		t.Fatalf("duplicate initialize response = %s, want error", content)
	}

	writerB, writerBErr := startExclusiveWriter(rootB)
	closeWriter(t, writerB, writerBErr)
	writerA, writerAErr = startExclusiveWriter(rootA)
	closeWriter(t, writerA, writerAErr)
	writeLSPMessage(t, input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	if err := <-runErr; err != nil {
		t.Fatal(err)
	}
}

func TestLSPReleasesInitializedRootWhenSessionSetupFails(t *testing.T) {
	root := t.TempDir()
	writeModAndLock(t, root)
	lock, err := project.LoadLockFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".doggo", "generations", lock.Generation, ".generation"), []byte("corrupt\n"), 0644); err != nil {
		t.Fatal(err)
	}
	withWorkingDirectory(t, t.TempDir())
	input, output, runErr := startLSPApp(t)
	writeLSPMessage(t, input, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{"rootPath": root},
	})
	waitForLSPOutput(t, output, `"error"`, runErr)

	writer, writerErr := startExclusiveWriter(root)
	closeWriter(t, writer, writerErr)
	writeLSPMessage(t, input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	if err := <-runErr; err != nil {
		t.Fatal(err)
	}
}

func startLSPApp(t *testing.T) (*os.File, *os.File, <-chan error) {
	t.Helper()
	stdin, input, err := os.Pipe()
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
		for _, file := range []*os.File{stdin, input, stdout, stderr} {
			_ = file.Close()
		}
	})
	runErr := make(chan error, 1)
	go func() { runErr <- NewApp("test", []string{"lsp"}).Dispatch() }()
	return input, stdout, runErr
}

func writeLSPMessage(t *testing.T, output io.Writer, message map[string]any) {
	t.Helper()
	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(output, "Content-Length: %d\r\n\r\n%s", len(payload), payload); err != nil {
		t.Fatal(err)
	}
}

func waitForLSPOutput(t *testing.T, output *os.File, marker string, runErr <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(output.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), marker) {
			return
		}
		select {
		case err := <-runErr:
			t.Fatalf("LSP exited before response: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for LSP output %s", marker)
}

func startExclusiveWriter(root string) (<-chan *project.GenerationLease, <-chan error) {
	writer := make(chan *project.GenerationLease, 1)
	errs := make(chan error, 1)
	go func() {
		lease, err := project.AcquireGenerationLease(root, true)
		if err != nil {
			errs <- err
			return
		}
		writer <- lease
	}()
	return writer, errs
}

func closeWriter(t *testing.T, writer <-chan *project.GenerationLease, errs <-chan error) {
	t.Helper()
	select {
	case lease := <-writer:
		if err := lease.Close(); err != nil {
			t.Error(err)
		}
	case err := <-errs:
		t.Error(err)
	case <-time.After(5 * time.Second):
		t.Error("exclusive writer remained blocked after runtime returned")
	}
}

func writeGenerationSourceCLIProject(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{"./dep"}}); err != nil {
		t.Fatal(err)
	}
	dependency := filepath.Join(root, "dep")
	for name, content := range map[string]string{
		project.ModFileName: `{:module "example/dep" :deps []}` + "\n",
		"dep/core.dog":      "(ns dep.core)\n(def value 42)\n",
		"dep0.txt":          "ordering sentinel\n",
	} {
		filename := filepath.Join(dependency, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatalf("generation.Service.PrepareSync: %v", err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatalf("PreparedGeneration.Commit: %v", err)
	}
	script := filepath.Join(root, "main.dog")
	if err := os.WriteFile(script, []byte("(require '[dep.core :as dep])\ndep/value\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return root, script
}
