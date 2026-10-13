package generation

import (
	"errors"
	"fmt"
	"io/fs"
	"reflect"

	"doggo/internal/project"
	"doggo/internal/toolchain/host"
)

type preparedState uint8

const (
	statePreparing preparedState = iota
	statePublished
	stateCommitted
	stateClosed
)

var (
	afterPreparedBuildVerifyForTest func(string)
	publishGeneration               = func(owner *project.GenerationOwner, generation string) (bool, error) {
		return owner.PublishStage(generation)
	}
	syncPublished       = func(owner *project.GenerationOwner, generation string) error { return owner.SyncPublished(generation) }
	syncGenerationTree  = func(owner *project.GenerationOwner) error { return owner.SyncStage() }
	writeGenerationLock = func(owner *project.GenerationOwner, lock *project.LockFile) error { return owner.WriteLock(lock) }
)

type commitOutcome struct{ err error }

type publishedOwnership struct {
	name  string
	lock  *project.LockFile
	owned bool
}

// PreparedGeneration owns one staged generation and its exclusive lease.
type PreparedGeneration struct {
	Lock *project.LockFile

	projectRoot  string
	stage        string
	owner        *project.GenerationOwner
	mod          *project.ModFile
	state        preparedState
	sealed       *project.LockFile
	publication  *publishedOwnership
	outcome      *commitOutcome
	abortOutcome *commitOutcome
}

func (p *PreparedGeneration) StageRoot() string {
	if p == nil || p.state != statePreparing {
		return ""
	}
	return p.stage
}

// RequireCurrentGeneration verifies the generation used to plan this write
// after the prepared generation has acquired the exclusive project lease.
func (p *PreparedGeneration) RequireCurrentGeneration(expected string) error {
	if p == nil || p.state != statePreparing {
		return fmt.Errorf("generation is not preparing")
	}
	current, err := p.owner.LoadLock()
	if err != nil {
		return err
	}
	if err := p.owner.ValidatePublished(p.mod, current); err != nil {
		return err
	}
	if current.Generation != expected {
		return fmt.Errorf("current generation changed from %s to %s", expected, current.Generation)
	}
	return nil
}

func (p *PreparedGeneration) Build() (retErr error) {
	if p == nil || p.state != statePreparing {
		return fmt.Errorf("generation is not preparing")
	}
	if p.Lock == nil {
		return fmt.Errorf("generation lock is nil")
	}
	if p.sealed != nil {
		return fmt.Errorf("generation is already sealed")
	}
	if err := p.owner.VerifyStage(); err != nil {
		return err
	}
	if afterPreparedBuildVerifyForTest != nil {
		afterPreparedBuildVerifyForTest(p.stage)
	}
	tree, err := p.owner.StageTree()
	if err != nil {
		return err
	}
	digest, err := host.BuildBinaryAndHashTree(tree, project.GenerationBinaryName())
	if err != nil {
		return err
	}
	if !project.IsLowerHexDigest(digest) {
		return fmt.Errorf("built binary has invalid SHA-256 digest")
	}
	if err := p.owner.VerifyStage(); err != nil {
		return err
	}
	p.Lock.Build = project.LockBuild{SHA256: digest}
	return nil
}

