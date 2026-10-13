package compiler

import (
	"sync"

	"doggo/internal/types"
)

// ConstantStore shares a synchronized constant pool across cooperating compilers
// and returns snapshots that remain stable while compilation continues.
type ConstantStore struct {
	mu     sync.RWMutex
	values []types.Value
}

func NewConstantStore(initial []types.Value) *ConstantStore {
	store := &ConstantStore{}
	if len(initial) > 0 {
		store.values = append(store.values, initial...)
	}
	return store
}

func (s *ConstantStore) Values() []types.Value {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]types.Value(nil), s.values...)
}

func (s *ConstantStore) Add(value types.Value) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = append(s.values, value)
	return len(s.values) - 1
}

func (s *ConstantStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.values)
}

func (s *ConstantStore) Truncate(n int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 0 {
		n = 0
	}
	if n > len(s.values) {
		n = len(s.values)
	}
	s.values = s.values[:n]
}
