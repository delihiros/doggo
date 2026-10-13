package depsnapshot

import (
	"errors"
	"fmt"

	"doggo/internal/depsource"
)

// PathDependencyModuleID reads the manifest identity in the declaration's context
// using the same retained roots and manifest checks as dependency snapshots.
func PathDependencyModuleID(projectRoot, source string) (_ string, retErr error) {
	parsed, err := depsource.Parse(source)
	if err != nil {
		return "", err
	}
	if parsed.Kind != depsource.Path {
		return "", fmt.Errorf("dependency %q is not a path source", source)
	}
	base, err := openSourceAncestry(projectRoot)
	if err != nil {
		return "", err
	}
	defer func() { retErr = errors.Join(retErr, base.close()) }()
	resolved, err := resolveRelativeSource(base.node, parsed.Declaration)
	if err != nil {
		return "", err
	}
	defer func() { retErr = errors.Join(retErr, resolved.close()) }()
	manifests, err := readModuleManifests(resolved.node.root, resolved.node.path)
	if err != nil {
		return "", err
	}
	return manifests.moduleID, nil
}
