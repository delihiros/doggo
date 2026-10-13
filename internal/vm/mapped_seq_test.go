package vm

import (
	"context"
	"strings"
	"testing"
	"time"

	"doggo/internal/compiler"
	"doggo/internal/opcode"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestPureMappedPipelineSkipsRecursiveOwnerTracking(t *testing.T) {
	instructions := append(opcode.EncodeInstruction(opcode.OpGetLocal, 0), opcode.EncodeInstruction(opcode.OpConst, 0)...)
	instructions = append(instructions, opcode.EncodeInstruction(opcode.OpGT)...)
	instructions = append(instructions, opcode.EncodeInstruction(opcode.OpReturnValue)...)
	predicate := &compiler.Closure{Fn: &compiler.CompiledFunction{
		Instructions:  instructions,
		NumParameters: 1,
	}}
	constants := []types.Value{atom.NewNumberInt(10)}
	mapped := newVMMappedSeq(
		context.Background(),
		nil,
		constants,
		coll.NewRangeLazySeq(0, 100, 1),
		predicate,
	)

	if mapped.state.trackOwner {
		t.Fatal("pure mapped sequence should not parse goroutine ownership")
	}
	if mappedRealizationCanReenter(mapped, predicate, nil) {
		t.Fatal("pure pipeline should preserve non-reentrant realization")
	}
}

func TestVMMappedSeqSupportsDirectNodeRealization(t *testing.T) {
	inc, ok := DefaultBuiltinCatalog().Lookup("inc")
	if !ok {
		t.Fatal("default builtin catalog is missing inc")
	}
	seq := NewVMMappedSeq(
		context.Background(),
		nil,
		coll.NewRangeLazySeq(0, 2, 1),
		inc,
	)

	realizer, ok := any(seq).(interface{ RealizeNode() error })
	if !ok {
		t.Fatal("VMMappedSeq must expose direct realization so error checks do not treat its source as a lazy alias")
	}
	if err := realizer.RealizeNode(); err != nil {
		t.Fatalf("RealizeNode() error = %v", err)
	}
	if got := seq.First(); got == nil || got.String() != "1" {
		t.Fatalf("First() = %v, want 1", got)
	}
}

// TestMappedSeqIntegration tests map with Doggo code.
func TestMappedSeqIntegration(t *testing.T) {
	t.Run("map_in_doggo", func(t *testing.T) {
		runner := MustNewRunner()
		// sum of (map inc (range 10)) = 1+2+...+10 = 55
		result, err := runner.Run("(reduce + 0 (map inc (range 10)))")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		num, ok := result.(*atom.Number)
		if !ok {
			t.Fatalf("expected number, got %s", result.Type())
		}
		if num.ToFloat64() != 55.0 {
			t.Errorf("expected 55, got %v", num.ToFloat64())
		}
	})

	t.Run("map_filter_pipeline", func(t *testing.T) {
		runner := MustNewRunner()
		// (reduce + 0 (filter even? (map inc (range 10))))
		// map inc: 1, 2, 3, 4, 5, 6, 7, 8, 9, 10
		// filter even?: 2, 4, 6, 8, 10
		// sum: 30
		result, err := runner.Run("(reduce + 0 (filter even? (map inc (range 10))))")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		num, ok := result.(*atom.Number)
		if !ok {
			t.Fatalf("expected number, got %s", result.Type())
		}
		if num.ToFloat64() != 30.0 {
			t.Errorf("expected 30, got %v", num.ToFloat64())
		}
	})
}

func TestMappedSeqMemoizesEveryRealizedNode(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(let [calls (atom 0)
		      mapped (map (fn [x] (swap! calls inc) x) [1 2])]
		  (first mapped)
		  (first (next mapped))
		  (first (next mapped))
		  @calls)
	`)
	if err != nil {
		t.Fatalf("repeated traversal failed: %v", err)
	}
	if got := result.String(); got != "2" {
		t.Fatalf("mapped function calls = %s, want 2", got)
	}
}

func TestMappedSeqReduceUsesRealizedValues(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(let [calls (atom 0)
		      mapped (map (fn [x] (swap! calls inc) x) [1 2])]
		  (reduce + 0 mapped)
		  (reduce + 0 mapped)
		  @calls)
	`)
	if err != nil {
		t.Fatalf("repeated reduction failed: %v", err)
	}
	if got := result.String(); got != "2" {
		t.Fatalf("mapped function calls = %s, want 2", got)
	}
}

func TestMappedSeqRecursiveRealizationReturnsError(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{
			name: "mapper",
			expr: `(def s (map (fn [x] (first s)) [1]))
			       (first s)`,
		},
		{
			name: "lazy source",
			expr: `(def s (map identity (lazy-seq (list (first s)))))
			       (first s)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			done := make(chan error, 1)
			go func() {
				_, err := runner.Run(tt.expr)
				done <- err
			}()

			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "recursive mapped sequence realization") {
					t.Fatalf("Run() error = %v, want recursive realization error", err)
				}
			case <-time.After(time.Second):
				t.Fatal("recursive mapped sequence realization deadlocked")
			}
		})
	}
}

func TestMappedSeqDirectReducePopulatesSharedCache(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(let [calls (atom 0)
		      mapped (map (fn [x] (swap! calls inc) (* x 2)) [1 2])
		      total (reduce + 0 mapped)]
		  [total (realized? mapped) (vec mapped) @calls])
	`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := result.String(); got != "[6 true [2 4] 2]" {
		t.Fatalf("Run() = %s, want [6 true [2 4] 2]", got)
	}
}

// BenchmarkMappedSeq benchmarks Doggo map performance.
func BenchmarkMappedSeq(b *testing.B) {
	b.Run("map_range_doggo_1000", func(b *testing.B) {
		runner := MustNewRunner()
		for i := 0; i < b.N; i++ {
			_, _ = runner.Run("(doall (map inc (range 1000)))")
		}
	})
}
