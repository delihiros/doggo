package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// renameRetainedForTest attempts a hostile replacement without closing retained
// handles. Windows can prevent it outright; verify that this leaves both paths
// unchanged instead of skipping the remaining safety assertions.
func renameRetainedForTest(t *testing.T, oldPath, newPath string) bool {
	t.Helper()
	before, err := os.Lstat(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldPath, newPath); err == nil {
		return true
	} else if runtime.GOOS != "windows" || (!errors.Is(err, fs.ErrPermission) && !errors.Is(err, syscall.Errno(32))) { // ERROR_SHARING_VIOLATION
		t.Fatal(err)
	}
	after, err := os.Lstat(oldPath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("blocked rename changed retained source: %v", err)
	}
	if _, err := os.Lstat(newPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("blocked rename created destination: %v", err)
	}
	return false
}

func TestSyncTreeSyncsRegularTree(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "file"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SyncTree(root); err != nil {
		t.Fatal(err)
	}
}

func TestSyncTreeRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := SyncTree(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("SyncTree error = %v, want symlink rejection", err)
	}
}

func TestSyncTreeRejectsSpecialFiles(t *testing.T) {
	root := t.TempDir()
	socketPath := filepath.Join(root, "socket")
	verifySocket := createSocketFileForTest(t, socketPath)
	verifySocket()
	// Directory enumeration preserves socket identity even when Windows refuses
	// to open the socket reparse point itself for Lstat.
	socketInfo := func() fs.FileInfo {
		directory, err := os.Open(root)
		if err != nil {
			t.Fatal(err)
		}
		entries, readErr := directory.Readdir(-1)
		closeErr := directory.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read special-file fixture directory: %v", errors.Join(readErr, closeErr))
		}
		if len(entries) != 1 || entries[0].Name() != "socket" {
			t.Fatalf("special-file fixture entries = %v, want only socket", entries)
		}
		return entries[0]
	}
	before := socketInfo()
	err := SyncTree(root)
	var pathErr *os.PathError
	windowsSocketRejected := runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1920)) && // ERROR_CANT_ACCESS_FILE
		errors.As(err, &pathErr) && pathErr.Op == "statat" && pathErr.Path == "socket"
	if err == nil || !strings.Contains(err.Error(), "special") && !windowsSocketRejected {
		t.Fatalf("SyncTree error = %v, want special-file rejection", err)
	}
	after := socketInfo()
	if !os.SameFile(before, after) || after.Mode() != before.Mode() {
		t.Fatal("rejected special file changed")
	}
	verifySocket()
}

func TestPublishDirectoryRenamesWithinGenerations(t *testing.T) {
	parent := filepath.Join(t.TempDir(), ".doggo", "generations")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(parent, ".stage-1")
	destination := filepath.Join(parent, strings.Repeat("a", 64))
	if err := os.Mkdir(stage, 0755); err != nil {
		t.Fatal(err)
	}
	if err := PublishDirectory(stage, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("published directory: %v", err)
	}
	if _, err := os.Stat(stage); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stage still exists: %v", err)
	}
}

func TestPublishRetainedDirectoryPublishesWholeTreeWithoutReplacingDestination(t *testing.T) {
	parentPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(parentPath, "stage"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parentPath, "stage", "private"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Close() })
	stage, err := openVerifiedSubroot(parent, "stage")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stage.Close() })

	if err := PublishRetainedDirectory(parent, stage, "stage", "keys"); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(parentPath, "keys", "private")); err != nil || string(content) != "secret" {
		t.Fatalf("published content = %q, err=%v", content, err)
	}
	if _, err := os.Lstat(filepath.Join(parentPath, "stage")); !os.IsNotExist(err) {
		t.Fatalf("stage still exists: %v", err)
	}
}

