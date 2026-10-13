package deps

import (
	"errors"
	"fmt"

	"doggo/internal/project"
)

type Auditor struct {
	OpenGenerationSession func(scriptPath string) (*project.GenerationSession, error)
	LoadModFile           func(path string) (*project.ModFile, error)
	AuditLock             func(contentRoot, sourceRoot string, mod *project.ModFile, lock *project.LockFile) ([]DependencyAuditFinding, error)
}

var auditWorkspaceCopyForTest func(*project.GenerationSession, *project.OwnedTempRoot) error

func (a Auditor) AuditPreparedLock(projectRoot, stagedRoot string, lock *project.LockFile) ([]DependencyAuditFinding, error) {
	if lock == nil {
		return nil, fmt.Errorf("audit lock must not be nil")
	}
	if lock.LockVersion != project.LockVersion {
		return nil, fmt.Errorf("unsupported lock version: %d", lock.LockVersion)
	}
	loadMod := a.LoadModFile
	if loadMod == nil {
		loadMod = project.LoadModFile
	}
	mod, err := loadMod(project.ModPath(projectRoot))
	if err != nil {
		return nil, err
	}
	if err := project.ValidateTentativeLock(mod, lock); err != nil {
		return nil, fmt.Errorf("invalid tentative %s: %w", project.LockFileName, err)
	}
	audit := a.AuditLock
	if audit == nil {
		audit = AuditResolvedLock
	}
	return audit(stagedRoot, projectRoot, mod, lock)
}

func (a Auditor) Audit(projectRoot string) (_ []DependencyAuditFinding, err error) {
	openSession := a.OpenGenerationSession
	if openSession == nil {
		openSession = project.OpenGenerationSession
	}
	session, err := openSession(project.ModPath(projectRoot))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	temporary, err := project.NewOwnedTempRoot("doggo-audit-")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, temporary.Close()) }()
	var copyErr error
	if auditWorkspaceCopyForTest != nil {
		copyErr = auditWorkspaceCopyForTest(session, temporary)
	} else {
		copyErr = session.CopyAuditWorkspaceTo(temporary.Tree())
	}
	if copyErr != nil {
		return nil, copyErr
	}
	if err := temporary.Verify(); err != nil {
		return nil, err
	}
	loadMod := a.LoadModFile
	if loadMod == nil {
		loadMod = project.LoadModFile
	}
	mod, err := loadMod(project.ModPath(session.Config.ProjectRoot))
	if err != nil {
		return nil, err
	}
	audit := a.AuditLock
	if audit == nil {
		audit = AuditResolvedLock
	}
	return audit(temporary.Path(), session.Config.ProjectRoot, mod, session.Config.Lock)
}
