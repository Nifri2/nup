package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Nifri2/nup/internal/config"
	"github.com/Nifri2/nup/internal/lock"
	"github.com/Nifri2/nup/internal/output"
	"github.com/Nifri2/nup/internal/overlay"
	"github.com/Nifri2/nup/internal/pkgset"
	"github.com/Nifri2/nup/internal/ui"
	"github.com/Nifri2/nup/internal/update"
)

// updateFlags are shared by `update` and `pin`.
type updateFlags struct {
	yes       bool
	dryRun    bool
	doSwitch  bool
	doBoot    bool
	noRebuild bool
	branch    string
	attr      string
	force     bool
	commit    bool
}

func (f *updateFlags) register(cmd *cobra.Command, withBranch bool) {
	fl := cmd.Flags()
	fl.BoolVarP(&f.yes, "yes", "y", false, "do not ask for confirmation")
	fl.BoolVar(&f.dryRun, "dry-run", false, "show what would change without writing anything")
	fl.BoolVar(&f.doSwitch, "switch", false, "run a rebuild switch afterwards")
	fl.BoolVar(&f.doBoot, "boot", false, "run a rebuild boot afterwards")
	fl.BoolVar(&f.noRebuild, "no-rebuild", false, "do not rebuild afterwards")
	fl.StringVar(&f.attr, "attr", "", "nixpkgs attribute path, when it differs from the package name")
	fl.BoolVar(&f.force, "force", false, "update even when the version is unchanged")
	fl.BoolVar(&f.commit, "commit", false, "commit the lock file change with git")
	if withBranch {
		fl.StringVar(&f.branch, "branch", "", "nixpkgs branch to update from (default from config)")
	}
	cmd.MarkFlagsMutuallyExclusive("switch", "boot", "no-rebuild")
}

// action turns the rebuild flags into a config.Action, falling back to the
// configured default.
func (f *updateFlags) action(cfg *config.Config) config.Action {
	switch {
	case f.doSwitch:
		return config.ActionSwitch
	case f.doBoot:
		return config.ActionBoot
	case f.noRebuild, f.dryRun:
		return config.ActionNone
	default:
		return cfg.AfterUpdate
	}
}

func newUpdateCommand(app *App) *cobra.Command {
	var f updateFlags
	cmd := &cobra.Command{
		Use:   "update <package>...",
		Short: "Update one or more packages to a newer nixpkgs revision",
		Long: "update resolves the head of the configured nixpkgs branch, evaluates and\n" +
			"builds the new package, shows what changes in its closure and, after one\n" +
			"confirmation for all packages, writes the pins and runs a single rebuild.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := commandContext(cmd)
			defer cancel()
			if f.attr != "" && len(args) != 1 {
				return errors.New("--attr applies to a single package; run update once per package")
			}
			reqs := make([]update.Request, 0, len(args))
			for _, name := range args {
				reqs = append(reqs, update.Request{Name: name, Attr: f.attr, Branch: f.branch, Force: f.force})
			}
			return app.runUpdate(ctx, reqs, &f)
		},
	}
	f.register(cmd, true)
	return cmd
}

func newPinCommand(app *App) *cobra.Command {
	var f updateFlags
	cmd := &cobra.Command{
		Use:   "pin <package> <rev>",
		Short: "Pin a package to a specific nixpkgs commit",
		Long: "pin records an explicit nixpkgs revision for a package instead of following\n" +
			"a branch. The revision may be any commit-ish nix can resolve.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := commandContext(cmd)
			defer cancel()
			req := update.Request{Name: args[0], Attr: f.attr, Rev: args[1], Force: f.force}
			return app.runUpdate(ctx, []update.Request{req}, &f)
		},
	}
	f.register(cmd, false)
	return cmd
}

// runUpdate is the shared pipeline behind `update` and `pin`.
func (a *App) runUpdate(ctx context.Context, reqs []update.Request, f *updateFlags) error {
	s := ui.S()

	installed, err := a.packages(ctx)
	if err != nil {
		return err
	}
	for i := range reqs {
		if matches := pkgset.Find(installed, reqs[i].Name); len(matches) > 0 {
			reqs[i].Current = matches[0]
		} else if reqs[i].Attr == "" {
			fmt.Fprintf(a.Err, "%s %q is not in this configuration; nup will pin it anyway, "+
				"but nothing will use the pin until you add the package.\n",
				s.Yellow.Render("note:"), reqs[i].Name)
		}
	}

	plans := make([]*update.Plan, 0, len(reqs))
	for _, req := range reqs {
		progress := func(msg string) {
			fmt.Fprintf(a.Err, "%s %s: %s\n", s.Dim.Render("::"), req.Name, s.Dim.Render(msg))
		}
		plan, err := a.Engine.Prepare(ctx, req, progress)
		if err != nil {
			return err
		}
		plans = append(plans, plan)
	}

	var changed []*update.Plan
	for _, p := range plans {
		if p.Unchanged {
			fmt.Fprintf(a.Out, "%s is already at %s (use --force to re-pin)\n",
				s.Bold.Render(p.Name), p.NewVersion)
			continue
		}
		changed = append(changed, p)
	}
	if len(changed) == 0 {
		return nil
	}

	if a.Format != output.FormatTable {
		return output.Render(a.Out, a.Format, nil, plansToJSON(changed))
	}

	fmt.Fprintln(a.Out)
	if err := ui.WriteSummary(a.Out, changed, ui.SummaryOptions{}); err != nil {
		return err
	}
	fmt.Fprintln(a.Out)

	if f.dryRun {
		fmt.Fprintf(a.Out, "%s nothing was written.\n", s.Dim.Render("dry run:"))
		return nil
	}

	if !f.yes {
		ok, err := a.confirm(fmt.Sprintf("Apply %d change(s) to %s?", len(changed), a.LockPath))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(a.Out, "aborted.")
			return nil
		}
	}

	if err := a.Engine.Apply(changed); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "wrote %s\n", a.LockPath)

	if f.commit || a.Cfg.AutoCommit {
		if err := a.commitLock(ctx, changed); err != nil {
			return err
		}
	}
	a.warnUntracked(ctx)

	action, err := a.resolveAction(f)
	if err != nil {
		return err
	}
	if action == config.ActionNone {
		fmt.Fprintf(a.Out, "%s\n", s.Dim.Render("skipping rebuild."))
		return nil
	}
	fmt.Fprintln(a.Out)
	return a.Engine.Rebuild(ctx, a.Cfg, action, a.In, a.Out, a.Err)
}

