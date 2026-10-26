package vm

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIOCopyMoveRejectSameFileIdentity(t *testing.T) {
	// Windows CI can place the package and TempDir on different volumes.
	t.Chdir(t.TempDir())
	for _, operation := range []string{"copy-tree", "move"} {
		for _, spelling := range []string{"same path", "relative source", "relative destination", "parent destination", "relative parent destination", "hardlink", "case alias", "ancestor alias"} {
			t.Run(operation+"/"+spelling, func(t *testing.T) {
				base := t.TempDir()
				source := filepath.Join(base, "source.txt")
				original := []byte("preserve source bytes\x00\r\n")
				writeIOIdentityFile(t, source, original)
				src, dst := source, source
				preserved := []string{source}
				switch spelling {
				case "relative source":
					src = relativeIOIdentityPath(t, source)
				case "relative destination":
					dst = relativeIOIdentityPath(t, source)
				case "parent destination":
					src, dst = relativeIOIdentityPath(t, source), base
				case "relative parent destination":
					dst = relativeIOIdentityPath(t, base)
				case "hardlink":
					dst = filepath.Join(base, "hardlink.txt")
					if err := os.Link(source, dst); err != nil {
						t.Skipf("hardlinks unavailable: %v", err)
					}
					preserved = append(preserved, dst)
				case "case alias":
					dst = filepath.Join(base, "SOURCE.TXT")
					requireIOIdentityAlias(t, source, dst)
					preserved = append(preserved, dst)
				case "ancestor alias":
					dst = filepath.Join(ioIdentityAncestorAlias(t, base), filepath.Base(source))
					preserved = append(preserved, dst)
				}

				before := ioIdentityInfos(t, preserved)
				err := runIOIdentityMutation(t, operation, src, dst, true)
				if err == nil || !strings.Contains(err.Error(), "source and destination must be different") {
					t.Errorf("expected same-source rejection before overwrite, got %v", err)
				}
				assertIOIdentityFilesUnchanged(t, preserved, before, original)
			})
		}
	}
}

func TestIOCopyMoveRejectSameDirectoryIdentity(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, operation := range []string{"copy-tree", "move"} {
		for _, spelling := range []string{"same path", "relative source", "relative destination", "case alias", "ancestor alias"} {
			t.Run(operation+"/"+spelling, func(t *testing.T) {
				source := filepath.Join(t.TempDir(), "source")
				child := filepath.Join(source, "child.txt")
				original := []byte("directory source\x00\n")
				writeIOIdentityFile(t, child, original)
				src, dst := source, source
				switch spelling {
				case "relative source":
					src = relativeIOIdentityPath(t, source)
				case "relative destination":
					dst = relativeIOIdentityPath(t, source)
				case "case alias":
					dst = filepath.Join(filepath.Dir(source), "SOURCE")
					requireIOIdentityAlias(t, source, dst)
				case "ancestor alias":
					dst = filepath.Join(ioIdentityAncestorAlias(t, filepath.Dir(source)), filepath.Base(source))
				}
				before := ioIdentityInfos(t, []string{source, child})
				err := runIOIdentityMutation(t, operation, src, dst, true)
				if err == nil || !strings.Contains(err.Error(), "destination must not be the source or a descendant of the source") {
					t.Errorf("expected recursive-destination rejection, got %v", err)
				}
				assertIOIdentityFilesUnchanged(t, []string{child}, before[1:], original)
				after, err := os.Lstat(source)
				if err != nil || !os.SameFile(before[0], after) {
					t.Errorf("source directory identity changed: %v", err)
				}
			})
		}
	}
}

func TestIOCopyMoveRejectDescendantAliases(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, operation := range []string{"copy-tree", "move"} {
		for _, spelling := range []string{"relative source", "relative destination", "ancestor alias"} {
			t.Run(operation+"/"+spelling, func(t *testing.T) {
				source := filepath.Join(t.TempDir(), "source")
				destination := filepath.Join(source, "z-destination")
				original := []byte("unchanged descendant guard fixture")
				child := filepath.Join(source, "a", "child.txt")
				blocker := filepath.Join(destination, "a")
				writeIOIdentityFile(t, child, original)
				// This shape conflict bounds the fixture even if the top-level
				// guard fails: the first directory entry cannot be merged.
				writeIOIdentityFile(t, blocker, original)
				src, dst := source, destination
				if spelling == "relative source" {
					src = relativeIOIdentityPath(t, source)
				} else if spelling == "relative destination" {
					dst = relativeIOIdentityPath(t, destination)
				} else {
					dst = filepath.Join(ioIdentityAncestorAlias(t, filepath.Dir(source)), filepath.Base(source), filepath.Base(destination))
				}
				preserved := []string{child, blocker}
				before := ioIdentityInfos(t, preserved)
				err := runIOIdentityMutation(t, operation, src, dst, true)
				if err == nil || !strings.Contains(err.Error(), "destination must not be the source or a descendant of the source") {
					t.Errorf("expected recursive-destination rejection, got %v", err)
				}
				assertIOIdentityFilesUnchanged(t, preserved, before, original)
				entries, err := os.ReadDir(destination)
				if err != nil || len(entries) != 1 || entries[0].Name() != "a" {
					t.Errorf("descendant destination changed: entries=%v err=%v", entries, err)
				}
			})
		}
	}
}

