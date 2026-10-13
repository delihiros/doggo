package deps

import (
	"errors"

	"doggo/internal/project"
)

func loadModAndMatchingLock(projectRoot string) (_ *project.ModFile, _ *project.LockFile, err error) {
	session, err := project.OpenGenerationSession(project.ModPath(projectRoot))
	if err != nil {
		return nil, nil, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	mod, err := project.LoadModFile(project.ModPath(session.Config.ProjectRoot))
	if err != nil {
		return nil, nil, err
	}
	lock := *session.Config.Lock
	lock.Deps = append([]project.LockDep(nil), lock.Deps...)
	lock.Scan.GoPackages = append([]string(nil), lock.Scan.GoPackages...)
	lock.Scan.ReifyTargets = append([]project.ReifyTarget(nil), lock.Scan.ReifyTargets...)
	return mod, &lock, nil
}
