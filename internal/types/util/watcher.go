package util

import "fmt"

// InvokeWatcher converts a watcher panic into an ordinary error.
func InvokeWatcher(fn func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("watcher panicked: %v", recovered)
		}
	}()
	return fn()
}
