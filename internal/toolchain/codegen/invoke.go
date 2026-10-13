package codegen

import (
	"errors"
	"path/filepath"

	"doggo/internal/project"
	"doggo/internal/toolchain/reifytarget"
)

type Invoker struct{}

func (i Invoker) GenerateInteropTree(tree *project.OwnedTree, stagePath string, goPackages []string) (err error) {
	packages := project.NormalizeStringSlice(goPackages)
	isolated, err := copyStageForCodegen(tree)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeCodegenStage(isolated)) }()
	isolatedRoot, err := isolated.Tree().OpenRoot("toolchain")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, isolatedRoot.Close()) }()
	projectRoot := filepath.Join(stagePath, "toolchain")
	isolatedProjectRoot := filepath.Join(isolated.Path(), "toolchain")
	if beforeCodegenPackagesLoadForTest != nil {
		beforeCodegenPackagesLoadForTest(projectRoot)
	}
	outputRel := filepath.Join("internal", "interop", "generated.go")
	err = generateInteropFileRoot(isolatedRoot, isolatedProjectRoot, filepath.Join(isolatedProjectRoot, outputRel), outputRel, packages, true)
	if err != nil {
		return err
	}
	isolatedOutput := filepath.ToSlash(filepath.Join("toolchain", outputRel))
	if err := isolated.Tree().RefreshFile(isolatedOutput); err != nil {
		return err
	}
	content, err := isolated.Tree().ReadFile(isolatedOutput)
	if err != nil {
		return err
	}
	if err := tree.WriteOwnedFile(filepath.ToSlash(filepath.Join("toolchain", outputRel)), content, 0644); err != nil {
		return err
	}
	if err := tree.VerifyComplete(); err != nil {
		return err
	}
	return nil
}

func (i Invoker) GenerateReifyTree(tree *project.OwnedTree, stagePath string, candidates []reifytarget.Candidate) (_ []project.ReifyTarget, err error) {
	isolated, err := copyStageForCodegen(tree)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, closeCodegenStage(isolated)) }()
	isolatedRoot, err := isolated.Tree().OpenRoot("toolchain")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, isolatedRoot.Close()) }()
	projectRoot := filepath.Join(stagePath, "toolchain")
	isolatedProjectRoot := filepath.Join(isolated.Path(), "toolchain")
	if beforeCodegenPackagesLoadForTest != nil {
		beforeCodegenPackagesLoadForTest(projectRoot)
	}
	outputRel := filepath.Join("internal", "reify", "generated_reify.go")
	targets, err := generateReifyFileRoot(isolatedRoot, isolatedProjectRoot, filepath.Join(isolatedProjectRoot, outputRel), outputRel, candidates, true)
	if err != nil {
		return nil, err
	}
	isolatedOutput := filepath.ToSlash(filepath.Join("toolchain", outputRel))
	if err := isolated.Tree().RefreshFile(isolatedOutput); err != nil {
		return nil, err
	}
	content, err := isolated.Tree().ReadFile(isolatedOutput)
	if err != nil {
		return nil, err
	}
	if err := tree.WriteOwnedFile(filepath.ToSlash(filepath.Join("toolchain", outputRel)), content, 0644); err != nil {
		return nil, err
	}
	if err := tree.VerifyComplete(); err != nil {
		return nil, err
	}
	return targets, nil
}

func copyStageForCodegen(tree *project.OwnedTree) (*project.OwnedTempRoot, error) {
	root, err := project.NewOwnedTempRoot(".doggo-codegen-")
	if err != nil {
		return nil, err
	}
	if err := tree.CopyTo(root.Tree(), nil); err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return root, nil
}

func closeCodegenStage(root *project.OwnedTempRoot) error {
	if beforeCodegenTempCleanupForTest != nil {
		beforeCodegenTempCleanupForTest(root.Path())
	}
	return root.Close()
}
