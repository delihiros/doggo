package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerationLeaseRejectsSymlinkProjectRootWithoutMutation(t *testing.T) {
	base := t.TempDir()
	target := t.TempDir()
	linked := filepath.Join(base, "project")
	if err := os.Symlink(target, linked); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if lease, err := AcquireGenerationLease(linked, true); err == nil {
		_ = lease.Close()
		t.Fatal("AcquireGenerationLease accepted symlink project root")
	}
	if _, err := os.Lstat(filepath.Join(target, ".doggo")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("symlink target mutated: %v", err)
	}
}

func TestGenerationLeaseSharedMissingMetadataDoesNotCreateIt(t *testing.T) {
	t.Run("doggo directory", func(t *testing.T) {
		root := t.TempDir()
		if lease, err := AcquireGenerationLease(root, false); err == nil {
			_ = lease.Close()
			t.Fatal("shared lease created missing metadata")
		}
		if _, err := os.Lstat(filepath.Join(root, ".doggo")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("shared lease mutated project: %v", err)
		}
	})

	t.Run("lock file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".doggo"), 0755); err != nil {
			t.Fatal(err)
		}
		if lease, err := AcquireGenerationLease(root, false); err == nil {
			_ = lease.Close()
			t.Fatal("shared lease created missing lock file")
		}
		entries, err := os.ReadDir(filepath.Join(root, ".doggo"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("shared lease created metadata: %v", entries)
		}
	})
}

func TestGenerationLeaseExclusiveCreatesVerifiedMetadata(t *testing.T) {
	root := t.TempDir()
	lease, err := AcquireGenerationLease(root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	info, err := os.Lstat(filepath.Join(root, ".doggo", "generation.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("generation lock mode = %s", info.Mode())
	}
}

func TestGenerationLeaseSharedLeasesBlockExclusiveUntilBothClose(t *testing.T) {
	root := t.TempDir()
	created, err := AcquireGenerationLease(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := created.Close(); err != nil {
		t.Fatal(err)
	}
	first, err := AcquireGenerationLease(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := AcquireGenerationLease(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	acquired := make(chan *GenerationLease, 1)
	errs := make(chan error, 1)
	go func() {
		lease, err := AcquireGenerationLease(root, true)
		if err != nil {
			errs <- err
			return
		}
		acquired <- lease
	}()

	assertLeaseBlocked(t, acquired, errs)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	assertLeaseBlocked(t, acquired, errs)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	lease := awaitLease(t, acquired, errs)
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationLeaseExclusiveBlocksShared(t *testing.T) {
	root := t.TempDir()
	exclusive, err := AcquireGenerationLease(root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer exclusive.Close()

	acquired := make(chan *GenerationLease, 1)
	errs := make(chan error, 1)
	go func() {
		lease, err := AcquireGenerationLease(root, false)
		if err != nil {
			errs <- err
			return
		}
		acquired <- lease
	}()

	assertLeaseBlocked(t, acquired, errs)
	if err := exclusive.Close(); err != nil {
		t.Fatal(err)
	}
	lease := awaitLease(t, acquired, errs)
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationLeaseCloseIsIdempotent(t *testing.T) {
	lease, err := AcquireGenerationLease(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestGenerationLeaseRejectsDoggoReplacementWhileWaiting(t *testing.T) {
	root := t.TempDir()
	holder, err := AcquireGenerationLease(root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()

	acquired, errs := startLeaseWaiterForTest(t, root, holder)
	assertLeaseBlocked(t, acquired, errs)

	doggo := filepath.Join(root, ".doggo")
	if !renameRetainedForTest(t, doggo, filepath.Join(root, ".doggo-old")) {
		assertLeaseAfterBlockedReplacement(t, holder, acquired, errs)
		return
	}
	if err := os.Mkdir(doggo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := holder.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case lease := <-acquired:
		_ = lease.Close()
		t.Fatal("lease returned for replaced .doggo directory")
	case err := <-errs:
		if err == nil {
			t.Fatal("nil replacement error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for replacement detection")
	}
}

func TestGenerationLeaseRejectsLockReplacementWhileWaiting(t *testing.T) {
	root := t.TempDir()
	holder, err := AcquireGenerationLease(root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()

	acquired, errs := startLeaseWaiterForTest(t, root, holder)
	assertLeaseBlocked(t, acquired, errs)

	path := filepath.Join(root, ".doggo", "generation.lock")
	if !renameRetainedForTest(t, path, path+".old") {
		assertLeaseAfterBlockedReplacement(t, holder, acquired, errs)
		return
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := holder.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case lease := <-acquired:
		_ = lease.Close()
		t.Fatal("lease returned for replaced generation.lock")
	case err := <-errs:
		if err == nil {
			t.Fatal("nil replacement error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for replacement detection")
	}
}

func startLeaseWaiterForTest(t *testing.T, root string, holder *GenerationLease) (<-chan *GenerationLease, <-chan error) {
	t.Helper()
	acquired := make(chan *GenerationLease, 1)
	errs := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		acquireLease(root, true, acquired, errs)
	}()
	t.Cleanup(func() {
		if err := holder.Close(); err != nil {
			t.Error(err)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("lease waiter did not exit during cleanup")
			return
		}
		select {
		case lease := <-acquired:
			if err := lease.Close(); err != nil {
				t.Error(err)
			}
		default:
		}
	})
	return acquired, errs
}

func assertLeaseAfterBlockedReplacement(t *testing.T, holder *GenerationLease, acquired <-chan *GenerationLease, errs <-chan error) {
	t.Helper()
	if err := holder.Close(); err != nil {
		t.Fatal(err)
	}
	lease := awaitLease(t, acquired, errs)
	defer lease.Close()
	if err := lease.verifyProject(); err != nil {
		t.Fatalf("blocked replacement changed the acquired lease: %v", err)
	}
}

func acquireLease(root string, exclusive bool, acquired chan<- *GenerationLease, errs chan<- error) {
	lease, err := AcquireGenerationLease(root, exclusive)
	if err != nil {
		errs <- err
		return
	}
	acquired <- lease
}

func assertLeaseBlocked(t *testing.T, acquired <-chan *GenerationLease, errs <-chan error) {
	t.Helper()
	select {
	case lease := <-acquired:
		_ = lease.Close()
		t.Fatal("lease acquired while conflicting lease was held")
	case err := <-errs:
		t.Fatalf("lease acquisition failed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func awaitLease(t *testing.T, acquired <-chan *GenerationLease, errs <-chan error) *GenerationLease {
	t.Helper()
	select {
	case lease := <-acquired:
		return lease
	case err := <-errs:
		t.Fatalf("lease acquisition failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for lease")
	}
	return nil
}
