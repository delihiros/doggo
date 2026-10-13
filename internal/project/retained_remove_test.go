package project

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestValidRetainedNameRejectsNonComponents(t *testing.T) {
	for _, name := range []string{"", ".", "..", string(filepath.Separator), "dir" + string(filepath.Separator) + "entry"} {
		if validRetainedName(name) {
			t.Errorf("validRetainedName(%q) = true, want false", name)
		}
	}
}

func TestRetainedDispositionResultKeepsOwnershipErrorWhenDispositionFails(t *testing.T) {
	dispositionErr := errors.New("set disposition failed")
	err := retainedDispositionResult("owned", true, fs.ErrNotExist, dispositionErr)
	if !errors.Is(err, ErrOwnershipChanged) || !errors.Is(err, dispositionErr) {
		t.Fatalf("retainedDispositionResult error = %v, want ownership and disposition failures", err)
	}
}

func TestFinishOwnedRemovalDropsLedgerEntriesAfterCommittedCloseError(t *testing.T) {
	closeErr := errors.New("close retained handle")
	removeErr := joinRetainedRemovalClose(nil, true, closeErr)
	files := map[string]fs.FileInfo{"file": nil}
	directories := map[string]*ownedTreeDirectory{"directory": nil}

	if err := finishOwnedRemoval(files, "file", removeErr); !errors.Is(err, closeErr) {
		t.Fatalf("file removal error = %v, want close failure", err)
	}
	if err := errors.Join(finishOwnedRemoval(directories, "directory", nil), closeErr); !errors.Is(err, closeErr) {
		t.Fatalf("directory removal error = %v, want close failure", err)
	}
	if _, ok := files["file"]; ok {
		t.Fatal("committed file remained in ownership ledger")
	}
	if _, ok := directories["directory"]; ok {
		t.Fatal("committed directory remained in ownership ledger")
	}
}
