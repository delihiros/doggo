package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
