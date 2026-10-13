package cli

import (
	"fmt"
)

func (app *App) runReleaseCommand() error {
	return app.runNamedSubcommand(defaultReleaseSubcommandRegistry())
}

func (app *App) runReleaseBundleSubcommand() error {
	opts, err := parseReleaseBundleCommandArgs(app.args[2:])
	if err != nil {
		return err
	}
	if app.services.Release == nil {
		return fmt.Errorf("release service is not configured")
	}

	outputs, err := app.services.Release.RunBundle(*opts)
	if err != nil {
		return err
	}
	fmt.Print(renderReleaseBundleSuccess(outputs, opts.AllowUnsigned))
	return nil
}

func (app *App) runReleaseVerifySubcommand() error {
	opts, err := parseReleaseVerifyCommandArgs(app.args[2:])
	if err != nil {
		return err
	}
	if app.services.Release == nil {
		return fmt.Errorf("release service is not configured")
	}

	if err := app.services.Release.RunVerify(*opts); err != nil {
		return err
	}
	fmt.Print(renderReleaseVerifySuccess())
	return nil
}

func (app *App) runReleaseGateSubcommand() error {
	opts, err := parseReleaseGateCommandArgs(app.args[2:])
	if err != nil {
		return err
	}
	if app.services.Release == nil {
		return fmt.Errorf("release service is not configured")
	}

	if err := app.services.Release.RunGate(*opts); err != nil {
		return err
	}
	fmt.Print(renderReleaseGateSuccess())
	return nil
}

func (app *App) runReleaseKeygenSubcommand() error {
	opts, err := parseReleaseKeygenCommandArgs(app.args[2:])
	if err != nil {
		return err
	}
	if app.services.Release == nil {
		return fmt.Errorf("release service is not configured")
	}

	outputs, err := app.services.Release.RunKeygen(*opts)
	if err != nil {
		return err
	}
	fmt.Print(renderReleaseKeygenSuccess(outputs))
	return nil
}
