// Package bench はDoggo言語のベンチマーク基盤を提供します。
package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Scenario はベンチマークシナリオを定義します。
type Scenario struct {
	Name        string // シナリオ名（例: "loop-fusion/map-reduce"）
	Description string // 説明
	Code        string // 実行するDoggoコード
	Setup       string // セットアップコード（オプション）
	Warmup      int    // ウォームアップ回数
	Iterations  int    // 計測回数
	Expected    string // 期待される結果（オプション、検証用）
}

// Result はベンチマーク結果を保持します。
type Result struct {
	// メタデータ
	Scenario  string    `json:"scenario"`
	Timestamp time.Time `json:"timestamp"`
	GitCommit string    `json:"git_commit,omitempty"`
	GoVersion string    `json:"go_version"`
	OS        string    `json:"os"`
	Arch      string    `json:"arch"`
	DoggoVer  string    `json:"doggo_version,omitempty"`

	// 実行設定
	Warmup     int `json:"warmup"`
	Iterations int `json:"iterations"`

	// 最適化設定
	OptConfig *OptConfig `json:"optimization,omitempty"`

	// 実行統計
	Stats Stats `json:"stats"`
}

// OptConfig はベンチマーク時の最適化設定を記録します。
type OptConfig struct {
	LoopFusion bool `json:"loop_fusion"`
}

// Stats は実行統計を保持します。
type Stats struct {
	// 時間統計（ナノ秒）
	TotalNs  int64   `json:"total_ns"`            // 全体の実行時間
	MeanNs   float64 `json:"mean_ns"`             // 平均実行時間/iteration
	MinNs    int64   `json:"min_ns"`              // 最小実行時間
	MaxNs    int64   `json:"max_ns"`              // 最大実行時間
	MedianNs int64   `json:"median_ns,omitempty"` // 中央値
	P95Ns    int64   `json:"p95_ns,omitempty"`    // 95パーセンタイル

	// メモリ統計（バイト）
	AllocBytes   uint64 `json:"alloc_bytes"`   // 総アロケーションバイト数
	AllocObjects uint64 `json:"alloc_objects"` // 総アロケーションオブジェクト数

	// コンパイル/実行分離統計（compile-once モード用）
	CompileTimeNs int64 `json:"compile_time_ns,omitempty"` // コンパイル時間
	ExecTimeNs    int64 `json:"exec_time_ns,omitempty"`    // 実行時間（コンパイル除く）

	// 最終計測iterationのVM命令統計
	LastIterationInstructionCount uint64            `json:"last_iteration_instruction_count,omitempty"`
	LastIterationOpcodeCounts     map[string]uint64 `json:"last_iteration_opcode_counts,omitempty"`

	// 最適化統計
	OptApplied map[string]int `json:"opt_applied,omitempty"` // 適用された最適化とその回数
}

// NewResult は新しいResultを作成します。
func NewResult(scenarioName string) *Result {
	return &Result{
		Scenario:  scenarioName,
		Timestamp: time.Now(),
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Stats:     Stats{},
	}
}

// SetGitCommit はGitコミットSHAを設定します。
func (r *Result) SetGitCommit(sha string) {
	r.GitCommit = sha
}

// SetDoggoVersion はDoggoバージョンを設定します。
func (r *Result) SetDoggoVersion(version string) {
	r.DoggoVer = version
}

// SetOptConfig は最適化設定を記録します。
func (r *Result) SetOptConfig(loopFusion bool) {
	r.OptConfig = &OptConfig{
		LoopFusion: loopFusion,
	}
}

// ToJSON はResultをJSON文字列に変換します。
func (r *Result) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// ResultFilename はベンチ結果のファイル名を生成します。
// フォーマット: {scenario}_{timestamp}.json
func ResultFilename(scenarioName string, timestamp time.Time) string {
	// シナリオ名をファイル名として安全な形式に変換
	safeName := scenarioName
	// スラッシュをハイフンに変換
	for _, c := range []string{"/", "\\", " "} {
		safeName = strings.ReplaceAll(safeName, c, "-")
	}
	ts := timestamp.Format("20060102_150405")
	return fmt.Sprintf("%s_%s.json", safeName, ts)
}

// SaveResult は結果をJSONファイルに保存します。
func SaveResult(result *Result, outputDir string) (string, error) {
	// 出力ディレクトリを作成
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// ファイル名を生成
	filename := ResultFilename(result.Scenario, result.Timestamp)
	filepath := filepath.Join(outputDir, filename)

	// JSONに変換
	data, err := result.ToJSON()
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	// ファイルに書き込み
	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write result file: %w", err)
	}

	return filepath, nil
}

// DefaultOutputDir はベンチ結果のデフォルト出力ディレクトリを返します。
func DefaultOutputDir() string {
	return "bench/results"
}
