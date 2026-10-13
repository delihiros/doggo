package bench

// BuiltinScenarios returns the built-in benchmark scenarios.
func BuiltinScenarios() []Scenario {
	return []Scenario{
		{
			Name:        "loop-fusion/map-reduce",
			Description: "map + reduce pipeline (1K elements)",
			Code: `
				(reduce + 0
					(map (fn [x] (* x 2))
						(range 1000)))
			`,
			Warmup:     3,
			Iterations: 10,
		},
		{
			Name:        "loop-fusion/filter-reduce",
			Description: "filter + reduce pipeline (1K elements)",
			Code: `
				(reduce + 0
					(filter (fn [x] (> x 500))
						(range 1000)))
			`,
			Warmup:     3,
			Iterations: 10,
		},
		{
			Name:        "loop-fusion/map-filter-reduce",
			Description: "map + filter + reduce pipeline (1K elements)",
			Code: `
				(->> (range 1000)
					(map (fn [x] (* x 2)))
					(filter (fn [x] (> x 500)))
					(reduce + 0))
			`,
			Warmup:     3,
			Iterations: 10,
		},
		{
			Name:        "loop-fusion/map-reduce-100",
			Description: "map + reduce pipeline (100 elements)",
			Code: `
				(reduce + 0
					(map (fn [x] (* x 2))
						(range 100)))
			`,
			Warmup:     5,
			Iterations: 100,
		},
		{
			Name:        "loop-fusion/map-reduce-10k",
			Description: "map + reduce pipeline (10K elements)",
			Code: `
				(reduce + 0
					(map (fn [x] (* x 2))
						(range 10000)))
			`,
			Warmup:     3,
			Iterations: 10,
		},
		{
			Name:        "loop-fusion/map-reduce-100k",
			Description: "map + reduce pipeline (100K elements)",
			Code: `
				(reduce + 0
					(map (fn [x] (* x 2))
						(range 100000)))
			`,
			Warmup:     2,
			Iterations: 5,
		},
		{
			Name:        "loop-fusion/map-filter-reduce-100",
			Description: "map + filter + reduce pipeline (100 elements)",
			Code: `
				(->> (range 100)
					(map (fn [x] (* x 2)))
					(filter (fn [x] (> x 50)))
					(reduce + 0))
			`,
			Warmup:     5,
			Iterations: 100,
		},
		{
			Name:        "loop-fusion/map-filter-reduce-10k",
			Description: "map + filter + reduce pipeline (10K elements)",
			Code: `
				(->> (range 10000)
					(map (fn [x] (* x 2)))
					(filter (fn [x] (> x 5000)))
					(reduce + 0))
			`,
			Warmup:     3,
			Iterations: 10,
		},
		{
			Name:        "loop-fusion/map-filter-reduce-100k",
			Description: "map + filter + reduce pipeline (100K elements)",
			Code: `
				(->> (range 100000)
					(map (fn [x] (* x 2)))
					(filter (fn [x] (> x 50000)))
					(reduce + 0))
			`,
			Warmup:     2,
			Iterations: 5,
		},
		{
			Name:        "baseline/reduce-only-100",
			Description: "reduce without map/filter (baseline, 100)",
			Code: `
				(reduce + 0 (range 100))
			`,
			Warmup:     5,
			Iterations: 100,
		},
		{
			Name:        "baseline/reduce-only",
			Description: "reduce without map/filter (baseline, 1K)",
			Code: `
				(reduce + 0 (range 1000))
			`,
			Warmup:     3,
			Iterations: 10,
		},
		{
			Name:        "baseline/loop-recur",
			Description: "manual loop/recur equivalent (1K)",
			Code: `
				(loop [i 0 sum 0]
					(if (>= i 1000)
						sum
						(recur (inc i) (+ sum (* i 2)))))
			`,
			Warmup:     3,
			Iterations: 10,
		},
		{
			Name:        "baseline/reduce-only-10k",
			Description: "reduce without map/filter (baseline, 10K)",
			Code: `
				(reduce + 0 (range 10000))
			`,
			Warmup:     3,
			Iterations: 10,
		},
		{
			Name:        "baseline/reduce-only-100k",
			Description: "reduce without map/filter (baseline, 100K)",
			Code: `
				(reduce + 0 (range 100000))
			`,
			Warmup:     2,
			Iterations: 5,
		},
		{
			Name:        "closure/iife-loop",
			Description: "immediately-invoked closure in loop (1K iterations)",
			Code: `
				(loop [sum 0 i 0]
					(if (< i 1000)
						(recur (+ sum ((fn [x] (* x 2)) i)) (inc i))
						sum))
			`,
			Warmup:     3,
			Iterations: 10,
		},
		{
			Name:        "closure/inline-arithmetic",
			Description: "inline arithmetic (no closure, for comparison)",
			Code: `
				(loop [sum 0 i 0]
					(if (< i 1000)
						(recur (+ sum (* i 2)) (inc i))
						sum))
			`,
			Warmup:     3,
			Iterations: 10,
		},
		{
			Name:        "non-fusion/map-no-reduce",
			Description: "map without reduce (no fusion)",
			Code: `
				(last (map (fn [x] (* x 2)) (range 1000)))
			`,
			Warmup:     3,
			Iterations: 10,
		},
		{
			Name:        "non-fusion/reduce-no-init",
			Description: "reduce without init (no fusion)",
			Code: `
				(reduce + (map (fn [x] x) (range 1 1001)))
			`,
			Warmup:     3,
			Iterations: 10,
		},
	}
}

// FindScenario looks up a benchmark scenario by name.
func FindScenario(name string) *Scenario {
	for _, scenario := range BuiltinScenarios() {
		if scenario.Name == name {
			copy := scenario
			return &copy
		}
	}
	return nil
}
