package depsnapshot

import (
	"os"
)

func chmodRootPath(root *os.Root, name string, mode os.FileMode) error {
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