func TestPublishDirectoryClassifiesGenerationParentSyncFailureAfterRename(t *testing.T) {
	parent := filepath.Join(t.TempDir(), ".doggo", "generations")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(parent, ".stage-1")
	destination := filepath.Join(parent, strings.Repeat("a", 64))
	if err := os.Mkdir(stage, 0755); err != nil {
		t.Fatal(err)
	}
	want := errors.New("injected generation-parent sync failure")
	original := atomicParentSync
	atomicParentSync = func(*os.File) error { return want }
	t.Cleanup(func() { atomicParentSync = original })

	err := PublishDirectory(stage, destination)
	if !errors.Is(err, want) {
		t.Fatalf("PublishDirectory error = %v, want injected failure", err)
	}
	var atomicErr *AtomicWriteError
	if !errors.As(err, &atomicErr) || !atomicErr.Renamed {
		t.Fatalf("PublishDirectory error = %T %v, want post-rename classification", err, err)
	}
	if _, statErr := os.Stat(destination); statErr != nil {
		t.Fatalf("destination missing after rename commit point: %v", statErr)
	}
	if _, statErr := os.Stat(stage); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("stage exists after rename: %v", statErr)
	}
}

func TestPublishDirectoryClassifiesPublisherFailureAfterRename(t *testing.T) {
	parent := filepath.Join(t.TempDir(), ".doggo", "generations")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(parent, ".stage-1")
	destination := filepath.Join(parent, strings.Repeat("a", 64))
	if err := os.Mkdir(stage, 0755); err != nil {
		t.Fatal(err)
	}
	want := errors.New("injected generation publisher failure after rename")
	original := generationRenameNoReplace
	generationRenameNoReplace = func(parent *os.File, retained *os.Root, oldName, newName string) (bool, error) {
		renamed, err := original(parent, retained, oldName, newName)
		if err != nil {
			return renamed, err
		}
		return true, want
	}
	t.Cleanup(func() { generationRenameNoReplace = original })

	err := PublishDirectory(stage, destination)
	if !errors.Is(err, want) {
		t.Fatalf("PublishDirectory error = %v, want injected failure", err)
	}
	var atomicErr *AtomicWriteError
	if !errors.As(err, &atomicErr) || !atomicErr.Renamed {
		t.Fatalf("PublishDirectory error = %T %v, want post-rename classification", err, err)
	}
	if _, statErr := os.Stat(destination); statErr != nil {
		t.Fatalf("destination missing after rename commit point: %v", statErr)
	}
	if _, statErr := os.Stat(stage); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("stage exists after rename: %v", statErr)
	}
}

func TestPublishDirectoryDoesNotOverwriteDestinationInsertedAtRename(t *testing.T) {
	parent := filepath.Join(t.TempDir(), ".doggo", "generations")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(parent, ".stage-1")
	destination := filepath.Join(parent, strings.Repeat("a", 64))
	if err := os.Mkdir(stage, 0755); err != nil {
		t.Fatal(err)
	}
	original := generationRenameNoReplace
	generationRenameNoReplace = func(parent *os.File, retained *os.Root, oldName, newName string) (bool, error) {
		if err := os.WriteFile(destination, []byte("inserted"), 0644); err != nil {
			return false, err
		}
		return original(parent, retained, oldName, newName)
	}
	t.Cleanup(func() { generationRenameNoReplace = original })

	err := PublishDirectory(stage, destination)
	var atomicErr *AtomicWriteError
	if !errors.As(err, &atomicErr) || atomicErr.Renamed {
		t.Fatalf("PublishDirectory error = %T %v, want pre-rename classification", err, err)
	}
	content, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "inserted" {
		t.Fatalf("destination content = %q, want inserted", content)
	}
	if info, statErr := os.Stat(stage); statErr != nil || !info.IsDir() {
		t.Fatalf("stage was moved on failed publication: %v", statErr)
	}
}