func TestIOCopyMoveRejectPhysicalDescendantAlias(t *testing.T) {
	for _, operation := range []string{"copy-tree", "move"} {
		for _, sourceSpelling := range []string{"direct source", "aliased source"} {
			t.Run(operation+"/"+sourceSpelling, func(t *testing.T) {
				base := t.TempDir()
				source := filepath.Join(base, "source")
				sourceFile := filepath.Join(source, "a.txt")
				descendant := filepath.Join(source, "z-child")
				output := filepath.Join(descendant, "out")
				preserved := filepath.Join(output, "a.txt")
				blocker := filepath.Join(output, "z-child")
				original, existing := []byte("source bytes\x00\n"), []byte("existing destination bytes\x00\r\n")
				writeIOIdentityFile(t, sourceFile, original)
				writeIOIdentityFile(t, preserved, existing)
				// The final root directory entry collides with a file, bounding
				// RED even if the guard allows the earlier file overwrite.
				writeIOIdentityFile(t, blocker, existing)
				alias := filepath.Join(base, "descendant-alias")
				if err := os.Symlink(descendant, alias); err != nil {
					t.Skipf("ancestor symlink unavailable: %v", err)
				}
				src := source
				if sourceSpelling == "aliased source" {
					src = filepath.Join(ioIdentityAncestorAlias(t, base), filepath.Base(source))
				}
				before := ioIdentityInfos(t, []string{sourceFile, preserved, blocker})
				err := runIOIdentityMutation(t, operation, src, filepath.Join(alias, "out"), true)
				if err == nil || !strings.Contains(err.Error(), "destination must not be the source or a descendant of the source") {
					t.Errorf("expected physical descendant rejection before mutation, got %v", err)
				}
				assertIOIdentityFilesUnchanged(t, []string{sourceFile}, before[:1], original)
				assertIOIdentityFilesUnchanged(t, []string{preserved, blocker}, before[1:], existing)
			})
		}
	}
}

func TestIORejectMissingPhysicalDescendantAlias(t *testing.T) {
	for _, sourceSpelling := range []string{"direct source", "aliased source"} {
		t.Run(sourceSpelling, func(t *testing.T) {
			base := t.TempDir()
			source := filepath.Join(base, "source")
			descendant := filepath.Join(source, "child")
			if err := os.MkdirAll(descendant, 0o755); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(base, "descendant-alias")
			if err := os.Symlink(descendant, alias); err != nil {
				t.Skipf("ancestor symlink unavailable: %v", err)
			}
			src := source
			if sourceSpelling == "aliased source" {
				src = filepath.Join(ioIdentityAncestorAlias(t, base), filepath.Base(source))
			}
			candidate := filepath.Join(alias, "missing", "output")
			// Inspect the real guard directly so RED cannot create an
			// unbounded recursive tree beneath the missing destination.
			recursive, err := ioPathIsSameOrDescendant(src, candidate)
			if err != nil || !recursive {
				t.Errorf("expected missing physical descendant rejection, got recursive=%v err=%v", recursive, err)
			}
			if _, err := os.Lstat(filepath.Join(descendant, "missing")); !os.IsNotExist(err) {
				t.Errorf("guard created a missing destination ancestor: %v", err)
			}
		})
	}
}

func TestIOCopyMoveDistinctFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, operation := range []string{"copy-tree", "move"} {
		for _, shape := range []string{"missing destination", "overwrite", "existing directory", "collision"} {
			t.Run(operation+"/"+shape, func(t *testing.T) {
				base := t.TempDir()
				source, destination := filepath.Join(base, "source.txt"), filepath.Join(base, "destination.txt")
				original, existing := []byte("source content\x00\n"), []byte("existing content")
				writeIOIdentityFile(t, source, original)
				dst, finalDst, overwrite := destination, destination, shape == "overwrite"
				switch shape {
				case "overwrite", "collision":
					writeIOIdentityFile(t, destination, existing)
				case "existing directory":
					if err := os.Mkdir(destination, 0o755); err != nil {
						t.Fatal(err)
					}
					finalDst = filepath.Join(destination, filepath.Base(source))
				}
				before := ioIdentityInfos(t, []string{source})
				err := runIOIdentityMutation(t, operation, relativeIOIdentityPath(t, source), dst, overwrite)
				if shape == "collision" {
					if err == nil || !strings.Contains(err.Error(), "destination exists and :overwrite is false") {
						t.Errorf("expected collision rejection, got %v", err)
					}
					assertIOIdentityFilesUnchanged(t, []string{source}, before, original)
					assertIOIdentityBytes(t, destination, existing)
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				assertIOIdentityBytes(t, finalDst, original)
				if operation == "move" {
					if _, err := os.Lstat(source); !os.IsNotExist(err) {
						t.Errorf("move source still exists: %v", err)
					}
				} else {
					assertIOIdentityFilesUnchanged(t, []string{source}, before, original)
				}
			})
		}
	}
}

