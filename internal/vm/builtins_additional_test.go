package vm

import "testing"

func TestAdditionalBuiltins(t *testing.T) {
	runner := MustNewRunner()
	if _, err := runner.Run(`
		(require '[doggo.edn :as edn]
		         '[doggo.set :as dset]
		         '[doggo.string :as string])
	`); err != nil {
		t.Fatalf("require additional namespaces: %v", err)
	}

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "nested map operations",
			source: `[(merge nil {:a 1} {:a 2 :b 3})
			          (zipmap [:a :b :c] [1 2])
			          (get-in {:a [{:b 7}]} [:a 0 :b] :missing)
			          (get-in {} [:a] :missing)
			          (assoc-in nil [:a :b] 1)
			          (update {:count 10} :count + 5)
			          (update-in {:a {:b 2}} [:a :b] * 3)
			          (merge-with + {:a 1 :b 2} {:a 3 :c 4})
			          (select-keys {:a 1 :b 2} [:b :missing])]`,
			want: `[{:a 2, :b 3} {:a 1, :b 2} 7 :missing {:a {:b 1}} {:count 15} {:a {:b 6}} {:a 4, :b 2, :c 4} {:b 2}]`,
		},
		{
			name: "aggregation and sorting",
			source: `[(count (get (group-by even? [1 2 3 4]) true))
			          (sort [3 1 2])
			          (sort ["b" "a"])
			          (get (first (sort-by :age [{:age 30} {:age 25}])) :age)
			          (sort [2 (edn/read-string "##NaN") 1])]`,
			want: `[2 (1 2 3) ("a" "b") 25 (1 2 NaN)]`,
		},
		{
			name: "sequence operations",
			source: `[(second [1 2 3])
			          (second [1])
			          (last [1 2 3])
			          (last nil)
			          (butlast [1 2 3])
			          (butlast [])
			          (distinct [1 2 1 3 2])
			          (dedupe [1 1 2 2 1 1 3])
			          (flatten [[1 2] [3 [4]]])
			          (get (frequencies [:a :b :a]) :a)
			          (interleave [1 2] [:a :b :c])
			          (interpose :sep [1 2 3])]`,
			want: `[2 nil 3 nil (1 2) nil (1 2 3) (1 2 1 3) (1 2 3 4) 2 (1 :a 2 :b) (1 :sep 2 :sep 3)]`,
		},
		{
			name: "partitioning",
			source: `[(partition 2 [1 2 3 4 5])
			          (partition 2 1 [1 2 3])
			          (partition-by even? [1 1 2 4 3])
			          (split-at 2 [1 2 3 4])
			          (split-with #(< % 3) [1 2 3 4])]`,
			want: `[((1 2) (3 4)) ((1 2) (2 3)) ((1 1) (2 4) (3)) [(1 2) (3 4)] [(1 2) (3 4)]]`,
		},
		{
			name: "set operations",
			source: `[(= #{1 2 3} (dset/union #{1 2} #{2 3}))
			          (= #{2 3} (dset/intersection #{1 2 3} #{2 3 4}))
			          (= #{1 3} (dset/difference #{1 2 3} #{2}))
			          (dset/subset? #{1 2} #{1 2 3})
			          (dset/subset? #{1 4} #{1 2 3})
			          (dset/superset? #{1 2 3} #{1 2})
			          (dset/superset? #{1 2} #{1 2 3})]`,
			want: `[true true true true false true false]`,
		},
		{
			name: "string operations",
			source: `[(format "%s: %d" "Count" 5)
			          (string/replace "abab" "ab" "cd")
			          (string/ends-with? "hello world" "world")
			          (string/includes? "hello world" "lo wo")]`,
			want: `["Count: 5" "cdcd" true true]`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertRunResult(t, runner, test.source, test.want)
		})
	}

	t.Run("merge rejects non-maps", func(t *testing.T) {
		assertRunErrorContains(t, runner, `(merge {:a 1} 2)`, "merge: expected map, got number")
	})
}
