package vm

import (
	"strings"
	"testing"
	"time"
)

func TestProfilerBasic(t *testing.T) {
	p := NewProfiler()

	// 初期状態ではプロファイリングは無効
	if p.IsEnabled() {
		t.Error("profiler should be disabled by default")
	}

	// 有効化
	p.Enable()
	if !p.IsEnabled() {
		t.Error("profiler should be enabled after Enable()")
	}

	// 無効化
	p.Disable()
	if p.IsEnabled() {
		t.Error("profiler should be disabled after Disable()")
	}
}

func TestProfilerRecordCompletedFrame(t *testing.T) {
	p := NewProfiler()
	p.Enable()

	// データを記録
	p.RecordCompletedFrame("test-fn", 100*time.Millisecond)
	p.RecordCompletedFrame("test-fn", 200*time.Millisecond)
	p.RecordCompletedFrame("another-fn", 50*time.Millisecond)

	data := p.Snapshot()

	// test-fn のデータを確認
	testFn, ok := data["test-fn"]
	if !ok {
		t.Fatal("test-fn should be recorded")
	}
	if testFn.CompletedFrameCount != 2 {
		t.Errorf("test-fn completed frame count: want 2, got %d", testFn.CompletedFrameCount)
	}
	if testFn.TotalTime != 300*time.Millisecond {
		t.Errorf("test-fn total time: want 300ms, got %v", testFn.TotalTime)
	}

	// another-fn のデータを確認
	anotherFn, ok := data["another-fn"]
	if !ok {
		t.Fatal("another-fn should be recorded")
	}
	if anotherFn.CompletedFrameCount != 1 {
		t.Errorf("another-fn completed frame count: want 1, got %d", anotherFn.CompletedFrameCount)
	}
}

func TestProfilerRecordCompletedFrameWhenDisabled(t *testing.T) {
	p := NewProfiler()

	// 無効化状態では記録されない
	p.RecordCompletedFrame("test-fn", 100*time.Millisecond)

	data := p.Snapshot()
	if len(data) != 0 {
		t.Errorf("no data should be recorded when disabled, got %d entries", len(data))
	}
}

func TestProfilerReset(t *testing.T) {
	p := NewProfiler()
	p.Enable()

	p.RecordCompletedFrame("test-fn", 100*time.Millisecond)
	p.Reset()

	data := p.Snapshot()
	if len(data) != 0 {
		t.Errorf("data should be empty after reset, got %d entries", len(data))
	}
}

func TestProfilerReport(t *testing.T) {
	p := NewProfiler()
	p.Enable()

	p.RecordCompletedFrame("fast-fn", 10*time.Microsecond)
	p.RecordCompletedFrame("slow-fn", 100*time.Millisecond)
	p.RecordCompletedFrame("slow-fn", 200*time.Millisecond)

	report := p.Report()

	// レポートに関数名が含まれているか
	if !strings.Contains(report, "fast-fn") {
		t.Error("report should contain fast-fn")
	}
	if !strings.Contains(report, "slow-fn") {
		t.Error("report should contain slow-fn")
	}
	if !strings.Contains(report, "Profile Report") {
		t.Error("report should contain header")
	}
	if !strings.Contains(report, "Completed Frames") {
		t.Error("report should identify completed VM frames")
	}
}

func TestProfilerReportEmpty(t *testing.T) {
	p := NewProfiler()
	p.Enable()

	report := p.Report()
	if !strings.Contains(report, "No profile data") {
		t.Error("empty report should indicate no data")
	}
}

func TestVMProfilerIntegration(t *testing.T) {
	// プロファイラ付きでVMを実行
	runner := MustNewRunner()
	profiler := NewProfiler()
	profiler.Enable()
	runner.SetProfiler(profiler)

	// 関数を定義して呼び出す
	code := `
		(defn add-one [x] (+ x 1))
		(add-one 1)
		(add-one 2)
		(add-one 3)
	`
	_, err := runner.Run(code)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	// プロファイルデータを確認
	data := profiler.Snapshot()
	if len(data) == 0 {
		t.Error("profiler should have collected data")
	}

	// レポートを生成
	report := profiler.Report()
	if report == "" {
		t.Error("report should not be empty")
	}
}

func TestVMProfilerMultipleFunctions(t *testing.T) {
	runner := MustNewRunner()
	profiler := NewProfiler()
	profiler.Enable()
	runner.SetProfiler(profiler)

	code := `
		(defn f1 [x] (+ x 1))
		(defn f2 [x] (* x 2))
		(defn f3 [x] (f1 (f2 x)))
		(f3 5)
	`
	_, err := runner.Run(code)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	data := profiler.Snapshot()
	if len(data) < 3 {
		t.Errorf("expected at least 3 function profiles, got %d", len(data))
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d        time.Duration
		expected string
	}{
		{500 * time.Nanosecond, "500ns"},
		{1500 * time.Nanosecond, "1.50µs"},
		{500 * time.Microsecond, "500.00µs"},
		{1500 * time.Microsecond, "1.50ms"},
		{500 * time.Millisecond, "500.00ms"},
		{1500 * time.Millisecond, "1.50s"},
	}

	for _, tt := range tests {
		result := formatDuration(tt.d)
		if result != tt.expected {
			t.Errorf("formatDuration(%v): want %s, got %s", tt.d, tt.expected, result)
		}
	}
}

func TestTruncateName(t *testing.T) {
	tests := []struct {
		name     string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly-ten", 11, "exactly-ten"},
		{"this-is-a-very-long-name", 10, "this-is..."}, // 10 - 3 = 7 + "..."
	}

	for _, tt := range tests {
		result := truncateName(tt.name, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncateName(%s, %d): want %s, got %s", tt.name, tt.maxLen, tt.expected, result)
		}
	}
}
