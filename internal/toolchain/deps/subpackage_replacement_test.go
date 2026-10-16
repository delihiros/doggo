package deps

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"doggo/internal/project"
	toolchainsync "doggo/internal/toolchain/sync"
)

func TestServiceSupportsRootlessSiblingGoReplacement(t *testing.T) {
	// Keep child package working directories below Windows' process cwd limit
	// after generation and dependency digests are appended.
	workspace, err := os.MkdirTemp("", "doggo-sub-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(workspace); err != nil {
			t.Error(err)
		}
	})
	app := filepath.Join(workspace, "app")
	writeSharedGoFile(t, workspace, "adapter/go.mod", "module example.com/adapter\n\ngo 1.24.0\n\nrequire example.com/shared v0.0.0\nreplace example.com/shared => ../shared\n")
	writeSharedGoFile(t, workspace, "adapter/adapter.go", "package adapter\nimport \"example.com/shared/sub\"\nfunc Hello() string { return sub.Hello() }\n")
	writeSharedGoFile(t, workspace, "adapter/adapter_test.go", "package adapter\nimport \"testing\"\nfunc TestHello(t *testing.T) { if got := Hello(); got != \"subpackage-control\" { t.Fatal(got) } }\n")
	writeSharedGoFile(t, workspace, "shared/go.mod", "module example.com/shared\n\ngo 1.24.0\n")
	writeSharedGoFile(t, workspace, "shared/sub/sub.go", "package sub\nfunc Hello() string { return \"subpackage-control\" }\ntype Handler interface { Handle() }\n")
	writeSharedGoFile(t, workspace, "app/main.dog", "(import \"example.com/adapter\")\n(println (adapter/Hello))\n")
	if err := project.WriteModFile(project.ModPath(app), &project.ModFile{Module: "audit/subpackage-app", Deps: []string{"../adapter"}}); err != nil {
		t.Fatal(err)
	}
	service := &Service{ResolveProjectRoot: func() (string, bool, error) { return app, true, nil }}
	for _, state := range []string{"rootless", "root-anchor", "empty-root"} {
		t.Run(state, func(t *testing.T) {
			if state == "root-anchor" {
				writeSharedGoFile(t, workspace, "shared/shared.go", "package shared\nconst Anchor = 1\n")
			}
			if state == "empty-root" {
				writeSharedGoFile(t, workspace, "shared/shared.go", "package shared\n")
			}
			native := exec.Command("go", "test", "./...")
			native.Dir = filepath.Join(workspace, "adapter")
			if out, err := native.CombinedOutput(); err != nil {
				t.Fatalf("native sibling replacement: %v\n%s", err, out)
			}
			lock, err := (toolchainsync.Service{}).Run(app)
			if err != nil {
				t.Fatal(err)
			}
			assertSubpackageGeneration(t, app, lock, state)
			// Use the production Service and its default Auditor for both published
			// audit and the selected update's tentative-generation audit.
			if summary, err := service.Audit(); err != nil || len(summary.Blocking) != 0 {
				t.Fatalf("default audit = %+v, %v", summary, err)
			}
			updated, results, err := service.UpdateDependencies("../adapter", false)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 {
				t.Fatalf("selected update results = %+v", results)
			}
			assertSubpackageGeneration(t, app, updated, state)
		})
	}
}

func assertSubpackageGeneration(t *testing.T, app string, lock *project.LockFile, state string) {
	t.Helper()
	rootless := state == "rootless"
	if slices.Contains(lock.Scan.GoPackages, "example.com/shared") == rootless || slices.Contains(lock.Scan.GoPackages, "example.com/shared/sub") != rootless {
		t.Fatalf("selected packages = %v", lock.Scan.GoPackages)
	}
	if !slices.Contains(lock.Scan.GoPackages, "example.com/adapter") {
		t.Fatalf("adapter package absent: %v", lock.Scan.GoPackages)
	}
	if got := slices.ContainsFunc(lock.Scan.ReifyTargets, func(target project.ReifyTarget) bool {
		return target.Source == "example.com/shared/sub.Handler" && target.Runtime == target.Source
	}); got != rootless {
		t.Fatalf("shared/sub.Handler selected = %t, targets %v", got, lock.Scan.ReifyTargets)
	}
	toolchain := filepath.Join(app, ".doggo", "generations", lock.Generation, "toolchain")
	temp := t.TempDir()
	probe := filepath.Join(temp, "subpackage_test.go")
	source := fmt.Sprintf(`package interop_test
import (
 "reflect"
 "testing"
 "doggo/internal/interop"
 _ "doggo/internal/reify"
)
func TestGeneratedSubpackage(t *testing.T) {
 hello, ok := interop.LookupSymbol("example.com/adapter", "Hello")
 if !ok || hello.(func() string)() != "subpackage-control" { t.Fatalf("generated Hello = %%v", hello) }
 if %t {
  handler, ok := interop.LookupSymbol("example.com/shared/sub", "Handler")
  if !ok { t.Fatal("subpackage Handler missing from registry") }
  if _, ok := interop.LookupReifyFactory(handler.(reflect.Type)); !ok { t.Fatal("subpackage Handler missing reify factory") }
 }
 if %t {
  exports, ok := interop.LookupPackage("example.com/shared")
  if !ok || len(exports) != 0 { t.Fatalf("empty root registration = %%v, %%t", exports, ok) }
 }
}
`, rootless, state == "empty-root")
	if err := os.WriteFile(probe, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(temp, "overlay.json")
	content, err := json.Marshal(map[string]any{"Replace": map[string]string{filepath.Join(toolchain, "internal", "interop", "subpackage_regression_test.go"): probe}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlay, content, 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-mod=readonly", "-overlay", overlay, "./internal/interop")
	cmd.Dir = toolchain
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("actual generated registry/reify execution: %v\n%s", err, out)
	}
}
