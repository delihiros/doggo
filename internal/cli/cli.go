package cli

import (
	"doggo/internal/vm"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// RunFileWithArgsAndVMFrameProfiling executes a Doggo script with explicit script arguments.
func RunFileWithArgsAndVMFrameProfiling(filename string, scriptArgs []string, enableVMFrameProfiling bool) (err error) {
	resolvedFilename, err := filepath.Abs(filepath.Clean(filename))
	if err != nil {
		return fmt.Errorf("resolve script path: %w", err)
	}

	session, err := defaultOpenGenerationSession(resolvedFilename)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	runner, err := newStandardVMRunner(session.Config, true)
	if err != nil {
		return err
	}
	runner.SetCommandLineArgs(scriptArgs)
	runner.SetEntryPointFile(resolvedFilename)

	// 正常終了したVMフレームの実行時間計測を有効化
	var profiler *vm.Profiler
	if enableVMFrameProfiling {
		profiler = vm.NewProfiler()
		profiler.Enable()
		runner.SetProfiler(profiler)
	}

	_, err = runner.LoadFile(filepath.ToSlash(resolvedFilename))
	if err != nil {
		return err
	}

	// プロファイルレポートを出力
	if enableVMFrameProfiling && profiler != nil {
		fmt.Fprint(os.Stderr, profiler.Report())
	}

	return nil
}

// PrintHelp はヘルプメッセージを表示します。
func PrintHelp() {
	fmt.Print(renderCLIHelp())
}

// PrintVersion はバージョン情報を表示します。
func PrintVersion(version string) {
	fmt.Printf("doggo version %s\n", version)
}
