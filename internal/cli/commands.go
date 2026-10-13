package cli

import (
	toolchaindeps "doggo/internal/toolchain/deps"
	"fmt"
	"strings"
)

// Run はコマンドライン引数を解析して適切なコマンドを実行します。
func (app *App) Run() error {
	remainingArgs, err := app.ParseGlobalFlags()
	if err != nil {
		return err
	}

	// プロファイリングの開始
	stopPprof, err := StartPprofSession(app.pprof)
	if err != nil {
		return fmt.Errorf("failed to start pprof session: %w", err)
	}
	defer stopPprof()

	app.args = remainingArgs
	return app.Dispatch()
}

// Dispatch は既に正規化されたコマンド引数を実行します。
func (app *App) Dispatch() error {
	if len(app.args) < 1 {
		return app.RunREPL()
	}

	command := app.args[0]
	registry := DefaultCLIRegistry()

	// ハイフンで始まらない、かつサブコマンドとして認識されない引数はファイルパスとみなす
	if command != "" && command[0] != '-' && registry.Lookup(command) == nil {
		return RunFileWithArgsAndVMFrameProfiling(command, app.args[1:], app.enableVMFrameProfiling)
	}

	handler := registry.Lookup(command)
	if handler == nil {
		return fmt.Errorf("unknown command: %s", command)
	}
	return handler.Run(app)
}

// isSubcommand は文字列が既知のサブコマンドかどうかを判定します。
func isSubcommand(s string) bool {
	return DefaultCLIRegistry().Lookup(s) != nil
}

func shortRef(kind, ref string) string {
	if kind == "go" {
		return ref
	}
	if len(ref) > 12 {
		return ref[:12]
	}
	return ref
}

func shortDigest(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

func displayOrUnknown(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func formatOutdatedStatusLine(status toolchaindeps.DependencyOutdatedStatus) string {
	if status.CheckError != "" {
		return fmt.Sprintf("%s [check-error] %s", status.Source, status.CheckError)
	}

	if status.Kind == "path" {
		if status.Outdated {
			return fmt.Sprintf(
				"%s [outdated] locked-digest=%s latest-digest=%s",
				status.Source,
				displayOrUnknown(shortDigest(status.LockedDigest)),
				displayOrUnknown(shortDigest(status.LatestDigest)),
			)
		}
		return fmt.Sprintf(
			"%s [up-to-date] digest=%s",
			status.Source,
			displayOrUnknown(shortDigest(status.LockedDigest)),
		)
	}

	if status.Outdated {
		return fmt.Sprintf(
			"%s [outdated] locked=%s latest=%s",
			status.Source,
			displayOrUnknown(shortRef(status.Kind, status.LockedRevision)),
			displayOrUnknown(shortRef(status.Kind, status.LatestRevision)),
		)
	}
	return fmt.Sprintf("%s [up-to-date] revision=%s", status.Source, displayOrUnknown(shortRef(status.Kind, status.LockedRevision)))
}

func formatDependencyUpdateLine(result toolchaindeps.DependencyUpdateResult) string {
	switch result.Kind {
	case "path":
		if result.Updated {
			return fmt.Sprintf(
				"%s [updated] old-digest=%s new-digest=%s",
				result.Source,
				displayOrUnknown(shortDigest(result.OldDigest)),
				displayOrUnknown(shortDigest(result.NewDigest)),
			)
		}
		return fmt.Sprintf("%s [up-to-date] digest=%s", result.Source, displayOrUnknown(shortDigest(result.NewDigest)))
	default:
		if result.Updated {
			return fmt.Sprintf(
				"%s [updated] old-revision=%s new-revision=%s",
				result.Source,
				displayOrUnknown(shortRef(result.Kind, result.OldRevision)),
				displayOrUnknown(shortRef(result.Kind, result.NewRevision)),
			)
		}
		return fmt.Sprintf("%s [up-to-date] revision=%s", result.Source, displayOrUnknown(shortRef(result.Kind, result.NewRevision)))
	}
}

func formatDependencyAuditFindingLine(finding toolchaindeps.DependencyAuditFinding) string {
	source := finding.Source
	if strings.TrimSpace(source) == "" {
		source = "<unknown>"
	}
	kind := finding.Kind
	if strings.TrimSpace(kind) == "" {
		kind = "unknown"
	}
	severity := strings.ToLower(strings.TrimSpace(finding.Severity))
	if severity == "" {
		severity = "unknown"
	}
	code := finding.Code
	if strings.TrimSpace(code) == "" {
		code = "unknown"
	}
	message := strings.TrimSpace(finding.Message)
	if message == "" {
		message = "no details"
	}
	return fmt.Sprintf("%s [%s/%s/%s] %s", source, severity, kind, code, message)
}
