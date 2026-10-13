package generation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"doggo/internal/project"
	"doggo/internal/toolchain/codegen"
	"doggo/internal/toolchain/depsnapshot"
	"doggo/internal/toolchain/host"
	"doggo/internal/toolchain/scan"
)

type Service struct{}

var (
	afterPrepareSyncStageVerifyForTest       func(string)
	afterPrepareSyncStageRootsCreatedForTest func(string)
)

func (Service) PrepareSync(projectRoot string, sources []string) (_ *PreparedGeneration, err error) {
	p, err := beginPreparation(projectRoot)
	if err != nil {
		return nil, err
	}
	return prepareSync(p, sources)
}

// PrepareSelectedSync refreshes one direct dependency and reuses all other
// direct snapshots and their closure under the same exclusive writer lease.
func (Service) PrepareSelectedSync(projectRoot, selected string, previous *project.LockFile, direct []project.LockDep) (_ *PreparedGeneration, err error) {
	p, err := beginPreparation(projectRoot)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, p.Abort())
		}
	}()
	current, err := p.owner.LoadLock()
	if err != nil {
		return nil, err
	}
	if previous == nil || current.Generation != previous.Generation {
		return nil, fmt.Errorf("current generation changed before selected update")
	}
	if project.HashModLockInputs(p.mod) != previous.ModLockInputsSHA256 {
		return nil, fmt.Errorf("%s changed before writer lease", project.ModFileName)
	}
	var prepared *PreparedGeneration
	err = p.owner.WithPublishedSnapshot(p.mod, current, func(snapshot *project.GenerationSnapshot) error {
		retained, retainedPaths, err := depsnapshot.RetainedDependencyClosureWithPaths(snapshot, current.Deps, direct, selected)
		if err != nil {
			return err
		}
		tree, err := p.owner.StageTree()
		if err != nil {
			return err
		}
		if err := snapshot.CopyDependencies(tree, retained); err != nil {
			return err
		}
		retainedDirect := make([]project.LockDep, 0, len(direct))
		for _, dep := range direct {
			if dep.Source != selected {
				retainedDirect = append(retainedDirect, dep)
			}
		}
		prepared, err = prepareSyncRetaining(p, []string{selected}, retained, retainedDirect, retainedPaths)
		return err
	})
	return prepared, err
}

// PrepareSyncTree records fresh-project generation output in tree from the
// first created entry, so its caller owns cleanup even if preparation fails.
func (Service) PrepareSyncTree(projectRoot *os.Root, projectPath string, tree *project.OwnedTree, sources []string) (_ *PreparedGeneration, err error) {
	p, err := beginPreparationTree(projectRoot, projectPath, tree)
	if err != nil {
		return nil, err
	}
	return prepareSync(p, sources)
}

func prepareSync(p *PreparedGeneration, sources []string) (_ *PreparedGeneration, err error) {
	return prepareSyncRetaining(p, sources, nil, nil, nil)
}

