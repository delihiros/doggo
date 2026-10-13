//go:build linux || android || darwin || ios

package project

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestGenerationSessionRunsDirectBinaryCommandWhileOpen(t *testing.T) {
	binary, err := os.ReadFile("/bin/echo")
	if err != nil {
		t.Fatal(err)
	}
	root, _, _ := writeValidGenerationBinary(t, binary)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	command, err := session.BinaryCommand("retained")
	if err != nil {
		t.Fatal(err)
	}
	wantPath := "/proc/self/fd/3"
	executablePath := ""
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		if session.binary.executable == nil {
			t.Fatal("binary command did not retain a private executable")
		}
		executablePath = filepath.Join(session.binary.executable.Path(), GenerationBinaryName())
		wantPath = executablePath
	}
	if command.Path != wantPath {
		t.Fatalf("binary command path = %q, want %q", command.Path, wantPath)
	}
	if got, want := command.Args, []string{session.binary.launchPath, "retained"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("binary command user arguments = %v, want %v", got, want)
	}
	if executablePath == "" && (len(command.ExtraFiles) != 1 || command.ExtraFiles[0] != session.binary.file) {
		t.Fatal("binary command did not use the session-retained descriptor")
	}
	if executablePath != "" && len(command.ExtraFiles) != 0 {
		t.Fatal("Darwin binary command unexpectedly inherited a descriptor")
	}
	output, err := command.Output()
	if err != nil || string(output) != "retained\n" {
		t.Fatalf("direct binary command = %q, %v", output, err)
	}
	if executablePath != "" {
		if _, err := os.Stat(executablePath); err != nil {
			t.Fatalf("private executable after child exit: %v", err)
		}
		if err := session.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(executablePath); !os.IsNotExist(err) {
			t.Fatalf("private executable after session close: %v", err)
		}
	}
}

func TestGenerationSessionBinaryCommandUsesRetainedExecutable(t *testing.T) {
	root, _, _ := writeValidGeneration(t, true)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	command, err := session.BinaryCommand()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		if session.binary.executable == nil || len(command.ExtraFiles) != 0 {
			t.Fatal("binary command did not use a private executable")
		}
		return
	}
	if len(command.ExtraFiles) != 1 || command.ExtraFiles[0] != session.binary.file {
		t.Fatal("binary command duplicated the session-retained descriptor")
	}
}

func TestGenerationSessionBinarySnapshotUsesRetainedFileAfterPathReplacement(t *testing.T) {
	root, _, lock := writeValidGeneration(t, true)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	binaryPath := filepath.Join(root, filepath.FromSlash(lock.Build.Binary))
	if err := os.Rename(binaryPath, binaryPath+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("replacement"), 0755); err != nil {
		t.Fatal(err)
	}

	snapshot, err := session.BinarySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	wantContent := []byte("doggo binary")
	wantSum := sha256.Sum256(wantContent)
	if snapshot.ProjectPath != lock.Build.Binary || snapshot.SHA256 != hex.EncodeToString(wantSum[:]) || !reflect.DeepEqual(snapshot.Content, wantContent) {
		t.Fatalf("binary snapshot = %#v", snapshot)
	}
}
