package cli

import (
	"fmt"
)

func (app *App) runPublishCommand() error {
	opts, err := parsePublishCommandArgs(app.args[1:])
	if err != nil {
		return err
	}

	if app.services.Publish == nil {
		return fmt.Errorf("publish service is not configured")
	}
	result, err := app.services.Publish(opts.RegistryPath, opts.Source, opts.AllowOverwrite)
	if err != nil {
		return err
	}

	fmt.Print(renderPublishResult(result))
	return nil
}
