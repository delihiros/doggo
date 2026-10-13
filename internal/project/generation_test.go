package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestDependencyKeyUsesFramedIdentity(t *testing.T) {
	dep := LockDep{Kind: "git", ModuleID: "doggo:example/dep", GoModule: "example.com/dep", Revision: "abc", Digest: "def"}
	const want = "5b755fa73b0f58b5d6c91b38aac9623218aa0f100742065eb4f3e5c03c6349f9"
	if got := DependencyKey(dep); got != want {
		t.Errorf("dependency key = %q, want %q", got, want)
	}
	alias := dep
	alias.Source, alias.InstallDir = "another-source", "another-directory"
	if DependencyKey(alias) != DependencyKey(dep) {
		t.Error("source and derived install directory changed the identity key")
	}
	for i := range 5 {
		changed := dep
		fields := []*string{&changed.Kind, &changed.ModuleID, &changed.GoModule, &changed.Revision, &changed.Digest}
		*fields[i] += "changed"
		if DependencyKey(changed) == DependencyKey(dep) {
			t.Errorf("changing field %d did not change the identity key", i)
		}
	}
	left, right := dep, dep
	left.ModuleID, left.GoModule = "a\x00b", "c"
	right.ModuleID, right.GoModule = "a", "b\x00c"
	if DependencyKey(left) == DependencyKey(right) {
		t.Error("embedded NUL crossed an identity field boundary")
	}
}

func TestGenerationBinaryPathUsesCanonicalPlatformName(t *testing.T) {
	wantName := "doggo"
	if runtime.GOOS == "windows" {
		wantName = "doggo.exe"
	}
	if got := GenerationBinaryName(); got != wantName {
		t.Fatalf("GenerationBinaryName() = %q, want %q", got, wantName)
	}
	wantPath := path.Join(".doggo", "generations", "example", "bin", wantName)
	if got := GenerationBinaryPath("example"); got != wantPath {
		t.Fatalf("GenerationBinaryPath() = %q, want %q", got, wantPath)
	}
}

func TestWriteDigestStreamUsesBoundedReadsAndLengthFraming(t *testing.T) {
	const size = int64(2 << 20)
	reader := &guardedDigestReader{remaining: size, maxRead: 32 << 10}
	got := sha256.New()
	if err := writeDigestStream(got, reader, size); err != nil {
		t.Fatal(err)
	}
	want := sha256.New()
	writeDigestField(want, bytes.Repeat([]byte{'x'}, int(size)))
	if !bytes.Equal(got.Sum(nil), want.Sum(nil)) {
		t.Fatal("stream digest framing differs from field framing")
	}
	if reader.largestRead > reader.maxRead {
		t.Fatalf("largest read = %d, want <= %d", reader.largestRead, reader.maxRead)
	}
}

func TestWriteDigestStreamRejectsShortContent(t *testing.T) {
	err := writeDigestStream(sha256.New(), strings.NewReader("short"), 6)
	if err == nil {
		t.Fatal("writeDigestStream accepted content shorter than framed length")
	}
}

func TestValidateGenerationRejectsOversizedMarkerBeforeReadingIt(t *testing.T) {
	root, mod, lock := writeValidGeneration(t, false)
	marker := filepath.Join(generationPath(root, lock.Generation), generationMarkerName)
	if err := os.Truncate(marker, 4<<20); err != nil {
		t.Fatal(err)
	}
	err := ValidateGeneration(root, mod, lock)
	if err == nil || !strings.Contains(err.Error(), "marker size") {
		t.Fatalf("ValidateGeneration error = %v, want marker size rejection", err)
	}
}

type guardedDigestReader struct {
	remaining   int64
	maxRead     int
	largestRead int
}

func (r *guardedDigestReader) Read(p []byte) (int, error) {
	if len(p) > r.maxRead {
		return 0, fmt.Errorf("unbounded read buffer: %d", len(p))
	}
	if len(p) > r.largestRead {
		r.largestRead = len(p)
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.remaining)
	for i := range int(n) {
		p[i] = 'x'
	}
	r.remaining -= n
	return int(n), nil
}

