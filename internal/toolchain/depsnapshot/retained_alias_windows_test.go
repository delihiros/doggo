package depsnapshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/project"
	"golang.org/x/sys/windows"
)

func TestSelectedSnapshotRejectsRenamedSharedChildPathAlias(t *testing.T) {
	for _, aliasKind := range []string{"case", "short"} {
		t.Run(aliasKind, func(t *testing.T) {
			root := t.TempDir()
			childName := "shared-dependency-long-name"
			childRoot := filepath.Join(root, "a", childName)
			writeFile(t, filepath.Join(root, "a", "doggo.mod"), `{:module "example/a" :deps ["./`+childName+`"]}`)
			writeFile(t, filepath.Join(root, "b", "doggo.mod"), `{:module "example/b" :deps ["../a/`+childName+`"]}`)
			writeFile(t, filepath.Join(childRoot, "doggo.mod"), `{:module "example/shared" :deps []}`)
			mod, lock := publishRetainedFixture(t, root, []string{"./a", "./b"})
			alias := strings.ToUpper(childName)
			if aliasKind == "short" {
				alias = filepath.Base(retainedShortPath(t, childRoot))
				if strings.EqualFold(alias, childName) {
					t.Skip("volume has no distinct child short path alias")
				}
			}
			ordinary, err := os.Stat(childRoot)
			if err != nil {
				t.Fatal(err)
			}
			aliased, err := os.Stat(filepath.Join(root, "a", alias))
			if err != nil || !os.SameFile(ordinary, aliased) {
				t.Fatalf("child alias does not identify original: %s, %v", alias, err)
			}
			t.Logf("shared child alias: %s -> %s", childName, alias)
			writeFile(t, filepath.Join(root, "a", "doggo.mod"), `{:module "example/a" :deps ["./`+alias+`"]}`)
			writeFile(t, filepath.Join(childRoot, "doggo.mod"), `{:module "example/renamed-shared" :deps []}`)
			assertRetainedAliasConflict(t, root, root, mod, lock)
		})
	}
}

func TestSelectedSnapshotRejectsSharedRootOriginAlias(t *testing.T) {
	for _, aliasKind := range []string{"case", "short"} {
		t.Run(aliasKind, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "a", "doggo.mod"), `{:module "example/a" :deps []}`)
			writeFile(t, filepath.Join(root, "b", "doggo.mod"), `{:module "example/b" :deps ["../a"]}`)
			mod, lock := publishRetainedFixture(t, root, []string{"./a", "./b"})
			alias := strings.ToUpper(root)
			if aliasKind == "short" {
				alias = retainedShortPath(t, root)
				if strings.EqualFold(alias, root) {
					t.Skip("volume has no distinct origin short path alias")
				}
			}
			writeFile(t, filepath.Join(root, "a", "doggo.mod"), `{:module "example/renamed" :deps []}`)
			assertRetainedAliasConflict(t, root, alias, mod, lock)
		})
	}
}

func retainedShortPath(t *testing.T, name string) string {
	t.Helper()
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 32768)
	n, err := windows.GetShortPathName(ptr, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || n >= uint32(len(buf)) {
		t.Fatalf("short path for %q: length %d, error %v", name, n, err)
	}
	return windows.UTF16ToString(buf[:n])
}

func assertRetainedAliasConflict(t *testing.T, root, projectAlias string, mod *project.ModFile, lock *project.LockFile) {
	t.Helper()
	deps, err := prepareRetainedTestSnapshot(t, root, projectAlias, mod, lock, "./a")
	if err == nil || !strings.Contains(err.Error(), "identity conflict") {
		t.Fatalf("path alias bypassed retained shared identity: deps=%#v error=%v", deps, err)
	}
}
