package scan

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/testutil"
	"doggo/internal/toolchain/reifytarget"
)

func TestScanServiceDiscoversRootlessPackages(t *testing.T) {
	for _, rootSource := range []string{"", "//go:build doggo_unset_test_tag\n\npackage shared\n", "package shared\n"} {
		name := "no-files"
		if strings.HasPrefix(rootSource, "//") {
			name = "build-excluded"
		} else if rootSource != "" {
			name = "test-only"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			moduleRoot := filepath.Join(root, "deps", "shared")
			writeScanFile(t, filepath.Join(moduleRoot, "go.mod"), "module example.com/shared\n\ngo 1.24.0\n")
			if rootSource != "" {
				file := "excluded.go"
				if name == "test-only" {
					file = "root_test.go"
				}
				writeScanFile(t, filepath.Join(moduleRoot, file), rootSource)
			}
			writeScanFile(t, filepath.Join(moduleRoot, "sub", "sub.go"), "package sub\nfunc Hello() string { return \"hello\" }\ntype Handler interface { Handle() }\n")
			// Excluded packages must never be type-loaded or become interface roots.
			writeScanFile(t, filepath.Join(moduleRoot, "internal", "bad", "bad.go"), "package bad\ntype Hidden interface { Hidden() }\nvar Broken = missing\n")
			writeScanFile(t, filepath.Join(moduleRoot, "cmd", "tool", "main.go"), "package main\ntype Hidden interface { Hidden() }\nvar Broken = missing\nfunc main() {}\n")
			writeScanFile(t, filepath.Join(moduleRoot, "nested", "go.mod"), "module example.com/nested\n\ngo 1.24.0\n")
			for _, path := range []string{"nested", "testdata", "vendor", ".hidden", "_hidden"} {
				writeScanFile(t, filepath.Join(moduleRoot, path, "bad.go"), "package bad\nvar Broken = missing\n")
			}
			t.Setenv("GOPROXY", "off")
			targets, err := (Service{}).DeriveTargetsTree(openScanTestTree(t, root), root, []project.LockDep{{GoModule: "example.com/shared", InstallDir: "deps/shared"}})
			if err != nil {
				t.Fatal(err)
			}
			assertDiscoveredSubpackage(t, targets)
		})
	}
}

func assertDiscoveredSubpackage(t *testing.T, targets Targets) {
	t.Helper()
	var actual []string
	for _, path := range targets.GoPackages {
		if strings.HasPrefix(path, "example.com/") {
			actual = append(actual, path)
		}
	}
	if !slices.Equal(actual, []string{"example.com/shared/sub"}) {
		t.Fatalf("actual dependency packages = %v", actual)
	}
	want := reifytarget.Candidate{Source: "example.com/shared/sub.Handler"}
	if !slices.Contains(targets.ReifyCandidates, want) {
		t.Fatalf("candidates = %v, missing %+v", targets.ReifyCandidates, want)
	}
	for _, candidate := range targets.ReifyCandidates {
		if strings.HasPrefix(candidate.Source, "example.com/") && candidate != want {
			t.Fatalf("excluded candidate = %+v", candidate)
		}
	}
}

