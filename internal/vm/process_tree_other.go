//go:build !unix && !windows

package vm

func newProcessTreeController() (processTreeController, error) {
	return nil, newProcessTreeUnsupportedError("process-tree management is unsupported on this platform")
}