func (p *PreparedGeneration) Commit() error {
	if p == nil {
		return fmt.Errorf("generation is not preparing")
	}
	if p.state == stateClosed {
		if p.outcome != nil {
			return p.outcome.err
		}
		return fmt.Errorf("generation is not preparing")
	}
	if p.state == statePublished {
		if err := p.checkPublishedLock(); err != nil {
			return err
		}
		if err := syncPublished(p.owner, p.publication.name); err != nil {
			return err
		}
		return p.commitPublished()
	}
	if p.state != statePreparing {
		return fmt.Errorf("generation is not preparing")
	}
	if p.sealed == nil {
		if err := project.ValidateTentativeLock(p.mod, p.Lock); err != nil {
			return err
		}
		p.sealed = cloneLock(p.Lock)
	} else if !reflect.DeepEqual(p.Lock, p.sealed) {
		return fmt.Errorf("generation lock changed after seal")
	}
	if p.sealed.Generation == "" {
		digest, err := p.owner.StageDigest(p.sealed)
		if err != nil {
			return err
		}
		p.sealed.Generation = digest
		qualifyBinary(p.sealed, digest)
		p.Lock = cloneLock(p.sealed)
	}
	digest := p.sealed.Generation
	if err := p.owner.EnsureStageMarker(digest); err != nil {
		return err
	}
	if err := syncGenerationTree(p.owner); err != nil {
		return err
	}

	owned, err := publishGeneration(p.owner, digest)
	if err != nil {
		var atomicErr *project.AtomicWriteError
		switch {
		case errors.As(err, &atomicErr) && atomicErr.Renamed:
			p.state = statePublished
			p.capturePublication(true)
			return err
		case errors.Is(err, fs.ErrExist):
			if syncErr := syncPublished(p.owner, p.sealed.Generation); syncErr != nil {
				return syncErr
			}
			if validationErr := p.owner.ValidatePublished(p.mod, p.sealed); validationErr != nil {
				return fmt.Errorf("reject generation reuse: %w", validationErr)
			}
			if adoptErr := p.owner.AdoptPublishedGeneration(p.sealed.Generation); adoptErr != nil {
				return adoptErr
			}
		default:
			return err
		}
	}
	p.state = statePublished
	p.capturePublication(owned)
	return p.commitPublished()
}

func (p *PreparedGeneration) commitPublished() error {
	if err := p.checkPublishedLock(); err != nil {
		return err
	}
	if err := p.owner.ValidatePublished(p.mod, p.publication.lock); err != nil {
		return err
	}

	currentMod, err := p.owner.LoadMod()
	if err != nil {
		return err
	}
	if project.HashModLockInputs(currentMod) != p.publication.lock.ModLockInputsSHA256 {
		return fmt.Errorf("%s changed during generation preparation", project.ModFileName)
	}
	err = writeGenerationLock(p.owner, p.publication.lock)
	if err != nil {
		var uncertain *project.CommitUncertainError
		if !errors.As(err, &uncertain) {
			return err
		}
	}
	p.state = stateCommitted
	result := errors.Join(err, p.close())
	p.outcome = &commitOutcome{err: result}
	return result
}

func (p *PreparedGeneration) capturePublication(owned bool) {
	p.publication = &publishedOwnership{name: p.sealed.Generation, lock: cloneLock(p.sealed), owned: owned}
}

func (p *PreparedGeneration) checkPublishedLock() error {
	if p.publication == nil || !reflect.DeepEqual(p.Lock, p.publication.lock) {
		return fmt.Errorf("generation lock changed after publication")
	}
	return nil
}

func qualifyBinary(lock *project.LockFile, generation string) {
	if lock.Build.SHA256 != "" {
		lock.Build.Binary = project.GenerationBinaryPath(generation)
	}
}

func (p *PreparedGeneration) Abort() error {
	if p == nil {
		return nil
	}
	if p.state == stateClosed {
		if p.abortOutcome != nil {
			return p.abortOutcome.err
		}
		return nil
	}
	if p.state == stateCommitted {
		return p.close()
	}
	var cleanupErr error
	if p.state == statePublished {
		cleanupErr = p.removeUnreferencedPublication()
	} else {
		cleanupErr = p.owner.RemoveStage()
	}
	result := errors.Join(cleanupErr, p.close())
	p.abortOutcome = &commitOutcome{err: result}
	return result
}

func (p *PreparedGeneration) close() error {
	if p.state == stateClosed {
		return nil
	}
	owner := p.owner
	p.owner = nil
	err := owner.Close()
	p.state = stateClosed
	return err
}

func (p *PreparedGeneration) removeUnreferencedPublication() error {
	if p.publication == nil {
		return fmt.Errorf("generation publication ownership is missing")
	}
	if !p.publication.owned {
		return nil
	}
	if !project.IsLowerHexDigest(p.publication.name) {
		return fmt.Errorf("generation publication is not owned")
	}
	if err := p.owner.ValidatePublished(p.mod, p.publication.lock); err != nil {
		return fmt.Errorf("cannot prove generation publication ownership: %w", err)
	}
	lock, err := p.owner.LoadLock()
	if err == nil {
		if lock.Generation == p.publication.name {
			return nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot prove generation is unreferenced: %w", err)
	}
	return p.owner.RemoveStage()
}
