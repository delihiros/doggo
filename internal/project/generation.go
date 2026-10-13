package project

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"doggo/internal/depsource"
	"golang.org/x/mod/module"
)

const generationMarkerName = ".generation"

type generationEntry struct {
	path       string
	directory  bool
	executable bool
	size       int64
	info       fs.FileInfo
}

func GenerationDigest(generationRoot string, lock *LockFile) (string, error) {
	if lock == nil {
		return "", fmt.Errorf("lock file is nil")
	}
	root, rootPath, err := openVerifiedRoot(generationRoot)
	if err != nil {
		return "", err
	}
	defer root.Close()
	digest, err := generationDigestRoot(root, lock)
	if err != nil {
		return "", err
	}
	if err := revalidateRootPath(rootPath, root); err != nil {
		return "", err
	}
	return digest, nil
}

func ValidateGeneration(projectRoot string, mod *ModFile, lock *LockFile) error {
	if err := validateGeneration(projectRoot, mod, lock); err != nil {
		return fmt.Errorf("%s is stale or corrupt: %w", LockFileName, err)
	}
	return nil
}

// ValidateTentativeLock validates generation inputs before content is sealed.
func ValidateTentativeLock(mod *ModFile, lock *LockFile) error {
	if lock == nil {
		return fmt.Errorf("tentative lock file is nil")
	}
	if lock.Generation != "" {
		return fmt.Errorf("tentative generation must be empty")
	}
	if lock.Build.Binary != "" {
		return fmt.Errorf("tentative binary path must be empty")
	}
	sealed := *lock
	sealed.Generation = strings.Repeat("0", 64)
	if sealed.Build.SHA256 != "" {
		sealed.Build.Binary = expectedGenerationBinary(sealed.Generation)
	}
	if err := validateLockSchema(mod, &sealed); err != nil {
		return fmt.Errorf("tentative lock file is invalid: %w", err)
	}
	return nil
}

func validateGeneration(projectRoot string, mod *ModFile, lock *LockFile) error {
	if mod == nil {
		return fmt.Errorf("mod file is nil")
	}
	if lock == nil {
		return fmt.Errorf("lock file is nil")
	}
	if err := validateLockSchema(mod, lock); err != nil {
		return err
	}
	project, projectPath, err := openVerifiedRoot(projectRoot)
	if err != nil {
		return err
	}
	defer project.Close()
	if err := validateGenerationProjectRoot(project, mod, lock); err != nil {
		return err
	}
	return revalidateRootPath(projectPath, project)
}

func validateGenerationProjectRoot(project *os.Root, mod *ModFile, lock *LockFile) error {
	generation, _, err := retainValidatedGenerationProjectRoot(project, mod, lock)
	if err != nil {
		return err
	}
	return generation.Close()
}

func retainValidatedGenerationProjectRoot(project *os.Root, mod *ModFile, lock *LockFile) (_ *os.Root, _ generationManifest, retErr error) {
	doggo, err := openVerifiedSubroot(project, ".doggo")
	if err != nil {
		return nil, nil, fmt.Errorf(".doggo: %w", err)
	}
	defer doggo.Close()
	generations, err := openVerifiedSubroot(doggo, "generations")
	if err != nil {
		return nil, nil, fmt.Errorf("generations: %w", err)
	}
	defer generations.Close()
	generation, err := openVerifiedSubroot(generations, lock.Generation)
	if err != nil {
		return nil, nil, fmt.Errorf("generation %s: %w", lock.Generation, err)
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, generation.Close())
		}
	}()
	var manifest generationManifest
	if err := validateGenerationRootWithManifest(generation, mod, lock, &manifest); err != nil {
		return nil, nil, err
	}
	if err := revalidateSubroot(generations, lock.Generation, generation); err != nil {
		return nil, nil, err
	}
	if err := revalidateSubroot(doggo, "generations", generations); err != nil {
		return nil, nil, err
	}
	if err := revalidateSubroot(project, ".doggo", doggo); err != nil {
		return nil, nil, err
	}
	return generation, manifest, nil
}

