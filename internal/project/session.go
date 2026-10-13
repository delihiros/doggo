package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var afterProjectFileOpenForTest func(string)
var afterProjectFileReadForTest func(string)
var afterGenerationAuditWorkspaceVerifyForTest func()

type GenerationSession struct {
	mu          sync.Mutex
	Config      RuntimeConfig
	binary      *generationBinary
	generation  *os.Root
	manifest    generationManifest
	auditPaths  []string
	directories map[string]fs.FileInfo
	sources     []*GenerationSource
	lease       *GenerationLease
}

func OpenGenerationSession(scriptPath string) (_ *GenerationSession, err error) {
	projectRoot, found, err := ResolveRoot(scriptPath)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%s not found. Run 'doggo mod init' first", ModFileName)
	}

	project, projectPath, err := openVerifiedRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	return openGenerationSessionRoot(project, projectPath)
}

// OpenGenerationSessionRoot consumes an already-open, path-verified project root.
func OpenGenerationSessionRoot(project *os.Root, projectPath string) (_ *GenerationSession, err error) {
	if project == nil {
		return nil, fmt.Errorf("project root is nil")
	}
	absolute, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, errors.Join(err, project.Close())
	}
	if err := revalidateRootPath(absolute, project); err != nil {
		return nil, errors.Join(err, project.Close())
	}
	return openGenerationSessionRoot(project, absolute)
}

func openGenerationSessionRoot(project *os.Root, projectPath string) (_ *GenerationSession, err error) {
	doggo, err := openVerifiedSubroot(project, ".doggo")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(fmt.Errorf("%s not found. Run 'doggo mod sync'", LockFileName), project.Close())
		}
		return nil, errors.Join(err, project.Close())
	}
	lease, err := acquireGenerationLeaseRoot(projectPath, project, doggo, false)
	if err != nil {
		closeErr := errors.Join(doggo.Close(), project.Close())
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(fmt.Errorf("%s not found. Run 'doggo mod sync'", LockFileName), closeErr)
		}
		return nil, errors.Join(err, closeErr)
	}
	lease.project = project
	lease.projectPath = projectPath
	lease.doggo = doggo
	var generation *os.Root
	var binary *generationBinary
	var sources []*GenerationSource
	defer func() {
		if err != nil {
			err = errors.Join(err, closeGenerationBinary(binary), closeGenerationSources(sources), closeRoot(&generation), lease.Close())
		}
	}()

	modBytes, err := readVerifiedRootFile(project, ModFileName)
	if err != nil {
		return nil, err
	}
	mod, err := parseModFileBytes(ModPath(projectPath), modBytes)
	if err != nil {
		return nil, err
	}
	lockBytes, err := readVerifiedRootFile(project, LockFileName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s not found. Run 'doggo mod sync'", LockFileName)
		}
		return nil, err
	}
	lock, err := parseLockFileBytes(LockPath(projectPath), lockBytes)
	if err != nil {
		return nil, err
	}
	if err := revalidateRootPath(projectPath, project); err != nil {
		return nil, err
	}
	manifest := generationManifest(nil)
	generation, manifest, err = retainValidatedGenerationProjectRoot(project, mod, lock)
	if err != nil {
		return nil, fmt.Errorf("%s is stale or corrupt: %w", LockFileName, err)
	}
	auditPaths := auditWorkspacePaths(lock)
	directories := auditWorkspaceDirectoryManifest(manifest, auditPaths)
	launchPath := ""
	if lock.Build.Binary != "" {
		launchPath = filepath.Join(projectPath, filepath.FromSlash(lock.Build.Binary))
	}
	binary, err = retainGenerationBinary(generation, manifest, lock, launchPath)
	if err != nil {
		return nil, fmt.Errorf("%s is stale or corrupt: %w", LockFileName, err)
	}

	loadPaths, err := liveProjectLoadPathsRoot(project, projectPath)
	if err != nil {
		return nil, err
	}
	for _, dep := range lock.Deps {
		root, err := openVerifiedRootPath(generation, filepath.FromSlash(dep.InstallDir))
		if err != nil {
			return nil, fmt.Errorf("dependency %s: %w", dep.ModuleID, err)
		}
		sources = append(sources, &GenerationSource{
			root: root, prefix: dep.InstallDir,
			display: "generation://" + lock.Generation + "/" + dep.InstallDir,
			files:   manifest,
		})
	}
	session := &GenerationSession{
		Config: RuntimeConfig{
			ProjectRoot:       projectPath,
			LoadPaths:         loadPaths,
			GenerationSources: append([]*GenerationSource(nil), sources...),
			Lock:              lock,
		},
		generation:  generation,
		binary:      binary,
		manifest:    manifest,
		auditPaths:  auditPaths,
		directories: directories,
		sources:     sources,
		lease:       lease,
	}
	return session, nil
}

func auditWorkspacePaths(lock *LockFile) []string {
	paths := make([]string, 1, len(lock.Deps)+1)
	paths[0] = "toolchain"
	for _, dep := range lock.Deps {
		paths = append(paths, dep.InstallDir)
	}
	sort.Strings(paths)
	return paths
}

