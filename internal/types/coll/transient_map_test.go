package coll_test

import (
	"runtime"
	"sync"
	"testing"

	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestTransientMapConcurrentAccess(t *testing.T) {
	tm := coll.NewTransientMap()
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 10_000; i++ {
			_, _ = tm.Assoc(atom.NewNumberInt(int64(i)), atom.NewNumberInt(int64(i)))
			runtime.Gosched()
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 10_000; i++ {
			_ = tm.Count()
			runtime.Gosched()
		}
	}()

	close(start)
	wg.Wait()
}
