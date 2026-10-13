package cli

import (
	"fmt"
	"os"

	"doggo/internal/toolchain/newproject"
)

func (app *App) runNewCommand() error {
	if app.services.NewProject == nil {
		return fmt.Errorf("new project service is not configured")
	}
	args, err := parseNewCommandArgs(app.args[1:])
	if err != nil {
		return err
	}
	baseDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve current directory: %w", err)
	}
	result, err := app.services.NewProject(newproject.Options{
		Template: args.Template,
		Name:     args.Name,
		Module:   args.Module,
		DryRun:   args.DryRun,
		BaseDir:  baseDir,
	})
	if err != nil {
		return err
	}
	fmt.Print(renderNewProjectResult(result))
	return nil
}
