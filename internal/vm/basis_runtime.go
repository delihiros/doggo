package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"doggo/internal/project"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type basisRuntimeConfig struct {
	projectRoot string
	loadPaths   []string
	lock        *project.LockFile
}

type basisSnapshotFn struct {
	runner  *Runner
	current bool
}

func (b *basisSnapshotFn) String() string {
	if b.current {
		return "#<builtin:basis-current*>"
	}
	return "#<builtin:basis-initial*>"
}

func (b *basisSnapshotFn) Type() string { return "function" }
func (b *basisSnapshotFn) Hash() uint32 { return 0 }
func (b *basisSnapshotFn) Equals(other types.Value) bool {
	o, ok := other.(*basisSnapshotFn)
	return ok && b == o
}
func (b *basisSnapshotFn) IsSelfEvaluating() bool { return true }

func (b *basisSnapshotFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s: expected 0 arguments, got %d", b.String(), len(args))
	}
	if b.current {
		return b.runner.currentBasisValue()
	}
	return b.runner.initialBasisValue()
}

func (r *Runner) currentBasisValue() (types.Value, error) {
	return r.runtimeServices.currentBasisValue()
}

func (r *Runner) initialBasisValue() (types.Value, error) {
	return r.runtimeServices.initialBasisValue()
}

func buildBasisValue(cfg basisRuntimeConfig) (types.Value, error) {
	projectRoot := filepath.ToSlash(cfg.projectRoot)
	loadPaths := stringsToVector(cfg.loadPaths, true)

	modFile, err := loadBasisModFile(cfg.projectRoot)
	if err != nil {
		return nil, err
	}
	lockFile := cfg.lock

	moduleName := types.Value(atom.NilValue)
	deps := coll.NewVector()
	if modFile != nil {
		if modFile.Module != "" {
			moduleName = atom.NewString(modFile.Module)
		}
		deps = stringsToVector(modFile.Deps, false)
	}

	lockDeps := coll.NewVector()
	generatedTargets := emptyGeneratedTargets()
	if lockFile != nil {
		lockDepItems := make([]types.Value, 0, len(lockFile.Deps))
		for _, dep := range lockFile.Deps {
			lockDepItems = append(lockDepItems, coll.NewMapFromPairs([]types.Value{
				atom.NewKeyword("source"), atom.NewString(dep.Source),
				atom.NewKeyword("kind"), atom.NewKeyword(dep.Kind),
				atom.NewKeyword("module-id"), atom.NewString(dep.ModuleID),
				atom.NewKeyword("go-module"), atom.NewString(dep.GoModule),
				atom.NewKeyword("revision"), atom.NewString(dep.Revision),
				atom.NewKeyword("digest"), atom.NewString(dep.Digest),
				atom.NewKeyword("install-dir"), atom.NewString(filepath.ToSlash(dep.InstallDir)),
			}))
		}
		lockDeps = coll.NewVector(lockDepItems...)
		reifyTargetItems := make([]types.Value, 0, len(lockFile.Scan.ReifyTargets))
		for _, target := range lockFile.Scan.ReifyTargets {
			reifyTargetItems = append(reifyTargetItems, coll.NewMapFromPairs([]types.Value{
				atom.NewKeyword("source"), atom.NewString(target.Source),
				atom.NewKeyword("runtime"), atom.NewString(target.Runtime),
			}))
		}
		generatedTargets = coll.NewMapFromPairs([]types.Value{
			atom.NewKeyword("go-packages"), stringsToVector(lockFile.Scan.GoPackages, false),
			atom.NewKeyword("reify-targets"), coll.NewVector(reifyTargetItems...),
			atom.NewKeyword("build"), coll.NewMapFromPairs([]types.Value{
				atom.NewKeyword("binary"), atom.NewString(filepath.ToSlash(lockFile.Build.Binary)),
				atom.NewKeyword("sha256"), atom.NewString(lockFile.Build.SHA256),
			}),
		})
	}

	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("project-root"), atom.NewString(projectRoot),
		atom.NewKeyword("module-name"), moduleName,
		atom.NewKeyword("deps"), deps,
		atom.NewKeyword("lock-deps"), lockDeps,
		atom.NewKeyword("load-paths"), loadPaths,
		atom.NewKeyword("generated-targets"), generatedTargets,
	}), nil
}

func loadBasisModFile(projectRoot string) (*project.ModFile, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return nil, nil
	}
	path := project.ModPath(projectRoot)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return project.LoadModFile(path)
}

func stringsToVector(values []string, normalizePath bool) *coll.Vector {
	items := make([]types.Value, 0, len(values))
	for _, value := range values {
		if normalizePath {
			value = filepath.ToSlash(value)
		}
		items = append(items, atom.NewString(value))
	}
	return coll.NewVector(items...)
}

func emptyGeneratedTargets() *coll.Map {
	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("go-packages"), coll.NewVector(),
		atom.NewKeyword("reify-targets"), coll.NewVector(),
		atom.NewKeyword("build"), coll.NewMapFromPairs(nil),
	})
}
