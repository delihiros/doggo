package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

type GenerationLease struct {
	file        *os.File
	project     *os.Root
	projectPath string
	doggo       *os.Root
	exclusive   bool
}

func AcquireGenerationLease(projectRoot string, exclusive bool) (*GenerationLease, error) {
	project, projectPath, err := openVerifiedRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	if exclusive {
		if err := project.Mkdir(".doggo", 0755); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, errors.Join(err, project.Close())
		}
	}
	doggo, err := openVerifiedSubroot(project, ".doggo")
	if err != nil {
		return nil, errors.Join(err, project.Close())
	}
	lease, err := acquireGenerationLeaseRoot(projectPath, project, doggo, exclusive)
	if err != nil {
		return nil, errors.Join(err, doggo.Close(), project.Close())
	}
	lease.project = project
	lease.projectPath = projectPath
	lease.doggo = doggo
	return lease, nil
}

func acquireGenerationLeaseRoot(projectPath string, project, doggo *os.Root, exclusive bool) (*GenerationLease, error) {
	return acquireGenerationLeaseRootMode(projectPath, project, doggo, exclusive, exclusive)
}

func acquireExistingGenerationLeaseRoot(projectPath string, project, doggo *os.Root, exclusive bool) (*GenerationLease, error) {
	return acquireGenerationLeaseRootMode(projectPath, project, doggo, exclusive, false)
}

func acquireGenerationLeaseRootMode(projectPath string, project, doggo *os.Root, exclusive, create bool) (*GenerationLease, error) {
	parent, err := doggo.Open(".")
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	file, err := openGenerationLockFile(parent, "generation.lock", create)
	if err != nil {
		return nil, err
	}
	if err := revalidateRootFile(doggo, "generation.lock", file); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := lockGenerationFile(file, exclusive); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := errors.Join(
		revalidateRootPath(projectPath, project),
		revalidateSubroot(project, ".doggo", doggo),
		revalidateRootFile(doggo, "generation.lock", file),
	); err != nil {
		return nil, errors.Join(err, unlockGenerationFile(file), file.Close())
	}
	return &GenerationLease{file: file, exclusive: exclusive}, nil
}

func (l *GenerationLease) Close() error {
	if l == nil {
		return nil
	}
	file := l.file
	l.file = nil
	var lockErr error
	if file != nil {
		lockErr = errors.Join(unlockGenerationFile(file), file.Close())
	}
	return errors.Join(lockErr, closeRoot(&l.doggo), closeRoot(&l.project))
}

func (l *GenerationLease) verifyProject() error {
	if l == nil || l.file == nil || l.project == nil || l.doggo == nil {
		return fmt.Errorf("generation lease does not own a project")
	}
	return errors.Join(
		revalidateRootPath(l.projectPath, l.project),
		revalidateSubroot(l.project, ".doggo", l.doggo),
		revalidateRootFile(l.doggo, "generation.lock", l.file),
	)
}

func (l *GenerationLease) requireExclusive() error {
	if err := l.verifyProject(); err != nil {
		return err
	}
	if !l.exclusive {
		return fmt.Errorf("generation lease is not exclusive")
	}
	return nil
}
