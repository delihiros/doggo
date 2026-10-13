package deps

import (
	"errors"

	"doggo/internal/depsource"
	"doggo/internal/project"
)

var afterDependencyLeaseForTest func()

func AddDependency(projectRoot, source string) (err error) {
	parsed, err := depsource.Parse(source)
	if err != nil {
		return err
	}
	lease, err := project.AcquireGenerationLease(projectRoot, true)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	if afterDependencyLeaseForTest != nil {
		afterDependencyLeaseForTest()
	}
	mod, err := lease.LoadMod()
	if err != nil {
		return err
	}
	mod.Deps = append(mod.Deps, parsed.Declaration)
	mod.Deps = project.NormalizeStringSlice(mod.Deps)
	return lease.WriteMod(mod)
}

func RemoveDependency(projectRoot, source string) (err error) {
	parsed, err := depsource.Parse(source)
	if err != nil {
		return err
	}
	lease, err := project.AcquireGenerationLease(projectRoot, true)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	if afterDependencyLeaseForTest != nil {
		afterDependencyLeaseForTest()
	}
	mod, err := lease.LoadMod()
	if err != nil {
		return err
	}
	deps := make([]string, 0, len(mod.Deps))
	for _, dep := range mod.Deps {
		if dep != parsed.Declaration {
			deps = append(deps, dep)
		}
	}
	mod.Deps = project.NormalizeStringSlice(deps)
	return lease.WriteMod(mod)
}

func ListDependencies(projectRoot string) ([]string, error) {
	mod, err := project.LoadModFile(project.ModPath(projectRoot))
	if err != nil {
		return nil, err
	}
	return append([]string(nil), mod.Deps...), nil
}
