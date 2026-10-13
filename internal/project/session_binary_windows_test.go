//go:build windows

package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

const generationBinaryWindowsHelperEnv = "DOGGO_GENERATION_BINARY_WINDOWS_HELPER"

func TestGenerationSessionRunsRetainedBinaryWindows(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	root, _, _ := writeValidGenerationBinary(t, binary)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	command, err := session.BinaryCommand("-test.run=^TestGenerationBinaryWindowsHelperProcess$")
	if err != nil {
		t.Fatal(err)
	}
	command.Env = append(os.Environ(), generationBinaryWindowsHelperEnv+"=1")
	output, err := command.Output()
	if err != nil || string(output) != "retained" {
		t.Fatalf("retained binary command = %q, %v", output, err)
	}
}

func TestGenerationBinaryWindowsHelperProcess(t *testing.T) {
	if os.Getenv(generationBinaryWindowsHelperEnv) != "1" {
		return
	}
	_, _ = fmt.Fprint(os.Stdout, "retained")
	os.Exit(0)
}

func TestGenerationSessionRetainedBinaryBlocksWindowsReplacement(t *testing.T) {
	root, _, lock := writeValidGeneration(t, true)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	binary := filepath.Join(root, filepath.FromSlash(lock.Build.Binary))
	command, err := session.BinaryCommand("first", "second")
	if err != nil {
		t.Fatal(err)
	}
	if command.Path != binary {
		t.Fatalf("binary command path = %q, want %q", command.Path, binary)
	}
	if got, want := command.Args, []string{binary, "first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("binary command arguments = %v, want %v", got, want)
	}
	if command.SysProcAttr != nil && len(command.SysProcAttr.AdditionalInheritedHandles) != 0 {
		t.Fatal("binary command retained an additional inherited handle")
	}
	moved := binary + ".moved"
	if err := os.WriteFile(binary, []byte("replacement"), 0755); err == nil {
		t.Fatal("write succeeded while retained binary was open")
	}
	if err := os.Rename(binary, moved); err == nil {
		t.Fatal("rename succeeded while retained binary was open")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("replacement"), 0755); err != nil {
		t.Fatalf("write after close: %v", err)
	}
	if err := os.Rename(binary, moved); err != nil {
		t.Fatalf("rename after close: %v", err)
	}
}

func TestOpenGenerationBinaryRejectsIntermediateJunction(t *testing.T) {
	generationPath := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, GenerationBinaryName()), []byte("redirected"), 0755); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(generationPath, "bin")
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, target).CombinedOutput(); err != nil {
		t.Fatalf("create directory junction: %v: %s", err, output)
	}
	generation, err := os.OpenRoot(generationPath)
	if err != nil {
		t.Fatal(err)
	}
	defer generation.Close()
	file, err := openGenerationBinary(generation, filepath.Join("bin", GenerationBinaryName()))
	if err == nil {
		_ = file.Close()
		t.Fatal("openGenerationBinary followed an intermediate junction")
	}
}
