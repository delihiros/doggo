package depsnapshot

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doggo/internal/depsource"
)

func TestRunGoModuleDownloadIgnoresAmbientModule(t *testing.T) {
	const modulePath = "github.com/acme/math"
	proxy := t.TempDir()
	writeGoProxyVersion(t, proxy, modulePath, "v1.0.0")
	writeGoProxyVersion(t, proxy, modulePath, "v1.1.0")
	writeFile(t, filepath.Join(proxy, filepath.FromSlash(modulePath), "@v", "list"), "v1.0.0\nv1.1.0\n")
	t.Setenv("GOPROXY", fileURLForTest(proxy))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GONOPROXY", "none")
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOPATH", t.TempDir())
	t.Setenv("GOMODCACHE", filepath.Join(t.TempDir(), "modcache"))

	ambient := t.TempDir()
	writeFile(t, filepath.Join(ambient, "go.mod"), "module ambient.invalid/test\n\ngo 1.25.0\n\nexclude github.com/acme/math v1.1.0\n")
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(ambient); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	stdout, stderr, err := runGoModuleDownload(modulePath + "@latest")
	if err != nil {
		t.Fatalf("go mod download: %v (%s)\n%s", err, stderr, stdout)
	}
	var download goModuleDownload
	if err := json.Unmarshal(stdout, &download); err != nil {
		t.Fatal(err)
	}
	if download.Version != "v1.1.0" {
		t.Fatalf("download version = %q, want v1.1.0; stderr=%q", download.Version, stderr)
	}
}

// File URLs always use an absolute slash-separated path, including the leading
// slash before a Windows drive letter. URL.Path must not contain native '\\'.
func fileURLForTest(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func TestResolveGoDependency(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/acme/math\n\ngo 1.25.0\n")

	var query string
	stubGoModuleDownload(t, func(got string) ([]byte, string, error) {
		query = got
		return []byte(fmt.Sprintf(`{"Path":"github.com/acme/math","Version":"v1.2.3","Dir":%q}`, dir)), "", nil
	})
	parsed, err := depsource.Parse("github.com/acme/math@latest")
	if err != nil {
		t.Fatal(err)
	}
	gotDir, version, err := resolveGoDependency(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if query != "github.com/acme/math@latest" || gotDir != dir || version != "v1.2.3" {
		t.Fatalf("resolve = (%q, %q), query %q", gotDir, version, query)
	}
}

func TestResolveGoDependencyRejectsInvalidDownload(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	writeFile(t, file, "not a directory")
	tests := []struct {
		name        string
		declaration string
		stdout      string
		stderr      string
		commandErr  error
		want        string
		alsoWant    string
	}{
		{name: "reported error", declaration: "github.com/acme/math@latest", stdout: `{"Path":"github.com/acme/math","Error":"module lookup failed"}`, want: "module lookup failed"},
		{name: "reported and command error", declaration: "github.com/acme/math@latest", stdout: `{"Path":"github.com/acme/math","Error":"module lookup failed"}`, stderr: "proxy unavailable", commandErr: errors.New("exit status 1"), want: "module lookup failed", alsoWant: "proxy unavailable"},
		{name: "command error", declaration: "github.com/acme/math@latest", stderr: "proxy unavailable", commandErr: errors.New("exit status 1"), want: "proxy unavailable"},
		{name: "decode and command error", declaration: "github.com/acme/math@latest", stdout: `{`, stderr: "proxy unavailable", commandErr: errors.New("exit status 1"), want: "decode JSON", alsoWant: "proxy unavailable"},
		{name: "valid JSON and command error", declaration: "github.com/acme/math@latest", stdout: fmt.Sprintf(`{"Path":"github.com/acme/math","Version":"v1.2.3","Dir":%q}`, dir), stderr: "proxy unavailable", commandErr: errors.New("exit status 1"), want: "proxy unavailable"},
		{name: "path mismatch", declaration: "github.com/acme/math@latest", stdout: fmt.Sprintf(`{"Path":"github.com/acme/other","Version":"v1.2.3","Dir":%q}`, dir), stderr: "proxy warning", want: "github.com/acme/other", alsoWant: "proxy warning"},
		{name: "empty version", declaration: "github.com/acme/math@latest", stdout: fmt.Sprintf(`{"Path":"github.com/acme/math","Dir":%q}`, dir), want: "version"},
		{name: "noncanonical version", declaration: "github.com/acme/math@latest", stdout: fmt.Sprintf(`{"Path":"github.com/acme/math","Version":"v1.2","Dir":%q}`, dir), want: "canonical"},
		{name: "exact version mismatch", declaration: "github.com/acme/math@v1.2.3", stdout: fmt.Sprintf(`{"Path":"github.com/acme/math","Version":"v1.2.4","Dir":%q}`, dir), want: "v1.2.3"},
		{name: "empty directory", declaration: "github.com/acme/math@latest", stdout: `{"Path":"github.com/acme/math","Version":"v1.2.3"}`, want: "directory"},
		{name: "missing directory", declaration: "github.com/acme/math@latest", stdout: fmt.Sprintf(`{"Path":"github.com/acme/math","Version":"v1.2.3","Dir":%q}`, filepath.Join(dir, "missing")), want: "directory"},
		{name: "non-directory", declaration: "github.com/acme/math@latest", stdout: fmt.Sprintf(`{"Path":"github.com/acme/math","Version":"v1.2.3","Dir":%q}`, file), want: "not a directory"},
		{name: "multiple objects", declaration: "github.com/acme/math@latest", stdout: `{"Path":"github.com/acme/math"} {"Path":"github.com/acme/math"}`, want: "JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubGoModuleDownload(t, func(string) ([]byte, string, error) {
				return []byte(tt.stdout), tt.stderr, tt.commandErr
			})
			parsed, err := depsource.Parse(tt.declaration)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = resolveGoDependency(parsed)
			if err == nil || !strings.Contains(err.Error(), tt.declaration) || !strings.Contains(err.Error(), tt.want) || tt.alsoWant != "" && !strings.Contains(err.Error(), tt.alsoWant) {
				t.Fatalf("error = %v, want declaration, %q, and %q", err, tt.want, tt.alsoWant)
			}
		})
	}
}

func stubGoModuleDownload(t *testing.T, fn func(string) ([]byte, string, error)) {
	t.Helper()
	original := runGoModuleDownload
	runGoModuleDownload = fn
	t.Cleanup(func() { runGoModuleDownload = original })
}

func writeGoProxyVersion(t *testing.T, proxy, modulePath, version string) {
	t.Helper()
	directory := filepath.Join(proxy, filepath.FromSlash(modulePath), "@v")
	writeFile(t, filepath.Join(directory, version+".info"), fmt.Sprintf(`{"Version":%q,"Time":"2026-01-01T00:00:00Z"}`, version))
	goMod := "module " + modulePath + "\n\ngo 1.25.0\n"
	writeFile(t, filepath.Join(directory, version+".mod"), goMod)

	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	file, err := writer.Create(modulePath + "@" + version + "/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(goMod)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(directory, version+".zip"), archive.String())
}
