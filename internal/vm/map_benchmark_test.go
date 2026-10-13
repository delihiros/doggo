package vm

import (
	"strings"
	"testing"

	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

// BenchmarkMapBuild benchmarks Map construction with repeated assoc
func BenchmarkMapBuild(b *testing.B) {
	b.Run("assoc_100_entries", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m := coll.NewEmptyMap()
			for j := 0; j < 100; j++ {
				key := atom.NewKeyword("key" + string(rune('a'+j%26)) + string(rune('0'+j/26)))
				val := atom.NewNumberFloat(float64(j))
				m = m.Assoc(key, val).(*coll.Map)
			}
		}
	})

	b.Run("assoc_500_entries", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m := coll.NewEmptyMap()
			for j := 0; j < 500; j++ {
				key := atom.NewKeyword("key" + string(rune('a'+j%26)) + string(rune('0'+j/26)))
				val := atom.NewNumberFloat(float64(j))
				m = m.Assoc(key, val).(*coll.Map)
			}
		}
	})

	b.Run("transient_100_entries", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tm := coll.NewTransientMap()
			for j := 0; j < 100; j++ {
				key := atom.NewKeyword("key" + string(rune('a'+j%26)) + string(rune('0'+j/26)))
				val := atom.NewNumberFloat(float64(j))
				_, _ = tm.Assoc(key, val)
			}
			_, _ = tm.Persistent()
		}
	})

	b.Run("transient_500_entries", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tm := coll.NewTransientMap()
			for j := 0; j < 500; j++ {
				key := atom.NewKeyword("key" + string(rune('a'+j%26)) + string(rune('0'+j/26)))
				val := atom.NewNumberFloat(float64(j))
				_, _ = tm.Assoc(key, val)
			}
			_, _ = tm.Persistent()
		}
	})
}

// TestTransientMap tests the transient map functionality
func TestTransientMap(t *testing.T) {
	t.Run("basic operations", func(t *testing.T) {
		tm := coll.NewTransientMap()
		_, _ = tm.Assoc(atom.NewKeyword("a"), atom.NewNumberFloat(1))
		_, _ = tm.Assoc(atom.NewKeyword("b"), atom.NewNumberFloat(2))
		_, _ = tm.Assoc(atom.NewKeyword("c"), atom.NewNumberFloat(3))

		m, _ := tm.Persistent()

		if m.Count() != 3 {
			t.Errorf("expected count 3, got %d", m.Count())
		}

		if m.Get(atom.NewKeyword("a")).(*atom.Number).ToFloat64() != 1.0 {
			t.Error("expected :a to be 1")
		}
		if m.Get(atom.NewKeyword("b")).(*atom.Number).ToFloat64() != 2.0 {
			t.Error("expected :b to be 2")
		}
		if m.Get(atom.NewKeyword("c")).(*atom.Number).ToFloat64() != 3.0 {
			t.Error("expected :c to be 3")
		}
	})

	t.Run("update existing key", func(t *testing.T) {
		tm := coll.NewTransientMap()
		_, _ = tm.Assoc(atom.NewKeyword("a"), atom.NewNumberFloat(1))
		_, _ = tm.Assoc(atom.NewKeyword("a"), atom.NewNumberFloat(10))

		m, _ := tm.Persistent()

		if m.Count() != 1 {
			t.Errorf("expected count 1, got %d", m.Count())
		}
		if m.Get(atom.NewKeyword("a")).(*atom.Number).ToFloat64() != 10.0 {
			t.Error("expected :a to be 10")
		}
	})

	t.Run("error after persistent", func(t *testing.T) {
		tm := coll.NewTransientMap()
		_, _ = tm.Assoc(atom.NewKeyword("a"), atom.NewNumberFloat(1))
		_, _ = tm.Persistent()

		// Persistent後のAssocはエラーを返すべき
		_, err := tm.Assoc(atom.NewKeyword("b"), atom.NewNumberFloat(2))
		if err == nil {
			t.Error("expected error after persistent, got nil")
		}
		if err != coll.ErrTransientNotEditable {
			t.Errorf("expected ErrTransientNotEditable, got %v", err)
		}

		// Persistent後のDissocもエラーを返すべき
		_, err = tm.Dissoc(atom.NewKeyword("a"))
		if err == nil {
			t.Error("expected error after persistent for Dissoc, got nil")
		}

		// Persistent後の再度Persistentもエラーを返すべき
		_, err = tm.Persistent()
		if err == nil {
			t.Error("expected error for double Persistent, got nil")
		}
	})
}

// TestTransientMapDoggo tests transient map functions from Doggo code
func TestTransientMapDoggo(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "transient and persistent",
			input:    `(persistent! (assoc! (transient {}) :a 1 :b 2))`,
			expected: "{:a 1, :b 2}",
		},
		{
			name:     "transient from existing map",
			input:    `(count (persistent! (assoc! (transient {:a 1}) :b 2 :c 3)))`,
			expected: "3",
		},
	}

	runner := MustNewRunner()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

// TestTransientMapErrorDoggo tests that transient map errors propagate correctly in Doggo
func TestTransientMapErrorDoggo(t *testing.T) {
	runner := MustNewRunner()
	_, err := runner.Run(`(let [tm (transient {})]
		(persistent! tm)
		(assoc! tm :a 1))`)
	if err == nil || !strings.Contains(err.Error(), "no longer editable") {
		t.Fatalf("expected transient edit error, got %v", err)
	}
}

func TestTransientMapRejectsUseFromFuture(t *testing.T) {
	runner := MustNewRunner()
	_, err := runner.Run(`(let [tm (transient {})]
		@(future (assoc! tm :a 1)))`)
	if err == nil {
		t.Fatal("expected transient use from another logical execution to fail")
	}
	if !strings.Contains(err.Error(), "owner execution") {
		t.Fatalf("expected owner execution error, got %q", err.Error())
	}
}
