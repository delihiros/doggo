package cli

// PprofOptions configures Go CPU and heap profile output.
type PprofOptions struct {
	CPUProfile string // CPUプロファイルの出力ファイル
	MemProfile string // メモリプロファイルの出力ファイル
}

// App はCLIアプリケーションを表します。
type App struct {
	version                string
	args                   []string
	pprof                  PprofOptions
	enableVMFrameProfiling bool
	services               appServices
}

// NewApp は新しいCLIアプリケーションを作成します。
func NewApp(version string, args []string) *App {
	return &App{
		version:  version,
		args:     args,
		services: defaultServices(),
	}
}
