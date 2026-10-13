package schema

import (
	"context"
	"errors"
	"math/rand"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

type checkTestFn struct {
	name   string
	invoke func(context.Context, ...types.Value) (types.Value, error)
}

func (f checkTestFn) Invoke(ctx context.Context, args ...types.Value) (types.Value, error) {
	return f.invoke(ctx, args...)
}

func (f checkTestFn) String() string {
	if f.name != "" {
		return "#<check-test-fn:" + f.name + ">"
	}
	return "#<check-test-fn>"
}

func (f checkTestFn) Type() string                  { return "check-test-fn" }
func (f checkTestFn) Hash() uint32                  { return 2 }
func (f checkTestFn) Equals(other types.Value) bool { _, ok := other.(checkTestFn); return ok }
func (f checkTestFn) IsSelfEvaluating() bool        { return true }

func mustSchema(t *testing.T, value types.Value) *Schema {
	t.Helper()

	schema, err := Normalize(value)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestCheckPassReportUsesPinnedDefaults(t *testing.T) {
	schema := mustSchema(t, atom.NewKeyword("int"))
	report, err := Check(context.Background(), checkTestFn{
		name: "always-true",
		invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
			if len(args) != 1 {
				t.Fatalf("expected 1 arg, got %d", len(args))
			}
			return atom.True, nil
		},
	}, schema, CheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Pass {
		t.Fatalf("expected pass report: %#v", report)
	}
	if got, want := report.Iterations, 100; got != want {
		t.Fatalf("iterations = %d, want %d", got, want)
	}
	if report.FailingInput != nil || report.ShrunkInput != nil || report.Failure != nil || report.Explain != nil {
		t.Fatalf("unexpected failure payload in success report: %#v", report)
	}
	if got := len(report.ShrinkSteps); got != 0 {
		t.Fatalf("success shrink-steps len = %d, want 0", got)
	}
}

func TestCheckIterationsZeroReturnsImmediatePass(t *testing.T) {
	schema := mustSchema(t, atom.NewKeyword("int"))
	called := false

	report, err := Check(context.Background(), checkTestFn{
		name: "should-not-run",
		invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
			called = true
			return atom.True, nil
		},
	}, schema, CheckOptions{Iterations: 0, IterationsSet: true})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("property was invoked for zero-iteration check")
	}
	if !report.Pass {
		t.Fatalf("expected pass report: %#v", report)
	}
	if got := report.Iterations; got != 0 {
		t.Fatalf("iterations = %d, want 0", got)
	}
}