func TestValidateGenerationAcceptsSyncOnlyAndBuilt(t *testing.T) {
	for _, built := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync-only", true: "built"}[built], func(t *testing.T) {
			root, mod, lock := writeValidGeneration(t, built)
			if err := ValidateGeneration(root, mod, lock); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateGenerationRejectsMalformedLockFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*LockFile)
	}{
		{"generation", func(lock *LockFile) { lock.Generation = "ABC" }},
		{"mod digest", func(lock *LockFile) { lock.ModLockInputsSHA256 = "ABC" }},
		{"dependency digest", func(lock *LockFile) { lock.Deps[0].Digest = "ABC" }},
		{"unknown kind", func(lock *LockFile) { lock.Deps[0].Kind = "archive" }},
		{"empty module payload", func(lock *LockFile) { lock.Deps[0].ModuleID = "doggo:" }},
		{"unknown module prefix", func(lock *LockFile) { lock.Deps[0].ModuleID = "npm:example" }},
		{"invalid Go module", func(lock *LockFile) { lock.Deps[0].GoModule = "bad module" }},
		{"mismatched Go identity", func(lock *LockFile) { lock.Deps[0].ModuleID = "go:example.com/other" }},
		{"git without revision", func(lock *LockFile) { lock.Deps[0].Kind, lock.Deps[0].Revision = "git", "" }},
		{"path with revision", func(lock *LockFile) { lock.Deps[0].Revision = "revision" }},
		{"absolute path source", func(lock *LockFile) { lock.Deps[0].Source = "/tmp/dep" }},
		{"noncanonical path source", func(lock *LockFile) { lock.Deps[0].Source = "./nested/../dep" }},
		{"noncanonical install directory", func(lock *LockFile) { lock.Deps[0].InstallDir = "deps/../deps/key" }},
		{"dependency key mismatch", func(lock *LockFile) { lock.Deps[0].InstallDir = "deps/" + strings.Repeat("f", 64) }},
		{"duplicate install directory", func(lock *LockFile) { lock.Deps = append(lock.Deps, lock.Deps[0]) }},
		{"binary without digest", func(lock *LockFile) { lock.Build = LockBuild{Binary: expectedBinaryPath(lock.Generation)} }},
		{"digest without binary", func(lock *LockFile) { lock.Build = LockBuild{SHA256: strings.Repeat("a", 64)} }},
		// This legacy fixed binary path is intentional: the rejection fixture must fail validation.
		{"wrong binary path", func(lock *LockFile) { lock.Build = LockBuild{Binary: "bin/doggo", SHA256: strings.Repeat("a", 64)} }},
		{"malformed binary digest", func(lock *LockFile) {
			lock.Build = LockBuild{Binary: expectedBinaryPath(lock.Generation), SHA256: "ABC"}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, mod, lock := writeValidGeneration(t, false)
			tt.mutate(lock)
			if err := ValidateGeneration(root, mod, lock); err == nil {
				t.Fatal("ValidateGeneration accepted malformed lock")
			}
		})
	}
}

