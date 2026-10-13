//go:build !windows

package project

func suspendOwnedTreeRenameDescendants(*OwnedTree) (func() error, error) {
	return func() error { return nil }, nil
}