func TestIOCopyMovePreserveTerminalSymlinkPolicy(t *testing.T) {
	for _, operation := range []string{"copy-tree", "move"} {
		for _, direction := range []string{"source symlink", "destination symlink"} {
			t.Run(operation+"/"+direction, func(t *testing.T) {
				base := t.TempDir()
				source, link := filepath.Join(base, "source.txt"), filepath.Join(base, "link.txt")
				original := []byte("symlink target must survive\x00\n")
				writeIOIdentityFile(t, source, original)
				if err := os.Symlink(source, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				if direction == "source symlink" {
					err := runIOIdentityMutation(t, operation, link, filepath.Join(base, "destination.txt"), true)
					if err == nil || !strings.Contains(err.Error(), "symlink handling is not supported") {
						t.Errorf("expected source symlink rejection, got %v", err)
					}
					if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
						t.Errorf("source symlink changed: info=%v err=%v", info, err)
					}
					assertIOIdentityBytes(t, source, original)
					return
				}
				if err := runIOIdentityMutation(t, operation, source, link, true); err != nil {
					t.Fatal(err)
				}
				if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink != 0 {
					t.Errorf("destination symlink was not replaced: info=%v err=%v", info, err)
				}
				assertIOIdentityBytes(t, link, original)
				if operation == "copy-tree" {
					assertIOIdentityBytes(t, source, original)
				} else if _, err := os.Lstat(source); !os.IsNotExist(err) {
					t.Errorf("move source still exists: %v", err)
				}
			})
		}
	}
}

func TestIOCopyMovePreserveTerminalDirectorySymlinkPolicy(t *testing.T) {
	for _, operation := range []string{"copy-tree", "move"} {
		t.Run(operation, func(t *testing.T) {
			base := t.TempDir()
			source := filepath.Join(base, "source")
			targetFile := filepath.Join(source, "child", "preserved.txt")
			original := []byte("terminal directory symlink target\x00\n")
			writeIOIdentityFile(t, targetFile, original)
			link := filepath.Join(base, "link")
			if err := os.Symlink(filepath.Dir(targetFile), link); err != nil {
				t.Skipf("directory symlinks unavailable: %v", err)
			}
			before := ioIdentityInfos(t, []string{targetFile, link})
			err := runIOIdentityMutation(t, operation, source, link, true)
			if err == nil || !strings.Contains(err.Error(), "incompatible source/destination shapes") {
				t.Errorf("expected terminal symlink shape rejection, got %v", err)
			}
			assertIOIdentityFilesUnchanged(t, []string{targetFile}, before[:1], original)
			if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 || !os.SameFile(before[1], info) {
				t.Errorf("terminal destination symlink changed: info=%v err=%v", info, err)
			}
		})
	}
}

func runIOIdentityMutation(t *testing.T, operation, src, dst string, overwrite bool) error {
	t.Helper()
	runner := MustNewRunner()
	_, err := runner.Run(fmt.Sprintf(`(require '[doggo.io :as io]) (io/%s %q %q {:overwrite %t})`,
		operation, filepath.ToSlash(src), filepath.ToSlash(dst), overwrite))
	return err
}

func relativeIOIdentityPath(t *testing.T, path string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, path)
	if err != nil {
		t.Fatal(err)
	}
	return relative
}

func writeIOIdentityFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireIOIdentityAlias(t *testing.T, source, alias string) {
	t.Helper()
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Lstat(alias)
	if os.IsNotExist(err) {
		t.Skip("fixture filesystem is case-sensitive")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(sourceInfo, aliasInfo) {
		t.Skip("case spelling does not identify the same entry")
	}
}

func ioIdentityAncestorAlias(t *testing.T, directory string) string {
	t.Helper()
	alias := filepath.Join(directory, "parent-alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Skipf("ancestor symlink unavailable: %v", err)
	}
	return alias
}

func ioIdentityInfos(t *testing.T, paths []string) []os.FileInfo {
	t.Helper()
	infos := make([]os.FileInfo, len(paths))
	for i, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		// Windows loads file IDs lazily. Capture identity before mutation
		// so a later lookup cannot silently observe a replacement entry.
		if !os.SameFile(info, info) {
			t.Fatalf("could not capture file identity for %s", path)
		}
		infos[i] = info
	}
	return infos
}

func assertIOIdentityFilesUnchanged(t *testing.T, paths []string, before []os.FileInfo, original []byte) {
	t.Helper()
	for i, path := range paths {
		assertIOIdentityBytes(t, path, original)
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before[i], after) {
			t.Errorf("file identity changed for %s: %v", path, err)
		}
	}
}

func assertIOIdentityBytes(t *testing.T, path string, original []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, original) {
		t.Errorf("file bytes changed for %s: got %q, want %q, err=%v", path, actual, original, err)
	}
}