func TestValidateLockDepGo(t *testing.T) {
	valid := LockDep{
		Source: "github.com/acme/math@v1.2.3", Kind: "go", ModuleID: "go:github.com/acme/math",
		GoModule: "github.com/acme/math", Revision: "v1.2.3", Digest: strings.Repeat("a", 64),
	}
	valid.InstallDir = "deps/" + DependencyKey(valid)
	if err := validateLockDep(valid); err != nil {
		t.Fatalf("validateLockDep(valid Go dependency): %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*LockDep)
	}{
		{"empty revision", func(dep *LockDep) { dep.Revision = "" }},
		{"noncanonical revision", func(dep *LockDep) { dep.Revision = "v1.2" }},
		{"source module mismatch", func(dep *LockDep) { dep.GoModule = "github.com/acme/other" }},
		{"exact query mismatch", func(dep *LockDep) { dep.Revision = "v1.2.4" }},
		{"git URL marked go", func(dep *LockDep) { dep.Source = "https://github.com/acme/math.git@v1.2.3" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := valid
			tt.mutate(&dep)
			dep.InstallDir = "deps/" + DependencyKey(dep)
			if err := validateLockDep(dep); err == nil {
				t.Fatal("validateLockDep accepted invalid Go dependency")
			}
		})
	}
}

func TestValidateGenerationRejectsCorruptAndMissingContent(t *testing.T) {
	tests := []struct {
		name   string
		built  bool
		mutate func(string, *LockFile) error
	}{
		{"generation hash mismatch", false, func(generationRoot string, _ *LockFile) error {
			return os.WriteFile(filepath.Join(generationRoot, "toolchain", "main.go"), []byte("changed"), 0644)
		}},
		{"binary hash mismatch", true, func(_ string, lock *LockFile) error {
			lock.Build.SHA256 = strings.Repeat("f", 64)
			return nil
		}},
		{"missing dependency tree", false, func(generationRoot string, lock *LockFile) error {
			return os.RemoveAll(filepath.Join(generationRoot, filepath.FromSlash(lock.Deps[0].InstallDir)))
		}},
		{"missing toolchain", false, func(generationRoot string, _ *LockFile) error {
			return os.RemoveAll(filepath.Join(generationRoot, "toolchain"))
		}},
		{"dependency symlink", false, func(generationRoot string, lock *LockFile) error {
			depRoot := filepath.Join(generationRoot, filepath.FromSlash(lock.Deps[0].InstallDir))
			if err := os.RemoveAll(depRoot); err != nil {
				return err
			}
			return os.Symlink(t.TempDir(), depRoot)
		}},
		{"toolchain symlink escape", false, func(generationRoot string, _ *LockFile) error {
			toolchain := filepath.Join(generationRoot, "toolchain")
			if err := os.RemoveAll(toolchain); err != nil {
				return err
			}
			return os.Symlink(t.TempDir(), toolchain)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, mod, lock := writeValidGeneration(t, tt.built)
			generationRoot := generationPath(root, lock.Generation)
			if err := tt.mutate(generationRoot, lock); err != nil {
				t.Skipf("filesystem mutation unavailable: %v", err)
			}
			if err := ValidateGeneration(root, mod, lock); err == nil {
				t.Fatal("ValidateGeneration accepted corrupt generation")
			}
		})
	}
}

func TestGenerationDigestExcludesSourceAndUsesFraming(t *testing.T) {
	root, _, lock := writeValidGeneration(t, false)
	generationRoot := generationPath(root, lock.Generation)

	changedSource := cloneLock(lock)
	changedSource.Deps[0].Source = "../same-content"
	got, err := GenerationDigest(generationRoot, changedSource)
	if err != nil {
		t.Fatal(err)
	}
	if got != lock.Generation {
		t.Fatalf("source changed generation digest: got %s, want %s", got, lock.Generation)
	}

	changedRecord := cloneLock(lock)
	changedRecord.Deps[0].ModuleID = "doggo:example/other"
	changed, err := GenerationDigest(generationRoot, changedRecord)
	if err != nil {
		t.Fatal(err)
	}
	if changed == lock.Generation {
		t.Fatal("canonical dependency record did not affect generation digest")
	}
}

func writeValidGeneration(t *testing.T, built bool) (string, *ModFile, *LockFile) {
	var binary []byte
	if built {
		binary = []byte("doggo binary")
	}
	return writeValidGenerationBinary(t, binary)
}

func writeValidGenerationBinary(t *testing.T, binaryContent []byte) (string, *ModFile, *LockFile) {
	t.Helper()
	root := t.TempDir()
	mod := &ModFile{Module: "example/project", Deps: []string{"./dep"}}
	if err := WriteModFile(ModPath(root), mod); err != nil {
		t.Fatal(err)
	}
	generations := filepath.Join(root, ".doggo", "generations")
	stage := filepath.Join(generations, "stage")
	depRoot := filepath.Join(stage, "deps", "pending")
	if err := os.MkdirAll(depRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depRoot, "doggo.mod"), []byte(`{:module "example/dep" :deps []}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(depRoot, "lib"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depRoot, "lib", "core.dog"), []byte("(ns core)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	depDigest, err := canonicalTreeDigestForTest(depRoot)
	if err != nil {
		t.Fatal(err)
	}
	dep := LockDep{
		Source: "./dep", Kind: "path", ModuleID: "doggo:example/dep", GoModule: "", Revision: "", Digest: depDigest,
	}
	dep.InstallDir = "deps/" + DependencyKey(dep)
	if err := os.Rename(depRoot, filepath.Join(stage, filepath.FromSlash(dep.InstallDir))); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stage, "toolchain"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "toolchain", "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	lock := &LockFile{
		LockVersion: LockVersion, ModLockInputsSHA256: HashModLockInputs(mod), Deps: []LockDep{dep},
		Scan: LockScan{GoPackages: []string{"fmt"}, ReifyTargets: []ReifyTarget{{Source: "fmt.Stringer", Runtime: "fmt.Stringer"}}},
	}
	if binaryContent != nil {
		binary := filepath.Join(stage, "bin", GenerationBinaryName())
		if err := os.MkdirAll(filepath.Dir(binary), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(binary, binaryContent, 0755); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(binaryContent)
		lock.Build.SHA256 = hex.EncodeToString(sum[:])
	}
	generation, err := GenerationDigest(stage, lock)
	if err != nil {
		t.Fatal(err)
	}
	lock.Generation = generation
	if binaryContent != nil {
		lock.Build.Binary = expectedBinaryPath(generation)
	}
	if err := os.WriteFile(filepath.Join(stage, ".generation"), []byte(generation+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	final := generationPath(root, generation)
	if err := os.Rename(stage, final); err != nil {
		t.Fatal(err)
	}
	if err := WriteLockFile(LockPath(root), lock); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".doggo", "generation.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	return root, mod, lock
}

func generationPath(root, generation string) string {
	return filepath.Join(root, ".doggo", "generations", generation)
}

func expectedBinaryPath(generation string) string {
	return GenerationBinaryPath(generation)
}

func cloneLock(lock *LockFile) *LockFile {
	copy := *lock
	copy.Deps = append([]LockDep(nil), lock.Deps...)
	copy.Scan.GoPackages = append([]string(nil), lock.Scan.GoPackages...)
	copy.Scan.ReifyTargets = append([]ReifyTarget(nil), lock.Scan.ReifyTargets...)
	return &copy
}

func canonicalTreeDigestForTest(root string) (string, error) {
	entries := make([]string, 0, 8)
	if err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == root {
			return nil
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(entries)
	hash := sha256.New()
	writeTestUint64(hash, uint64(len(entries)))
	for _, rel := range entries {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		kind, mode := []byte{'f'}, []byte("0644")
		var content []byte
		if info.IsDir() {
			kind, mode = []byte{'d'}, []byte("0755")
		} else {
			content, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				return "", err
			}
			if info.Mode()&0111 != 0 {
				mode = []byte("0755")
			}
		}
		for _, field := range [][]byte{kind, []byte(rel), mode, content} {
			writeTestUint64(hash, uint64(len(field)))
			_, _ = hash.Write(field)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeTestUint64(writer io.Writer, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = writer.Write(encoded[:])
}
