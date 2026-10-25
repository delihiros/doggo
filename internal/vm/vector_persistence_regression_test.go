package vm

import "testing"

func TestRunnerVectorPersistence(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "sibling branches",
			source: `(let [base (conj [] 1 2 3)
			              left (conj base 4)
			              right (conj base 5)]
			          [left right])`,
			want: `[[1 2 3 4] [1 2 3 5]]`,
		},
		{
			name: "retained sequence",
			source: `(let [base (conj [] 1 2 3)
			              left (conj base 4)
			              view (seq left)
			              right (conj base 5)]
			          [view right])`,
			want: `[(1 2 3 4) [1 2 3 5]]`,
		},
		{
			name: "map key",
			source: `(let [base (conj [] 1 2 3)
			              key (conj base 4)
			              index (hash-map key :found)
			              sibling (conj base 5)]
			          [(get index key :missing) (get index [1 2 3 4] :missing)])`,
			want: `[:found :found]`,
		},
		{
			name: "set member",
			source: `(let [base (conj [] 1 2 3)
			              key (conj base 4)
			              members (conj #{} key)
			              sibling (conj base 5)]
			          [(contains? members key) (contains? members [1 2 3 4])])`,
			want: `[true true]`,
		},
		{
			name: "metadata view branches",
			source: `(let [base (conj [] 1 2 3)
			              decorated (with-meta base {:tag :source})
			              left (conj base 4)
			              right (conj decorated 5)]
			          [left right (meta decorated) (meta right)])`,
			want: `[[1 2 3 4] [1 2 3 5] {:tag :source} {}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRunResult(t, MustNewRunner(), tt.source, tt.want)
		})
	}
}