func auditWorkspaceDirectoryManifest(manifest generationManifest, auditPaths []string) map[string]fs.FileInfo {
	directories := make(map[string]fs.FileInfo)
	for name, entry := range manifest {
		if entry.directory && auditWorkspaceDirectory(name, auditPaths) {
			directories[name] = entry.info
		}
	}
	return directories
}

func auditWorkspaceDirectory(name string, auditPaths []string) bool {
	if auditWorkspacePath(name, auditPaths) {
		return true
	}
	for _, auditPath := range auditPaths {
		if strings.HasPrefix(auditPath, name+"/") {
			return true
		}
	}
	return false
}

func auditWorkspacePath(name string, auditPaths []string) bool {
	for _, auditPath := range auditPaths {
		if name == auditPath || strings.HasPrefix(name, auditPath+"/") {
			return true
		}
	}
	return false
}

// CopyAuditWorkspaceTo copies the validated toolchain and locked dependencies
// through retained generation identities into a caller-owned destination.
func (s *GenerationSession) CopyAuditWorkspaceTo(destination *OwnedTree) error {
	if s == nil {
		return fmt.Errorf("generation session is closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation == nil || s.lease == nil {
		return fmt.Errorf("generation session is closed")
	}
	if destination == nil {
		return fmt.Errorf("audit workspace destination is nil")
	}
	if err := destination.VerifyComplete(); err != nil {
		return err
	}
	before, err := s.generation.Stat(".")
	if err != nil {
		return err
	}
	if err := s.verifyAuditWorkspaceManifest(); err != nil {
		return err
	}
	if afterGenerationAuditWorkspaceVerifyForTest != nil {
		afterGenerationAuditWorkspaceVerifyForTest()
	}

	directories := make([]string, 0, len(s.directories))
	for name := range s.directories {
		directories = append(directories, name)
	}
	sort.Strings(directories)
	for _, name := range directories {
		root, err := destination.CreateRoot(name, s.directories[name].Mode().Perm(), nil)
		if err != nil {
			return fmt.Errorf("copy audit workspace directory %s: %w", name, err)
		}
		if err := root.Close(); err != nil {
			return err
		}
	}

	files := make([]string, 0, len(s.manifest))
	for name, entry := range s.manifest {
		if !entry.directory && auditWorkspacePath(name, s.auditPaths) {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	source := GenerationSource{root: s.generation, files: s.manifest}
	for _, name := range files {
		entry := s.manifest[name]
		content, err := source.ReadFile(name)
		if err != nil {
			return fmt.Errorf("copy audit workspace file %s: %w", name, err)
		}
		if err := destination.WriteOwnedFile(name, content, entry.info.Mode().Perm()); err != nil {
			return fmt.Errorf("copy audit workspace file %s: %w", name, err)
		}
	}
	if err := s.verifyAuditWorkspaceManifest(); err != nil {
		return err
	}
	after, err := s.generation.Stat(".")
	if err != nil || !before.IsDir() || !after.IsDir() || !os.SameFile(before, after) {
		return errors.Join(fmt.Errorf("generation root changed during audit workspace copy"), err)
	}
	return destination.VerifyComplete()
}

func (s *GenerationSession) verifyAuditWorkspaceManifest() error {
	entries, err := collectRootEntries(s.generation)
	if err != nil {
		return err
	}
	seenDirectories := make(map[string]bool, len(s.directories))
	seenFiles := make(map[string]bool, len(s.manifest))
	for _, entry := range entries {
		if entry.directory {
			if !auditWorkspaceDirectory(entry.path, s.auditPaths) {
				continue
			}
			expected, ok := s.directories[entry.path]
			if !ok || !expected.IsDir() || !entry.info.IsDir() || !os.SameFile(expected, entry.info) {
				return fmt.Errorf("generation audit workspace contains unknown or changed directory: %s", entry.path)
			}
			seenDirectories[entry.path] = true
			continue
		}
		if !auditWorkspacePath(entry.path, s.auditPaths) {
			continue
		}
		expected, ok := s.manifest[entry.path]
		if !ok || expected.directory || !sameRegularFile(expected.info, entry.info) {
			return fmt.Errorf("generation audit workspace contains unknown or changed file: %s", entry.path)
		}
		seenFiles[entry.path] = true
	}
	for name := range s.directories {
		if !seenDirectories[name] {
			return fmt.Errorf("generation audit workspace directory is missing: %s", name)
		}
	}
	for name, entry := range s.manifest {
		if !entry.directory && auditWorkspacePath(name, s.auditPaths) && !seenFiles[name] {
			return fmt.Errorf("generation audit workspace file is missing: %s", name)
		}
	}
	return nil
}

func (s *GenerationSession) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lease == nil {
		return nil
	}
	lease := s.lease
	generation := s.generation
	binary := s.binary
	sources := s.sources
	s.lease = nil
	s.generation = nil
	s.binary = nil
	s.manifest = nil
	s.auditPaths = nil
	s.directories = nil
	s.sources = nil
	return errors.Join(closeGenerationBinary(binary), closeGenerationSources(sources), closeRoot(&generation), lease.Close())
}

func closeGenerationSources(sources []*GenerationSource) error {
	var err error
	for _, source := range sources {
		err = errors.Join(err, source.close())
	}
	return err
}
