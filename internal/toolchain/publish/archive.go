package publish

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"doggo/internal/depsource"
	"doggo/internal/project"
	toolchaindeps "doggo/internal/toolchain/deps"
)

type publishConfig struct {
	loadModFile     func(path string) (*project.ModFile, error)
	resolveRegistry func(projectRoot, override string, forWrite bool) (string, error)
	loadRegistry    func(path string) (*project.RegistryFile, error)
	writeRegistry   func(path string, registry *project.RegistryFile) error
	createArchive   func(projectRoot, name, version string) (string, error)
}

func runPublishWorkflow(projectRoot string, opts Options, cfg publishConfig) (*Result, error) {
	loadModFile := cfg.loadModFile
	if loadModFile == nil {
		loadModFile = project.LoadModFile
	}
	resolveRegistry := cfg.resolveRegistry
	if resolveRegistry == nil {
		resolveRegistry = toolchaindeps.ResolveRegistryPath
	}
	loadRegistry := cfg.loadRegistry
	if loadRegistry == nil {
		loadRegistry = project.LoadRegistryFile
	}
	writeRegistry := cfg.writeRegistry
	if writeRegistry == nil {
		writeRegistry = project.WriteRegistryFile
	}
	createArchive := cfg.createArchive
	if createArchive == nil {
		createArchive = createPublishArchive
	}

	mod, err := loadModFile(project.ModPath(projectRoot))
	if err != nil {
		return nil, err
	}
	if err := validatePublishMetadata(mod); err != nil {
		return nil, err
	}

	source := strings.TrimSpace(opts.Source)
	if source == "" {
		return nil, fmt.Errorf("publish requires --source")
	}
	parsed, err := depsource.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("invalid publish source %q: %w", source, err)
	}
	source = parsed.Declaration
	versionForRegistry := project.NormalizeRegistryVersion(mod.Version)

	registryPath, err := resolveRegistry(projectRoot, opts.RegistryPath, true)
	if err != nil {
		return nil, err
	}
	registry, err := loadOrCreateRegistryFile(registryPath, loadRegistry)
	if err != nil {
		return nil, err
	}

	entry := project.RegistryPackage{
		Name:    mod.Name,
		Version: versionForRegistry,
		Source:  source,
		License: mod.License,
		Compat:  mod.Compat,
	}

	existingIndex := -1
	for i := range registry.Packages {
		if registry.Packages[i].Name == entry.Name && project.RegistryVersionsEqual(registry.Packages[i].Version, entry.Version) {
			if !opts.AllowOverwrite {
				return nil, fmt.Errorf("registry already contains %s@%s (use --allow-overwrite to replace)", entry.Name, entry.Version)
			}
			existingIndex = i
			break
		}
	}

	archivePath, err := createArchive(projectRoot, mod.Name, versionForRegistry)
	if err != nil {
		return nil, err
	}

	if existingIndex >= 0 {
		registry.Packages[existingIndex] = entry
	} else {
		registry.Packages = append(registry.Packages, entry)
	}
	project.SortRegistryPackages(registry.Packages)

	if err := writeRegistry(registryPath, registry); err != nil {
		return nil, err
	}

	return &Result{
		Package:      entry,
		RegistryPath: registryPath,
		ArchivePath:  archivePath,
	}, nil
}

func validatePublishMetadata(mod *project.ModFile) error {
	if strings.TrimSpace(mod.Name) == "" {
		return fmt.Errorf("%s: :name is required for publish", project.ModFileName)
	}
	if strings.TrimSpace(mod.Version) == "" {
		return fmt.Errorf("%s: :version is required for publish", project.ModFileName)
	}
	if !project.IsSemver(mod.Version) {
		return fmt.Errorf("%s: :version must be semver (e.g. 1.2.3)", project.ModFileName)
	}
	if strings.TrimSpace(mod.License) == "" {
		return fmt.Errorf("%s: :license is required for publish", project.ModFileName)
	}
	if strings.TrimSpace(mod.Compat) == "" {
		return fmt.Errorf("%s: :compat is required for publish", project.ModFileName)
	}
	return nil
}

func createPublishArchive(projectRoot, pkgName, version string) (string, error) {
	files, err := collectPublishFiles(projectRoot)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no publishable files found in project")
	}

	outputDir := filepath.Join(projectRoot, "dist", "publish")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", err
	}
	fileName := fmt.Sprintf("%s-%s.tgz", sanitizePackageName(pkgName), sanitizePackageName(version))
	archivePath := filepath.Join(outputDir, fileName)

	// Build beside the destination so failures cannot truncate a previous
	// archive or follow an output symlink to an unrelated file.
	outFile, err := createPublishTemp(outputDir, fileName)
	if err != nil {
		return "", err
	}
	defer os.Remove(outFile.Name())
	if err := func() (retErr error) {
		defer func() { retErr = errors.Join(retErr, outFile.Close()) }()
		if info, err := os.Lstat(archivePath); err == nil && info.Mode().IsRegular() {
			// Replacing a regular archive preserves its permissions, as the
			// previous in-place write did. Never copy a symlink target's mode.
			if err := outFile.Chmod(info.Mode().Perm()); err != nil {
				return err
			}
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := writePublishArchive(projectRoot, files, outFile); err != nil {
			return err
		}
		return outFile.Sync()
	}(); err != nil {
		return "", err
	}
	if err := os.Rename(outFile.Name(), archivePath); err != nil {
		return "", err
	}
	return archivePath, nil
}

func createPublishTemp(outputDir, fileName string) (*os.File, error) {
	for {
		name := filepath.Join(outputDir, "."+fileName+".tmp-"+rand.Text())
		// O_EXCL prevents following an existing entry. Mode 0666 lets the
		// caller's umask restrict a new artifact just as os.Create did.
		file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return file, err
	}
}

func writePublishArchive(projectRoot string, files []string, output io.Writer) (retErr error) {
	gw := gzip.NewWriter(output)
	tw := tar.NewWriter(gw)
	// Closing tar emits its end markers; closing gzip flushes compressed bytes
	// and its checksum. Both must succeed before the archive can be published.
	defer func() { retErr = errors.Join(retErr, tw.Close(), gw.Close()) }()

	for _, rel := range files {
		abs := filepath.Join(projectRoot, filepath.FromSlash(rel))
		info, err := os.Lstat(abs)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(abs)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		if err := errors.Join(copyErr, f.Close()); err != nil {
			return err
		}
	}
	return nil
}

func collectPublishFiles(projectRoot string) ([]string, error) {
	files := []string{}
	skipDirs := map[string]struct{}{
		".git":         {},
		".doggo":       {},
		"dist":         {},
		"vendor":       {},
		"node_modules": {},
	}

	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(projectRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		base := d.Name()
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if _, skip := skipDirs[base]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash == project.ModFileName || strings.HasSuffix(relSlash, ".dog") || base == "README.md" || strings.HasPrefix(base, "LICENSE") {
			files = append(files, relSlash)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func sanitizePackageName(v string) string {
	cleaned := strings.TrimSpace(v)
	if cleaned == "" {
		return "package"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-", "@", "-")
	return replacer.Replace(cleaned)
}

func loadOrCreateRegistryFile(path string, loadRegistry func(path string) (*project.RegistryFile, error)) (*project.RegistryFile, error) {
	if fileExists(path) {
		return loadRegistry(path)
	}
	return &project.RegistryFile{
		Packages:   []project.RegistryPackage{},
		Advisories: []project.RegistryAdvisory{},
	}, nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