func TestPublishDirectoryDetectsStageSwapAtRename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows publisher rejects the replaced source handle before rename")
	}
	parent := filepath.Join(t.TempDir(), ".doggo", "generations")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(parent, ".stage-1")
	movedStage := filepath.Join(parent, ".stage-original")
	destination := filepath.Join(parent, strings.Repeat("a", 64))
	if err := os.Mkdir(stage, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "original"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	original := generationRenameNoReplace
	generationRenameNoReplace = func(parent *os.File, retained *os.Root, oldName, newName string) (bool, error) {
		if err := os.Rename(stage, movedStage); err != nil {
			return false, err
		}
		if err := os.Mkdir(stage, 0755); err != nil {
			return false, err
		}
		if err := os.WriteFile(filepath.Join(stage, "replacement"), []byte("replacement"), 0644); err != nil {
			return false, err
		}
		return original(parent, retained, oldName, newName)
	}
	t.Cleanup(func() { generationRenameNoReplace = original })

	err := PublishDirectory(stage, destination)
	var atomicErr *AtomicWriteError
	if !errors.As(err, &atomicErr) || !atomicErr.Renamed {
		t.Fatalf("PublishDirectory error = %T %v, want renamed stage-identity failure", err, err)
	}
	if _, statErr := os.Stat(filepath.Join(destination, "replacement")); statErr != nil {
		t.Fatalf("replacement stage was not renamed: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(movedStage, "original")); statErr != nil {
		t.Fatalf("retained original stage changed: %v", statErr)
	}
}

func TestPublishDirectoryDoesNotReplaceExistingDestination(t *testing.T) {
	for _, tt := range []struct {
		name   string
		create func(string) error
	}{
		{name: "file", create: func(path string) error { return os.WriteFile(path, []byte("existing"), 0644) }},
		{name: "directory", create: func(path string) error { return os.Mkdir(path, 0755) }},
		{name: "symlink", create: func(path string) error { return os.Symlink("missing", path) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), ".doggo", "generations")
			if err := os.MkdirAll(parent, 0755); err != nil {
				t.Fatal(err)
			}
			stage := filepath.Join(parent, ".stage-1")
			destination := filepath.Join(parent, strings.Repeat("a", 64))
			if err := os.Mkdir(stage, 0755); err != nil {
				t.Fatal(err)
			}
			if err := tt.create(destination); err != nil {
				t.Skipf("destination type unavailable: %v", err)
			}
			err := PublishDirectory(stage, destination)
			var atomicErr *AtomicWriteError
			if !errors.As(err, &atomicErr) || atomicErr.Renamed {
				t.Fatalf("PublishDirectory error = %T %v, want pre-rename rejection", err, err)
			}
			if info, statErr := os.Stat(stage); statErr != nil || !info.IsDir() {
				t.Fatalf("stage was moved on failed publication: %v", statErr)
			}
		})
	}
}

func TestPublishDirectoryRejectsSymlinkStage(t *testing.T) {
	parent := filepath.Join(t.TempDir(), ".doggo", "generations")
	outside := t.TempDir()
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(parent, ".stage-1")
	if err := os.Symlink(outside, stage); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	destination := filepath.Join(parent, strings.Repeat("a", 64))
	if err := PublishDirectory(stage, destination); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("PublishDirectory error = %v, want symlink rejection", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside target changed: %v", err)
	}
}

func TestPublishDirectoryRejectsSymlinkDoggoParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideGenerations := filepath.Join(outside, "generations")
	if err := os.Mkdir(outsideGenerations, 0755); err != nil {
		t.Fatal(err)
	}
	outsideStage := filepath.Join(outsideGenerations, ".stage-1")
	if err := os.Mkdir(outsideStage, 0755); err != nil {
		t.Fatal(err)
	}
	stageIdentity, err := os.Stat(outsideStage)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outsideStage, "marker")
	if err := os.WriteFile(marker, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".doggo")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	stage := filepath.Join(root, ".doggo", "generations", ".stage-1")
	destination := filepath.Join(root, ".doggo", "generations", strings.Repeat("a", 64))
	if err := PublishDirectory(stage, destination); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("PublishDirectory error = %v, want symlink-parent rejection", err)
	}
	if _, err := os.Stat(filepath.Join(outsideGenerations, strings.Repeat("a", 64))); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("outside destination exists: %v", err)
	}
	after, err := os.Stat(outsideStage)
	if err != nil || !os.SameFile(stageIdentity, after) {
		t.Fatalf("outside stage identity changed: %v", err)
	}
	if content, err := os.ReadFile(marker); err != nil || string(content) != "outside" {
		t.Fatalf("outside stage content changed: %q, %v", content, err)
	}
}

