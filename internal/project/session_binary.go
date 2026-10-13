package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type generationBinary struct {
	file       *os.File
	executable *OwnedTempRoot
	info       fs.FileInfo
	size       int64
	sha256     string
	path       string
	launchPath string
}

type GenerationBinarySnapshot struct {
	ProjectPath string
	SHA256      string
	Content     []byte
}

func retainGenerationBinary(generation *os.Root, manifest generationManifest, lock *LockFile, launchPath string) (*generationBinary, error) {
	if lock.Build.Binary == "" {
		return nil, nil
	}
	prefix := ".doggo/generations/" + lock.Generation + "/"
	name, ok := strings.CutPrefix(lock.Build.Binary, prefix)
	if !ok {
		return nil, fmt.Errorf("binary path is outside generation")
	}
	entry, ok := manifest[name]
	if !ok || entry.directory || entry.sha256 != lock.Build.SHA256 {
		return nil, fmt.Errorf("binary manifest mismatch")
	}
	file, err := openGenerationBinary(generation, filepath.FromSlash(name))
	if err != nil {
		return nil, fmt.Errorf("binary: %w", err)
	}
	opened, statErr := file.Stat()
	if statErr != nil || !sameRegularFile(entry.info, opened) || opened.Size() != entry.size {
		return nil, errors.Join(fmt.Errorf("binary identity changed while retaining"), statErr, file.Close())
	}
	hash := sha256.New()
	_, readErr := io.CopyN(hash, io.NewSectionReader(file, 0, entry.size), entry.size)
	after, afterErr := file.Stat()
	digest := hex.EncodeToString(hash.Sum(nil))
	if readErr != nil || afterErr != nil || !sameRegularFile(opened, after) || digest != entry.sha256 || digest != lock.Build.SHA256 {
		return nil, errors.Join(fmt.Errorf("binary content changed while retaining"), readErr, afterErr, file.Close())
	}
	return &generationBinary{file: file, info: opened, size: entry.size, sha256: digest, path: lock.Build.Binary, launchPath: launchPath}, nil
}

func (s *GenerationSession) HasBinary() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lease != nil && s.binary != nil
}

func (s *GenerationSession) BinarySnapshot() (GenerationBinarySnapshot, error) {
	if s == nil {
		return GenerationBinarySnapshot{}, fmt.Errorf("generation session is closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lease == nil {
		return GenerationBinarySnapshot{}, fmt.Errorf("generation session is closed")
	}
	if s.binary == nil {
		return GenerationBinarySnapshot{}, fmt.Errorf("current generation has no executable; run 'doggo build' first")
	}
	return snapshotGenerationBinary(s.binary)
}

func snapshotGenerationBinary(binary *generationBinary) (GenerationBinarySnapshot, error) {
	before, err := binary.file.Stat()
	if err != nil || !sameRegularFile(binary.info, before) || before.Size() != binary.size {
		return GenerationBinarySnapshot{}, errors.Join(fmt.Errorf("generation binary identity changed before snapshot"), err)
	}
	if binary.size < 0 || uint64(binary.size) > uint64(int(^uint(0)>>1)) {
		return GenerationBinarySnapshot{}, fmt.Errorf("generation binary size %d cannot be represented", binary.size)
	}
	content := make([]byte, int(binary.size))
	n, readErr := binary.file.ReadAt(content, 0)
	after, statErr := binary.file.Stat()
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	if n != len(content) || readErr != nil || statErr != nil || !sameRegularFile(binary.info, after) || after.Size() != binary.size || digest != binary.sha256 {
		return GenerationBinarySnapshot{}, errors.Join(fmt.Errorf("generation binary content changed while snapshotting"), readErr, statErr)
	}
	return GenerationBinarySnapshot{ProjectPath: binary.path, SHA256: binary.sha256, Content: content}, nil
}

// BinaryCommand returns a command backed by the retained generation binary.
// The command is valid only while the GenerationSession remains open.
func (s *GenerationSession) BinaryCommand(args ...string) (*exec.Cmd, error) {
	if s == nil {
		return nil, fmt.Errorf("generation session is closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lease == nil {
		return nil, fmt.Errorf("generation session is closed")
	}
	if s.binary == nil {
		return nil, fmt.Errorf("current generation has no executable; run 'doggo build' first")
	}
	return generationBinaryCommand(s.binary, args...)
}

func closeGenerationBinary(binary *generationBinary) error {
	if binary == nil {
		return nil
	}
	file := binary.file
	executable := binary.executable
	binary.file = nil
	binary.executable = nil
	var fileErr error
	if file != nil {
		fileErr = file.Close()
	}
	return errors.Join(executable.Close(), fileErr)
}