func (a *App) commitLock(ctx context.Context, plans []*update.Plan) error {
	if !a.Git.IsRepo(ctx) {
		fmt.Fprintf(a.Err, "%s %s is not a git repository, skipping commit\n",
			ui.S().Yellow.Render("note:"), a.FlakeDir)
		return nil
	}
	paths := []string{lock.Name}
	if a.Git.IsTracked(ctx, overlay.Name) || fileExists(overlay.Path(a.FlakeDir)) {
		paths = append(paths, overlay.Name)
	}
	if err := a.Git.Commit(ctx, update.CommitMessage(plans), paths...); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	fmt.Fprintf(a.Out, "committed %s\n", strings.Join(paths, ", "))
	return nil
}

// resolveAction decides what to do after the lock file was written, asking when
// neither flags nor config settle it.
func (a *App) resolveAction(f *updateFlags) (config.Action, error) {
	action := f.action(a.Cfg)
	if action != config.ActionAsk {
		return action, nil
	}
	if f.yes {
		// Non-interactive runs must not block on a question.
		return config.ActionNone, nil
	}
	out := stdoutFile(a.Out)
	if out == nil || !isTerminal(out) {
		return config.ActionNone, nil
	}
	fmt.Fprintf(a.Out, "Rebuild now? [s]witch / [b]oot / [n]othing: ")
	var answer string
	if _, err := fmt.Fscanln(a.In, &answer); err != nil {
		return config.ActionNone, nil
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "s", "switch":
		return config.ActionSwitch, nil
	case "b", "boot":
		return config.ActionBoot, nil
	default:
		return config.ActionNone, nil
	}
}

// planJSON is the machine-readable shape of a plan.
type planJSON struct {
	Name       string  `json:"name" yaml:"name"`
	Attr       string  `json:"attr" yaml:"attr"`
	OldVersion string  `json:"oldVersion" yaml:"oldVersion"`
	NewVersion string  `json:"newVersion" yaml:"newVersion"`
	Bump       string  `json:"bump" yaml:"bump"`
	OldRev     string  `json:"oldRev" yaml:"oldRev"`
	NewRev     string  `json:"newRev" yaml:"newRev"`
	Sha256     string  `json:"sha256" yaml:"sha256"`
	Branch     string  `json:"branch,omitempty" yaml:"branch,omitempty"`
	OldOutPath string  `json:"oldOutPath,omitempty" yaml:"oldOutPath,omitempty"`
	NewOutPath string  `json:"newOutPath" yaml:"newOutPath"`
	OldSize    int64   `json:"oldClosureSize" yaml:"oldClosureSize"`
	NewSize    int64   `json:"newClosureSize" yaml:"newClosureSize"`
	SizeDelta  int64   `json:"closureSizeDelta" yaml:"closureSizeDelta"`
	Changelog  string  `json:"changelog,omitempty" yaml:"changelog,omitempty"`
	Homepage   string  `json:"homepage,omitempty" yaml:"homepage,omitempty"`
	Changes    []entry `json:"closureChanges,omitempty" yaml:"closureChanges,omitempty"`
}

type entry struct {
	Name      string   `json:"name" yaml:"name"`
	Kind      string   `json:"kind" yaml:"kind"`
	Old       []string `json:"old,omitempty" yaml:"old,omitempty"`
	New       []string `json:"new,omitempty" yaml:"new,omitempty"`
	SizeDelta int64    `json:"sizeDelta,omitempty" yaml:"sizeDelta,omitempty"`
}

func plansToJSON(plans []*update.Plan) []planJSON {
	out := make([]planJSON, 0, len(plans))
	for _, p := range plans {
		j := planJSON{
			Name: p.Name, Attr: p.Attr,
			OldVersion: p.OldVersion, NewVersion: p.NewVersion, Bump: p.Bump.String(),
			OldRev: p.OldRev, NewRev: p.NewRev, Sha256: p.Sha256, Branch: p.Branch,
			OldOutPath: p.OldOutPath, NewOutPath: p.NewOutPath,
			OldSize: p.OldSize, NewSize: p.NewSize, SizeDelta: p.SizeDelta(),
			Changelog: p.Changelog, Homepage: p.Homepage,
		}
		for _, e := range p.Entries {
			j.Changes = append(j.Changes, entry{
				Name: e.Name, Kind: e.Kind.String(), Old: e.Old, New: e.New, SizeDelta: e.SizeDelta,
			})
		}
		out = append(out, j)
	}
	return out
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
