package index

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileURIStandardPaths(t *testing.T) {
	tests := []struct{ path, uri string }{}
	if runtime.GOOS == "windows" {
		tests = append(tests,
			struct{ path, uri string }{`C:\projects\a b#%.dog`, "file:///C:/projects/a%20b%23%25.dog"},
			struct{ path, uri string }{`\\server\share\a b.dog`, "file://server/share/a%20b.dog"},
		)
	} else {
		tests = append(tests, struct{ path, uri string }{"/projects/a b#%.dog", "file:///projects/a%20b%23%25.dog"})
	}
	for _, tc := range tests {
		t.Run(tc.uri, func(t *testing.T) {
			if got := PathToFileURI(tc.path); got != tc.uri {
				t.Errorf("PathToFileURI = %q, want %q", got, tc.uri)
			}
			got, err := FileURIToPath(tc.uri)
			if err != nil {
				t.Fatal(err)
			}
			if got != filepath.Clean(tc.path) {
				t.Errorf("FileURIToPath = %q, want %q", got, tc.path)
			}
		})
	}
}

func TestIndexUsesOneIdentityForEscapedFileURI(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.dog")
	mustWriteFile(t, path, "(ns disk)\n(def old 1)\n")
	idx, err := BuildWorkspaceIndex(BuildOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	uri := PathToFileURI(path)
	alias := uri[:len(uri)-len("main.dog")] + "%6dain.dog"
	if err := idx.IndexDocument(alias, "(ns overlay)\n(def current 1)\n"); err != nil {
		t.Fatal(err)
	}
	if got := idx.NamespaceForURI(uri); got != "overlay" {
		t.Fatalf("namespace = %q", got)
	}
	if _, ok := idx.ResolveDefinition("disk", "old"); ok {
		t.Fatal("escaped URI left duplicate disk symbols")
	}
	if syms := idx.DocumentSymbols(uri); len(syms) != 1 || syms[0].Name != "current" {
		t.Fatalf("symbols = %v", syms)
	}
	if err := idx.RemoveDocument(alias); err != nil {
		t.Fatal(err)
	}
	if syms := idx.DocumentSymbols(uri); len(syms) != 0 {
		t.Fatalf("remove left aliases: %v", syms)
	}
}