func prepareSyncRetaining(p *PreparedGeneration, sources []string, retained, retainedDirect []project.LockDep, retainedPaths map[string]project.LockDep) (_ *PreparedGeneration, err error) {
	defer func() {
		if err != nil {
			err = errors.Join(err, p.Abort())
		}
	}()

	if sources == nil {
		sources = p.mod.Deps
	}
	projectRoot, err := p.owner.OpenProjectRoot()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, projectRoot.Close()) }()
	stageTree, err := p.owner.StageTree()
	if err != nil {
		return nil, err
	}
	if err := verifyPrepareSyncBoundary(p, "dependencies"); err != nil {
		return nil, err
	}
	deps, err := depsnapshot.SnapshotDependenciesTreeRetainingPaths(projectRoot, p.projectRoot, stageTree, sources, retained, retainedDirect, retainedPaths)
	if err != nil {
		return nil, fmt.Errorf("dependency snapshot: %w", err)
	}
	if afterPrepareSyncStageRootsCreatedForTest != nil {
		afterPrepareSyncStageRootsCreatedForTest("dependencies")
	}
	if err := verifyPrepareSyncBoundary(p, "scan"); err != nil {
		return nil, err
	}
	targets, err := (scan.Service{}).DeriveTargetsTree(stageTree, p.stage, deps)
	if err != nil {
		return nil, fmt.Errorf("target scan: %w", err)
	}
	if err := verifyPrepareSyncBoundary(p, "host"); err != nil {
		return nil, err
	}
	if err := host.MaterializeHostWorkspaceTree(stageTree, p.stage, deps); err != nil {
		return nil, fmt.Errorf("host materialization: %w", err)
	}
	if afterPrepareSyncStageRootsCreatedForTest != nil {
		afterPrepareSyncStageRootsCreatedForTest("toolchain")
	}
	if err := verifyPrepareSyncBoundary(p, "interop"); err != nil {
		return nil, err
	}
	invoker := codegen.Invoker{}
	if err := invoker.GenerateInteropTree(stageTree, p.stage, targets.GoPackages); err != nil {
		return nil, fmt.Errorf("interop generation: %w", err)
	}
	if err := verifyPrepareSyncBoundary(p, "reify"); err != nil {
		return nil, err
	}
	reifyTargets, err := invoker.GenerateReifyTree(stageTree, p.stage, targets.ReifyCandidates)
	if err != nil {
		return nil, fmt.Errorf("reify generation: %w", err)
	}
	if err := verifyPrepareSyncBoundary(p, "tidy"); err != nil {
		return nil, err
	}
	if err := host.TidyHostWorkspaceTree(stageTree, p.stage); err != nil {
		return nil, fmt.Errorf("host tidy: %w", err)
	}
	if err := p.owner.VerifyStage(); err != nil {
		return nil, err
	}
	p.Lock = &project.LockFile{
		LockVersion:         project.LockVersion,
		ModLockInputsSHA256: project.HashModLockInputs(p.mod),
		Deps:                project.NormalizeLockDeps(deps),
		Scan: project.LockScan{
			GoPackages:   project.NormalizeStringSlice(targets.GoPackages),
			ReifyTargets: project.NormalizeReifyTargets(reifyTargets),
		},
	}
	return p, nil
}

func verifyPrepareSyncBoundary(p *PreparedGeneration, boundary string) error {
	if err := p.owner.VerifyStage(); err != nil {
		return err
	}
	if afterPrepareSyncStageVerifyForTest != nil {
		afterPrepareSyncStageVerifyForTest(boundary)
	}
	return nil
}

func (Service) PrepareCurrentBuild(projectRoot string) (_ *PreparedGeneration, err error) {
	p, err := beginPreparation(projectRoot)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, p.Abort())
		}
	}()

	current, err := p.owner.LoadLock()
	if err != nil {
		return nil, err
	}
	if err := p.owner.ValidatePublished(p.mod, current); err != nil {
		return nil, err
	}
	if err := p.owner.CopyGeneration(current.Generation); err != nil {
		return nil, err
	}
	p.Lock = cloneLock(current)
	p.Lock.Generation = ""
	p.Lock.Build = project.LockBuild{}
	return p, nil
}

func beginPreparation(projectRoot string) (*PreparedGeneration, error) {
	absoluteRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}
	owner, err := project.BeginGenerationOwner(absoluteRoot)
	if err != nil {
		return nil, err
	}
	return finishPreparation(owner, absoluteRoot)
}

func beginPreparationTree(root *os.Root, projectRoot string, tree *project.OwnedTree) (*PreparedGeneration, error) {
	if root == nil || tree == nil {
		if root != nil {
			return nil, errors.Join(errors.New("project root or tree is nil"), root.Close())
		}
		return nil, errors.New("project root or tree is nil")
	}
	absoluteRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	owner, err := project.BeginGenerationOwnerTree(root, absoluteRoot, tree)
	if err != nil {
		return nil, err
	}
	return finishPreparation(owner, absoluteRoot)
}

func finishPreparation(owner *project.GenerationOwner, absoluteRoot string) (*PreparedGeneration, error) {
	fail := func(cause error) (*PreparedGeneration, error) {
		return nil, errors.Join(cause, owner.Close())
	}
	mod, err := owner.LoadMod()
	if err != nil {
		return fail(err)
	}
	return &PreparedGeneration{
		projectRoot: absoluteRoot,
		stage:       owner.StagePath(),
		owner:       owner,
		mod:         mod,
		state:       statePreparing,
	}, nil
}

func cloneLock(lock *project.LockFile) *project.LockFile {
	clone := *lock
	clone.Deps = append(lock.Deps[:0:0], lock.Deps...)
	clone.Scan.GoPackages = append(lock.Scan.GoPackages[:0:0], lock.Scan.GoPackages...)
	clone.Scan.ReifyTargets = append(lock.Scan.ReifyTargets[:0:0], lock.Scan.ReifyTargets...)
	return &clone
}
