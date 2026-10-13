package bench

import (
	"runtime"
	"sort"
	"time"
)

// StatsCollector は実行統計を収集します。
type StatsCollector struct {
	scenario   string
	warmup     int
	iterations int

	// 計測データ
	durations []time.Duration
	memBefore runtime.MemStats
	memAfter  runtime.MemStats

	// 開始時刻
	started time.Time

	// コンパイル/実行時間の分離計測
	compileTime time.Duration   // コンパイル時間（compile-once モード用）
	execTimes   []time.Duration // 各iterationの実行時間（compile除く）

	// 最終計測iterationのVM命令実行統計
	lastIterationInstructionCount uint64
	lastIterationOpcodeCounts     map[string]uint64

	// 最適化の適用統計
	optApplied map[string]int
}

// NewStatsCollector は新しいStatsCollectorを作成します。
func NewStatsCollector(scenario string, warmup, iterations int) *StatsCollector {
	return &StatsCollector{
		scenario:                  scenario,
		warmup:                    warmup,
		iterations:                iterations,
		durations:                 make([]time.Duration, 0, iterations),
		execTimes:                 make([]time.Duration, 0, iterations),
		lastIterationOpcodeCounts: make(map[string]uint64),
		optApplied:                make(map[string]int),
	}
}

// CaptureMemoryBaseline はGC後のメモリ統計を基準値として記録します。
func (c *StatsCollector) CaptureMemoryBaseline() {
	runtime.GC() // GCを実行してメモリを安定させる
	runtime.ReadMemStats(&c.memBefore)
}

// CaptureMemoryEndpoint はメモリ統計を終点値として記録します。
func (c *StatsCollector) CaptureMemoryEndpoint() {
	runtime.ReadMemStats(&c.memAfter)
}

// RecordDuration は1回の実行時間を記録します。
func (c *StatsCollector) RecordDuration(d time.Duration) {
	c.durations = append(c.durations, d)
}

// SetStartTime は計測開始時刻を設定します。
func (c *StatsCollector) SetStartTime(t time.Time) {
	c.started = t
}

// RecordOptApplied は最適化の適用を記録します。
func (c *StatsCollector) RecordOptApplied(optName string) {
	c.optApplied[optName]++
}

// SetCompileTime はコンパイル時間を記録します（compile-once モード用）。
func (c *StatsCollector) SetCompileTime(d time.Duration) {
	c.compileTime = d
}

// RecordExecTime は実行時間（コンパイル除く）を記録します。
func (c *StatsCollector) RecordExecTime(d time.Duration) {
	c.execTimes = append(c.execTimes, d)
}

// SetLastIterationInstructionStats は最終計測iterationのVM命令統計を記録します。
func (c *StatsCollector) SetLastIterationInstructionStats(total uint64, byOpcode map[string]uint64) {
	c.lastIterationInstructionCount = total
	if byOpcode != nil {
		c.lastIterationOpcodeCounts = byOpcode
	}
}

// ToResult は収集した統計からResultを生成します。
func (c *StatsCollector) ToResult() *Result {
	result := NewResult(c.scenario)
	result.Warmup = c.warmup
	result.Iterations = c.iterations

	if !c.started.IsZero() {
		result.Timestamp = c.started
	}

	// 時間統計を計算
	if len(c.durations) > 0 {
		var total int64
		min := c.durations[0].Nanoseconds()
		max := c.durations[0].Nanoseconds()

		for _, d := range c.durations {
			ns := d.Nanoseconds()
			total += ns
			if ns < min {
				min = ns
			}
			if ns > max {
				max = ns
			}
		}

		result.Stats.TotalNs = total
		result.Stats.MeanNs = float64(total) / float64(len(c.durations))
		result.Stats.MinNs = min
		result.Stats.MaxNs = max
	}

	// コンパイル時間（compile-once モード）
	if c.compileTime > 0 {
		result.Stats.CompileTimeNs = c.compileTime.Nanoseconds()
	}

	// 実行時間統計（コンパイル除く）
	if len(c.execTimes) > 0 {
		var execTotal int64
		for _, d := range c.execTimes {
			execTotal += d.Nanoseconds()
		}
		result.Stats.ExecTimeNs = execTotal
	}

	// メモリ統計を計算
	result.Stats.AllocBytes = c.memAfter.TotalAlloc - c.memBefore.TotalAlloc
	result.Stats.AllocObjects = c.memAfter.Mallocs - c.memBefore.Mallocs

	// 最適化統計
	if len(c.optApplied) > 0 {
		result.Stats.OptApplied = c.optApplied
	}

	// 最終計測iterationのVM命令統計
	if c.lastIterationInstructionCount > 0 {
		result.Stats.LastIterationInstructionCount = c.lastIterationInstructionCount
	}
	if len(c.lastIterationOpcodeCounts) > 0 {
		result.Stats.LastIterationOpcodeCounts = c.lastIterationOpcodeCounts
	}

	// 中央値とP95（durationsがある場合のみ）
	if len(c.durations) > 0 {
		result.Stats.MedianNs = int64(c.Median())
		result.Stats.P95Ns = int64(c.Percentile(95))
	}

	return result
}

// Median はdurationsの中央値を返します（ソート済みのコピーを作成）。
func (c *StatsCollector) Median() time.Duration {
	if len(c.durations) == 0 {
		return 0
	}

	// コピーを作成してソート
	sorted := make([]time.Duration, len(c.durations))
	copy(sorted, c.durations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

// Percentile はdurationsの指定パーセンタイルを返します。
func (c *StatsCollector) Percentile(p float64) time.Duration {
	if len(c.durations) == 0 || p < 0 || p > 100 {
		return 0
	}

	// コピーを作成してソート
	sorted := make([]time.Duration, len(c.durations))
	copy(sorted, c.durations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	idx := int(float64(len(sorted)-1) * p / 100)
	return sorted[idx]
}

// Timer は単一のタイミング計測を行うヘルパーです。
type Timer struct {
	start time.Time
}

// NewTimer は新しいTimerを開始します。
func NewTimer() *Timer {
	return &Timer{start: time.Now()}
}

// Elapsed は経過時間を返します。
func (t *Timer) Elapsed() time.Duration {
	return time.Since(t.start)
}

// Reset はタイマーをリセットします。
func (t *Timer) Reset() {
	t.start = time.Now()
}
