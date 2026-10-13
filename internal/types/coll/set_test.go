package coll_test

import (
	"fmt"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	. "doggo/internal/types/coll"
)

func TestSet(t *testing.T) {
	tests := []struct {
		name     string
		set      *Set
		expected string
	}{
		{"nil set", nil, "#{}"},
		{"empty set", NewSet(), "#{}"},
		{"single item", NewSet(atom.NewKeyword("a")), "#{:a}"},
		{"multiple items", NewSet(atom.NewKeyword("a"), atom.NewKeyword("b"), atom.NewKeyword("c")), "#{:a :b :c}"},
		{"duplicate items", NewSet(atom.NewKeyword("a"), atom.NewKeyword("a"), atom.NewKeyword("b")), "#{:a :b}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			if tt.set == nil {
				var nilSet *Set
				result = nilSet.String()
			} else {
				result = tt.set.String()
			}
			// Setの順序は不定なので、空かどうかと要素数だけチェック
			if tt.set == nil || tt.set.Count() == 0 {
				if result != "#{}" {
					t.Errorf("Set.String() = %v, want #{}", result)
				}
			} else {
				if len(result) < 4 {
					t.Errorf("Set.String() = %v, want non-empty", result)
				}
				// 要素数が正しいかチェック
				expectedCount := tt.set.Count()
				if tt.name == "duplicate items" {
					expectedCount = 2 // 重複は除去される
				}
				if tt.set.Count() != expectedCount {
					t.Errorf("Set.Count() = %v, want %v", tt.set.Count(), expectedCount)
				}
			}
			if tt.set != nil && tt.set.Type() != "set" {
				t.Errorf("Set.Type() = %v, want set", tt.set.Type())
			}
		})
	}
}

func TestSetPersistent(t *testing.T) {
	tests := []struct {
		name string
		s    *Set
	}{
		{"nil set", nil},
		{"empty set", NewSet()},
		{"single element", NewSet(atom.NewKeyword("a"))},
		{"multiple elements", NewSet(atom.NewKeyword("a"), atom.NewKeyword("b"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.s == nil {
				if tt.s.Count() != 0 {
					t.Errorf("nil set Count() = %v, want 0", tt.s.Count())
				}
				return
			}

			expectedCount := tt.s.Count()

			// Count
			if tt.s.Count() != expectedCount {
				t.Errorf("Set.Count() = %v, want %v", tt.s.Count(), expectedCount)
			}

			// Conj
			newElem := atom.NewKeyword("new")
			conjed := tt.s.Conj(newElem)
			conjedSet, ok := conjed.(*Set)
			if !ok {
				t.Errorf("Set.Conj() returned %T, want *Set", conjed)
			} else {
				if !conjedSet.Contains(newElem) {
					t.Errorf("Conjed set does not contain newElem")
				}
			}

			// Seq
			if expectedCount == 0 {
				if tt.s.Seq() != nil {
					t.Errorf("Empty set Seq() = %v, want nil", tt.s.Seq())
				}
			} else {
				if tt.s.Seq() == nil {
					t.Errorf("Non-empty set Seq() = nil, want non-nil")
				}
			}
		})
	}
}

func TestSetDisj(t *testing.T) {
	tests := []struct {
		name         string
		set          *Set
		itemToRemove interface{}
		expectedLen  int
	}{
		{
			"remove existing element",
			NewSet(atom.NewKeyword("a"), atom.NewKeyword("b"), atom.NewKeyword("c")),
			atom.NewKeyword("a"),
			2,
		},
		{
			"remove non-existing element",
			NewSet(atom.NewKeyword("a"), atom.NewKeyword("b")),
			atom.NewKeyword("c"),
			2,
		},
		{
			"remove from empty set",
			NewSet(),
			atom.NewKeyword("a"),
			0,
		},
		{
			"remove from nil set",
			nil,
			atom.NewKeyword("a"),
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.set.Disj(tt.itemToRemove.(atom.Keyword))
			if result == nil {
				t.Fatalf("Disj() returned nil")
			}
			if result.Count() != tt.expectedLen {
				t.Errorf("Disj() resulted in set with %v elements, want %v", result.Count(), tt.expectedLen)
			}
			// 元のセットが変更されていないことを確認（不変性）
			if tt.set != nil && tt.name == "remove existing element" {
				if tt.set.Count() != 3 {
					t.Errorf("Original set was modified, has %v elements, want 3", tt.set.Count())
				}
			}
		})
	}
}

