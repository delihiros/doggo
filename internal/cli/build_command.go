package cli

import (
	"fmt"
)

func (app *App) runBuildCommand() error {
	opts, err := parseBuildCommandArgs(app.args[1:])
	if err != nil {
		return err
	}
	if app.services.Build == nil {
		return fmt.Errorf("build service is not configured")
	}
	return app.services.Build(opts.WithSync)
}
