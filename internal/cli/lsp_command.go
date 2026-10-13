package cli

import (
	"fmt"
	"os"

	"doggo/internal/lsp"
)

func (app *App) runLSPCommand() error {
	if len(app.args) > 1 {
		return fmt.Errorf("lsp command does not accept arguments")
	}
	return lsp.NewServer(os.Stdin, os.Stdout, defaultOpenGenerationSession).Run()
}
