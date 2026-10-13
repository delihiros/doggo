// Package vm はDoggo言語の仮想マシンを実装します。
package vm

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ProfileData は関数ごとのプロファイルデータを保持します。
type ProfileData struct {
	CompletedFrameCount int64         // 正常終了したVMフレーム数
	TotalTime           time.Duration // 総実行時間（子呼び出し含む）
}

// Profiler はVM実行時のプロファイリングを行います。
type Profiler struct {
	mu      sync.Mutex
	enabled bool
	data    map[string]*ProfileData
}

// NewProfiler は新しいProfilerを作成します。
func NewProfiler() *Profiler {
	return &Profiler{
		enabled: false,
		data:    make(map[string]*ProfileData),
	}
}

// Enable はプロファイリングを有効にします。
func (p *Profiler) Enable() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled = true
}

// Disable はプロファイリングを無効にします。
func (p *Profiler) Disable() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled = false
}

// IsEnabled はプロファイリングが有効かどうかを返します。
func (p *Profiler) IsEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enabled
}

// RecordCompletedFrame は正常終了したVMフレームの実行時間を記録します。
func (p *Profiler) RecordCompletedFrame(fnName string, duration time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.enabled {
		return
	}

	data, ok := p.data[fnName]
	if !ok {
		data = &ProfileData{}
		p.data[fnName] = data
	}
	data.CompletedFrameCount++
	data.TotalTime += duration
}

// Reset はプロファイルデータをクリアします。
func (p *Profiler) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data = make(map[string]*ProfileData)
}

// profileEntry はレポート用のエントリです。
type profileEntry struct {
	Name                string
	CompletedFrameCount int64
	TotalTime           time.Duration
	AvgTime             time.Duration
}

// Report はプロファイルデータのレポートを生成します。
func (p *Profiler) Report() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.data) == 0 {
		return "No profile data collected.\n"
	}

	// エントリを収集
	entries := make([]profileEntry, 0, len(p.data))
	for name, data := range p.data {
		avgTime := time.Duration(0)
		if data.CompletedFrameCount > 0 {
			avgTime = data.TotalTime / time.Duration(data.CompletedFrameCount)
		}
		entries = append(entries, profileEntry{
			Name:                name,
			CompletedFrameCount: data.CompletedFrameCount,
			TotalTime:           data.TotalTime,
			AvgTime:             avgTime,
		})
	}

	// TotalTimeで降順ソート
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].TotalTime > entries[j].TotalTime
	})

	// レポートを生成
	var sb strings.Builder
	sb.WriteString("\n=== Profile Report ===\n")
	sb.WriteString(fmt.Sprintf("%-40s %16s %15s %15s\n", "Function", "Completed Frames", "Total Time", "Avg Time"))
	sb.WriteString(strings.Repeat("-", 88) + "\n")

	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("%-40s %16d %15s %15s\n",
			truncateName(e.Name, 40),
			e.CompletedFrameCount,
			formatDuration(e.TotalTime),
			formatDuration(e.AvgTime),
		))
	}

	sb.WriteString(strings.Repeat("-", 88) + "\n")
	return sb.String()
}

// Snapshot はプロファイルデータのコピーを返します（テスト用）。
func (p *Profiler) Snapshot() map[string]*ProfileData {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make(map[string]*ProfileData, len(p.data))
	for k, v := range p.data {
		result[k] = &ProfileData{
			CompletedFrameCount: v.CompletedFrameCount,
			TotalTime:           v.TotalTime,
		}
	}
	return result
}

// truncateName は名前を指定した長さに切り詰めます。
func truncateName(name string, maxLen int) string {
	if len(name) <= maxLen {
		return name
	}
	return name[:maxLen-3] + "..."
}

// formatDuration は時間を読みやすい形式にフォーマットします。
func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.2fµs", float64(d.Nanoseconds())/1000)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1000000)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}
