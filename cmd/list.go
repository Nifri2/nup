package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Nifri2/nup/internal/lock"
	"github.com/Nifri2/nup/internal/output"
	"github.com/Nifri2/nup/internal/pkgset"
	"github.com/Nifri2/nup/internal/ui"
)

func newListCommand(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List every installed package",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := commandContext(cmd)
			defer cancel()
			pkgs, err := app.packages(ctx)
			if err != nil {
				return err
			}
			app.warnUntracked(ctx)
			return app.renderPackages(pkgs)
		},
	}
}

func newQueryCommand(app *App) *cobra.Command {
	var useRegex bool
	cmd := &cobra.Command{
		Use:     "query <string>",
		Aliases: []string{"search", "q"},
		Short:   "Search installed packages by name",
		Long: "query matches the given string against package names, case-insensitively,\n" +
			"as a substring by default or as a regular expression with --regex.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := commandContext(cmd)
			defer cancel()
			pkgs, err := app.packages(ctx)
			if err != nil {
				return err
			}
			matched, err := Filter(pkgs, args[0], useRegex)
			if err != nil {
				return err
			}
			if len(matched) == 0 && app.Format == output.FormatTable {
				fmt.Fprintf(app.Err, "no package matches %q\n", args[0])
				return nil
			}
			return app.renderPackages(matched)
		},
	}
	cmd.Flags().BoolVarP(&useRegex, "regex", "r", false, "treat the query as a regular expression")
	return cmd
}

// Filter narrows a package list by name. Matching is case-insensitive in both
// modes; the description is searched too, which makes query useful for finding
// a package whose name you cannot remember.
func Filter(pkgs []pkgset.Package, query string, useRegex bool) ([]pkgset.Package, error) {
	var match func(pkgset.Package) bool
	if useRegex {
		re, err := regexp.Compile("(?i)" + query)
		if err != nil {
			return nil, fmt.Errorf("invalid regular expression %q: %w", query, err)
		}
		match = func(p pkgset.Package) bool {
			return re.MatchString(p.Name) || re.MatchString(p.Attr)
		}
	} else {
		needle := strings.ToLower(query)
		match = func(p pkgset.Package) bool {
			return strings.Contains(strings.ToLower(p.Name), needle) ||
				strings.Contains(strings.ToLower(p.Attr), needle)
		}
	}
	out := make([]pkgset.Package, 0, len(pkgs))
	for _, p := range pkgs {
		if match(p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// renderPackages writes a package list in the selected format.
func (a *App) renderPackages(pkgs []pkgset.Package) error {
	if pkgs == nil {
		pkgs = []pkgset.Package{}
	}
	return output.Render(a.Out, a.Format, PackageTable(pkgs), pkgs)
}

// PackageTable builds the table shown by list, query and outdated.
func PackageTable(pkgs []pkgset.Package) *output.Table {
	s := ui.S()
	showLatest := false
	for _, p := range pkgs {
		if p.Latest != "" {
			showLatest = true
			break
		}
	}

	headers := []string{"name", "version"}
	if showLatest {
		headers = append(headers, "latest")
	}
	headers = append(headers, "pinned", "source")

	t := &output.Table{Headers: headers}
	for _, p := range pkgs {
		row := []output.Cell{
			output.Plain(p.Name),
			output.Plain(output.OrDash(p.Version)),
		}
		if showLatest {
			cell := output.Plain(output.OrDash(p.Latest))
			if p.Latest != "" && p.Latest != p.Version {
				cell.Style = s.Green
			}
			row = append(row, cell)
		}
		pinned := output.Plain(output.Dash)
		if p.Pinned() {
			pinned = output.Plain(lock.ShortRev(p.PinnedRev))
			// A pin on a different revision than the flake's own nixpkgs is
			// worth pointing out: it may be older than the rest of the system.
			if p.Stale {
				pinned.Style = s.Yellow
			}
		}
		row = append(row, pinned, output.Plain(string(p.Source)))
		t.Rows = append(t.Rows, row)
	}
	return t
}
