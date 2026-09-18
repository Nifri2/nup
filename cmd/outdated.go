package cmd

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/spf13/cobra"

	"github.com/Nifri2/nup/internal/output"
	"github.com/Nifri2/nup/internal/pkgset"
	"github.com/Nifri2/nup/internal/ui"
	"github.com/Nifri2/nup/internal/version"
)

// outdatedWorkers bounds how many nix evaluations run at once. Each one is a
// separate nix process, so more is not faster past a small number.
const outdatedWorkers = 8

func newOutdatedCommand(app *App) *cobra.Command {
	var branch string
	cmd := &cobra.Command{
		Use:   "outdated",
		Short: "Show packages for which a newer version exists",
		Long: "outdated evaluates the head of the nixpkgs branch for every installed package\n" +
			"and reports those whose version is newer there than what is installed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := commandContext(cmd)
			defer cancel()

			pkgs, err := app.packages(ctx)
			if err != nil {
				return err
			}
			if branch == "" {
				branch = app.Cfg.Branch
			}

			results := CheckOutdated(ctx, app.Engine, pkgs, branch, nil)
			if len(results) == 0 && app.Format == output.FormatTable {
				fmt.Fprintf(app.Out, "%s\n", ui.S().Green.Render("everything is up to date"))
				return nil
			}
			return app.renderPackages(results)
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "nixpkgs branch to compare against (default from config)")
	return cmd
}

// Outdated reports one package's latest version. A failed lookup is not fatal:
// many packages simply have no matching top-level attribute.
type Outdated struct {
	Package pkgset.Package
	Latest  string
	Err     error
}

// CheckOutdated evaluates the branch head version for every package and returns
// those that are behind. progress, when set, is called once per finished
// package with the number completed so far.
func CheckOutdated(ctx context.Context, engine interface {
	Latest(ctx context.Context, attr, branch string) (string, string, error)
}, pkgs []pkgset.Package, branch string, progress func(done, total int)) []pkgset.Package {
	type job struct {
		idx int
		pkg pkgset.Package
	}

	jobs := make(chan job)
	var (
		mu       sync.Mutex
		outdated []pkgset.Package
		done     int
	)

	var wg sync.WaitGroup
	for i := 0; i < outdatedWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				latest, _, err := engine.Latest(ctx, j.pkg.Attr, branch)
				mu.Lock()
				done++
				if progress != nil {
					progress(done, len(pkgs))
				}
				if err == nil && latest != "" && j.pkg.Version != "" &&
					version.Compare(j.pkg.Version, latest) < 0 {
					p := j.pkg
					p.Latest = latest
					outdated = append(outdated, p)
				}
				mu.Unlock()
			}
		}()
	}
	for i, p := range pkgs {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return outdated
		case jobs <- job{idx: i, pkg: p}:
		}
	}
	close(jobs)
	wg.Wait()

	sort.Slice(outdated, func(i, j int) bool { return outdated[i].Name < outdated[j].Name })
	return outdated
}
