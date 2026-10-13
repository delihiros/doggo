package coll

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"doggo/internal/types"
)

func TestLazySeqConcurrentRealizationRunsOnce(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	seq := NewLazySeq(func(types.Environment) (types.Value, error) {
		calls.Add(1)
		close(started)
		<-release
		return NewList(RangeInt(1)), nil
	}, nil)

	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if got := seq.First(); got == nil || got.String() != "1" {
				t.Errorf("First() = %v, want 1", got)
			}
		}()
	}
	<-started
	close(release)
	wait.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("evaluation calls = %d, want 1", got)
	}
}

func TestSequenceHelpersPreserveLazyEvaluationErrors(t *testing.T) {
	seq := NewLazySeq(func(types.Environment) (types.Value, error) {
		return nil, errors.New("helper boom")
	}, nil)
	empty := NewList()

	if seqEquals(seq, empty) {
		t.Fatal("errored lazy sequence compared equal to an empty sequence")
	}
	if SeqHash(seq) == SeqHash(empty) {
		t.Fatal("errored lazy sequence hashed as an empty sequence")
	}
	if got := SeqString(seq); !strings.Contains(got, "helper boom") {
		t.Fatalf("SeqString() = %q, want evaluation error details", got)
	}
}

func TestSeqTraversalRiskDistinguishesFiniteAndInfiniteSources(t *testing.T) {
	finite := NewLazySeqFrom(NewList(RangeInt(1)), func(types.Environment) (types.Value, error) {
		return NewList(RangeInt(1)), nil
	}, nil)
	if provenance, err := newSeqProvenanceScan().inspect(finite); err != nil || provenance != seqProvenanceNone {
		t.Fatalf("finite source provenance = (%v, %v), want (none, nil)", provenance, err)
	}

	infinite := NewConsSeq(RangeInt(-1), NewInfiniteRangeLazySeq())
	if provenance, err := newSeqProvenanceScan().inspect(infinite); err != nil || provenance == seqProvenanceNone {
		t.Fatalf("infinite sourced sequence provenance = (%v, %v), want non-none", provenance, err)
	}
}

func TestMetaSeqPreservesLazyEvaluationError(t *testing.T) {
	wantErr := errors.New("metadata boom")
	seq := NewMetaSeq(NewLazySeq(func(types.Environment) (types.Value, error) {
		return nil, wantErr
	}, nil), nil)

	if err := RealizeSequenceNode(seq); !errors.Is(err, wantErr) {
		t.Fatalf("RealizeSequenceNode() = %v, want %v", err, wantErr)
	}
}

func TestHashedCollectionsDoNotInsertValuesWhoseHashFails(t *testing.T) {
	wantErr := errors.New("hash boom")
	badKey := func() types.Value {
		return NewLazySeq(func(types.Environment) (types.Value, error) {
			return nil, wantErr
		}, nil)
	}

	values := []struct {
		name  string
		value types.Value
		count int
	}{
		{name: "map constructor", value: NewMapFromPairs([]types.Value{badKey(), NewList()}), count: 0},
		{name: "map assoc", value: NewEmptyMap().Assoc(badKey(), NewList()), count: 0},
		{name: "set constructor", value: NewSet(badKey()), count: 0},
		{name: "set conj", value: NewSet().Conj(badKey()), count: 0},
	}
	poisonedMap := NewMapFromPairs([]types.Value{badKey(), NewList()})
	values = append(values, struct {
		name  string
		value types.Value
		count int
	}{name: "nested poisoned collection", value: NewSet(poisonedMap), count: 0})

	for _, test := range values {
		t.Run(test.name, func(t *testing.T) {
			counted, ok := test.value.(types.Countable)
			if !ok {
				t.Fatalf("%T is not countable", test.value)
			}
			if got := counted.Count(); got != test.count {
				t.Fatalf("count = %d, want %d", got, test.count)
			}
			if err := CollectionError(test.value); !errors.Is(err, wantErr) {
				t.Fatalf("CollectionError() = %v, want %v", err, wantErr)
			}
		})
	}
}

// TestLazySeqErrorPropagation は LazySeq のエラー伝播をテストします。
func TestLazySeqErrorPropagation(t *testing.T) {
	// エラーを発生させる evalFn を作成
	testErr := errors.New("evaluation error in lazy-seq")

	ls := NewLazySeq(func(_ types.Environment) (types.Value, error) {
		return nil, testErr
	}, nil)

	// Seq() を呼び出すとエラーが発生し、LazySeqError が返る
	result := ls.Seq()

	// 結果が LazySeqError であることを確認
	lazySeqErr, ok := result.(*LazySeqError)
	if !ok {
		t.Fatalf("expected *LazySeqError, got %T", result)
	}

	// エラーの内容を確認
	if lazySeqErr.Err != testErr {
		t.Errorf("expected error %v, got %v", testErr, lazySeqErr.Err)
	}
}

// TestLazySeqErrorString は LazySeq.String() のエラー時の動作をテストします。
// 注: LazySeq.String() は内部で Seq() を呼び出すため、エラー時の動作を確認します。
func TestLazySeqErrorString(t *testing.T) {
	testErr := errors.New("string error")

	ls := NewLazySeq(func(_ types.Environment) (types.Value, error) {
		return nil, testErr
	}, nil)

	str := ls.String()
	if !strings.Contains(str, testErr.Error()) {
		t.Fatalf("String() = %q, want evaluation error", str)
	}
}

func TestLazySeqStringUsesSequencePrintLimit(t *testing.T) {
	items := make([]types.Value, seqPrintLimit+1)
	for i := range items {
		items[i] = RangeInt(i)
	}
	ls := NewLazySeq(func(_ types.Environment) (types.Value, error) {
		return NewList(items...), nil
	}, nil)

	if got := ls.String(); got != "(0 1 2 3 4 5 6 7 8 9 ...)" {
		t.Fatalf("String() = %q, want truncated sequence", got)
	}
}

// TestLazySeqErrorType は LazySeqError の Value インターフェース実装をテストします。
func TestLazySeqErrorType(t *testing.T) {
	testErr := errors.New("type test error")
	lazySeqErr := &LazySeqError{Err: testErr}

	// Type()
	if lazySeqErr.Type() != "lazyseq-error" {
		t.Errorf("expected Type() to return 'lazyseq-error', got %s", lazySeqErr.Type())
	}

	// String()
	str := lazySeqErr.String()
	if !strings.Contains(str, "LazySeqError") {
		t.Errorf("expected String() to contain 'LazySeqError', got %s", str)
	}
	if !strings.Contains(str, "type test error") {
		t.Errorf("expected String() to contain error message, got %s", str)
	}

	// Hash()
	if lazySeqErr.Hash() != 0 {
		t.Errorf("expected Hash() to return 0, got %d", lazySeqErr.Hash())
	}

	// Equals()
	if lazySeqErr.Equals(lazySeqErr) {
		t.Error("expected Equals() to return false")
	}

	// IsSelfEvaluating()
	if !lazySeqErr.IsSelfEvaluating() {
		t.Error("expected IsSelfEvaluating() to return true")
	}

	// Cause()
	if lazySeqErr.Cause() != testErr {
		t.Errorf("expected Cause() to return the original error, got %v", lazySeqErr.Cause())
	}
}
