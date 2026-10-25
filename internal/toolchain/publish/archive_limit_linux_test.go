package publish

import (
	"bytes"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"

	"doggo/internal/project"
)

// Run the process-wide file-size limit in a child, so it cannot affect other tests.
func TestServiceRunArchiveFinalizationFailure(t *testing.T) {
	const childFlag = "DOGGO_TEST_PUBLISH_FILE_LIMIT"
	if os.Getenv(childFlag) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestServiceRunArchiveFinalizationFailure$", "-test.v")
		cmd.Env = append(os.Environ(), childFlag+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("file-limit child failed: %v\n%s", err, output)
		}
		return
	}

	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new archive", true: "existing archive"}[existing], func(t *testing.T) {
			root, archive, opts := publishOutputFixture(t)
			previous := []byte("previous valid artifact stays untouched")
			if existing {
				if err := os.WriteFile(archive, previous, 0644); err != nil {
					t.Fatal(err)
				}
			}
			registry := &project.RegistryFile{Packages: []project.RegistryPackage{}, Advisories: []project.RegistryAdvisory{}}
			if err := project.WriteRegistryFile(opts.RegistryPath, registry); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(opts.RegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			service := Service{CreateArchive: func(root, name, version string) (string, error) {
				var old syscall.Rlimit
				if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
					t.Fatal(err)
				}
				signal.Ignore(syscall.SIGXFSZ)
				defer signal.Reset(syscall.SIGXFSZ)
				if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 10, Max: old.Max}); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
						t.Fatal(err)
					}
				}()
				// The gzip header fits; the final compressed bytes fail with EFBIG.
				return createPublishArchive(root, name, version)
			}}
			if result, err := service.Run(root, opts); err == nil || result != nil {
				t.Errorf("expected archive finalization error, got result=%+v err=%v", result, err)
			}
			after, err := os.ReadFile(opts.RegistryPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Errorf("registry changed after archive failure: %q, %v", after, err)
			}
			actual, err := os.ReadFile(archive)
			if existing {
				if err != nil || !bytes.Equal(actual, previous) {
					t.Errorf("previous archive changed after failure: %q, %v", actual, err)
				}
			} else if !os.IsNotExist(err) {
				t.Errorf("incomplete archive was published: %q, %v", actual, err)
			}
			assertNoPublishTemps(t, archive)
		})
	}
}

func TestServiceRunArchivePermissions(t *testing.T) {
	const childFlag = "DOGGO_TEST_PUBLISH_UMASK"
	if os.Getenv(childFlag) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestServiceRunArchivePermissions$", "-test.v")
		cmd.Env = append(os.Environ(), childFlag+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("umask child failed: %v\n%s", err, output)
		}
		return
	}
	for _, tc := range []struct {
		name     string
		existing os.FileMode
		want     os.FileMode
	}{
		{name: "new archive honors umask", want: 0600},
		{name: "private archive remains private", existing: 0600, want: 0600},
		{name: "existing permissions are preserved", existing: 0640, want: 0640},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, archive, opts := publishOutputFixture(t)
			if tc.existing != 0 {
				if err := os.WriteFile(archive, []byte("previous artifact"), tc.existing); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(archive, tc.existing); err != nil {
					t.Fatal(err)
				}
			}
			oldMask := syscall.Umask(0077)
			defer syscall.Umask(oldMask)
			if _, err := (Service{}).Run(root, opts); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(archive)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != tc.want {
				t.Errorf("archive permissions=%#o, want %#o", got, tc.want)
			}
		})
	}
}
