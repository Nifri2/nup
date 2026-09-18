package cmd

import (
	"context"

	"github.com/Nifri2/nup/internal/tui"
)

// runTUI starts the interactive interface. It hands the TUI the same engine the
// CLI uses, so both behave identically.
func runTUI(ctx context.Context, app *App) error {
	return tui.Run(ctx, tui.Deps{
		Engine:   app.Engine,
		Lister:   app.Lister,
		Config:   app.Cfg,
		Lock:     app.Lock,
		MainRev:  app.MainRev,
		LockPath: app.LockPath,
		Refresh:  app.Refresh,
	})
}