func TestPublishDirectoryRejectsPathsOutsideOneGenerationsParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, ".doggo", "generations")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(parent, ".stage-1")
	if err := os.Mkdir(stage, 0755); err != nil {
		t.Fatal(err)
	}
	if err := PublishDirectory(stage, filepath.Join(root, strings.Repeat("a", 64))); err == nil {
		t.Fatal("PublishDirectory accepted destination outside generations parent")
	}
}

func TestWriteLockFileAtomicTempWriteFailurePreservesOldLock(t *testing.T) {
	path, oldBytes := writeOldLock(t)
	want := errors.New("injected temp write failure")
	original := atomicTempWrite
	atomicTempWrite = func(*os.File, []byte) (int, error) { return 0, want }
	t.Cleanup(func() { atomicTempWrite = original })

	err := WriteLockFileAtomic(path, testGenerationLock())
	assertPreRenameLockFailure(t, path, oldBytes, want, err)
}

func TestWriteLockFileAtomicTempSyncFailurePreservesOldLock(t *testing.T) {
	path, oldBytes := writeOldLock(t)
	want := errors.New("injected temp sync failure")
	original := atomicTempSync
	atomicTempSync = func(*os.File) error { return want }
	t.Cleanup(func() { atomicTempSync = original })

	err := WriteLockFileAtomic(path, testGenerationLock())
	assertPreRenameLockFailure(t, path, oldBytes, want, err)
}

func TestWriteLockFileAtomicRenameFailurePreservesOldLock(t *testing.T) {
	path, oldBytes := writeOldLock(t)
	want := errors.New("injected rename failure")
	original := atomicRootRename
	atomicRootRename = func(*os.File, *os.File, string, string) error { return want }
	t.Cleanup(func() { atomicRootRename = original })

	err := WriteLockFileAtomic(path, testGenerationLock())
	assertPreRenameLockFailure(t, path, oldBytes, want, err)
}

func TestWriteLockFileAtomicKeepsCommitInHeldRootWhenPathIsReplaced(t *testing.T) {
	base := t.TempDir()
	projectRoot := filepath.Join(base, "project")
	movedRoot := filepath.Join(base, "project-original")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{projectRoot, outside} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(projectRoot, LockFileName)
	old := testGenerationLock()
	old.Generation = strings.Repeat("c", 64)
	if err := WriteLockFile(path, old); err != nil {
		t.Fatal(err)
	}
	lock := testGenerationLock()
	original := atomicRootRename
	replaced := false
	atomicRootRename = func(parent, temp *os.File, oldName, newName string) error {
		replaced = renameRetainedForTest(t, projectRoot, movedRoot)
		if replaced {
			if err := os.Symlink(outside, projectRoot); err != nil {
				return err
			}
		}
		return original(parent, temp, oldName, newName)
	}
	t.Cleanup(func() { atomicRootRename = original })

	err := WriteLockFileAtomic(path, lock)
	var uncertain *CommitUncertainError
	if replaced && (!errors.As(err, &uncertain) || uncertain.Generation != lock.Generation) {
		t.Fatalf("error = %T %v, want uncertainty for %s", err, err, lock.Generation)
	}
	if !replaced && err != nil {
		t.Fatalf("commit after blocked root replacement: %v", err)
	}
	committedRoot := projectRoot
	if replaced {
		committedRoot = movedRoot
	}
	content, readErr := os.ReadFile(filepath.Join(committedRoot, LockFileName))
	if readErr != nil {
		t.Fatal(readErr)
	}
	want := canonicalLockString(lock) + "\n"
	if string(content) != want {
		t.Fatalf("held-root lock = %q, want %q", content, want)
	}
	if _, statErr := os.Lstat(filepath.Join(outside, LockFileName)); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("replacement/outside lock exists: %v", statErr)
	}
}

