package cli

import (
	"flag"
	"io"
	"os"
	"strings"
)

// parseGlobalFlags はグローバルフラグを解析し、残りの引数を返します。
func (app *App) parseGlobalFlags() ([]string, error) {
	return app.parseGlobalFlagsWithOutput(os.Stderr)
}

func (app *App) parseGlobalFlagsWithOutput(output io.Writer) ([]string, error) {
	flagSet := flag.NewFlagSet("doggo", flag.ContinueOnError)
	flagSet.SetOutput(output)
	flagSet.StringVar(&app.pprof.CPUProfile, "cpu-profile", "", "Go CPUプロファイルを指定ファイルに出力")
	flagSet.StringVar(&app.pprof.MemProfile, "mem-profile", "", "Goヒーププロファイルを指定ファイルに出力")
	flagSet.BoolVar(&app.enableVMFrameProfiling, "vm-frame-profile", false, "正常終了したVMフレームの実行時間を表示")

	// -h と --help のデフォルト動作を無効化
	flagSet.Usage = func() {}

	// フラグとそれ以外の引数を分離する
	var flagArgs []string
	var remainingArgs []string
	inFlags := true

	for i := 0; i < len(app.args); i++ {
		arg := app.args[i]
		if inFlags && strings.HasPrefix(arg, "-") {
			// help aliases はコマンドとして扱う（フラグとして処理しない）
			if arg == "-h" || arg == "--help" || arg == "-?" || arg == "-help" {
				inFlags = false
				remainingArgs = append(remainingArgs, arg)
				continue
			}
			// フラグの場合
			if strings.Contains(arg, "=") {
				// --flag=value 形式
				flagArgs = append(flagArgs, arg)
			} else if arg == "-cpu-profile" || arg == "-mem-profile" ||
				arg == "--cpu-profile" || arg == "--mem-profile" {
				// 値を取るフラグ
				flagArgs = append(flagArgs, arg)
				if i+1 < len(app.args) {
					i++
					flagArgs = append(flagArgs, app.args[i])
				}
			} else {
				// -vm-frame-profile など
				flagArgs = append(flagArgs, arg)
			}
		} else {
			// フラグでない引数が見つかったら、以降はすべて残りの引数
			inFlags = false
			remainingArgs = append(remainingArgs, arg)
		}
	}

	if err := flagSet.Parse(flagArgs); err != nil {
		return nil, err
	}

	return remainingArgs, nil
}

// ParseGlobalFlags はグローバルフラグを解析し、コマンド引数を返します。
func (app *App) ParseGlobalFlags() ([]string, error) {
	return app.parseGlobalFlags()
}
