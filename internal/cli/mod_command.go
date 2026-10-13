package cli

import (
	"fmt"
)

func (app *App) runModCommand() error {
	return app.runNamedSubcommand(defaultModSubcommandRegistry())
}

func (app *App) runNamedSubcommand(registry *cliSubcommandRegistry) error {
	if len(app.args) < 2 {
		return fmt.Errorf("%s command requires a subcommand (available: %s)", registry.parent, registry.AvailableList())
	}

	subcommand := registry.Lookup(app.args[1])
	if subcommand == nil {
		return fmt.Errorf("unknown %s subcommand: %s (available: %s)", registry.parent, app.args[1], registry.AvailableList())
	}
	return subcommand.Run(app)
}

func (app *App) runModInitSubcommand() error {
	if app.services.InitProject == nil {
		return fmt.Errorf("init service is not configured")
	}
	return app.services.InitProject()
}

func (app *App) runModSyncSubcommand() error {
	if app.services.Sync == nil {
		return fmt.Errorf("sync service is not configured")
	}
	return app.services.Sync()
}
