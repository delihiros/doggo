package host

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"doggo"
	"doggo/internal/project"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

var (
	beforeGoModTidyForTest       func(string)
	beforeTidyTempCleanupForTest func(string)
)

func MaterializeHostWorkspaceTree(tree *project.OwnedTree, stagePath string, deps []project.LockDep) (retErr error) {
	toolchain, err := tree.CreateRoot("toolchain", 0755, nil)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, toolchain.Close()) }()
	if err := extractSourceTree(tree); err != nil {
		return err
	}
	toolchainRoot := filepath.Join(stagePath, "toolchain")
	depsRoot := filepath.Join(stagePath, "deps")
	goModPath := filepath.Join(toolchainRoot, "go.mod")
	content, err := tree.ReadFile("toolchain/go.mod")
	if err != nil {
		return fmt.Errorf("read bundled go.mod: %w", err)
	}
	parsed, err := modfile.Parse(goModPath, content, nil)
	if err != nil {
		return fmt.Errorf("parse bundled go.mod: %w", err)
	}
	for _, dep := range project.NormalizeLockDeps(deps) {
		if dep.GoModule == "" {
			continue
		}
		installDir := filepath.Clean(filepath.FromSlash(dep.InstallDir))
		if filepath.IsAbs(installDir) || filepath.ToSlash(installDir) != dep.InstallDir || filepath.Dir(installDir) != "deps" {
			return fmt.Errorf("invalid Go dependency install directory %q", dep.InstallDir)
		}
		dependencyRoot := filepath.Join(depsRoot, filepath.Base(installDir))
		replacement, err := filepath.Rel(toolchainRoot, dependencyRoot)
		if err != nil || filepath.IsAbs(replacement) {
			return fmt.Errorf("derive relative replacement for %s: %w", dep.GoModule, err)
		}
		if err := parsed.AddRequire(dep.GoModule, moduleVersion(dep.GoModule)); err != nil {
			return fmt.Errorf("require dependency %s: %w", dep.GoModule, err)
		}
		if err := parsed.AddReplace(dep.GoModule, "", filepath.ToSlash(replacement), ""); err != nil {
			return fmt.Errorf("replace dependency %s: %w", dep.GoModule, err)
		}
	}
	formatted, err := parsed.Format()
	if err != nil {
		return fmt.Errorf("format host go.mod: %w", err)
	}
	if err := tree.WriteOwnedFile("toolchain/go.mod", formatted, 0644); err != nil {
		return fmt.Errorf("write host go.mod: %w", err)
	}
	if err := tree.VerifyComplete(); err != nil {
		return err
	}
	return nil
}

func TidyHostWorkspaceTree(tree *project.OwnedTree, stagePath string) (err error) {
	isolated, err := project.NewOwnedTempRoot(".doggo-go-")
	if err != nil {
		return err
	}
	defer func() {
		if beforeTidyTempCleanupForTest != nil {
			beforeTidyTempCleanupForTest(isolated.Path())
		}
		err = errors.Join(err, isolated.Close())
	}()
	if err := tree.CopyTo(isolated.Tree(), nil); err != nil {
		return err
	}
	workspace := filepath.Join(isolated.Path(), "toolchain")
	isolatedToolchain, err := isolated.Tree().OpenRoot("toolchain")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, isolatedToolchain.Close()) }()
	if beforeGoModTidyForTest != nil {
		beforeGoModTidyForTest(filepath.Join(stagePath, "toolchain"))
	}
	if err := project.VerifyRootPath(workspace, isolatedToolchain); err != nil {
		return err
	}
	if err := runGoModTidy(workspace); err != nil {
		return err
	}
	if err := isolated.Tree().RefreshFile("toolchain/go.mod"); err != nil {
		return err
	}
	if err := isolated.Tree().RefreshOptionalFile("toolchain/go.sum"); err != nil {
		return err
	}
	if err := project.VerifyRootPath(workspace, isolatedToolchain); err != nil {
		return err
	}
	if err := tree.VerifyComplete(); err != nil {
		return err
	}
	if err := copyTidyResultTree(isolated.Tree(), tree, "go.mod", true); err != nil {
		return err
	}
	if err := copyTidyResultTree(isolated.Tree(), tree, "go.sum", false); err != nil {
		return err
	}
	return tree.VerifyComplete()
}

