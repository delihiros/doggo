//go:build windows

package project

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSocketFixturePreventsEntryReplacement(t *testing.T) {
	for _, operation := range []string{"rename", "remove", "replace"} {
		t.Run(operation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "socket")
			handle, verifySocket := createSocketFixtureForTest(t, path)
			before := socketInfoForTest(t, filepath.Dir(path))
			if err := verifySocket(before, before); err != nil {
				t.Fatal(err)
			}
			// Remove the tag through the retained handle so ERROR_CANT_ACCESS_FILE
			// cannot mask the handle's protection against replacement. Restore it
			// before the fixture's cleanup, even when an assertion fails.
			var reparseData [8]byte
			binary.LittleEndian.PutUint32(reparseData[:4], 0x80000023)
			var returned uint32
			if err := windows.DeviceIoControl(handle, windows.FSCTL_DELETE_REPARSE_POINT,
				&reparseData[0], uint32(len(reparseData)), nil, 0, &returned, nil); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := windows.DeviceIoControl(handle, windows.FSCTL_SET_REPARSE_POINT,
					&reparseData[0], uint32(len(reparseData)), nil, 0, &returned, nil); err != nil {
					t.Error(err)
				}
			})
			if err := verifySocket(before, before); err == nil {
				t.Fatal("socket verifier accepted removed socket tag")
			}
			name, err := windows.UTF16PtrFromString(path)
			if err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "rename":
				err = os.Rename(path, path+".moved")
			case "remove":
				err = os.Remove(path)
			case "replace":
				replacement := path + ".replacement"
				if err := os.WriteFile(replacement, []byte("replacement"), 0600); err != nil {
					t.Fatal(err)
				}
				replacementName, nameErr := windows.UTF16PtrFromString(replacement)
				if nameErr != nil {
					t.Fatal(nameErr)
				}
				err = windows.MoveFileEx(replacementName, name, windows.MOVEFILE_REPLACE_EXISTING)
			}
			// MoveFileEx can report access denied when the destination's
			// retained handle does not share delete access.
			if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) &&
				!(operation == "replace" && errors.Is(err, windows.ERROR_ACCESS_DENIED)) {
				t.Fatalf("%s error = %v, want retained-handle denial protecting socket entry", operation, err)
			}
			if operation == "replace" {
				if err := os.Remove(path + ".replacement"); err != nil {
					t.Fatal(err)
				}
			}
			if err := windows.DeviceIoControl(handle, windows.FSCTL_SET_REPARSE_POINT,
				&reparseData[0], uint32(len(reparseData)), nil, 0, &returned, nil); err != nil {
				t.Fatal(err)
			}
			if err := verifySocket(before, socketInfoForTest(t, filepath.Dir(path))); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSocketFixtureVerifiesIdentityWithoutPathLookup(t *testing.T) {
	root := t.TempDir()
	verifySocket := createSocketFileForTest(t, filepath.Join(root, "socket"))
	// Opaque snapshots deterministically make os.SameFile unable to obtain
	// identity, independently of the host's eager directory file-ID support.
	before := socketInfoWithoutIdentityForTest{socketInfoForTest(t, root)}
	after := socketInfoWithoutIdentityForTest{socketInfoForTest(t, root)}
	if os.SameFile(before, after) {
		t.Fatal("pathname identity lookup unexpectedly available")
	}
	if err := verifySocket(before, after); err != nil {
		t.Fatal(err)
	}
	changedMode := socketInfoWithModeForTest{FileInfo: after, mode: after.Mode() ^ 0222}
	if err := verifySocket(before, changedMode); err == nil {
		t.Fatal("socket verifier accepted changed mode without pathname identity")
	}
}

func TestSocketFixtureAllowsPathStatMetadataRefresh(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "socket")
	verifySocket := createSocketFileForTest(t, path)
	before := socketInfoForTest(t, root)
	if err := verifySocket(before, before); err != nil {
		t.Fatal(err)
	}
	// A pathname stat can refresh directory attributes even when Windows
	// rejects opening the AF_UNIX point. The retained handle's tag stays put.
	_, statErr := os.Lstat(path)
	if statErr != nil && !errors.Is(statErr, windows.ERROR_CANT_ACCESS_FILE) {
		t.Fatal(statErr)
	}
	after := socketInfoForTest(t, root)
	t.Logf("directory mode before=%s after=%s; pathname stat=%v", before.Mode(), after.Mode(), statErr)
	if err := verifySocket(before, after); err != nil {
		t.Fatal(err)
	}
}

