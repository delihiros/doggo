package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOpenGenerationSessionReadsVerifiedProjectFileDescriptors(t *testing.T) {
	for _, target := range []string{ModFileName, LockFileName} {
		t.Run(target, func(t *testing.T) {
			root, _, lock := writeValidGeneration(t, false)
			replacement := filepath.Join(t.TempDir(), target)
			if target == ModFileName {
				if err := WriteModFile(replacement, &ModFile{Module: "substituted", Deps: []string{}}); err != nil {
					t.Fatal(err)
				}
			} else {
				bad := cloneLock(lock)
				bad.Generation = strings.Repeat("f", 64)
				if err := WriteLockFile(replacement, bad); err != nil {
					t.Fatal(err)
				}
			}

			original := filepath.Join(root, target)
			backup := original + ".verified"
			swapped := false
			afterProjectFileOpenForTest = func(name string) {
				if name != target {
					return
				}
				if err := os.Rename(original, backup); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, original); err != nil {
					t.Fatal(err)
				}
				swapped = true
			}
			afterProjectFileReadForTest = func(name string) {
				if name != target || !swapped {
					return
				}
				if err := os.Rename(original, replacement); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(backup, original); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				afterProjectFileOpenForTest = nil
				afterProjectFileReadForTest = nil
			})

			session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if !swapped {
				t.Fatal("test did not substitute project file")
			}
			if session.Config.Lock.Generation != lock.Generation {
				t.Fatalf("session read substituted %s", target)
			}
		})
	}
}

func TestOpenGenerationSessionReturnsRetainedBinaryWithoutBlockingWriter(t *testing.T) {
	root, _, lock := writeValidGeneration(t, true)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	wantPaths := []string{root}
	if !reflect.DeepEqual(session.Config.LoadPaths, wantPaths) {
		t.Fatalf("load paths = %v, want %v", session.Config.LoadPaths, wantPaths)
	}
	if len(session.Config.GenerationSources) != 1 {
		t.Fatalf("generation sources = %d, want 1", len(session.Config.GenerationSources))
	}
	content, err := session.Config.GenerationSources[0].ReadFile("lib/core.dog")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "(ns core)\n" {
		t.Fatalf("generation source = %q", content)
	}
	wantDisplay := "generation://" + lock.Generation + "/" + lock.Deps[0].InstallDir + "/lib/core.dog"
	if got := session.Config.GenerationSources[0].DisplayPath("lib/core.dog"); got != wantDisplay {
		t.Fatalf("display path = %q, want %q", got, wantDisplay)
	}
	if !session.HasBinary() {
		t.Fatal("built generation has no retained binary")
	}
	if runtime.GOOS != "windows" {
		command, err := session.BinaryCommand("first", "second")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := command.Args[1:], []string{"first", "second"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("binary command user arguments = %v, want %v", got, want)
		}
	}

	acquired := make(chan *GenerationLease, 1)
	errs := make(chan error, 1)
	go acquireLease(root, true, acquired, errs)
	lease := awaitLease(t, acquired, errs)
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := session.Config.GenerationSources[0].ReadFile("lib/core.dog"); err == nil {
		t.Fatal("closed generation source accepted read")
	}
	if session.HasBinary() {
		t.Fatal("closed generation session retained binary")
	}
	if _, err := session.BinaryCommand(); err == nil {
		t.Fatal("closed generation session created binary command")
	}
}

func TestGenerationSessionBinarySnapshotReturnsFreshContent(t *testing.T) {
	root, _, lock := writeValidGeneration(t, true)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	first, err := session.BinarySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if first.ProjectPath != lock.Build.Binary || first.SHA256 != lock.Build.SHA256 || string(first.Content) != "doggo binary" {
		t.Fatalf("binary snapshot = %#v", first)
	}
	first.Content[0] = 'X'
	second, err := session.BinarySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if string(second.Content) != "doggo binary" {
		t.Fatalf("second binary snapshot reused mutable content: %q", second.Content)
	}
}

func TestGenerationSessionBinarySnapshotRejectsUnavailableSession(t *testing.T) {
	var nilSession *GenerationSession
	if _, err := nilSession.BinarySnapshot(); err == nil || !strings.Contains(err.Error(), "generation session is closed") {
		t.Fatalf("nil session error = %v", err)
	}

	root, _, _ := writeValidGeneration(t, true)
	closed, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := closed.BinarySnapshot(); err == nil || !strings.Contains(err.Error(), "generation session is closed") {
		t.Fatalf("closed session error = %v", err)
	}

	root, _, _ = writeValidGeneration(t, false)
	syncOnly, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer syncOnly.Close()
	if _, err := syncOnly.BinarySnapshot(); err == nil || !strings.Contains(err.Error(), "doggo build") {
		t.Fatalf("sync-only session error = %v", err)
	}
}