func TestWriteLockFileAtomicParentSyncFailureInstallsNewLock(t *testing.T) {
	path, oldBytes := writeOldLock(t)
	lock := testGenerationLock()
	want := errors.New("injected project directory sync failure")
	original := atomicParentSync
	atomicParentSync = func(*os.File) error { return want }
	t.Cleanup(func() { atomicParentSync = original })

	err := WriteLockFileAtomic(path, lock)
	var uncertain *CommitUncertainError
	if !errors.As(err, &uncertain) {
		t.Fatalf("error = %T %v, want *CommitUncertainError", err, err)
	}
	if uncertain.Generation != lock.Generation || !errors.Is(err, want) {
		t.Fatalf("uncertain error = %#v, want generation %q and injected cause", uncertain, lock.Generation)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	wantBytes := []byte(canonicalLockString(lock) + "\n")
	if string(content) != string(wantBytes) || string(content) == string(oldBytes) {
		t.Fatalf("visible lock = %q, want newly committed %q", content, wantBytes)
	}
	loaded, loadErr := LoadLockFile(path)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if loaded.Generation != lock.Generation {
		t.Fatalf("reloaded generation = %q, want %q", loaded.Generation, lock.Generation)
	}
	assertNoLockTemps(t, path)
}

func TestWriteLockFileAtomicWritesCanonicalLock(t *testing.T) {
	lock := testGenerationLock()
	path := filepath.Join(t.TempDir(), LockFileName)
	if err := WriteLockFileAtomic(path, lock); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := canonicalLockString(lock) + "\n"
	if string(content) != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}

func TestWriteLockFileAtomicRejectsSymlinkProjectDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(outside, linked); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := WriteLockFileAtomic(filepath.Join(linked, LockFileName), testGenerationLock()); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("WriteLockFileAtomic error = %v, want symlink rejection", err)
	}
	if _, err := os.Stat(filepath.Join(outside, LockFileName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("outside lock exists: %v", err)
	}
}

func TestCommitUncertainErrorSupportsErrorsAs(t *testing.T) {
	cause := errors.New("sync failed")
	err := error(&CommitUncertainError{Generation: strings.Repeat("a", 64), Cause: cause})
	var uncertain *CommitUncertainError
	if !errors.As(err, &uncertain) || !errors.Is(err, cause) {
		t.Fatalf("error chain does not expose uncertainty and cause: %v", err)
	}
}

func TestIsLowerHexDigest(t *testing.T) {
	valid := strings.Repeat("0123456789abcdef", 4)
	for _, tt := range []struct {
		value string
		want  bool
	}{
		{value: valid, want: true},
		{value: strings.ToUpper(valid), want: false},
		{value: valid[:63], want: false},
		{value: valid + "0", want: false},
		{value: strings.Repeat("g", 64), want: false},
	} {
		if got := IsLowerHexDigest(tt.value); got != tt.want {
			t.Errorf("IsLowerHexDigest(%q) = %t, want %t", tt.value, got, tt.want)
		}
	}
}

func testGenerationLock() *LockFile {
	return &LockFile{
		LockVersion:         LockVersion,
		Generation:          strings.Repeat("a", 64),
		ModLockInputsSHA256: strings.Repeat("b", 64),
		Deps:                []LockDep{},
		Scan:                LockScan{GoPackages: []string{}, ReifyTargets: []ReifyTarget{}},
		Build:               LockBuild{},
	}
}

func writeOldLock(t *testing.T) (string, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), LockFileName)
	old := testGenerationLock()
	old.Generation = strings.Repeat("c", 64)
	if err := WriteLockFile(path, old); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, content
}

func assertPreRenameLockFailure(t *testing.T, path string, oldBytes []byte, cause, err error) {
	t.Helper()
	var uncertain *CommitUncertainError
	if errors.As(err, &uncertain) {
		t.Fatalf("pre-rename error classified as uncertain: %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want injected cause", err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != string(oldBytes) {
		t.Fatalf("old lock changed: got %q, want %q", content, oldBytes)
	}
	assertNoLockTemps(t, path)
}

func assertNoLockTemps(t *testing.T, path string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary lock files remain: %v", matches)
	}
}
