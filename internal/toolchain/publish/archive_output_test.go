package publish

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"doggo/internal/project"
)

func publishOutputFixture(t *testing.T) (string, string, Options) {
	t.Helper()
	root := t.TempDir()
	mod := &project.ModFile{
		Module: "github.com/acme/math", Name: "acme/math", Version: "1.2.3",
		License: "MIT", Compat: ">=0.1.0", Deps: []string{},
	}
	if err := project.WriteModFile(project.ModPath(root), mod); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "math.dog"), []byte("(ns acme.math)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "dist", "publish", "acme-math-1.2.3.tgz")
	if err := os.MkdirAll(filepath.Dir(archive), 0755); err != nil {
		t.Fatal(err)
	}
	return root, archive, Options{
		RegistryPath: filepath.Join(root, "doggo.registry"),
		Source:       "https://github.com/acme/math.git@v1.2.3",
	}
}

func assertReadablePublishArchive(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	entries := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		entries[hdr.Name] = true
		if _, err := io.Copy(io.Discard, tr); err != nil {
			t.Fatal(err)
		}
	}
	// Read through the gzip checksum/trailer, beyond the tar end markers.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		t.Fatal(err)
	}
	if !entries[project.ModFileName] || !entries["math.dog"] {
		t.Fatalf("missing archive entries: %v", entries)
	}
}

func assertNoPublishTemps(t *testing.T, archive string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(archive))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(archive) {
			t.Errorf("unexpected leftover publish file %s", entry.Name())
		}
	}
}

func TestServiceRunPreservesArchiveSymlinkTarget(t *testing.T) {
	root, archive, opts := publishOutputFixture(t)
	outside := filepath.Join(t.TempDir(), "unrelated.txt")
	marker := []byte("unrelated content must be preserved\n")
	if err := os.WriteFile(outside, marker, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, archive); err != nil {
		t.Fatal(err)
	}
	if _, err := (Service{}).Run(root, opts); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, marker) {
		t.Errorf("publishing overwrote the archive symlink target: %q", actual)
	}
	assertReadablePublishArchive(t, archive)
	assertNoPublishTemps(t, archive)
}

func TestServiceRunReplacesExistingArchive(t *testing.T) {
	root, archive, opts := publishOutputFixture(t)
	if err := os.WriteFile(archive, []byte("previous artifact"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Service{}).Run(root, opts); err != nil {
		t.Fatal(err)
	}
	assertReadablePublishArchive(t, archive)
	assertNoPublishTemps(t, archive)
}

func TestServiceRunArchiveRenameFailureLeavesRegistryUntouched(t *testing.T) {
	root, archive, opts := publishOutputFixture(t)
	if err := os.Mkdir(archive, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(archive, "retained.txt")
	if err := os.WriteFile(marker, []byte("retained"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Service{}).Run(root, opts); err == nil {
		t.Fatal("expected failure when an archive path is an existing directory")
	}
	if actual, err := os.ReadFile(marker); err != nil || string(actual) != "retained" {
		t.Fatalf("archive directory contents changed: %q, %v", actual, err)
	}
	if _, err := os.Stat(opts.RegistryPath); !os.IsNotExist(err) {
		t.Fatalf("registry was created after archive failure: %v", err)
	}
	assertNoPublishTemps(t, archive)
}

type failingArchiveWriter struct {
	remaining int
	err       error
}

func (w *failingArchiveWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		n := w.remaining
		w.remaining = 0
		return n, w.err
	}
	w.remaining -= len(p)
	return len(p), nil
}

func TestWritePublishArchiveReportsWriteFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bytes int
	}{
		{name: "header", bytes: 0},
		{name: "compressed payload finalization", bytes: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _, _ := publishOutputFixture(t)
			files, err := collectPublishFiles(root)
			if err != nil {
				t.Fatal(err)
			}
			want := errors.New("archive output full")
			writer := &failingArchiveWriter{remaining: tc.bytes, err: want}
			if err := writePublishArchive(root, files, writer); !errors.Is(err, want) {
				t.Fatalf("expected archive write error, got %v", err)
			}
		})
	}
}
