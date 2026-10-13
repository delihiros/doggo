package licenses

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseNoticesExist(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, item := range []struct {
		path      string
		fragments []string
	}{
		{"THIRD_PARTY_NOTICES", []string{"Go runtime", "github.com/Masterminds/semver/v3", "github.com/chzyer/readline", "golang.org/x/sys", "Redistribution and use in source and binary forms"}},
		{filepath.Join("editors", "vscode", "THIRD_PARTY_NOTICES"), []string{"vscode-languageclient", "vscode-jsonrpc", "Copyright (c) Microsoft Corporation", "Permission is hereby granted, free of charge", "ISC License"}},
	} {
		t.Run(filepath.ToSlash(item.path), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, item.path))
			if err != nil {
				t.Fatalf("release is missing required third-party notices: %v", err)
			}
			for _, fragment := range item.fragments {
				if !strings.Contains(string(data), fragment) {
					t.Errorf("%s is missing required notice text %q", item.path, fragment)
				}
			}
		})
	}
}

func TestReadLicenseFilesPreservesAttribution(t *testing.T) {
	dir := t.TempDir()
	want := []byte("Copyright Example <person@example.test>\r\nPermission text\r\n")
	if err := os.WriteFile(filepath.Join(dir, "License.txt"), want, 0644); err != nil {
		t.Fatal(err)
	}
	files, err := readLicenseFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !bytes.Equal(files[0].data, want) {
		t.Fatalf("license text changed: %#v", files)
	}
}

func TestReadLicenseFilesRejectsMissingLicense(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "NOTICE"), []byte("Attribution alone"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readLicenseFiles(dir); err == nil || !strings.Contains(err.Error(), "missing license") {
		t.Fatalf("error = %v", err)
	}
}

func TestVerifyArtifactRejectsMissingOrChangedNotices(t *testing.T) {
	root := t.TempDir()
	license := []byte("Doggo license\n")
	notices := []byte("Component: Go runtime " + runtime.Version() + "\n")
	for name, data := range map[string][]byte{"LICENSE": license, "THIRD_PARTY_NOTICES": notices} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	binaryPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		notices []byte
		want    string
	}{
		{"missing", nil, "missing THIRD_PARTY_NOTICES"},
		{"changed", []byte("stripped notice"), "THIRD_PARTY_NOTICES differs"},
		{"complete", notices, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artifact := filepath.Join(root, tc.name+".zip")
			entries := map[string][]byte{"LICENSE": license, "doggo.exe": binary}
			if tc.notices != nil {
				entries["THIRD_PARTY_NOTICES"] = tc.notices
			}
			writeZipFixture(t, artifact, entries)
			err := VerifyArtifact(root, artifact)
			if tc.want == "" && err != nil {
				t.Fatal(err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestVerifyVSIXRequiresCompleteNotices(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "editors", "vscode")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"LICENSE": []byte("own license"), "THIRD_PARTY_NOTICES": []byte("Microsoft complete permission\n")} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	artifact := filepath.Join(root, "extension.vsix")
	writeZipFixture(t, artifact, map[string][]byte{"extension/LICENSE.txt": []byte("own license"), "extension/dist/extension.js": []byte("bundle")})
	if err := VerifyArtifact(root, artifact); err == nil || !strings.Contains(err.Error(), "missing extension/THIRD_PARTY_NOTICES") {
		t.Fatalf("error = %v", err)
	}
}

func TestVerifyVSIXRejectsStaleBundle(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "editors", "vscode")
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{
		"extension/LICENSE.txt":         []byte("own license"),
		"extension/THIRD_PARTY_NOTICES": []byte("complete permissions"),
		"extension/dist/extension.js":   []byte("current bundled inputs"),
	}
	for name, data := range entries {
		name = strings.TrimPrefix(name, "extension/")
		if name == "LICENSE.txt" {
			name = "LICENSE"
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	artifact := filepath.Join(root, "extension.vsix")
	writeZipFixture(t, artifact, entries)
	if err := VerifyArtifact(root, artifact); err != nil {
		t.Fatal(err)
	}
	entries["extension/dist/extension.js"] = []byte("stale bundle from other inputs")
	writeZipFixture(t, artifact, entries)
	if err := VerifyArtifact(root, artifact); err == nil || !strings.Contains(err.Error(), "extension/dist/extension.js differs") {
		t.Fatalf("error = %v", err)
	}
}

func writeZipFixture(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, data := range entries {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
