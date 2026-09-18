package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Nifri2/nup/internal/config"
	"github.com/Nifri2/nup/internal/ui"
)

func newResetCommand(app *App) *cobra.Command {
	var f updateFlags
	cmd := &cobra.Command{
		Use:     "reset <package>...",
		Aliases: []string{"unpin"},
		Short:   "Remove pins so packages come from the main nixpkgs again",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := commandContext(cmd)
			defer cancel()
			s := ui.S()

			if f.dryRun {
				for _, n := range args {
					if _, ok := app.Lock.Get(n); ok {
						fmt.Fprintf(app.Out, "would remove pin for %s\n", n)
					} else {
						fmt.Fprintf(app.Out, "%s is not pinned\n", n)
					}
				}
				return nil
			}

			removed, err := app.Engine.Unpin(args)
			if err != nil {
				return err
			}
			if len(removed) == 0 {
				fmt.Fprintf(app.Out, "nothing to do: none of %s is pinned\n", strings.Join(args, ", "))
				return nil
			}
			fmt.Fprintf(app.Out, "removed pin for %s\n", s.Bold.Render(strings.Join(removed, ", ")))
			fmt.Fprintf(app.Out, "wrote %s\n", app.LockPath)

			if f.commit || app.Cfg.AutoCommit {
				if app.Git.IsRepo(ctx) {
					msg := "nup: reset " + strings.Join(removed, ", ")
					if err := app.Git.Commit(ctx, msg, "nup.lock.json"); err != nil {
						return fmt.Errorf("git commit: %w", err)
					}
					fmt.Fprintln(app.Out, "committed nup.lock.json")
				}
			}

			action, err := app.resolveAction(&f)
			if err != nil {
				return err
			}
			if action == config.ActionNone {
				return nil
			}
			fmt.Fprintln(app.Out)
			return app.Engine.Rebuild(ctx, app.Cfg, action, app.In, app.Out, app.Err)
		},
	}
	f.register(cmd, false)
	return cmd
}
