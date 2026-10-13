package cli

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
)

// StartPprofSession starts Go CPU profiling and returns a function that stops
// it and writes the requested heap profile.
func StartPprofSession(opts PprofOptions) (func(), error) {
	var cpuFile *os.File

	// CPUプロファイリングの開始
	if opts.CPUProfile != "" {
		f, err := os.Create(opts.CPUProfile)
		if err != nil {
			return nil, fmt.Errorf("could not create CPU profile: %w", err)
		}
		cpuFile = f
		if err := pprof.StartCPUProfile(f); err != nil {
			cpuFile.Close()
			return nil, fmt.Errorf("could not start CPU profile: %w", err)
		}
	}

	// クリーンアップ関数を返す
	cleanup := func() {
		// CPUプロファイリングの停止
		if cpuFile != nil {
			pprof.StopCPUProfile()
			cpuFile.Close()
			fmt.Fprintf(os.Stderr, "CPU profile written to %s\n", opts.CPUProfile)
		}

		// メモリプロファイリング
		if opts.MemProfile != "" {
			f, err := os.Create(opts.MemProfile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not create memory profile: %v\n", err)
				return
			}
			defer f.Close()
			runtime.GC() // GCを実行して正確なメモリ統計を取得
			if err := pprof.WriteHeapProfile(f); err != nil {
				fmt.Fprintf(os.Stderr, "could not write memory profile: %v\n", err)
				return
			}
			fmt.Fprintf(os.Stderr, "Memory profile written to %s\n", opts.MemProfile)
		}
	}

	return cleanup, nil
}