func TestScanServiceKeepsValidRootSelection(t *testing.T) {
	root := t.TempDir()
	moduleRoot := filepath.Join(root, "deps", "shared")
	writeScanFile(t, filepath.Join(moduleRoot, "go.mod"), "module example.com/shared\n\ngo 1.24.0\n")
	writeScanFile(t, filepath.Join(moduleRoot, "shared.go"), "package shared\nconst Anchor = 1\ntype Root interface { Root() }\n")
	for _, path := range []string{"public", "internal/bad", "cmd/tool"} {
		writeScanFile(t, filepath.Join(moduleRoot, filepath.FromSlash(path), "bad.go"), "package bad\nvar = broken\n")
	}
	targets, err := (Service{}).DeriveTargetsTree(openScanTestTree(t, root), root, []project.LockDep{{GoModule: "example.com/shared", InstallDir: "deps/shared"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(targets.GoPackages, "example.com/shared") || slices.Contains(targets.GoPackages, "example.com/shared/public") {
		t.Fatalf("packages = %v", targets.GoPackages)
	}
	if !slices.Contains(targets.ReifyCandidates, reifytarget.Candidate{Source: "example.com/shared.Root"}) {
		t.Fatalf("candidates = %v", targets.ReifyCandidates)
	}
}

func TestScanServiceRootlessExcludesMetadataErrorsOnlyWhenProven(t *testing.T) {
	for _, path := range []string{"internal/bad", "cmd/tool", "public", "public-blank-name", "public-type-error"} {
		t.Run(path, func(t *testing.T) {
			root := t.TempDir()
			moduleRoot := filepath.Join(root, "deps", "shared")
			writeScanFile(t, filepath.Join(moduleRoot, "go.mod"), "module example.com/shared\n\ngo 1.24.0\n")
			writeScanFile(t, filepath.Join(moduleRoot, "sub", "sub.go"), "package sub\ntype Handler interface { Handle() }\n")
			name := "bad"
			if path == "cmd/tool" {
				name = "main"
			}
			source := "package " + name + "\nimport \"\n"
			if path == "public-blank-name" {
				source = "not a Go source file\n"
			}
			if path == "public-type-error" {
				source = "package bad\nvar Broken = missing\n"
			}
			writeScanFile(t, filepath.Join(moduleRoot, filepath.FromSlash(path), "bad.go"), source)
			targets, err := (Service{}).DeriveTargetsTree(openScanTestTree(t, root), root, []project.LockDep{{GoModule: "example.com/shared", InstallDir: "deps/shared"}})
			if strings.HasPrefix(path, "public") {
				if err == nil {
					t.Fatal("malformed eligible public package accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertDiscoveredSubpackage(t, targets)
		})
	}
}

func TestScanServiceDoesNotFallbackForBrokenRoot(t *testing.T) {
	for name, source := range map[string]string{"syntax": "package shared\nvar = broken\n", "import": "package shared\nimport _ \"example.invalid/not-present\"\n", "blank-name": "not a Go source file\n", "go-mod": "package shared\n"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			moduleRoot := filepath.Join(root, "deps", "shared")
			goMod := "module example.com/shared\n\ngo 1.24.0\n"
			if name == "go-mod" {
				goMod += "invalid directive\n"
			}
			writeScanFile(t, filepath.Join(moduleRoot, "go.mod"), goMod)
			writeScanFile(t, filepath.Join(moduleRoot, "shared.go"), source)
			writeScanFile(t, filepath.Join(moduleRoot, "sub", "sub.go"), "package sub\ntype Handler interface { Handle() }\n")
			t.Setenv("GOPROXY", "off")
			if _, err := (Service{}).DeriveTargetsTree(openScanTestTree(t, root), root, []project.LockDep{{GoModule: "example.com/shared", InstallDir: "deps/shared"}}); err == nil {
				t.Fatal("broken root accepted through fallback")
			}
		})
	}
}

func TestScanServiceRootlessRequiresUsablePublicPackage(t *testing.T) {
	for _, module := range []string{"example.com/shared", "example.com/internal/shared"} {
		t.Run(module, func(t *testing.T) {
			root := t.TempDir()
			moduleRoot := filepath.Join(root, "deps", "shared")
			writeScanFile(t, filepath.Join(moduleRoot, "go.mod"), "module "+module+"\n\ngo 1.24.0\n")
			dir := "internal/only"
			if strings.Contains(module, "/internal/") {
				dir = "sub"
			}
			writeScanFile(t, filepath.Join(moduleRoot, filepath.FromSlash(dir), "sub.go"), "package sub\ntype Handler interface { Handle() }\n")
			_, err := (Service{}).DeriveTargetsTree(openScanTestTree(t, root), root, []project.LockDep{{GoModule: module, InstallDir: "deps/shared"}})
			if err == nil || !strings.Contains(err.Error(), "no usable Go packages") {
				t.Fatalf("no eligible package error = %v", err)
			}
		})
	}
}

func TestScanServiceRootlessRetainsOwnedBoundaries(t *testing.T) {
	for _, boundary := range []string{"source", "cleanup"} {
		t.Run(boundary, func(t *testing.T) {
			root := t.TempDir()
			moduleRoot := filepath.Join(root, "deps", "shared")
			writeScanFile(t, filepath.Join(moduleRoot, "go.mod"), "module example.com/shared\n\ngo 1.24.0\n")
			writeScanFile(t, filepath.Join(moduleRoot, "sub", "sub.go"), "package sub\ntype Handler interface { Handle() }\n")
			var replacement, moved string
			attempted, replaced := false, false
			hook := func(path string) {
				attempted = true
				replacement, moved = path, path+"-owned"
				replaced = testutil.RenameRetained(t, path, moved)
				if !replaced {
					return
				}
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "replacement"), []byte("racer"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if boundary == "source" {
				beforeDependencyPackagesLoadForTest = hook
			} else {
				beforeScanTempCleanupForTest = hook
			}
			t.Cleanup(func() {
				beforeDependencyPackagesLoadForTest = nil
				beforeScanTempCleanupForTest = nil
				if boundary == "cleanup" {
					_ = os.RemoveAll(replacement)
					_ = os.RemoveAll(moved)
				}
			})
			tree := openScanTestTree(t, root)
			targets, err := (Service{}).DeriveTargetsTree(tree, root, []project.LockDep{{GoModule: "example.com/shared", InstallDir: "deps/shared"}})
			if !attempted {
				t.Fatal("owned boundary hook was not called")
			}
			if !replaced {
				if err := errors.Join(err, tree.VerifyComplete()); err != nil {
					t.Fatal(err)
				}
				assertDiscoveredSubpackage(t, targets)
				return
			}
			if err == nil {
				t.Fatal("replaced owned boundary accepted")
			}
			entries, readErr := os.ReadDir(replacement)
			if readErr != nil || len(entries) != 1 || entries[0].Name() != "replacement" {
				t.Fatalf("replacement mutated: %v, %v", entries, readErr)
			}
		})
	}
}
