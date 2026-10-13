package codegen

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"doggo/internal/toolchain/reifytarget"
)

func TestGenerateInteropFileLoadsTargetPackagesTogether(t *testing.T) {
	moduleRoot := t.TempDir()
	writeTestGoMod(t, moduleRoot)
	logPath := logPackageDriverRequests(t)
	output := filepath.Join(moduleRoot, "internal", "interop", "generated.go")
	if err := GenerateInteropFile(moduleRoot, output, []string{"bytes", "fmt"}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, export := range []string{`"NewBuffer":\s+bytes\.NewBuffer`, `"Println":\s+fmt\.Println`} {
		if !regexp.MustCompile(export).Match(content) {
			t.Fatalf("generated exports omit %s", export)
		}
	}
	requests := readPackageDriverRequests(t, logPath)
	if !reflect.DeepEqual(requests, [][]string{{"bytes", "fmt"}, {"./internal/interop"}}) {
		t.Fatalf("package driver requests = %v, want batched targets followed by generated-package validation", requests)
	}
}

func TestGenerateReifyFileSharesPackageLoadingAndCombinedValidation(t *testing.T) {
	moduleRoot := t.TempDir()
	writeTestGoMod(t, moduleRoot)
	logPath := logPackageDriverRequests(t)
	output := filepath.Join(moduleRoot, "internal", "reify", "generated_reify.go")
	targets, err := GenerateReifyFile(moduleRoot, output, requiredCandidates("fmt.Stringer", "net/http.Handler"))
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].Source != "fmt.Stringer" || targets[1].Source != "net/http.Handler" {
		t.Fatalf("accepted targets = %v, want both required interfaces", targets)
	}
	requests := readPackageDriverRequests(t, logPath)
	if !reflect.DeepEqual(requests, [][]string{{"fmt", "net/http"}, {"./internal/reify"}}) {
		t.Fatalf("package driver requests = %v, want shared target loading and one combined validation", requests)
	}
}

func TestGenerateReifyFileRequiredAliasCannotHideForbiddenInternalSource(t *testing.T) {
	moduleRoot := t.TempDir()
	writeTestGoMod(t, moduleRoot)
	writeTestFiles(t, moduleRoot, map[string]string{
		"owner/internal/secret/secret.go": "package secret\ntype Interface interface { Public() }\n",
		"owner/aaa/aaa.go":                "package aaa\nimport \"doggo/owner/internal/secret\"\ntype Public = secret.Interface\n",
	})
	output := filepath.Join(moduleRoot, "internal", "reify", "generated_reify.go")
	_, err := GenerateReifyFile(moduleRoot, output, requiredCandidates("doggo/owner/aaa.Public", "doggo/owner/internal/secret.Interface"))
	if err == nil || !strings.Contains(err.Error(), "doggo/owner/internal/secret.Interface") {
		t.Fatalf("required internal source error = %v, want its compiler diagnostic even with a public alias", err)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("invalid required source was published: %v", err)
	}
}

func TestGenerateReifyFilePreservesRelativePackageCandidates(t *testing.T) {
	for _, required := range []bool{true, false} {
		name := "optional"
		if required {
			name = "required"
		}
		t.Run(name, func(t *testing.T) {
			moduleRoot := t.TempDir()
			writeTestGoMod(t, moduleRoot)
			writeTestFiles(t, moduleRoot, map[string]string{
				"facade/facade.go": "package facade\ntype Public interface { Do() }\n",
			})
			output := filepath.Join(moduleRoot, "internal", "reify", "generated_reify.go")
			targets, err := GenerateReifyFile(moduleRoot, output, []reifytarget.Candidate{{Source: "./facade.Public", Required: required}})
			if err != nil {
				t.Fatal(err)
			}
			if len(targets) != 1 || targets[0].Source != "doggo/facade.Public" || targets[0].Runtime != "doggo/facade.Public" {
				t.Fatalf("relative package targets = %v, want the resolved canonical interface", targets)
			}
			content, err := os.ReadFile(output)
			if err != nil || !strings.Contains(string(content), "type FacadePublicImpl struct") {
				t.Fatalf("relative package implementation missing: %s, %v", content, err)
			}
		})
	}
}

func logPackageDriverRequests(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "driver.go")
	// This observes the external-driver boundary and delegates actual package
	// loading to Go, including type checking and export data.
	const program = `package main
import ("encoding/json"; "fmt"; "io"; "os")
func main() {
 io.Copy(io.Discard, os.Stdin)
 file, err := os.OpenFile(os.Getenv("DOGGO_DRIVER_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
 if err != nil { panic(err) }
 if err := json.NewEncoder(file).Encode(os.Args[1:]); err != nil { panic(err) }
 if err := file.Close(); err != nil { panic(err) }
 fmt.Print("{\"NotHandled\":true}")
}
`
	if err := os.WriteFile(source, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "package-driver")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	command := exec.Command("go", "build", "-o", executable, source)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build package driver: %v\n%s", err, output)
	}
	logPath := filepath.Join(dir, "requests.jsonl")
	t.Setenv("DOGGO_DRIVER_LOG", logPath)
	t.Setenv("GOPACKAGESDRIVER", executable)
	return logPath
}

func readPackageDriverRequests(t *testing.T, path string) [][]string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var requests [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		var request []string
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
	}
	return requests
}
