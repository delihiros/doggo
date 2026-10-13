package engine

import (
	"os"
	"path/filepath"
	"strings"

	"doggo/internal/project"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type BasisServices struct {
	loader       NamespaceLoader
	lock         *project.LockFile
	initial      basisSnapshot
	initialValue types.Value
	initialErr   error
}

type basisSnapshot struct {
	projectRoot string
	loadPaths   []string
	lock        *project.LockFile
}

func NewBasisServices(loader NamespaceLoader, lock *project.LockFile) *BasisServices {
	return &BasisServices{
		loader: loader,
		lock:   cloneBasisLock(lock),
	}
}

func (s *BasisServices) Current() (types.Value, error) {
	return buildBasisValue(captureBasisSnapshot(s.loader, s.lock))
}

func (s *BasisServices) Initial() (types.Value, error) {
	if s == nil {
		return buildBasisValue(basisSnapshot{})
	}
	if s.initialErr != nil {
		return nil, s.initialErr
	}
	if s.initialValue != nil {
		return s.initialValue, nil
	}
	return buildBasisValue(s.initial)
}

func (s *BasisServices) CaptureInitial() {
	if s == nil {
		return
	}
	s.initial = captureBasisSnapshot(s.loader, s.lock)
	s.initialValue, s.initialErr = buildBasisValue(s.initial)
}

func captureBasisSnapshot(loader NamespaceLoader, lock *project.LockFile) basisSnapshot {
	if loader == nil {
		return basisSnapshot{lock: lock}
	}
	return basisSnapshot{
		projectRoot: loader.ProjectRoot(),
		loadPaths:   append([]string{}, loader.GetLoadPaths()...),
		lock:        lock,
	}
}

func buildBasisValue(snapshot basisSnapshot) (types.Value, error) {
	projectRoot := filepath.ToSlash(snapshot.projectRoot)
	loadPaths := stringsToVector(snapshot.loadPaths, true)

	modFile, err := loadBasisModFile(snapshot.projectRoot)
	if err != nil {
		return nil, err
	}
	lockFile := snapshot.lock

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

func cloneBasisLock(lock *project.LockFile) *project.LockFile {
	if lock == nil {
		return nil
	}
	clone := *lock
	clone.Deps = append([]project.LockDep(nil), lock.Deps...)
	clone.Scan.GoPackages = append([]string(nil), lock.Scan.GoPackages...)
	clone.Scan.ReifyTargets = append([]project.ReifyTarget(nil), lock.Scan.ReifyTargets...)
	return &clone
}

func emptyGeneratedTargets() *coll.Map {
	return coll.NewMapFromPairs([]types.Value{
		atom.NewKeyword("go-packages"), coll.NewVector(),
		atom.NewKeyword("reify-targets"), coll.NewVector(),
		atom.NewKeyword("build"), coll.NewMapFromPairs(nil),
	})
}
