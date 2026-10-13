package coll

import (
	"testing"
	"time"

	"doggo/internal/types"
)

func TestValueEqualsResultInfiniteRangesTerminates(t *testing.T) {
	tests := []struct {
		name  string
		left  *InfiniteRangeLazySeq
		right *InfiniteRangeLazySeq
		want  bool
	}{
		{name: "equal", left: NewInfiniteRangeLazySeq(), right: NewInfiniteRangeLazySeq(), want: true},
		{name: "unequal", left: NewInfiniteRangeLazySeq(), right: NewInfiniteRangeLazySeq().Next().(*InfiniteRangeLazySeq), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			type result struct {
				equal bool
				err   error
			}
			done := make(chan result, 1)
			go func() {
				equal, err := ValueEqualsResult(tt.left, tt.right)
				done <- result{equal: equal, err: err}
			}()

			select {
			case got := <-done:
				if got.err != nil {
					t.Fatalf("ValueEqualsResult returned error: %v", got.err)
				}
				if got.equal != tt.want {
					t.Fatalf("ValueEqualsResult = %v, want %v", got.equal, tt.want)
				}
			case <-time.After(100 * time.Millisecond):
				t.Fatal("infinite range equality did not terminate")
			}
		})
	}
}

func TestValueEqualsResultMetadataWrappedInfiniteRangesTerminates(t *testing.T) {
	type result struct {
		equal bool
		err   error
	}
	done := make(chan result, 1)
	go func() {
		left := NewMetaSeq(NewInfiniteRangeLazySeq(), NewMap(nil))
		right := NewMetaSeq(NewInfiniteRangeLazySeq(), NewMap(nil))
		equal, err := ValueEqualsResult(left, right)
		done <- result{equal: equal, err: err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("ValueEqualsResult returned error: %v", got.err)
		}
		if !got.equal {
			t.Fatal("metadata-wrapped equal infinite ranges compared unequal")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("metadata-wrapped infinite range equality did not terminate")
	}
}

func TestValueEqualsResultSameSourcedInfiniteRangeTerminates(t *testing.T) {
	type result struct {
		equal bool
		err   error
	}
	done := make(chan result, 1)
	go func() {
		seq := NewConsSeq(RangeInt(-1), NewInfiniteRangeLazySeq())
		equal, err := ValueEqualsResult(seq, seq)
		done <- result{equal: equal, err: err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("ValueEqualsResult returned error: %v", got.err)
		}
		if !got.equal {
			t.Fatal("identical sourced infinite range compared unequal")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("identical sourced infinite range equality did not terminate")
	}
}

func TestValueHashResultInfiniteRangeTerminates(t *testing.T) {
	want := NewInfiniteRangeLazySeq().Hash()
	values := []struct {
		name  string
		value types.Value
	}{
		{name: "raw", value: NewInfiniteRangeLazySeq()},
		{name: "metadata-wrapped", value: NewMetaSeq(NewInfiniteRangeLazySeq(), NewMap(nil))},
	}

	for _, tt := range values {
		t.Run(tt.name, func(t *testing.T) {
			type result struct {
				hash uint32
				err  error
			}
			done := make(chan result, 1)
			go func() {
				hash, err := ValueHashResult(tt.value)
				done <- result{hash: hash, err: err}
			}()

			select {
			case got := <-done:
				if got.err != nil {
					t.Fatalf("ValueHashResult returned error: %v", got.err)
				}
				if got.hash != want {
					t.Fatalf("ValueHashResult = %d, want %d", got.hash, want)
				}
			case <-time.After(100 * time.Millisecond):
				t.Fatal("infinite range hash did not terminate")
			}
		})
	}
}

func TestValueHashResultRejectsSourcedInfiniteRange(t *testing.T) {
	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		seq := NewConsSeq(RangeInt(-1), NewInfiniteRangeLazySeq())
		_, err := ValueHashResult(seq)
		done <- result{err: err}
	}()

	select {
	case got := <-done:
		if got.err == nil {
			t.Fatal("ValueHashResult succeeded, want an unbounded sequence error")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("transformed infinite range hash did not terminate")
	}
}
