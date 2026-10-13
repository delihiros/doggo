package sync

import (
	"errors"
	"fmt"

	"doggo/internal/project"
	"doggo/internal/toolchain/generation"
)

type Service struct {
	PrepareSync func(projectRoot string, sources []string) (*generation.PreparedGeneration, error)
}

func (s Service) Run(projectRoot string) (_ *project.LockFile, retErr error) {
	prepare := s.PrepareSync
	if prepare == nil {
		prepare = (generation.Service{}).PrepareSync
	}
	prepared, err := prepare(projectRoot, nil)
	if err != nil {
		return nil, fmt.Errorf("sync stage prepare-generation: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, prepared.Abort()) }()
	if err := prepared.Commit(); err != nil {
		return nil, fmt.Errorf("sync stage commit-generation: %w", err)
	}
	return prepared.Lock, nil
}
