//go:build windows

package project

import (
	"encoding/binary"
	"testing"

	"golang.org/x/sys/windows"
)

func createSocketFileForTest(t *testing.T, path string) func() {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Windows represents socket files with this native reparse tag. Retain the
	// original file handle so cleanup can clear the tag without opening an
	// active AF_UNIX endpoint, which some Windows versions refuse to access.
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
	return func() {
		var data [8]byte
		var returned uint32
		if err := windows.DeviceIoControl(handle, windows.FSCTL_GET_REPARSE_POINT,
			nil, 0, &data[0], uint32(len(data)), &returned, nil); err != nil {
			t.Fatal(err)
		}
		if returned != uint32(len(data)) || data != reparseData {
			t.Fatalf("special-file fixture reparse data = %x (%d bytes), want socket tag %x", data, returned, reparseData)
		}
	}
}
