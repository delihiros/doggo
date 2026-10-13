package bench

import (
	"testing"
	"time"
)

func TestStatsCollector(t *testing.T) {
	sc := NewStatsCollector("test-scenario", 3, 10)

	// 時間を記録
	durations := []time.Duration{
		100 * time.Millisecond,
		110 * time.Millisecond,
		90 * time.Millisecond,
		105 * time.Millisecond,
		95 * time.Millisecond,
	}

	for _, d := range durations {
		sc.RecordDuration(d)
	}

	result := sc.ToResult()

	// 基本フィールドを確認
	if result.Scenario != "test-scenario" {
		t.Errorf("scenario mismatch")
	}
	if result.Warmup != 3 {
		t.Errorf("warmup mismatch")
	}
	if result.Iterations != 10 {
		t.Errorf("iterations mismatch")
	}

	// 時間統計を確認
	expectedTotal := int64(500 * time.Millisecond)
	if result.Stats.TotalNs != expectedTotal {
		t.Errorf("total_ns: expected %d, got %d", expectedTotal, result.Stats.TotalNs)
	}

	expectedMean := float64(100 * time.Millisecond)
	if result.Stats.MeanNs != expectedMean {
		t.Errorf("mean_ns: expected %f, got %f", expectedMean, result.Stats.MeanNs)
	}

	expectedMin := int64(90 * time.Millisecond)
	if result.Stats.MinNs != expectedMin {
		t.Errorf("min_ns: expected %d, got %d", expectedMin, result.Stats.MinNs)
	}

	expectedMax := int64(110 * time.Millisecond)
	if result.Stats.MaxNs != expectedMax {
		t.Errorf("max_ns: expected %d, got %d", expectedMax, result.Stats.MaxNs)
	}
}

func TestStatsCollectorMemory(t *testing.T) {
	sc := NewStatsCollector("mem-test", 0, 1)

	sc.CaptureMemoryBaseline()

	// メモリを割り当て
	_ = make([]byte, 1024*1024) // 1MB

	sc.CaptureMemoryEndpoint()

	result := sc.ToResult()

	// メモリが記録されていることを確認（厳密な値は環境依存なので下限のみチェック）
	if result.Stats.AllocBytes < 1024*1024 {
		t.Errorf("alloc_bytes should be at least 1MB, got %d", result.Stats.AllocBytes)
	}
	if result.Stats.AllocObjects == 0 {
		t.Errorf("alloc_objects should be non-zero")
	}
}

func TestStatsCollectorOptApplied(t *testing.T) {
	sc := NewStatsCollector("opt-test", 0, 1)

	sc.RecordOptApplied("loop-fusion")
	sc.RecordOptApplied("loop-fusion")
	sc.RecordOptApplied("constant-folding")

	result := sc.ToResult()

	if result.Stats.OptApplied == nil {
		t.Fatalf("opt_applied should not be nil")
	}

	if result.Stats.OptApplied["loop-fusion"] != 2 {
		t.Errorf("loop-fusion count: expected 2, got %d", result.Stats.OptApplied["loop-fusion"])
	}
	if result.Stats.OptApplied["constant-folding"] != 1 {
		t.Errorf("constant-folding count: expected 1, got %d", result.Stats.OptApplied["constant-folding"])
	}
}

func TestStatsCollectorRecordsLastIterationInstructionStats(t *testing.T) {
	sc := NewStatsCollector("instruction-test", 0, 2)
	sc.SetLastIterationInstructionStats(7, map[string]uint64{"OpConst": 1})

	stats := sc.ToResult().Stats
	if stats.LastIterationInstructionCount != 7 {
		t.Fatalf("last iteration instruction count = %d, want 7", stats.LastIterationInstructionCount)
	}
	if stats.LastIterationOpcodeCounts["OpConst"] != 1 {
		t.Fatalf("last iteration OpConst count = %d, want 1", stats.LastIterationOpcodeCounts["OpConst"])
	}
}

func TestMedian(t *testing.T) {
	tests := []struct {
		name      string
		durations []time.Duration
		expected  time.Duration
	}{
		{
			name:      "odd count",
			durations: []time.Duration{100, 200, 300, 400, 500},
			expected:  300,
		},
		{
			name:      "even count",
			durations: []time.Duration{100, 200, 300, 400},
			expected:  250, // (200 + 300) / 2
		},
		{
			name:      "unsorted",
			durations: []time.Duration{500, 100, 300, 200, 400},
			expected:  300,
		},
		{
			name:      "empty",
			durations: []time.Duration{},
			expected:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := NewStatsCollector("test", 0, len(tt.durations))
			for _, d := range tt.durations {
				sc.RecordDuration(d)
			}

			median := sc.Median()
			if median != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, median)
			}
		})
	}
}

func TestPercentile(t *testing.T) {
	sc := NewStatsCollector("test", 0, 10)

	// 1から10までの値を記録
	for i := 1; i <= 10; i++ {
		sc.RecordDuration(time.Duration(i) * time.Millisecond)
	}

	tests := []struct {
		percentile float64
		expected   time.Duration
	}{
		{0, 1 * time.Millisecond},
		{50, 5 * time.Millisecond},
		{100, 10 * time.Millisecond},
	}

	for _, tt := range tests {
		result := sc.Percentile(tt.percentile)
		// パーセンタイル計算は近似なので、範囲内であることを確認
		if result < tt.expected-time.Millisecond || result > tt.expected+time.Millisecond {
			t.Errorf("p%v: expected ~%v, got %v", tt.percentile, tt.expected, result)
		}
	}
}

func TestStartTime(t *testing.T) {
	sc := NewStatsCollector("test", 0, 1)

	customTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	sc.SetStartTime(customTime)

	result := sc.ToResult()
	if !result.Timestamp.Equal(customTime) {
		t.Errorf("timestamp should be %v, got %v", customTime, result.Timestamp)
	}
}