func TestOpenGenerationSessionIncludesProjectSrc(t *testing.T) {
	root, _, lock := writeValidGeneration(t, false)
	src := filepath.Join(root, "src")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}
	session, err := OpenGenerationSession(filepath.Join(root, "test", "demo_test.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	want := []string{root, src}
	if !reflect.DeepEqual(session.Config.LoadPaths, want) {
		t.Fatalf("load paths = %v, want %v", session.Config.LoadPaths, want)
	}
	if len(session.Config.GenerationSources) != len(lock.Deps) {
		t.Fatalf("generation sources = %d, want %d", len(session.Config.GenerationSources), len(lock.Deps))
	}
	if session.HasBinary() {
		t.Fatal("sync-only generation has retained binary")
	}
	if _, err := session.BinaryCommand(); err == nil {
		t.Fatal("sync-only generation created binary command")
	}
}

func TestGenerationSessionBinaryCommandCloseRace(t *testing.T) {
	for range 10 {
		root, _, _ := writeValidGeneration(t, true)
		session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		type commandResult struct {
			hasCommand bool
			err        error
		}
		result := make(chan commandResult, 1)
		closeErr := make(chan error, 1)
		go func() {
			<-start
			command, err := session.BinaryCommand()
			result <- commandResult{command != nil, err}
		}()
		go func() {
			<-start
			closeErr <- session.Close()
		}()
		close(start)
		got := <-result
		if err := <-closeErr; err != nil {
			t.Fatal(err)
		}
		if got.hasCommand == (got.err != nil) {
			t.Fatalf("BinaryCommand returned command=%v, error=%v", got.hasCommand, got.err)
		}
	}
}

func TestGenerationSourceNeverReadsReplacement(t *testing.T) {
	root, _, lock := writeValidGeneration(t, false)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if len(session.Config.GenerationSources) != 1 {
		t.Fatalf("generation sources = %d, want 1", len(session.Config.GenerationSources))
	}
	dep := filepath.Join(generationPath(root, lock.Generation), filepath.FromSlash(lock.Deps[0].InstallDir))
	original := filepath.Join(dep, "lib", "core.dog")
	backup := original + ".verified"
	if err := os.Rename(original, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, []byte("(ns compromised)\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if content, err := session.Config.GenerationSources[0].ReadFile("lib/core.dog"); err == nil {
		t.Fatalf("read replacement: %q", content)
	} else if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("corruption reported as missing: %v", err)
	} else if !strings.Contains(err.Error(), lock.Deps[0].InstallDir+"/lib/core.dog") {
		t.Fatalf("integrity error = %v, want generation-relative path", err)
	}
}

func TestGenerationSourceReportsDeletedManifestFileAsIntegrityError(t *testing.T) {
	root, _, lock := writeValidGeneration(t, false)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	relative := lock.Deps[0].InstallDir + "/lib/core.dog"
	physical := filepath.Join(generationPath(root, lock.Generation), filepath.FromSlash(relative))
	if err := os.Remove(physical); err != nil {
		t.Fatal(err)
	}
	_, err = session.Config.GenerationSources[0].ReadFile("lib/core.dog")
	if err == nil {
		t.Fatal("ReadFile accepted deleted manifested file")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("deleted manifested file reported as missing: %v", err)
	}
	if !strings.Contains(err.Error(), relative) {
		t.Fatalf("integrity error = %v, want generation-relative path %q", err, relative)
	}
}

func TestGenerationSourceReadsOpenedFileAfterPathReplacement(t *testing.T) {
	root, _, lock := writeValidGeneration(t, false)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	dep := filepath.Join(generationPath(root, lock.Generation), filepath.FromSlash(lock.Deps[0].InstallDir))
	original := filepath.Join(dep, "lib", "core.dog")
	backup := original + ".opened"
	fired := false
	afterGenerationSourceFileOpenForTest = func(name string) {
		if name != "lib/core.dog" {
			return
		}
		fired = true
		if err := os.Rename(original, backup); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(original, []byte("(ns replacement)\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterGenerationSourceFileOpenForTest = nil })

	content, err := session.Config.GenerationSources[0].ReadFile("lib/core.dog")
	if !fired {
		t.Fatal("post-open seam did not run")
	}
	if err != nil {
		if !strings.Contains(err.Error(), "integrity") {
			t.Fatalf("ReadFile error = %v, want integrity failure", err)
		}
		return
	}
	if string(content) != "(ns core)\n" {
		t.Fatalf("ReadFile returned replacement: %q", content)
	}
}

func TestGenerationSessionCloseWaitsForReadAndIsConcurrentSafe(t *testing.T) {
	root, _, _ := writeValidGeneration(t, false)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	opened := make(chan struct{})
	releaseRead := make(chan struct{})
	var releaseOnce sync.Once
	unblockRead := func() { releaseOnce.Do(func() { close(releaseRead) }) }
	t.Cleanup(unblockRead)
	afterGenerationSourceFileOpenForTest = func(name string) {
		if name == "lib/core.dog" {
			close(opened)
			<-releaseRead
		}
	}
	t.Cleanup(func() { afterGenerationSourceFileOpenForTest = nil })

	type readResult struct {
		content []byte
		err     error
	}
	readDone := make(chan readResult, 1)
	go func() {
		content, err := session.Config.GenerationSources[0].ReadFile("lib/core.dog")
		readDone <- readResult{content: content, err: err}
	}()
	select {
	case <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("ReadFile did not reach post-open seam")
	}

	startClose := make(chan struct{})
	closeDone := make(chan error, 2)
	for range 2 {
		go func() {
			<-startClose
			closeDone <- session.Close()
		}()
	}
	close(startClose)

	select {
	case err := <-closeDone:
		unblockRead()
		t.Fatalf("Close completed during retained read: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	acquired := make(chan *GenerationLease, 1)
	leaseErrs := make(chan error, 1)
	go acquireLease(root, true, acquired, leaseErrs)
	lease := awaitLease(t, acquired, leaseErrs)
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}

	unblockRead()
	var read readResult
	select {
	case read = <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("ReadFile did not finish")
	}
	for range 2 {
		select {
		case err := <-closeDone:
			if err != nil {
				t.Fatalf("concurrent Close: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Close did not finish")
		}
	}
	if read.err != nil || string(read.content) != "(ns core)\n" {
		t.Fatalf("ReadFile = %q, %v", read.content, read.err)
	}
}

func TestGenerationSessionCopyAuditWorkspaceToUsesVerifiedRetainedFiles(t *testing.T) {
	root, _, lock := writeValidGeneration(t, false)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	destination, err := NewOwnedTempRoot("doggo-copy-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()

	generationRoot := generationPath(root, lock.Generation)
	retainedRoot := generationRoot + "-retained"
	if renameRetainedForTest(t, generationRoot, retainedRoot) {
		if err := os.MkdirAll(filepath.Join(generationRoot, "toolchain"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(generationRoot, "toolchain", "main.go"), []byte("replacement\n"), 0755); err != nil {
			t.Fatal(err)
		}
	} else {
		retainedRoot = generationRoot
	}

	if err := session.CopyAuditWorkspaceTo(destination.Tree()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination.Path(), "toolchain", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n" {
		t.Fatalf("copied toolchain = %q, want retained bytes", got)
	}
	gotInfo, err := os.Stat(filepath.Join(destination.Path(), "toolchain", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	wantInfo, err := os.Stat(filepath.Join(retainedRoot, "toolchain", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if gotInfo.Mode().Perm() != wantInfo.Mode().Perm() {
		t.Fatalf("copied mode = %v, want %v", gotInfo.Mode().Perm(), wantInfo.Mode().Perm())
	}
}

func TestGenerationSessionCopyAuditWorkspaceToRejectsReplacedToolchainDirectory(t *testing.T) {
	root, _, lock := writeValidGeneration(t, false)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	destination, err := NewOwnedTempRoot("doggo-copy-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()

	toolchain := filepath.Join(generationPath(root, lock.Generation), "toolchain")
	retained := toolchain + "-retained"
	fired := false
	afterGenerationAuditWorkspaceVerifyForTest = func() {
		if fired {
			return
		}
		fired = true
		if err := os.Rename(toolchain, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(toolchain, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(filepath.Join(retained, "main.go"), filepath.Join(toolchain, "main.go")); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterGenerationAuditWorkspaceVerifyForTest = nil })

	copyErr := session.CopyAuditWorkspaceTo(destination.Tree())
	if !fired {
		t.Fatal("toolchain replacement seam did not run")
	}
	if _, err := os.Stat(filepath.Join(toolchain, "main.go")); err != nil {
		t.Fatalf("replacement toolchain was not preserved: %v", err)
	}
	if copyErr == nil {
		t.Fatal("CopyAuditWorkspaceTo accepted a replaced toolchain directory with validated hard links")
	}
}

func TestGenerationSessionCopyAuditWorkspaceToRejectsUnknownFiles(t *testing.T) {
	root, _, lock := writeValidGeneration(t, false)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	destination, err := NewOwnedTempRoot("doggo-copy-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()

	unknown := filepath.Join(generationPath(root, lock.Generation), "toolchain", "unknown.go")
	if err := os.WriteFile(unknown, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := session.CopyAuditWorkspaceTo(destination.Tree()); err == nil {
		t.Fatal("CopyAuditWorkspaceTo accepted an unmanifested toolchain file")
	}
}

func TestGenerationSessionCopyAuditWorkspaceToCleansPublishedFilesAfterLaterFailure(t *testing.T) {
	root, _, lock := writeValidGeneration(t, false)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	destination, err := NewOwnedTempRoot("doggo-copy-test-")
	if err != nil {
		t.Fatal(err)
	}
	destinationPath := destination.Path()

	want := errors.New("later audit workspace write failed")
	originalSync := atomicTempSync
	calls := 0
	atomicTempSync = func(file *os.File) error {
		calls++
		if calls == 2 {
			return want
		}
		return originalSync(file)
	}
	t.Cleanup(func() { atomicTempSync = originalSync })

	copyErr := session.CopyAuditWorkspaceTo(destination.Tree())
	if _, err := os.Stat(filepath.Join(destinationPath, filepath.FromSlash(lock.Deps[0].InstallDir), "doggo.mod")); err != nil {
		t.Fatalf("first audit workspace file was not published: %v", err)
	}
	closeErr := destination.Close()
	if !errors.Is(copyErr, want) {
		t.Fatalf("copy error = %v, want %v", copyErr, want)
	}
	if closeErr != nil {
		t.Fatalf("cleanup after copy error: %v", closeErr)
	}
	if _, err := os.Stat(destinationPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial audit workspace remained after cleanup: %v", err)
	}
}

func TestGenerationSessionCloseWaitsForAuditWorkspaceCopy(t *testing.T) {
	root, _, _ := writeValidGeneration(t, false)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	destination, err := NewOwnedTempRoot("doggo-copy-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()

	opened := make(chan struct{})
	releaseCopy := make(chan struct{})
	afterGenerationSourceFileOpenForTest = func(name string) {
		if name == "toolchain/main.go" {
			close(opened)
			<-releaseCopy
		}
	}
	t.Cleanup(func() { afterGenerationSourceFileOpenForTest = nil })

	copyDone := make(chan error, 1)
	go func() { copyDone <- session.CopyAuditWorkspaceTo(destination.Tree()) }()
	select {
	case <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("CopyAuditWorkspaceTo did not reach retained read")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- session.Close() }()
	select {
	case err := <-closeDone:
		close(releaseCopy)
		t.Fatalf("Close completed during toolchain copy: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseCopy)
	if err := <-copyDone; err != nil {
		t.Fatal(err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
}

func TestGenerationSourceRejectsInvalidAndUnmanifestedNames(t *testing.T) {
	root, _, _ := writeValidGeneration(t, false)
	session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	source := session.Config.GenerationSources[0]

	for _, name := range []string{"/lib/core.dog", "../lib/core.dog", "lib/../../core.dog"} {
		if _, err := source.ReadFile(name); err == nil || errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("ReadFile(%q) error = %v, want invalid path", name, err)
		}
	}
	if _, err := source.ReadFile("lib/missing.dog"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file error = %v, want fs.ErrNotExist", err)
	}
}

func TestOpenGenerationSessionClosesLeaseOnSetupError(t *testing.T) {
	root, _, _ := writeValidGeneration(t, false)
	if err := os.WriteFile(LockPath(root), []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}
	if session, err := OpenGenerationSession(filepath.Join(root, "main.dog")); err == nil {
		_ = session.Close()
		t.Fatal("OpenGenerationSession accepted corrupt lock")
	}

	acquired := make(chan *GenerationLease, 1)
	errs := make(chan error, 1)
	go acquireLease(root, true, acquired, errs)
	select {
	case lease := <-acquired:
		_ = lease.Close()
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("setup error leaked shared lease")
	}
}

func TestOpenGenerationSessionRejectsCorruptionWithoutRepair(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(string, *LockFile) error
	}{
		{"deleted file", func(generationRoot string, _ *LockFile) error {
			return os.Remove(filepath.Join(generationRoot, "toolchain", "main.go"))
		}},
		{"modified file", func(generationRoot string, _ *LockFile) error {
			return os.WriteFile(filepath.Join(generationRoot, "toolchain", "main.go"), []byte("corrupt"), 0644)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, _, lock := writeValidGeneration(t, false)
			if err := tt.mutate(generationPath(root, lock.Generation), lock); err != nil {
				t.Fatal(err)
			}
			before := snapshotTree(t, root)
			session, err := OpenGenerationSession(filepath.Join(root, "main.dog"))
			if err == nil {
				_ = session.Close()
				t.Fatal("OpenGenerationSession accepted corrupt generation")
			}
			if !strings.Contains(err.Error(), "stale or corrupt") {
				t.Fatalf("error = %v, want stale or corrupt", err)
			}
			after := snapshotTree(t, root)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("read repaired or changed project:\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func snapshotTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		line := filepath.ToSlash(rel) + ":" + info.Mode().String()
		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			line += ":" + string(content)
		}
		out = append(out, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}