func BuildBinaryAndHashTree(tree *project.OwnedTree, outputName string) (_ string, retErr error) {
	content, digest, err := buildBinaryFromTree(tree, outputName)
	if err != nil {
		return "", err
	}
	bin, err := tree.CreateRoot("bin", 0755, nil)
	if err != nil {
		return "", err
	}
	if err := bin.Close(); err != nil {
		return "", err
	}
	if err := tree.WriteFile(filepath.ToSlash(filepath.Join("bin", outputName)), content, 0755); err != nil {
		return "", err
	}
	return digest, tree.VerifyComplete()
}

func buildBinaryFromTree(tree *project.OwnedTree, outputName string) (_ []byte, _ string, retErr error) {
	if outputName == "" || filepath.Base(outputName) != outputName {
		return nil, "", fmt.Errorf("binary output name is invalid: %q", outputName)
	}
	isolated, err := project.NewOwnedTempRoot(".doggo-build-")
	if err != nil {
		return nil, "", err
	}
	defer func() { retErr = errors.Join(retErr, isolated.Close()) }()
	if err := tree.CopyTo(isolated.Tree(), func(path string, _ fs.FileInfo) bool { return path == "bin" }); err != nil {
		return nil, "", err
	}
	isolatedToolchain, err := isolated.Tree().OpenRoot("toolchain")
	if err != nil {
		return nil, "", err
	}
	defer func() { retErr = errors.Join(retErr, isolatedToolchain.Close()) }()
	isolatedBin, err := isolated.Tree().CreateRoot("bin", 0755, nil)
	if err != nil {
		return nil, "", err
	}
	if err := isolatedBin.Close(); err != nil {
		return nil, "", err
	}
	workspace := filepath.Join(isolated.Path(), "toolchain")
	output := filepath.Join(isolated.Path(), "bin", outputName)
	if err := errors.Join(isolated.Verify(), project.VerifyRootPath(workspace, isolatedToolchain)); err != nil {
		return nil, "", err
	}
	cmd := exec.Command("go", "build", "-mod=readonly", "-trimpath", "-o", output, "./cmd/doggo")
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("build project runtime: %w", err)
	}
	outputRel := filepath.ToSlash(filepath.Join("bin", outputName))
	if err := isolated.Tree().RefreshFile(outputRel); err != nil {
		return nil, "", err
	}
	if err := errors.Join(isolated.Verify(), project.VerifyRootPath(workspace, isolatedToolchain)); err != nil {
		return nil, "", err
	}
	content, err := isolated.Tree().ReadFile(outputRel)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(content)
	return content, hex.EncodeToString(digest[:]), nil
}

func moduleVersion(modulePath string) string {
	if _, major, ok := module.SplitPathVersion(modulePath); ok && major != "" {
		return strings.TrimLeft(major, "/.") + ".0.0"
	}
	return "v0.0.0"
}

func runGoModTidy(workspace string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run go mod tidy: %w", err)
	}
	return nil
}

func copyTidyResultTree(source, destination *project.OwnedTree, name string, required bool) error {
	path := filepath.ToSlash(filepath.Join("toolchain", name))
	content, err := source.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && !required {
		return destination.RemoveFile(path)
	}
	if err != nil {
		return err
	}
	return destination.WriteOwnedFile(path, content, 0644)
}

func extractSourceTree(tree *project.OwnedTree) error {
	return fs.WalkDir(doggo.ToolchainSource, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		destination := filepath.ToSlash(filepath.Join("toolchain", filepath.FromSlash(path)))
		if entry.IsDir() {
			return tree.CreateAll(destination, 0755)
		}
		content, err := fs.ReadFile(doggo.ToolchainSource, path)
		if err != nil {
			return fmt.Errorf("read bundled source %s: %w", path, err)
		}
		if err := tree.WriteFile(destination, content, 0644); err != nil {
			return fmt.Errorf("write bundled source %s: %w", path, err)
		}
		return nil
	})
}