func validateGenerationRoot(generation *os.Root, mod *ModFile, lock *LockFile) error {
	return validateGenerationRootWithManifest(generation, mod, lock, nil)
}

func validateGenerationRootWithManifest(generation *os.Root, mod *ModFile, lock *LockFile, manifest *generationManifest) error {
	if mod == nil {
		return fmt.Errorf("mod file is nil")
	}
	if lock == nil {
		return fmt.Errorf("lock file is nil")
	}
	if err := validateLockSchema(mod, lock); err != nil {
		return err
	}

	markerInfo, err := generation.Lstat(generationMarkerName)
	if err != nil {
		return fmt.Errorf("generation marker: %w", err)
	}
	if !markerInfo.Mode().IsRegular() || markerInfo.Size() != int64(len(lock.Generation)+1) {
		return fmt.Errorf("generation marker size or type is invalid")
	}
	marker, err := readVerifiedRootFileInfo(generation, generationMarkerName, markerInfo)
	if err != nil {
		return fmt.Errorf("generation marker: %w", err)
	}
	if string(marker) != lock.Generation+"\n" {
		return fmt.Errorf("generation marker mismatch")
	}

	depsRoot, err := openVerifiedSubroot(generation, "deps")
	if err != nil {
		return fmt.Errorf("dependency root: %w", err)
	}
	if err := depsRoot.Close(); err != nil {
		return err
	}
	toolchain, err := openVerifiedSubroot(generation, "toolchain")
	if err != nil {
		return fmt.Errorf("toolchain root: %w", err)
	}
	if err := toolchain.Close(); err != nil {
		return err
	}

	for _, dep := range lock.Deps {
		depRoot, err := openVerifiedRootPath(generation, filepath.FromSlash(dep.InstallDir))
		if err != nil {
			return fmt.Errorf("dependency %s: %w", dep.ModuleID, err)
		}
		digest, hashErr := CanonicalRootDigest(depRoot)
		closeErr := depRoot.Close()
		if hashErr != nil {
			return fmt.Errorf("dependency %s: %w", dep.ModuleID, hashErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if digest != dep.Digest {
			return fmt.Errorf("dependency %s digest mismatch", dep.ModuleID)
		}
	}

	if lock.Build.Binary == "" {
		if _, err := generation.Lstat("bin"); err == nil {
			return fmt.Errorf("sync-only generation contains bin")
		} else if !os.IsNotExist(err) {
			return err
		}
	} else {
		binaryName := filepath.FromSlash(path.Join("bin", GenerationBinaryName()))
		binaryInfo, err := generation.Lstat(binaryName)
		if err != nil {
			return fmt.Errorf("binary: %w", err)
		}
		if !binaryInfo.Mode().IsRegular() {
			return fmt.Errorf("binary is not a regular file")
		}
		sum := sha256.New()
		if err := streamVerifiedRootFile(generation, generationEntry{path: filepath.ToSlash(binaryName), size: binaryInfo.Size(), info: binaryInfo}, sum, false); err != nil {
			return fmt.Errorf("binary: %w", err)
		}
		if hex.EncodeToString(sum.Sum(nil)) != lock.Build.SHA256 {
			return fmt.Errorf("binary digest mismatch")
		}
	}

	digest, files, err := generationDigestSnapshotRoot(generation, lock)
	if err != nil {
		return err
	}
	if digest != lock.Generation {
		return fmt.Errorf("generation digest mismatch")
	}
	if manifest != nil {
		*manifest = files
	}
	return nil
}

func validateLockSchema(mod *ModFile, lock *LockFile) error {
	if lock.LockVersion != LockVersion {
		return fmt.Errorf("unsupported lock version: %d", lock.LockVersion)
	}
	if !IsLowerHexDigest(lock.Generation) {
		return fmt.Errorf("invalid generation digest")
	}
	if !IsLowerHexDigest(lock.ModLockInputsSHA256) {
		return fmt.Errorf("invalid mod lock inputs digest")
	}
	if lock.ModLockInputsSHA256 != HashModLockInputs(mod) {
		return fmt.Errorf("mod lock inputs hash mismatch")
	}

	installDirs := make(map[string]struct{}, len(lock.Deps))
	moduleIDs := make(map[string][2]string, len(lock.Deps))
	goModules := make(map[string][3]string, len(lock.Deps))
	for i, dep := range lock.Deps {
		if err := validateLockDep(dep); err != nil {
			return fmt.Errorf("dependency %d: %w", i, err)
		}
		if _, exists := installDirs[dep.InstallDir]; exists {
			return fmt.Errorf("duplicate install directory %s", dep.InstallDir)
		}
		installDirs[dep.InstallDir] = struct{}{}
		moduleSnapshot := [2]string{dep.Revision, dep.Digest}
		if current, exists := moduleIDs[dep.ModuleID]; exists && current != moduleSnapshot {
			return fmt.Errorf("module identity conflict for %s", dep.ModuleID)
		}
		moduleIDs[dep.ModuleID] = moduleSnapshot
		if dep.GoModule != "" {
			goSnapshot := [3]string{dep.ModuleID, dep.Revision, dep.Digest}
			if current, exists := goModules[dep.GoModule]; exists && current != goSnapshot {
				return fmt.Errorf("Go module conflict for %s", dep.GoModule)
			}
			goModules[dep.GoModule] = goSnapshot
		}
	}
	if err := validateCanonicalScan(lock.Scan); err != nil {
		return err
	}

	binary, digest := lock.Build.Binary, lock.Build.SHA256
	switch {
	case binary == "" && digest == "":
		return nil
	case binary == "" || digest == "":
		return fmt.Errorf("binary path and digest must both be empty or non-empty")
	case !IsLowerHexDigest(digest):
		return fmt.Errorf("invalid binary digest")
	case binary != expectedGenerationBinary(lock.Generation):
		return fmt.Errorf("invalid binary path")
	default:
		return nil
	}
}

func validateLockDep(dep LockDep) error {
	parsed, err := depsource.Parse(dep.Source)
	if err != nil {
		return fmt.Errorf("invalid dependency source: %w", err)
	}
	if parsed.Kind != dep.Kind {
		return fmt.Errorf("dependency source kind %q does not match kind %q", parsed.Kind, dep.Kind)
	}
	if parsed.Declaration != dep.Source {
		return fmt.Errorf("dependency source is not canonical")
	}

	prefix, payload, ok := strings.Cut(dep.ModuleID, ":")
	if !ok || payload == "" || (prefix != "doggo" && prefix != "go") {
		return fmt.Errorf("invalid module ID %q", dep.ModuleID)
	}
	if strings.TrimSpace(payload) != payload || strings.ContainsAny(payload, " \\\t\r\n") {
		return fmt.Errorf("invalid module ID %q", dep.ModuleID)
	}
	if dep.GoModule != "" {
		if err := module.CheckPath(dep.GoModule); err != nil {
			return fmt.Errorf("invalid Go module %q: %w", dep.GoModule, err)
		}
	}
	if prefix == "go" && dep.GoModule != payload {
		return fmt.Errorf("Go module does not match module ID")
	}
	if !IsLowerHexDigest(dep.Digest) {
		return fmt.Errorf("invalid dependency digest")
	}
	switch parsed.Kind {
	case depsource.Git:
		if dep.Source == "" || strings.TrimSpace(dep.Source) != dep.Source {
			return fmt.Errorf("git dependency has invalid source")
		}
		if dep.Revision == "" {
			return fmt.Errorf("git dependency has empty revision")
		}
	case depsource.Path:
		if dep.Revision != "" {
			return fmt.Errorf("path dependency has revision")
		}
		if err := validatePathSource(dep.Source); err != nil {
			return err
		}
	case depsource.Go:
		if dep.GoModule != parsed.ModulePath {
			return fmt.Errorf("Go module does not match dependency source")
		}
		if dep.Revision == "" {
			return fmt.Errorf("Go dependency has empty revision")
		}
		if module.CanonicalVersion(dep.Revision) != dep.Revision {
			return fmt.Errorf("Go dependency has noncanonical revision %q", dep.Revision)
		}
		if err := module.Check(dep.GoModule, dep.Revision); err != nil {
			return fmt.Errorf("invalid Go module revision: %w", err)
		}
		if parsed.Query != "" && parsed.Query != "latest" && parsed.Query != dep.Revision {
			return fmt.Errorf("Go dependency revision does not match exact query")
		}
	default:
		return fmt.Errorf("unknown dependency kind %q", dep.Kind)
	}
	wantDir := "deps/" + DependencyKey(dep)
	if dep.InstallDir != wantDir {
		return fmt.Errorf("install directory %q does not match dependency key", dep.InstallDir)
	}
	return nil
}

func validatePathSource(source string) error {
	if source == "" || strings.Contains(source, "\\") || path.IsAbs(source) || filepath.IsAbs(filepath.FromSlash(source)) {
		return fmt.Errorf("path dependency source must be relative")
	}
	if source != "." && source != ".." && !strings.HasPrefix(source, "./") && !strings.HasPrefix(source, "../") {
		return fmt.Errorf("path dependency source is not a path declaration")
	}
	clean := path.Clean(source)
	if clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") {
		clean = "./" + clean
	}
	if clean != source {
		return fmt.Errorf("path dependency source is not canonical")
	}
	return nil
}

func validateCanonicalScan(scan LockScan) error {
	packages := normalizeStringSlice(scan.GoPackages)
	if len(packages) != len(scan.GoPackages) {
		return fmt.Errorf("Go package scan is not canonical")
	}
	for i := range packages {
		if packages[i] != scan.GoPackages[i] {
			return fmt.Errorf("Go package scan is not canonical")
		}
		if err := module.CheckImportPath(packages[i]); err != nil {
			return fmt.Errorf("invalid Go package %q: %w", packages[i], err)
		}
	}
	targets := NormalizeReifyTargets(scan.ReifyTargets)
	if len(targets) != len(scan.ReifyTargets) {
		return fmt.Errorf("reify target scan is not canonical")
	}
	for i := range targets {
		if targets[i] != scan.ReifyTargets[i] || targets[i].Source == "" || targets[i].Runtime == "" {
			return fmt.Errorf("reify target scan is not canonical")
		}
	}
	return nil
}

func generationDigestRoot(root *os.Root, lock *LockFile) (string, error) {
	digest, _, err := generationDigestSnapshotRoot(root, lock)
	return digest, err
}

func generationDigestSnapshotRoot(root *os.Root, lock *LockFile) (string, generationManifest, error) {
	entries, err := collectRootEntries(root)
	if err != nil {
		return "", nil, err
	}
	manifest := make(generationManifest, len(entries))
	files := entries[:0]
	for _, entry := range entries {
		if entry.directory {
			manifest[entry.path] = generationFile{info: entry.info, directory: true}
		} else if entry.path != generationMarkerName {
			files = append(files, entry)
		}
	}

	hash := sha256.New()
	writeDigestField(hash, []byte("doggo-generation-v1"))
	writeDigestUint64(hash, uint64(len(files)))
	for _, entry := range files {
		writeDigestField(hash, []byte(entry.path))
		writeDigestField(hash, normalizedMode(entry.executable))
		writeDigestUint64(hash, uint64(entry.size))
		fileHash := sha256.New()
		if err := streamVerifiedRootFile(root, entry, io.MultiWriter(hash, fileHash), false); err != nil {
			return "", nil, err
		}
		manifest[entry.path] = generationFile{
			info: entry.info, size: entry.size, executable: entry.executable,
			sha256: hex.EncodeToString(fileHash.Sum(nil)),
		}
	}

	deps := append([]LockDep(nil), lock.Deps...)
	sort.Slice(deps, func(i, j int) bool { return compareGenerationDeps(deps[i], deps[j]) < 0 })
	writeDigestField(hash, []byte("dependencies"))
	writeDigestUint64(hash, uint64(len(deps)))
	for _, dep := range deps {
		for _, field := range []string{dep.Kind, dep.ModuleID, dep.GoModule, dep.Revision, dep.Digest, dep.InstallDir} {
			writeDigestField(hash, []byte(field))
		}
	}

	packages := normalizeStringSlice(lock.Scan.GoPackages)
	writeDigestField(hash, []byte("go-packages"))
	writeDigestUint64(hash, uint64(len(packages)))
	for _, pkg := range packages {
		writeDigestField(hash, []byte(pkg))
	}
	targets := NormalizeReifyTargets(lock.Scan.ReifyTargets)
	writeDigestField(hash, []byte("reify-targets"))
	writeDigestUint64(hash, uint64(len(targets)))
	for _, target := range targets {
		writeDigestField(hash, []byte(target.Source))
		writeDigestField(hash, []byte(target.Runtime))
	}
	return hex.EncodeToString(hash.Sum(nil)), manifest, nil
}

func compareGenerationDeps(a, b LockDep) int {
	left := []string{a.Kind, a.ModuleID, a.GoModule, a.Revision, a.Digest, a.InstallDir}
	right := []string{b.Kind, b.ModuleID, b.GoModule, b.Revision, b.Digest, b.InstallDir}
	for i := range left {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	return 0
}

// CanonicalRootDigest hashes a dependency tree for snapshot creation and validation.
// Entries use slash-separated paths, canonical ordering, normalized modes, and
// length-framed content. Files are streamed with identity checks; symlinks and
// special files are rejected. The caller retains ownership of root.
func CanonicalRootDigest(root *os.Root) (string, error) {
	entries, err := collectRootEntries(root)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	writeDigestUint64(hash, uint64(len(entries)))
	for _, entry := range entries {
		kind := []byte{'f'}
		mode := normalizedMode(entry.executable)
		if entry.directory {
			kind = []byte{'d'}
			mode = []byte("0755")
		}
		writeDigestField(hash, kind)
		writeDigestField(hash, []byte(entry.path))
		writeDigestField(hash, mode)
		if entry.directory {
			writeDigestField(hash, nil)
		} else if err := streamVerifiedRootFile(root, entry, hash, true); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func collectRootEntries(root *os.Root) ([]generationEntry, error) {
	entries := make([]generationEntry, 0, 32)
	if err := collectRootDirectory(root, ".", &entries); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return entries, nil
}

func collectRootDirectory(root *os.Root, directory string, entries *[]generationEntry) error {
	opened, err := root.Open(directory)
	if err != nil {
		return err
	}
	before, err := opened.Stat()
	if err != nil || !before.IsDir() {
		opened.Close()
		return fmt.Errorf("directory changed while opening: %s", directory)
	}
	names, readErr := opened.Readdirnames(-1)
	if readErr != nil {
		opened.Close()
		return readErr
	}
	sort.Strings(names)
	for _, name := range names {
		rel := name
		if directory != "." {
			rel = filepath.Join(directory, name)
		}
		info, err := root.Lstat(rel)
		if err != nil {
			opened.Close()
			return err
		}
		canonical := filepath.ToSlash(rel)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			opened.Close()
			return fmt.Errorf("generation entry %s is a symlink", canonical)
		case info.IsDir():
			*entries = append(*entries, generationEntry{path: canonical, directory: true, info: info})
			if err := collectRootDirectory(root, rel, entries); err != nil {
				opened.Close()
				return err
			}
		case info.Mode().IsRegular():
			*entries = append(*entries, generationEntry{path: canonical, executable: info.Mode()&0111 != 0, size: info.Size(), info: info})
		default:
			opened.Close()
			return fmt.Errorf("generation entry %s is special (%s)", canonical, info.Mode().Type())
		}
	}
	after, lstatErr := root.Lstat(directory)
	closeErr := opened.Close()
	if lstatErr != nil || !os.SameFile(before, after) {
		return fmt.Errorf("generation directory changed: %s", directory)
	}
	return closeErr
}

func readVerifiedRootFile(root *os.Root, name string) ([]byte, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	return readVerifiedRootFileInfo(root, name, before)
}

func readVerifiedRootFileInfo(root *os.Root, name string, before fs.FileInfo) ([]byte, error) {
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	opened, statErr := file.Stat()
	current, currentErr := root.Lstat(name)
	if statErr != nil || currentErr != nil || current.Mode()&os.ModeSymlink != 0 || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(opened, current) {
		file.Close()
		return nil, fmt.Errorf("regular file changed while opening: %s", name)
	}
	if afterProjectFileOpenForTest != nil {
		afterProjectFileOpenForTest(name)
	}
	content, readErr := io.ReadAll(file)
	if afterProjectFileReadForTest != nil {
		afterProjectFileReadForTest(name)
	}
	after, lstatErr := root.Lstat(name)
	closeErr := file.Close()
	if statErr != nil || lstatErr != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		return nil, fmt.Errorf("regular file changed while opening: %s", name)
	}
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return content, nil
}

func streamVerifiedRootFile(root *os.Root, entry generationEntry, hash io.Writer, framed bool) error {
	name := filepath.FromSlash(entry.path)
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	opened, statErr := file.Stat()
	current, lstatErr := root.Lstat(name)
	if statErr != nil || lstatErr != nil || !sameRegularFile(entry.info, opened) || !sameRegularFile(opened, current) {
		file.Close()
		return fmt.Errorf("regular file changed while opening: %s", entry.path)
	}
	if framed {
		err = writeDigestStream(hash, file, entry.size)
	} else {
		_, err = io.CopyN(hash, file, entry.size)
	}
	openedAfter, afterStatErr := file.Stat()
	after, afterLstatErr := root.Lstat(name)
	closeErr := file.Close()
	if afterStatErr != nil || afterLstatErr != nil || !sameRegularFile(entry.info, openedAfter) || !sameRegularFile(openedAfter, after) {
		return fmt.Errorf("regular file changed while reading: %s", entry.path)
	}
	return errors.Join(err, closeErr)
}

func sameRegularFile(before, after fs.FileInfo) bool {
	return before != nil && after != nil && before.Mode().IsRegular() && after.Mode().IsRegular() && before.Size() == after.Size() && before.Mode() == after.Mode() && before.ModTime() == after.ModTime() && os.SameFile(before, after)
}

func openVerifiedRootPath(root *os.Root, name string) (*os.Root, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return nil, fmt.Errorf("%s is not an owned directory", name)
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, statErr := child.Stat(".")
	after, lstatErr := root.Lstat(name)
	if statErr != nil || lstatErr != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		child.Close()
		return nil, fmt.Errorf("%s changed while opening", name)
	}
	return child, nil
}

// DependencyKey identifies a snapshot by its framed dependency identity, not its
// source alias or derived install directory. Creation and validation share it.
func DependencyKey(dep LockDep) string {
	hash := sha256.New()
	writeDigestField(hash, []byte("doggo-dependency-v1"))
	for _, field := range []string{dep.Kind, dep.ModuleID, dep.GoModule, dep.Revision, dep.Digest} {
		writeDigestField(hash, []byte(field))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// GenerationBinaryName returns the canonical built-generation binary name for
// the host platform.
func GenerationBinaryName() string {
	if runtime.GOOS == "windows" {
		return "doggo.exe"
	}
	return "doggo"
}

// GenerationBinaryPath returns the canonical project-relative lock path for a
// built generation binary.
func GenerationBinaryPath(generation string) string {
	return path.Join(".doggo", "generations", generation, "bin", GenerationBinaryName())
}

func expectedGenerationBinary(generation string) string {
	return GenerationBinaryPath(generation)
}

func normalizedMode(executable bool) []byte {
	if executable {
		return []byte("0755")
	}
	return []byte("0644")
}

func writeDigestField(hash io.Writer, value []byte) {
	writeDigestUint64(hash, uint64(len(value)))
	_, _ = hash.Write(value)
}

func writeDigestStream(hash io.Writer, reader io.Reader, size int64) error {
	writeDigestUint64(hash, uint64(size))
	_, err := io.CopyN(hash, reader, size)
	return err
}

func writeDigestUint64(hash io.Writer, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = hash.Write(encoded[:])
}
