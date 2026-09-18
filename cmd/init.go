package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"context"

	"github.com/spf13/cobra"

	"github.com/Nifri2/nup/internal/config"
	"github.com/Nifri2/nup/internal/lock"
	"github.com/Nifri2/nup/internal/overlay"
	"github.com/Nifri2/nup/internal/ui"
)

func newInitCommand(app *App) *cobra.Command {
	var (
		force  bool
		gitAdd bool
		noGit  bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the lock file, the overlay and a config file",
		Long: "init writes nup.lock.json and nup-overlay.nix into the flake directory and\n" +
			"creates a config file if none exists. It never modifies flake.nix: the overlay\n" +
			"has to be wired in once by hand, and init prints the snippet for it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := commandContext(cmd)
			defer cancel()
			return runInit(ctx, app, force, gitAdd, noGit)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing nup-overlay.nix")
	cmd.Flags().BoolVar(&gitAdd, "git-add", false, "stage the generated files without asking")
	cmd.Flags().BoolVar(&noGit, "no-git", false, "never touch git")
	return cmd
}

func runInit(ctx context.Context, app *App, force, gitAdd, noGit bool) error {
	s := ui.S()

	if _, err := os.Stat(app.LockPath); os.IsNotExist(err) {
		if err := lock.New().Save(app.LockPath); err != nil {
			return err
		}
		fmt.Fprintf(app.Out, "created %s\n", app.LockPath)
	} else {
		fmt.Fprintf(app.Out, "kept %s (%d pinned package(s))\n", app.LockPath, len(app.Lock.Packages))
	}

	overlayPath := overlay.Path(app.FlakeDir)
	written, err := overlay.Write(overlayPath, force)
	if err != nil {
		return err
	}
	if written {
		fmt.Fprintf(app.Out, "created %s\n", overlayPath)
	} else {
		fmt.Fprintf(app.Out, "kept %s (use --force to regenerate)\n", overlayPath)
	}

	cfgPath := config.Path()
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		cfg := config.Default()
		cfg.Flake = app.FlakeDir
		cfg.Host = app.Host
		if err := cfg.Save(cfgPath); err != nil {
			return err
		}
		fmt.Fprintf(app.Out, "created %s\n", cfgPath)
	} else {
		fmt.Fprintf(app.Out, "kept %s\n", cfgPath)
	}

	if !noGit && app.Git.IsRepo(ctx) {
		if err := maybeGitAdd(ctx, app, gitAdd); err != nil {
			return err
		}
	}

	fmt.Fprintf(app.Out, "\n%s\n", s.Bold.Render("Add the overlay to your configuration once:"))
	fmt.Fprintf(app.Out, "\n%s\n", s.Dim.Render("  # in a NixOS module, e.g. configuration.nix"))
	fmt.Fprintf(app.Out, "%s\n", overlay.Snippet)
	fmt.Fprintf(app.Out, "\n%s\n", s.Dim.Render(
		"  # if the overlay lives elsewhere, adjust the path; nup never edits flake.nix itself"))
	fmt.Fprintf(app.Out, "\nThen run %s to see your packages.\n", s.Bold.Render("nup list"))
	return nil
}

// maybeGitAdd stages nup's files, asking first unless told otherwise. A git
// flake only sees tracked files, so skipping this makes nix ignore the pins.
func maybeGitAdd(ctx context.Context, app *App, always bool) error {
	var untracked []string
	for _, f := range []string{lock.Name, overlay.Name} {
		if _, err := os.Stat(filepath.Join(app.FlakeDir, f)); err != nil {
			continue
		}
		if !app.Git.IsTracked(ctx, f) {
			untracked = append(untracked, f)
		}
	}
	if len(untracked) == 0 {
		return nil
	}

	fmt.Fprintf(app.Out, "\n%s is a git flake, and nix only sees files git knows about.\n", app.FlakeDir)
	fmt.Fprintf(app.Out, "Untracked: %v\n", untracked)

	add := always
	if !add {
		ok, err := app.confirm("Stage these files with git add?")
		if err != nil {
			// Not interactive: just tell the user what to run.
			fmt.Fprintf(app.Err, "  run: git -C %s add %v\n", app.FlakeDir, untracked)
			return nil
		}
		add = ok
	}
	if !add {
		fmt.Fprintf(app.Out, "  skipped; run: git -C %s add %v\n", app.FlakeDir, untracked)
		return nil
	}
	if err := app.Git.Add(ctx, untracked...); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	fmt.Fprintf(app.Out, "staged %v\n", untracked)
	return nil
}
