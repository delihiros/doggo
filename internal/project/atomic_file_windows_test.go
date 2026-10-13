//go:build windows

package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/windows"
)

func TestAtomicWindowsPublishesAbsentOwnedFile(t *testing.T) {
	root, err := NewOwnedTempRoot(".doggo-atomic-windows-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("cleanup temporary root: %v", err)
		}
	})

	if err := root.Tree().WriteOwnedFile("go.sum", []byte("owned"), 0644); err != nil {
		t.Fatal(err)
	}
	content, err := root.Tree().ReadFile("go.sum")
	if err != nil || string(content) != "owned" {
		t.Fatalf("published content = %q, %v", content, err)
	}
	if err := root.Tree().VerifyComplete(); err != nil {
		t.Fatalf("verify complete tree: %v", err)
	}
	if err := root.Close(); err != nil {
		t.Fatalf("cleanup temporary root: %v", err)
	}
}

func TestAtomicWindowsNoReplacePreservesRacingFile(t *testing.T) {
	root, err := NewOwnedTempRoot(".doggo-atomic-windows-")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root.Path(), "go.sum")
	var racingInfo fs.FileInfo
	t.Cleanup(func() {
		if current, err := os.Stat(destination); err == nil && racingInfo != nil && os.SameFile(racingInfo, current) {
			_ = os.Remove(destination)
		}
		if err := root.Close(); err != nil {
			t.Errorf("cleanup temporary root: %v", err)
		}
	})

	originalSync := atomicTempSync
	atomicTempSync = func(file *os.File) error {
		if err := originalSync(file); err != nil {
			return err
		}
		if err := os.WriteFile(destination, []byte("racer"), 0644); err != nil {
			return err
		}
		racingInfo, err = os.Stat(destination)
		return err
	}
	t.Cleanup(func() { atomicTempSync = originalSync })

	err = root.Tree().WriteOwnedFile("go.sum", []byte("owned"), 0644)
	content, readErr := os.ReadFile(destination)
	if readErr != nil || string(content) != "racer" {
		t.Fatalf("racing destination content = %q, %v", content, readErr)
	}
	current, statErr := os.Stat(destination)
	if statErr != nil || racingInfo == nil || !os.SameFile(racingInfo, current) {
		t.Fatalf("racing destination identity changed: %v", statErr)
	}
	if !errors.Is(err, fs.ErrExist) {
		t.Errorf("WriteOwnedFile error = %v, want fs.ErrExist", err)
	}
	var atomicErr *AtomicWriteError
	if !errors.As(err, &atomicErr) {
		t.Fatalf("WriteOwnedFile error = %T %v, want *AtomicWriteError", err, err)
	}
	if atomicErr.Renamed {
		t.Errorf("AtomicWriteError.Renamed = true, want false")
	}

	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := root.Tree().VerifyComplete(); err != nil {
		t.Fatalf("temporary file cleanup: %v", err)
	}
	if err := root.Close(); err != nil {
		t.Fatalf("cleanup temporary root: %v", err)
	}
}

func TestWriteFileAtomicWindowsDurableReplace(t *testing.T) {
	dir := t.TempDir()
	to := filepath.Join(dir, "to")
	if err := os.WriteFile(to, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomic(to, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(to)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("content = %q, want new", content)
	}
}

func TestAtomicWindowsReplaceFallsBackToLegacyInformation(t *testing.T) {
	dir := t.TempDir()
	to := filepath.Join(dir, "to")
	if err := os.WriteFile(to, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	original := atomicNtSetInformationFile
	var classes []uint32
	atomicNtSetInformationFile = func(handle windows.Handle, status *windows.IO_STATUS_BLOCK, buffer *byte, length, class uint32) error {
		classes = append(classes, class)
		if class == 65 {
			return windows.STATUS_INVALID_INFO_CLASS
		}
		return original(handle, status, buffer, length, class)
	}
	t.Cleanup(func() { atomicNtSetInformationFile = original })

	if err := WriteFileAtomic(to, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(to); err != nil || string(content) != "new" {
		t.Fatalf("replacement content = %q, %v", content, err)
	}
	if want := []uint32{65, windows.FileRenameInformation}; !slices.Equal(classes, want) {
		t.Fatalf("rename information classes = %v, want %v", classes, want)
	}
}
