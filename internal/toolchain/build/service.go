package build

import (
	"errors"
	"fmt"

	"doggo/internal/project"
	"doggo/internal/toolchain/generation"
)

type Service struct {
	PrepareSync         func(projectRoot string, sources []string) (*generation.PreparedGeneration, error)
	PrepareCurrentBuild func(projectRoot string) (*generation.PreparedGeneration, error)
	AuditPrepared       func(projectRoot, stageRoot string, lock *project.LockFile) (int, error)
}

func (s Service) Run(projectRoot string, withSync bool) (_ *project.LockFile, retErr error) {
	var (
		prepared *generation.PreparedGeneration
		err      error
	)
	if withSync {
		prepare := s.PrepareSync
		if prepare == nil {
			prepare = (generation.Service{}).PrepareSync
		}
		prepared, err = prepare(projectRoot, nil)
	} else {
		prepare := s.PrepareCurrentBuild
		if prepare == nil {
			prepare = (generation.Service{}).PrepareCurrentBuild
		}
		prepared, err = prepare(projectRoot)
	}
	if err != nil {
		return nil, fmt.Errorf("build stage prepare-generation: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, prepared.Abort()) }()

	if withSync && s.AuditPrepared != nil {
		blockers, err := s.AuditPrepared(projectRoot, prepared.StageRoot(), prepared.Lock)
		if err != nil {
			return nil, fmt.Errorf("build stage audit-generation: %w", err)
		}
		if blockers > 0 {
			return nil, fmt.Errorf("build --sync blocked by dependency audit: %d high-severity issue(s). Run 'doggo deps audit'", blockers)
		}
	}
	if err := prepared.Build(); err != nil {
		return nil, fmt.Errorf("build stage build-generation: %w", err)
	}
	if err := prepared.Commit(); err != nil {
		return nil, fmt.Errorf("build stage commit-generation: %w", err)
	}
	return prepared.Lock, nil
}
