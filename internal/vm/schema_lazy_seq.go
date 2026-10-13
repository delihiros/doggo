package vm

import (
	"iter"
	"runtime"
	"sync"

	"doggo/internal/types"
	"doggo/internal/types/coll"
)

type streamLazySeqState struct {
	next func() (types.Value, bool)
	stop func()
	once sync.Once
}

func newLazySeqFromValueStream(stream iter.Seq[types.Value]) *coll.LazySeq {
	next, stop := iter.Pull(stream)
	state := &streamLazySeqState{
		next: next,
		stop: stop,
	}
	runtime.SetFinalizer(state, func(s *streamLazySeqState) {
		s.close()
	})
	return state.lazySeq()
}

func (s *streamLazySeqState) lazySeq() *coll.LazySeq {
	return coll.NewLazySeq(func(_ types.Environment) (types.Value, error) {
		value, ok := s.next()
		if !ok {
			s.close()
			return nil, nil
		}
		return coll.NewConsSeq(value, s.lazySeq()), nil
	}, nil)
}

func (s *streamLazySeqState) close() {
	s.once.Do(func() {
		runtime.SetFinalizer(s, nil)
		if s.stop != nil {
			s.stop()
		}
	})
}
