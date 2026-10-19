package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Compile and execute freshly generated implementations against the actual VM,
// values, and reflection converters, rather than the lightweight codegen stubs.
func TestGeneratedReifyResultContracts(t *testing.T) {
	sourceRoot, err := findProjectRoot()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("testdata/reify_results")); err != nil {
		t.Fatal(err)
	}
	goMod, err := os.ReadFile(filepath.Join(sourceRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	goMod = []byte(strings.Replace(string(goMod), "module doggo", "module doggo/reifyprobe", 1) +
		fmt.Sprintf("\nrequire doggo v0.0.0\nreplace doggo => %q\n", filepath.ToSlash(sourceRoot)))
	if err := os.WriteFile(filepath.Join(root, "go.mod"), goMod, 0644); err != nil {
		t.Fatal(err)
	}
	goSum, err := os.ReadFile(filepath.Join(sourceRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.sum"), goSum, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")
	if _, err := GenerateReifyFile(root, filepath.Join(root, "generated.go"), requiredCandidates(
		"doggo/reifyprobe/native.Checker", "doggo/reifyprobe/native.Values",
		"doggo/reifyprobe/native.Plain", "doggo/reifyprobe/native.Alias", "doggo/reifyprobe/native.Chain",
		"doggo/reifyprobe/native.AliasPair", "doggo/reifyprobe/native.NoError", "doggo/reifyprobe/native.Concrete",
		"doggo/reifyprobe/native.Variadic", "doggo/reifyprobe/native.MultiError",
		"doggo/reifyprobe/native.Arguments", "doggo/reifyprobe/native.Direct", "doggo/reifyprobe/native.Any",
		"doggo/reifyprobe/native.LocalCollision", "doggo/reifyprobe/native.ValuesCollision",
	)); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-v", ".")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated callback contracts: %v\n%s", err, out)
	}
	t.Logf("generated callback contracts:\n%s", out)
}