func TestSocketFixtureAllowsOnlyRegularSocketTypeDrift(t *testing.T) {
	root := t.TempDir()
	verifySocket := createSocketFileForTest(t, filepath.Join(root, "socket"))
	info := socketInfoWithoutIdentityForTest{socketInfoForTest(t, root)}
	mode := info.Mode() &^ fs.ModeType
	for _, types := range []struct {
		name          string
		before, after fs.FileMode
	}{
		{"regular-to-regular", 0, 0},
		{"regular-to-socket", 0, fs.ModeSocket},
		{"socket-to-regular", fs.ModeSocket, 0},
		{"socket-to-socket", fs.ModeSocket, fs.ModeSocket},
	} {
		t.Run(types.name, func(t *testing.T) {
			before := socketInfoWithModeForTest{FileInfo: info, mode: mode | types.before}
			after := socketInfoWithModeForTest{FileInfo: info, mode: mode | types.after}
			if err := verifySocket(before, after); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, foreign := range []fs.FileMode{fs.ModeDir, fs.ModeSymlink, fs.ModeNamedPipe, fs.ModeDevice, fs.ModeCharDevice, fs.ModeIrregular, fs.ModeSocket | fs.ModeSymlink} {
		t.Run(foreign.String(), func(t *testing.T) {
			socket := socketInfoWithModeForTest{FileInfo: info, mode: mode | fs.ModeSocket}
			other := socketInfoWithModeForTest{FileInfo: info, mode: mode | foreign}
			for _, pair := range [][2]fs.FileInfo{{socket, other}, {other, socket}, {other, other}} {
				if err := verifySocket(pair[0], pair[1]); err == nil {
					t.Fatalf("socket verifier accepted foreign mode before=%s after=%s", pair[0].Mode(), pair[1].Mode())
				}
			}
		})
	}
}

type socketInfoWithoutIdentityForTest struct{ fs.FileInfo }

type socketInfoWithModeForTest struct {
	fs.FileInfo
	mode fs.FileMode
}

func (info socketInfoWithModeForTest) Mode() fs.FileMode { return info.mode }

func createSocketFileForTest(t *testing.T, path string) func(fs.FileInfo, fs.FileInfo) error {
	t.Helper()
	_, verifySocket := createSocketFixtureForTest(t, path)
	return verifySocket
}

func createSocketFixtureForTest(t *testing.T, path string) (windows.Handle, func(fs.FileInfo, fs.FileInfo) error) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Windows represents socket files with this native reparse tag. Retain the
	// original file handle so cleanup can clear the tag without opening an
	// active AF_UNIX endpoint, which some Windows versions refuse to access.
	// Omitting FILE_SHARE_DELETE pins the original directory entry: no rename,
	// removal, or replacement can change which file the socket path names.
	var reparseData [8]byte
	binary.LittleEndian.PutUint32(reparseData[:4], 0x80000023) // IO_REPARSE_TAG_AF_UNIX
	installed := false
	t.Cleanup(func() {
		if installed {
			var returned uint32
			if err := windows.DeviceIoControl(handle, windows.FSCTL_DELETE_REPARSE_POINT,
				&reparseData[0], uint32(len(reparseData)), nil, 0, &returned, nil); err != nil {
				t.Error(err)
			}
		}
		if err := windows.CloseHandle(handle); err != nil {
			t.Error(err)
		}
	})
	var returned uint32
	if err := windows.DeviceIoControl(handle, windows.FSCTL_SET_REPARSE_POINT,
		&reparseData[0], uint32(len(reparseData)), nil, 0, &returned, nil); err != nil {
		t.Fatal(err)
	}
	installed = true
	return handle, func(before, after fs.FileInfo) error {
		var data [8]byte
		var returned uint32
		if err := windows.DeviceIoControl(handle, windows.FSCTL_GET_REPARSE_POINT,
			nil, 0, &data[0], uint32(len(data)), &returned, nil); err != nil {
			return fmt.Errorf("read special-file fixture reparse data: %w", err)
		}
		if returned != uint32(len(data)) || data != reparseData {
			return fmt.Errorf("special-file fixture reparse data = %x (%d bytes), want socket tag %x", data, returned, reparseData)
		}
		// SameFile may lazily reopen directory entries on Windows, which can
		// fail for an unchanged AF_UNIX reparse point. The retained handle pins
		// the entry instead, and the exact native tag above verifies its type.
		// Directory attributes can refresh from regular to socket after a
		// pathname stat. Allow only those reported types; all other mode bits
		// must stay unchanged.
		beforeMode := before.Mode() &^ fs.ModeSocket
		afterMode := after.Mode() &^ fs.ModeSocket
		if !beforeMode.IsRegular() || !afterMode.IsRegular() || afterMode != beforeMode {
			return fmt.Errorf("rejected special file mode = %s, before = %s", after.Mode(), before.Mode())
		}
		return nil
	}
}
