package codegen

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateInteropFileAliasesStandardPackagesWithSameName(t *testing.T) {
	for _, targets := range [][]string{{"crypto/rand"}, {"math/rand"}, {"math/rand", "crypto/rand"}} {
		t.Run(strings.Join(targets, "+"), func(t *testing.T) {
			root := t.TempDir()
			writeTestGoMod(t, root)
			output := filepath.Join(root, "internal", "interop", "generated.go")
			if err := GenerateInteropFile(root, output, targets); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range targets {
				if !strings.Contains(string(content), fmt.Sprintf("RegisterPackage(%q", path)) {
					t.Errorf("missing full-path registration for %q", path)
				}
			}
		})
	}
}

func TestGenerateInteropFileAliasesLocalModulesAndRunsExports(t *testing.T) {
	root := t.TempDir()
	writeTestGoMod(t, root)
	writeTestFiles(t, root, map[string]string{
		"go.mod":                       "module doggo\n\ngo 1.24.0\n\nrequire (\n example.invalid/first v0.0.0\n example.invalid/second v0.0.0\n)\nreplace example.invalid/first => ./modules/first\nreplace example.invalid/second => ./modules/second\n",
		"internal/interop/registry.go": "package interop\nvar Packages = map[string]map[string]interface{}{}\nfunc RegisterPackage(path string, exports map[string]interface{}) { Packages[path] = exports }\n",
		"cmd/probe/main.go": `package main
import ("fmt"; "reflect"; "doggo/internal/interop")
func main() {
 for i, path := range []string{"example.invalid/first", "example.invalid/second"} {
  if i > 0 { fmt.Print("|") }
  exports := interop.Packages[path]
  fmt.Printf("%d:%d:%d:%s", exports["Value"].(func() int)(), exports["Variable"].(int), exports["Constant"].(int), exports["Token"].(reflect.Type).PkgPath())
 }
}
`,
	})
	for i, name := range []string{"first", "second"} {
		writeTestFiles(t, root, map[string]string{
			"modules/" + name + "/go.mod":   "module example.invalid/" + name + "\n\ngo 1.24.0\n",
			"modules/" + name + "/value.go": fmt.Sprintf("package shared\nfunc Value() int { return %d }\nvar Variable = %d\nconst Constant = %d\ntype Token struct{}\n", i+1, i+11, i+21),
		})
	}
	output := filepath.Join(root, "internal", "interop", "generated.go")
	var first []byte
	for _, targets := range [][]string{
		{"example.invalid/second", "example.invalid/first"},
		{"example.invalid/first", "example.invalid/second", "example.invalid/first"},
	} {
		if err := GenerateInteropFile(root, output, targets); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = content
		} else if !bytes.Equal(first, content) {
			t.Fatalf("reversed or duplicate targets changed generated output:\n%s\n%s", first, content)
		}
	}
	cmd := exec.Command("go", "run", "-mod=readonly", "./cmd/probe")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	got, err := cmd.CombinedOutput()
	want := "1:11:21:example.invalid/first|2:12:22:example.invalid/second"
	if err != nil || string(got) != want {
		t.Fatalf("generated registry = %q, error %v, want %q", got, err, want)
	}
}

func TestGenerateInteropFileAliasesReservedNames(t *testing.T) {
	root := t.TempDir()
	writeTestGoMod(t, root)
	var targets []string
	for _, name := range []string{"reflect", "string", "nil", "RegisterPackage", "init", "context", "fmt", "os", "types", "atom", "interop"} {
		path := "doggo/pkg/" + strings.ToLower(name)
		targets = append(targets, path)
		writeTestFiles(t, root, map[string]string{
			"pkg/" + strings.ToLower(name) + "/value.go": "package " + name + "\ntype Token struct{}\nfunc Value() int { return 1 }\n",
		})
	}
	// Include reflect itself alongside a different package named reflect.
	targets = append(targets, "reflect")
	output := filepath.Join(root, "internal", "interop", "generated.go")
	if err := GenerateInteropFile(root, output, targets); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"reflect", "string", "nil", "RegisterPackage", "init"} {
		if !strings.Contains(string(content), fmt.Sprintf("%s2 %q", name, "doggo/pkg/"+strings.ToLower(name))) {
			t.Errorf("reserved name %q did not get a safe import alias:\n%s", name, content)
		}
	}
	for _, path := range targets {
		if !strings.Contains(string(content), fmt.Sprintf("RegisterPackage(%q", path)) {
			t.Errorf("missing full-path registration for %q", path)
		}
	}
}
