package codegen

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/toolchain/reifytarget"
)

func TestGeneratorsDarwinWorkspaceRelativeTargetsWithMixedSystemPaths(t *testing.T) {
	for _, logicalRoot := range []bool{false, true} {
		t.Run(fmt.Sprintf("logicalRoot=%t", logicalRoot), func(t *testing.T) {
			physical, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(physical, "/private/var/") {
				t.Skip("temporary directory is outside the standard /var system alias")
			}
			logical := strings.TrimPrefix(physical, "/private")
			writeGeneratorWorkspaceFixture(t, physical)
			root, workspaceRoot := physical, logical
			if logicalRoot {
				root, workspaceRoot = logical, physical
			}
			workFile := filepath.Join(physical, "go.work")
			contents := fmt.Sprintf("go 1.24.0\nuse (\n%q\n%q\n)\n", filepath.Join(workspaceRoot, "app"), filepath.Join(workspaceRoot, "greeter"))
			if err := os.WriteFile(workFile, []byte(contents), 0644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOWORK", workFile)
			t.Setenv("GOPROXY", "off")
			appRoot := filepath.Join(root, "app")
			interopOutput := filepath.Join(workspaceRoot, "app", "internal", "interop", "generated.go")
			if err := GenerateInteropFile(appRoot, interopOutput, []string{"../greeter"}); err != nil {
				t.Fatalf("GenerateInteropFile with relative target: %v", err)
			}
			reifyOutput := filepath.Join(workspaceRoot, "app", "internal", "reify", "generated_reify.go")
			targets, err := GenerateReifyFile(appRoot, reifyOutput, []reifytarget.Candidate{{Source: "../greeter.Greeter", Required: true}})
			if err != nil || len(targets) != 1 {
				t.Fatalf("GenerateReifyFile with relative target = %v, %v", targets, err)
			}
		})
	}
}

func TestGeneratorCLIsDarwinWorkspaceSystemPaths(t *testing.T) {
	for _, tt := range []struct {
		name, workSpelling, useSpelling, gowork string
	}{
		{"logical workspace relative use", "logical", "relative", "explicit"},
		{"physical workspace relative use", "physical", "relative", "explicit"},
		{"physical workspace logical absolute use", "physical", "logical", "explicit"},
		{"logical workspace physical absolute use", "logical", "physical", "explicit"},
		{"auto workspace logical absolute use", "physical", "logical", "auto"},
		{"default workspace logical absolute use", "physical", "logical", ""},
		{"persisted logical workspace", "logical", "relative", "persisted"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			physical, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(physical, "/private/var/") {
				t.Skip("temporary directory is outside the standard /var system alias")
			}
			logical := strings.TrimPrefix(physical, "/private")
			appRoot := writeGeneratorWorkspaceFixture(t, physical)
			workRoot := physical
			if tt.workSpelling == "logical" {
				workRoot = logical
			}
			workFile := filepath.Join(workRoot, "go.work")
			if tt.useSpelling != "relative" {
				useRoot := physical
				if tt.useSpelling == "logical" {
					useRoot = logical
				}
				contents := fmt.Sprintf("go 1.24.0\nuse (\n%q\n%q\n)\n", filepath.Join(useRoot, "app"), filepath.Join(useRoot, "greeter"))
				if err := os.WriteFile(workFile, []byte(contents), 0644); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(workFile)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOENV", "off")
			gowork := tt.gowork
			switch gowork {
			case "explicit":
				gowork = workFile
			case "persisted":
				goenv := filepath.Join(physical, "go.env")
				if err := os.WriteFile(goenv, []byte("GOWORK="+workFile+"\n"), 0644); err != nil {
					t.Fatal(err)
				}
				t.Setenv("GOENV", goenv)
				gowork = ""
			}
			t.Setenv("GOWORK", gowork)
			t.Setenv("GOPROXY", "off")
			restore := chdirForTest(t, appRoot)
			defer restore()
			assertWorkspaceGenerators(t, appRoot)
			after, err := os.ReadFile(workFile)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("workspace changed during generation: %v", err)
			}
			if os.Getenv("GOWORK") != gowork {
				t.Fatal("generation changed the process workspace setting")
			}
		})
	}
}

func TestGenerateInteropFileDarwinMixedSystemPaths(t *testing.T) {
	for _, logicalRoot := range []bool{false, true} {
		physical, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(physical, "/private/var/") {
			t.Skip("temporary directory is outside the standard /var system alias")
		}
		logical := strings.TrimPrefix(physical, "/private")
		root, outputRoot := physical, logical
		if logicalRoot {
			root, outputRoot = logical, physical
		}
		writeTestGoMod(t, root)
		output := filepath.Join(outputRoot, "internal", "interop", "generated.go")
		if err := GenerateInteropFile(root, output, []string{"fmt"}); err != nil {
			t.Fatalf("logicalRoot=%t: %v", logicalRoot, err)
		}
		outside := t.TempDir()
		link := filepath.Join(physical, "output-link")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		if _, _, err := containedOutput(root, filepath.Join(outputRoot, "output-link", "generated.go")); err == nil {
			t.Fatal("accepted an output symlink below the system alias")
		}
		if _, err := os.Stat(filepath.Join(outside, "generated.go")); !os.IsNotExist(err) {
			t.Fatalf("outside output was touched: %v", err)
		}
	}
}
