package vm

import "testing"

func TestPartitionLargeSizeAndStep(t *testing.T) {
	for _, test := range []struct {
		name, source, want string
	}{
		{"partition max step", `(partition 1 9223372036854775807 [1 2])`, `((1))`},
		{"partition max step with larger chunk", `(partition 2 9223372036854775807 [1 2 3])`, `((1 2))`},
		{"partition max size", `(partition 9223372036854775807 [1 2])`, `()`},
		{"partition max size with overlap", `(partition 9223372036854775807 1 [1 2])`, `()`},
		{"partition max size and step", `(partition 9223372036854775807 9223372036854775807 [1 2])`, `()`},
		{"partition size above 32 bits", `(partition 4294967296 1 [1 2])`, `()`},
		{"partition step above 32 bits", `(partition 1 4294967296 [1 2])`, `((1))`},
		{"partition max size empty", `(partition 9223372036854775807 [])`, `()`},
		{"partition max step empty", `(partition 1 9223372036854775807 nil)`, `()`},
		{"partition-all max size", `(partition-all 9223372036854775807 [1 2])`, `((1 2))`},
		{"partition-all max size with overlap", `(partition-all 9223372036854775807 1 [1 2])`, `((1 2) (2))`},
		{"partition-all near max size with overlap", `(partition-all 9223372036854775806 1 [1 2 3])`, `((1 2 3) (2 3) (3))`},
		{"partition-all max step", `(partition-all 1 9223372036854775807 [1 2])`, `((1))`},
		{"partition-all max size and step", `(partition-all 9223372036854775807 9223372036854775807 [1 2])`, `((1 2))`},
		{"partition-all size above 32 bits", `(partition-all 4294967296 1 [1 2])`, `((1 2) (2))`},
		{"partition-all step above 32 bits", `(partition-all 1 4294967296 [1 2])`, `((1))`},
		{"partition-all max size empty", `(partition-all 9223372036854775807 [])`, `()`},
		{"partition-all max step empty", `(partition-all 1 9223372036854775807 nil)`, `()`},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Report any host panic as a failure and continue the independent
			// boundary cases so the full regression evidence is retained.
			defer func() {
				if panicValue := recover(); panicValue != nil {
					t.Errorf("host panic for %s: %v", test.source, panicValue)
				}
			}()
			assertRunResult(t, MustNewRunner(), test.source, test.want)
		})
	}
}

func TestPartitionNormalSizeAndStep(t *testing.T) {
	for _, test := range []struct {
		name, source, want string
	}{
		{"partition complete chunks", `(partition 2 [1 2 3 4 5])`, `((1 2) (3 4))`},
		{"partition overlap", `(partition 2 1 [1 2 3])`, `((1 2) (2 3))`},
		{"partition gaps", `(partition 2 3 [1 2 3 4 5 6])`, `((1 2) (4 5))`},
		{"partition exact length", `(partition 3 [1 2 3])`, `((1 2 3))`},
		{"partition incomplete", `(partition 3 [1 2])`, `()`},
		{"partition empty", `(partition 2 [])`, `()`},
		{"partition-all includes tail", `(partition-all 2 [1 2 3 4 5])`, `((1 2) (3 4) (5))`},
		{"partition-all overlap", `(partition-all 3 1 [1 2])`, `((1 2) (2))`},
		{"partition-all gaps", `(partition-all 2 3 [1 2 3 4 5 6 7])`, `((1 2) (4 5) (7))`},
		{"partition-all exact length", `(partition-all 3 [1 2 3])`, `((1 2 3))`},
		{"partition-all empty", `(partition-all 2 nil)`, `()`},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertRunResult(t, MustNewRunner(), test.source, test.want)
		})
	}
}

func TestPartitionRejectsNonPositiveSizeAndStep(t *testing.T) {
	for _, source := range []string{
		`(partition 0 [1 2])`,
		`(partition -1 [1 2])`,
		`(partition 1 0 [1 2])`,
		`(partition 1 -1 [1 2])`,
		`(partition-all 0 [1 2])`,
		`(partition-all -1 [1 2])`,
		`(partition-all 1 0 [1 2])`,
		`(partition-all 1 -1 [1 2])`,
	} {
		t.Run(source, func(t *testing.T) {
			assertRunErrorContains(t, MustNewRunner(), source, "must be positive")
		})
	}
}
