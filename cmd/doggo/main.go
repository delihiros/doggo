package main

import (
	"doggo/internal/cli"
	_ "doggo/internal/interop" // 生成されたinit()関数を実行するため
	_ "doggo/internal/reify"   // reifyファクトリを登録するため
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
)

// version is set by release builds with -ldflags "-X main.version=<tag>".
// Go-installed module builds fall back to debug.BuildInfo below.
var version string

func currentVersion() string {
	info, ok := debug.ReadBuildInfo()
	return resolveVersion(version, info, ok)
}

func resolveVersion(linkerVersion string, info *debug.BuildInfo, buildInfoOK bool) string {
	if value := strings.TrimSpace(linkerVersion); value != "" {
		return value
	}
	if buildInfoOK && info != nil {
		if value := strings.TrimSpace(info.Main.Version); value != "" && value != "(devel)" {
			return value
		}
	}
	return "dev"
}

func main() {
	// os.Args[0] はプログラム名なのでスキップ
	app := cli.NewApp(currentVersion(), os.Args[1:])

	if err := app.Run(); err != nil {
		if code, ok := childExitCode(err); ok {
			os.Exit(code)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintln(os.Stderr, "Run 'doggo help' for usage information.")
		os.Exit(1)
	}
}

func childExitCode(err error) (int, bool) {
	var exitError *cli.ChildExitError
	if !errors.As(err, &exitError) {
		return 0, false
	}
	code := exitError.ExitCode()
	return code, code >= 0
}
