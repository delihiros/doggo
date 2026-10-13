package vm

import "testing"

func TestClojureIdioms(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[
		(as-> 5 x (+ x 3) (* x 2))
		(as-> {:a 1} m (assoc m :b 2) (:b m))
		(when-some [x nil] (+ x 1))
		(when-some [x false] :then)
		(if-some [x nil] (+ x 1) 0)
		(if-some [x false] :then :else)
		(when-first [x [1 2 3]] (+ x 10))
		(when-first [x []] (+ x 10))
		(some-> {:a {:b 1}} :a :b inc)
		(some-> {:a 1} :b inc)
		(some->> [1 2 3] (map inc) first)
		(some->> nil (map inc))
		((fnil + 0) nil 5)
		((fnil + 0 0) nil nil)
		((fnil + 0 0 0) nil nil nil)
		(condp = 1 1 "one" 2 "two" "other")
		(condp = 2 1 "one" 2 "two" "other")
		(condp = 5 1 "one" 2 "two" "other")
		(doto [1 2] count first)
		(cond-> 1 true inc false inc)
		(cond->> 5 true (- 10) false (* 2))
	]`, `[16 2 nil :then 0 :then 11 nil 2 nil 2 nil 5 0 0 "one" "two" "other" [1 2] 2 5]`)
}

func TestStdlibUtils(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[
		(keep #(if (even? %) (* % 2)) [1 2 3 4 5])
		(keep identity [])
		(remove even? [1 2 3 4 5])
		(remove even? [])
		(map-indexed + [10 20 30])
		(map-indexed list [])
		(apply min-key count ["aaa" "b" "cc"])
		(min-key identity 5)
		(apply max-key count ["aaa" "b" "cc"])
		(partition-all 3 [1 2 3 4 5])
		(partition-all 2 1 [1 2 3])
		(partition-all 2 [])
		(let [result (shuffle [1 2 3])] [(count result) (= (set result) #{1 2 3})])
		(shuffle [])
	]`, `[(4 8) () (1 3 5) () (10 21 32) () "b" 5 "aaa" ((1 2 3) (4 5)) ((1 2) (2 3) (3)) () [3 true] []]`)
}

func TestSeqUtils(t *testing.T) {
	runner := MustNewRunner()
	assertRunResult(t, runner, `[
		(next [1 2 3])
		(next [1])
		(next [])
		(not= 1 2)
		(not= 1 1)
		(not-any? even? [1 3 5])
		(not-any? even? [1 2 3])
		(not-any? even? [])
		(not-every? even? [2 4 5])
		(not-every? even? [2 4 6])
		(not-every? even? [])
		((some-fn even? neg?) 2)
		((some-fn even? neg?) -3)
		((some-fn even? neg?) 3)
		((every-pred even? pos?) 2)
		((every-pred even? pos?) 3)
		((every-pred even? pos?) -2)
		(rseq [1 2 3])
		(rseq [])
		(let [a (atom 0) result (run! (fn [x] (swap! a + x)) [1 2 3])] [result @a])
		(run! identity [])
	]`, `[(2 3) nil nil true false true false true true false false true true nil true false false (3 2 1) nil [nil 6] nil]`)

	if _, err := runner.Run(`(rseq {:a 1})`); err == nil {
		t.Fatal("expected rseq map error")
	}
}

func TestClojureCoreUtils(t *testing.T) {
	assertRunResult(t, MustNewRunner(), `[
		((comp str inc +) 1 2)
		((comp) 5)
		((comp +) 1 2 3)
		(into [] '(1 2 3))
		(into '() [1 2 3])
		(count (into #{} [1 2 2 3]))
		(into {} [[:a 1] [:b 2]])
		(do (defrecord User [name]) (:name (into (->User "Ada") [[:name "Bob"]])))
		(reduce-kv (fn [acc k v] (assoc acc k v)) {} {:a 1 :b 2})
		(vec (reductions + [1 2 3 4]))
		(vec (reductions + 10 [1 2 3]))
		(vec (reductions + []))
		(vec (take-nth 2 [1 2 3 4 5]))
		(vec (take-nth 2 []))
		(vec (nthrest [1 2 3 4 5] 2))
		(vec (nthrest [1 2] 5))
		(vec (nthrest [1 2 3] 0))
		(vec (keep-indexed (fn [i x] (if (odd? i) x)) [:a :b :c :d]))
		(vec (keep-indexed (fn [i x] nil) [1 2 3]))
		(contains? #{1 2 3 4 5} (rand-nth [1 2 3 4 5]))
		(vec (repeatedly 3 (fn [] 42)))
		(vec (take 3 (repeatedly (fn [] 1))))
		(vec (take 5 (take-nth 2 (range))))
		(vec (take 5 (reductions + (range))))
		(vec (take 3 (keep-indexed (fn [i x] (if (even? i) x)) (range))))
	]`, `["4" 5 6 [1 2 3] (3 2 1) 3 {:a 1, :b 2} "Bob" {:a 1, :b 2} [1 3 6 10] [10 11 13 16] [] [1 3 5] [] [3 4 5] [] [1 2 3] [:b :d] [] true [42 42 42] [1 1 1] [0 2 4 6 8] [0 1 3 6 10] [0 2 4]]`)
}
