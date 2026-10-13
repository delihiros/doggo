package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStartPprofSessionNoOptions(t *testing.T) {
	opts := PprofOptions{}
	cleanup, err := StartPprofSession(opts)
	if err != nil {
		t.Fatalf("StartPprofSession with empty options failed: %v", err)
	}
	cleanup()
}

func TestStartPprofSessionCPUProfile(t *testing.T) {
	tmpDir := t.TempDir()
	cpuFile := filepath.Join(tmpDir, "cpu.prof")

	opts := PprofOptions{
		CPUProfile: cpuFile,
	}
	cleanup, err := StartPprofSession(opts)
	if err != nil {
		t.Fatalf("StartPprofSession with CPU profile failed: %v", err)
	}

	// CPUプロファイリングが有効な状態で何か処理をする
	sum := 0
	for i := 0; i < 1000; i++ {
		sum += i
	}
	_ = sum

	cleanup()

	// ファイルが作成されたことを確認
	if _, err := os.Stat(cpuFile); os.IsNotExist(err) {
		t.Error("CPU profile file was not created")
	}
}

func TestStartPprofSessionMemProfile(t *testing.T) {
	tmpDir := t.TempDir()
	memFile := filepath.Join(tmpDir, "mem.prof")

	opts := PprofOptions{
		MemProfile: memFile,
	}
	cleanup, err := StartPprofSession(opts)
	if err != nil {
		t.Fatalf("StartPprofSession with memory profile failed: %v", err)
	}

	// メモリを使用する
	data := make([]byte, 1024*1024)
	_ = data

	cleanup()

	// ファイルが作成されたことを確認
	if _, err := os.Stat(memFile); os.IsNotExist(err) {
		t.Error("Memory profile file was not created")
	}
}

func TestStartPprofSessionBothProfiles(t *testing.T) {
	tmpDir := t.TempDir()
	cpuFile := filepath.Join(tmpDir, "cpu.prof")
	memFile := filepath.Join(tmpDir, "mem.prof")

	opts := PprofOptions{
		CPUProfile: cpuFile,
		MemProfile: memFile,
	}
	cleanup, err := StartPprofSession(opts)
	if err != nil {
		t.Fatalf("StartPprofSession with both profiles failed: %v", err)
	}

	cleanup()

	// 両方のファイルが作成されたことを確認
	if _, err := os.Stat(cpuFile); os.IsNotExist(err) {
		t.Error("CPU profile file was not created")
	}
	if _, err := os.Stat(memFile); os.IsNotExist(err) {
		t.Error("Memory profile file was not created")
	}
}

func TestStartPprofSessionInvalidCPUProfilePath(t *testing.T) {
	opts := PprofOptions{
		CPUProfile: "/nonexistent/directory/cpu.prof",
	}
	_, err := StartPprofSession(opts)
	if err == nil {
		t.Error("StartPprofSession should fail with invalid CPU profile path")
	}
}

func TestParseGlobalFlags(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantCPUProfile string
		wantMemProfile string
		wantRemaining  []string
		wantErr        bool
	}{
		{
			name:          "no flags",
			args:          []string{"script.dog"},
			wantRemaining: []string{"script.dog"},
		},
		{
			name:           "cpu-profile flag",
			args:           []string{"-cpu-profile", "cpu.prof", "script.dog"},
			wantCPUProfile: "cpu.prof",
			wantRemaining:  []string{"script.dog"},
		},
		{
			name:           "mem-profile flag",
			args:           []string{"-mem-profile", "mem.prof", "script.dog"},
			wantMemProfile: "mem.prof",
			wantRemaining:  []string{"script.dog"},
		},
		{
			name:           "all flags",
			args:           []string{"-cpu-profile", "cpu.prof", "-mem-profile", "mem.prof", "script.dog"},
			wantCPUProfile: "cpu.prof",
			wantMemProfile: "mem.prof",
			wantRemaining:  []string{"script.dog"},
		},
		{
			name:          "subcommand",
			args:          []string{"version"},
			wantRemaining: []string{"version"},
		},
		{
			name:          "profile with subcommand",
			args:          []string{"-vm-frame-profile", "repl"},
			wantRemaining: []string{"repl"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp("1.0.0", tt.args)
			remaining, err := app.parseGlobalFlags()

			if (err != nil) != tt.wantErr {
				t.Errorf("parseGlobalFlags() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			if app.pprof.CPUProfile != tt.wantCPUProfile {
				t.Errorf("CPUProfile = %q, want %q", app.pprof.CPUProfile, tt.wantCPUProfile)
			}
			if app.pprof.MemProfile != tt.wantMemProfile {
				t.Errorf("MemProfile = %q, want %q", app.pprof.MemProfile, tt.wantMemProfile)
			}

			if len(remaining) != len(tt.wantRemaining) {
				t.Errorf("remaining = %v, want %v", remaining, tt.wantRemaining)
				return
			}
			for i := range remaining {
				if remaining[i] != tt.wantRemaining[i] {
					t.Errorf("remaining[%d] = %q, want %q", i, remaining[i], tt.wantRemaining[i])
				}
			}
		})
	}
}