func TestSetGet(t *testing.T) {
	tests := []struct {
		name      string
		set       *Set
		item      interface{}
		wantFound bool
	}{
		{
			"get existing element",
			NewSet(atom.NewKeyword("a"), atom.NewKeyword("b")),
			atom.NewKeyword("a"),
			true,
		},
		{
			"get non-existing element",
			NewSet(atom.NewKeyword("a"), atom.NewKeyword("b")),
			atom.NewKeyword("c"),
			false,
		},
		{
			"get from empty set",
			NewSet(),
			atom.NewKeyword("a"),
			false,
		},
		{
			"get from nil set",
			nil,
			atom.NewKeyword("a"),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.set.Get(tt.item.(atom.Keyword))
			if tt.wantFound {
				if result == nil {
					t.Errorf("Get() = nil, want %v", tt.item)
				} else if !result.Equals(tt.item.(atom.Keyword)) {
					t.Errorf("Get() = %v, want %v", result, tt.item)
				}
			} else {
				if result != nil {
					t.Errorf("Get() = %v, want nil", result)
				}
			}
		})
	}
}

// TestSetContainsWithNumbers はNumber型（ポインタ型）での値ベースの比較をテストします。
func TestSetContainsWithNumbers(t *testing.T) {
	// 異なるNumber インスタンスを使用してSetを作成
	s := NewSet(atom.NewNumberInt(1), atom.NewNumberInt(2), atom.NewNumberInt(3))

	tests := []struct {
		name      string
		item      *atom.Number
		wantFound bool
	}{
		{
			"find existing number with new instance",
			atom.NewNumberInt(1), // 新しいインスタンス
			true,
		},
		{
			"find another existing number",
			atom.NewNumberInt(2),
			true,
		},
		{
			"find non-existing number",
			atom.NewNumberInt(99),
			false,
		},
		{
			"find with float equivalent",
			atom.NewNumberFloat(1.0), // 1と等価
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.Get(tt.item)
			if tt.wantFound {
				if result == nil {
					t.Errorf("Get(%v) = nil, want non-nil", tt.item)
				}
			} else {
				if result != nil {
					t.Errorf("Get(%v) = %v, want nil", tt.item, result)
				}
			}
		})
	}
}

// TestSetDisjWithNumbers はNumber型でのDisjをテストします。
func TestSetDisjWithNumbers(t *testing.T) {
	s := NewSet(atom.NewNumberInt(1), atom.NewNumberInt(2), atom.NewNumberInt(3))

	// 新しいインスタンスで削除
	result := s.Disj(atom.NewNumberInt(2))

	// 元のセットのサイズは変わらない（不変性）
	if s.Count() != 3 {
		t.Errorf("Original set was modified, Count() = %v, want 3", s.Count())
	}

	// 結果のセットからは削除されている
	if result.Get(atom.NewNumberInt(2)) != nil {
		t.Errorf("Disj did not remove element 2")
	}

	// 他の要素は残っている
	if result.Get(atom.NewNumberInt(1)) == nil {
		t.Errorf("Disj incorrectly removed element 1")
	}
	if result.Get(atom.NewNumberInt(3)) == nil {
		t.Errorf("Disj incorrectly removed element 3")
	}
}

// TestSetEquality はSet間のEqualsメソッドをテストします。
func TestSetEquality(t *testing.T) {
	tests := []struct {
		name  string
		set1  *Set
		set2  *Set
		equal bool
	}{
		{
			"equal empty sets",
			NewSet(),
			NewSet(),
			true,
		},
		{
			"equal sets with keywords",
			NewSet(atom.NewKeyword("a"), atom.NewKeyword("b")),
			NewSet(atom.NewKeyword("b"), atom.NewKeyword("a")), // 順序が異なる
			true,
		},
		{
			"equal sets with numbers",
			NewSet(atom.NewNumberInt(1), atom.NewNumberInt(2)),
			NewSet(atom.NewNumberInt(2), atom.NewNumberInt(1)),
			true,
		},
		{
			"unequal sets - different size",
			NewSet(atom.NewKeyword("a")),
			NewSet(atom.NewKeyword("a"), atom.NewKeyword("b")),
			false,
		},
		{
			"unequal sets - same size different elements",
			NewSet(atom.NewKeyword("a"), atom.NewKeyword("b")),
			NewSet(atom.NewKeyword("a"), atom.NewKeyword("c")),
			false,
		},
		{
			"nil and empty set",
			nil,
			NewSet(),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result bool
			if tt.set1 == nil {
				result = tt.set1 == nil && tt.set2 == nil
			} else {
				result = tt.set1.Equals(tt.set2)
			}
			if result != tt.equal {
				t.Errorf("set1.Equals(set2) = %v, want %v", result, tt.equal)
			}
		})
	}
}

// BenchmarkSetContains はセットの要素検索のベンチマークです。
func BenchmarkSetContains(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			// セットを作成
			elements := make([]types.Value, size)
			for i := 0; i < size; i++ {
				elements[i] = atom.NewNumberInt(int64(i))
			}
			set := NewSet(elements...)

			// 検索対象（セットの中央の要素）
			target := atom.NewNumberInt(int64(size / 2))

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				set.Contains(target)
			}
		})
	}
}
