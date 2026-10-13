package vm

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIOWalkWindowsSymlinkTargetErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"unresolvable target", windows.ERROR_CANT_RESOLVE_FILENAME, true},
		{"access denied", windows.ERROR_ACCESS_DENIED, false},
		{"sharing violation", windows.ERROR_SHARING_VIOLATION, false},
		{"inaccessible file", windows.ERROR_CANT_ACCESS_FILE, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := &os.PathError{Op: "CreateFile", Path: "loop", Err: tt.err}
			if got := ioWalkNonDescendableSymlinkTargetError(err); got != tt.want {
				t.Fatalf("non-descendable target error = %v, want %v for %v", got, tt.want, err)
			}
		})
	}
}
