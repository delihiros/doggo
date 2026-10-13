package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResultJSON(t *testing.T) {
	result := NewResult("loop-fusion/map-reduce")
	result.Warmup = 5
	result.Iterations = 100
	result.Stats = Stats{
		TotalNs:                       1000000000, // 1秒
		MeanNs:                        10000000,   // 10ms/iteration
		MinNs:                         9000000,
		MaxNs:                         15000000,
		AllocBytes:                    1024 * 1024,
		AllocObjects:                  1000,
		LastIterationInstructionCount: 7,
		LastIterationOpcodeCounts:     map[string]uint64{"OpConst": 1},
	}

	data, err := result.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// JSONをパースして必須フィールドを確認
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	requiredFields := []string{
		"scenario",
		"timestamp",
		"go_version",
		"os",
		"arch",
		"warmup",
		"iterations",
		"stats",
	}

	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	// stats内の必須フィールドを確認
	stats, ok := parsed["stats"].(map[string]interface{})
	if !ok {
		t.Fatalf("stats is not an object")
	}

	statsFields := []string{
		"total_ns",
		"mean_ns",
		"min_ns",
		"max_ns",
		"alloc_bytes",
		"alloc_objects",
		"last_iteration_instruction_count",
		"last_iteration_opcode_counts",
	}

	for _, field := range statsFields {
		if _, ok := stats[field]; !ok {
			t.Errorf("missing required stats field: %s", field)
		}
	}
}

func TestResultFilename(t *testing.T) {
	timestamp := time.Date(2025, 12, 18, 15, 30, 45, 0, time.UTC)

	tests := []struct {
		scenario string
		expected string
	}{
		{"loop-fusion", "loop-fusion_20251218_153045.json"},
		{"map-reduce", "map-reduce_20251218_153045.json"},
	}

	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			filename := ResultFilename(tt.scenario, timestamp)
			if filename != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, filename)
			}
		})
	}
}

func TestSaveResult(t *testing.T) {
	// 一時ディレクトリを作成
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "bench", "results")

	result := NewResult("test-scenario")
	result.Warmup = 3
	result.Iterations = 10
	result.Stats = Stats{
		TotalNs:      500000000,
		MeanNs:       50000000,
		MinNs:        45000000,
		MaxNs:        60000000,
		AllocBytes:   2048,
		AllocObjects: 20,
	}

	savedPath, err := SaveResult(result, outputDir)
	if err != nil {
		t.Fatalf("SaveResult failed: %v", err)
	}

	// ファイルが存在することを確認
	if _, err := os.Stat(savedPath); os.IsNotExist(err) {
		t.Fatalf("saved file does not exist: %s", savedPath)
	}

	// ファイル名が規約に従っていることを確認
	filename := filepath.Base(savedPath)
	if !strings.HasPrefix(filename, "test-scenario_") {
		t.Errorf("filename should start with scenario name: %s", filename)
	}
	if !strings.HasSuffix(filename, ".json") {
		t.Errorf("filename should end with .json: %s", filename)
	}

	// ファイルの内容を確認
	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	var loaded Result
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to parse saved JSON: %v", err)
	}

	if loaded.Scenario != "test-scenario" {
		t.Errorf("scenario mismatch: expected %s, got %s", "test-scenario", loaded.Scenario)
	}
	if loaded.Iterations != 10 {
		t.Errorf("iterations mismatch: expected %d, got %d", 10, loaded.Iterations)
	}
}

func TestNewResult(t *testing.T) {
	result := NewResult("my-scenario")

	if result.Scenario != "my-scenario" {
		t.Errorf("scenario mismatch")
	}
	if result.GoVersion == "" {
		t.Errorf("go_version should not be empty")
	}
	if result.OS == "" {
		t.Errorf("os should not be empty")
	}
	if result.Arch == "" {
		t.Errorf("arch should not be empty")
	}
	if result.Timestamp.IsZero() {
		t.Errorf("timestamp should not be zero")
	}
}