func TestCheckUnaryFailuresAndThrownExceptions(t *testing.T) {
	t.Run("return-false", func(t *testing.T) {
		schema := mustSchema(t, atom.NewKeyword("int"))
		report, err := Check(context.Background(), checkTestFn{
			name: "return-false",
			invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
				return atom.False, nil
			},
		}, schema, CheckOptions{
			Iterations:    1,
			IterationsSet: true,
			GeneratorOverrides: map[Kind]any{
				KindInt: atom.NewNumberInt(10),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Pass {
			t.Fatalf("expected failure report: %#v", report)
		}
		if got, want := report.Failure.Kind.String(), ":return-false"; got != want {
			t.Fatalf("failure kind = %s, want %s", got, want)
		}
		if got := report.FailingInput.String(); got != "10" {
			t.Fatalf("failing input = %s, want 10", got)
		}
	})

	t.Run("return-nil", func(t *testing.T) {
		schema := mustSchema(t, atom.NewKeyword("int"))
		report, err := Check(context.Background(), checkTestFn{
			name: "return-nil",
			invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
				return nil, nil
			},
		}, schema, CheckOptions{
			Iterations:    1,
			IterationsSet: true,
			GeneratorOverrides: map[Kind]any{
				KindInt: atom.NewNumberInt(10),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Pass {
			t.Fatalf("expected failure report: %#v", report)
		}
		if got, want := report.Failure.Kind.String(), ":return-nil"; got != want {
			t.Fatalf("failure kind = %s, want %s", got, want)
		}
	})

	t.Run("threw", func(t *testing.T) {
		schema := mustSchema(t, atom.NewKeyword("int"))
		report, err := Check(context.Background(), checkTestFn{
			name: "threw",
			invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
				return nil, errors.New("boom")
			},
		}, schema, CheckOptions{
			Iterations:    1,
			IterationsSet: true,
			GeneratorOverrides: map[Kind]any{
				KindInt: atom.NewNumberInt(10),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Pass {
			t.Fatalf("expected failure report: %#v", report)
		}
		if got, want := report.Failure.Kind.String(), ":threw"; got != want {
			t.Fatalf("failure kind = %s, want %s", got, want)
		}
		if report.Failure.Exception == nil {
			t.Fatal("expected canonical exception data")
		}
		if got, want := report.Failure.Exception.Message, "boom"; got != want {
			t.Fatalf("exception message = %q, want %q", got, want)
		}
	})
}

func TestCheckTupleAndZeroArgContracts(t *testing.T) {
	t.Run("tuple", func(t *testing.T) {
		schema := mustSchema(t, coll.NewVector(
			atom.NewKeyword("tuple"),
			coll.NewVector(atom.NewKeyword("enum"), atom.NewNumberInt(3)),
			coll.NewVector(atom.NewKeyword("enum"), atom.NewNumberInt(4)),
		))
		report, err := Check(context.Background(), checkTestFn{
			name: "tuple",
			invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
				if len(args) != 2 {
					t.Fatalf("expected 2 args, got %d", len(args))
				}
				if got := args[0].String(); got != "3" {
					t.Fatalf("arg0 = %s, want 3", got)
				}
				if got := args[1].String(); got != "4" {
					t.Fatalf("arg1 = %s, want 4", got)
				}
				return atom.False, nil
			},
		}, schema, CheckOptions{Iterations: 1, IterationsSet: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := report.FailingInput.String(); got != "[3 4]" {
			t.Fatalf("failing input = %s, want [3 4]", got)
		}
	})

	t.Run("zero-arg", func(t *testing.T) {
		schema := mustSchema(t, coll.NewVector(atom.NewKeyword("tuple")))
		report, err := Check(context.Background(), checkTestFn{
			name: "zero-arg",
			invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
				if len(args) != 0 {
					t.Fatalf("expected zero args, got %d", len(args))
				}
				return atom.False, nil
			},
		}, schema, CheckOptions{Iterations: 1, IterationsSet: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := report.FailingInput.String(); got != "[]" {
			t.Fatalf("failing input = %s, want []", got)
		}
		if got := report.ShrunkInput.String(); got != "[]" {
			t.Fatalf("shrunk input = %s, want []", got)
		}
		if got := len(report.ShrinkSteps); got != 0 {
			t.Fatalf("zero-arg shrink-steps len = %d, want 0", got)
		}
	})
}

func TestCheckSeedShrinkAndErrors(t *testing.T) {
	t.Run("deterministic-seed", func(t *testing.T) {
		schema := mustSchema(t, atom.NewKeyword("int"))
		run := func() *CheckReport {
			report, err := Check(context.Background(), checkTestFn{
				name: "deterministic-seed",
				invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
					return atom.False, nil
				},
			}, schema, CheckOptions{Seed: 7, SeedSet: true, Iterations: 1, IterationsSet: true})
			if err != nil {
				t.Fatal(err)
			}
			return report
		}

		first := run()
		second := run()
		if !first.FailingInput.Equals(second.FailingInput) {
			t.Fatalf("expected deterministic failing input, got %s and %s", first.FailingInput, second.FailingInput)
		}
	})

	t.Run("seed-propagates-to-generator-overrides", func(t *testing.T) {
		schema := mustSchema(t, atom.NewKeyword("int"))
		run := func(opts CheckOptions) *CheckReport {
			report, err := Check(context.Background(), checkTestFn{
				name: "seed-override",
				invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
					return atom.False, nil
				},
			}, schema, CheckOptions{
				Seed:           opts.Seed,
				SeedSet:        opts.SeedSet,
				Iterations:     1,
				IterationsSet:  true,
				ShrinkLimit:    0,
				ShrinkLimitSet: true,
				GeneratorOverrides: map[Kind]any{
					KindInt: GeneratorFunc(func(_ *Schema, _ *rand.Rand, genOpts GenerateOptions) (types.Value, error) {
						return atom.NewNumberInt(genOpts.Seed), nil
					}),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			return report
		}

		assertReportSeed := func(report *CheckReport) {
			t.Helper()
			got, ok := report.FailingInput.(types.Int64Valuer)
			if !ok {
				t.Fatalf("failing input %T does not expose Int64Value", report.FailingInput)
			}
			seedValue, ok := got.Int64Value()
			if !ok {
				t.Fatal("failing input does not expose int64 seed")
			}
			if seedValue != report.Seed {
				t.Fatalf("override seed = %d, report seed = %d", seedValue, report.Seed)
			}
		}

		explicit := run(CheckOptions{Seed: 7, SeedSet: true})
		assertReportSeed(explicit)
		if explicit.Seed != 7 {
			t.Fatalf("explicit report seed = %d, want 7", explicit.Seed)
		}

		implicit := run(CheckOptions{})
		assertReportSeed(implicit)
		if implicit.Seed == 0 {
			t.Fatal("implicit report seed = 0, want runtime-generated seed")
		}
	})

	t.Run("shrink-loop-reduces-counterexample", func(t *testing.T) {
		schema := mustSchema(t, atom.NewKeyword("int"))
		report, err := Check(context.Background(), checkTestFn{
			name: "positive-only-fails",
			invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
				n, _ := args[0].(types.Int64Valuer).Int64Value()
				return atom.NewBoolean(n <= 0), nil
			},
		}, schema, CheckOptions{
			Iterations:    1,
			IterationsSet: true,
			GeneratorOverrides: map[Kind]any{
				KindInt: atom.NewNumberInt(10),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Pass {
			t.Fatalf("expected failure report: %#v", report)
		}
		if report.ShrunkInput.Equals(report.FailingInput) {
			t.Fatalf("expected shrunk input to differ from original: %#v", report)
		}
	})

	t.Run("shrink-loop-uses-later-tuple-child-candidate", func(t *testing.T) {
		schema := mustSchema(t, coll.NewVector(atom.NewKeyword("tuple"), atom.NewKeyword("int")))
		report, err := Check(context.Background(), checkTestFn{
			name: "above-four-fails",
			invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
				if len(args) != 1 {
					t.Fatalf("expected 1 arg, got %d", len(args))
				}
				n, _ := args[0].(types.Int64Valuer).Int64Value()
				return atom.NewBoolean(n <= 4), nil
			},
		}, schema, CheckOptions{
			Iterations:     1,
			IterationsSet:  true,
			ShrinkLimit:    10,
			ShrinkLimitSet: true,
			GeneratorOverrides: map[Kind]any{
				KindInt: atom.NewNumberInt(10),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Pass {
			t.Fatalf("expected failure report: %#v", report)
		}
		if got, want := report.FailingInput.String(), "[10]"; got != want {
			t.Fatalf("failing input = %s, want %s", got, want)
		}
		if got, want := report.ShrunkInput.String(), "[5]"; got != want {
			t.Fatalf("shrunk input = %s, want %s", got, want)
		}
		if got, want := valuesString(report.ShrinkSteps), "[[5]]"; got != want {
			t.Fatalf("shrink steps = %s, want %s", got, want)
		}
	})

	t.Run("shrink-limit-bounds-property-reevaluation", func(t *testing.T) {
		schema := mustSchema(t, coll.NewVector(atom.NewKeyword("tuple"), atom.NewKeyword("int")))
		invocations := 0
		report, err := Check(context.Background(), checkTestFn{
			name: "limit-one",
			invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
				invocations++
				if len(args) != 1 {
					t.Fatalf("expected 1 arg, got %d", len(args))
				}
				n, _ := args[0].(types.Int64Valuer).Int64Value()
				return atom.NewBoolean(n <= 4), nil
			},
		}, schema, CheckOptions{
			Iterations:     1,
			IterationsSet:  true,
			ShrinkLimit:    1,
			ShrinkLimitSet: true,
			GeneratorOverrides: map[Kind]any{
				KindInt: atom.NewNumberInt(10),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Pass {
			t.Fatalf("expected failure report: %#v", report)
		}
		if got, want := report.ShrunkInput.String(), "[10]"; got != want {
			t.Fatalf("shrunk input = %s, want %s", got, want)
		}
		if got, want := invocations, 2; got != want {
			t.Fatalf("property invocations = %d, want %d", got, want)
		}
		if got := len(report.ShrinkSteps); got != 0 {
			t.Fatalf("shrink steps len = %d, want 0", got)
		}
	})

	t.Run("shrink-limit-zero", func(t *testing.T) {
		schema := mustSchema(t, atom.NewKeyword("int"))
		report, err := Check(context.Background(), checkTestFn{
			name: "limit-zero",
			invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
				return atom.False, nil
			},
		}, schema, CheckOptions{
			Iterations:     1,
			IterationsSet:  true,
			ShrinkLimit:    0,
			ShrinkLimitSet: true,
			GeneratorOverrides: map[Kind]any{
				KindInt: atom.NewNumberInt(10),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !report.ShrunkInput.Equals(report.FailingInput) {
			t.Fatalf("expected shrink-limit 0 to preserve failing input: %#v", report)
		}
		if got := len(report.ShrinkSteps); got != 0 {
			t.Fatalf("shrink steps len = %d, want 0", got)
		}
	})

	t.Run("malformed-options-and-nil-property", func(t *testing.T) {
		schema := mustSchema(t, atom.NewKeyword("int"))
		_, err := Check(context.Background(), nil, schema, CheckOptions{})
		var checkErr *CheckError
		if !errors.As(err, &checkErr) {
			t.Fatalf("Check(nil property) returned %T, want *CheckError", err)
		}

		var typedNil types.Fn = (*checkTestFn)(nil)
		_, err = Check(context.Background(), typedNil, schema, CheckOptions{})
		if !errors.As(err, &checkErr) {
			t.Fatalf("Check(typed-nil property) returned %T, want *CheckError", err)
		}

		_, err = Check(context.Background(), checkTestFn{
			name: "negative-iterations",
			invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
				return atom.True, nil
			},
		}, schema, CheckOptions{Iterations: -1, IterationsSet: true})
		if !errors.As(err, &checkErr) {
			t.Fatalf("Check(negative iterations) returned %T, want *CheckError", err)
		}

		_, err = Check(context.Background(), checkTestFn{
			name: "malformed-generator-overrides",
			invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
				return atom.True, nil
			},
		}, schema, CheckOptions{
			Iterations:    1,
			IterationsSet: true,
			GeneratorOverrides: map[Kind]any{
				KindInt: "not-a-generator",
			},
		})
		if !errors.As(err, &checkErr) {
			t.Fatalf("Check(malformed generator override) returned %T, want *CheckError", err)
		}
	})

	t.Run("lower-layer-errors-propagate", func(t *testing.T) {
		schema := mustSchema(t, coll.NewVector(atom.NewKeyword("predicate"), testCallable{}))
		_, err := Check(context.Background(), checkTestFn{
			name: "predicate-lower-layer",
			invoke: func(_ context.Context, args ...types.Value) (types.Value, error) {
				return atom.True, nil
			},
		}, schema, CheckOptions{Iterations: 1, IterationsSet: true, Seed: 7, SeedSet: true})
		var genErr *GenerateError
		if !errors.As(err, &genErr) {
			t.Fatalf("Check(predicate schema) returned %T, want *GenerateError", err)
		}
	})
}
