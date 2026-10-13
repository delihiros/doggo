package util

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

var ErrRecursiveOnce = errors.New("recursive lazy sequence realization")

// RecursionDetectingOnce is sync.Once with one extra behavior: recursive
// execution by the owner returns an error while concurrent callers still wait.
type RecursionDetectingOnce struct {
	mu      sync.Mutex
	cond    *sync.Cond
	done    bool
	running bool
	owner   uint64
}

func (once *RecursionDetectingOnce) Do(fn func()) error {
	return once.do(fn, true)
}

// DoWithoutRecursionCheck skips owner tracking when the caller can prove fn
// cannot realize the same value recursively.
func (once *RecursionDetectingOnce) DoWithoutRecursionCheck(fn func()) error {
	return once.do(fn, false)
}

func (once *RecursionDetectingOnce) do(fn func(), trackOwner bool) error {
	once.mu.Lock()
	if once.done {
		once.mu.Unlock()
		return nil
	}

	var caller uint64
	if trackOwner {
		var err error
		caller, err = CurrentGoroutineID()
		if err != nil {
			once.mu.Unlock()
			return err
		}
	}
	if once.cond == nil {
		once.cond = sync.NewCond(&once.mu)
	}
	for once.running {
		if trackOwner && once.owner == caller {
			once.mu.Unlock()
			return ErrRecursiveOnce
		}
		once.cond.Wait()
		if once.done {
			once.mu.Unlock()
			return nil
		}
	}

	once.running = true
	if trackOwner {
		once.owner = caller
	}
	once.mu.Unlock()
	defer once.finish()
	fn()
	return nil
}

func (once *RecursionDetectingOnce) finish() {
	once.mu.Lock()
	once.done = true
	once.running = false
	once.owner = 0
	once.cond.Broadcast()
	once.mu.Unlock()
}

// CurrentGoroutineID distinguishes recursive realization from a concurrent
// waiter. ponytail: replace Stack parsing if Go exposes a supported API.
func CurrentGoroutineID() (uint64, error) {
	var buffer [64]byte
	n := runtime.Stack(buffer[:], false)
	line := string(buffer[:n])
	const prefix = "goroutine "
	if !strings.HasPrefix(line, prefix) {
		return 0, fmt.Errorf("cannot identify lazy sequence realization owner")
	}
	line = line[len(prefix):]
	separator := strings.IndexByte(line, ' ')
	if separator < 1 {
		return 0, fmt.Errorf("cannot identify lazy sequence realization owner")
	}
	id, err := strconv.ParseUint(line[:separator], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot identify lazy sequence realization owner: %w", err)
	}
	return id, nil
}
