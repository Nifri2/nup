// Package cmd wires the command line up to the internal packages. It contains
// no update logic of its own; that lives in internal/update.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Nifri2/nup/internal/config"
	"github.com/Nifri2/nup/internal/gitutil"
	"github.com/Nifri2/nup/internal/lock"
	"github.com/Nifri2/nup/internal/nix"
	"github.com/Nifri2/nup/internal/output"
	"github.com/Nifri2/nup/internal/overlay"
	"github.com/Nifri2/nup/internal/pkgset"
	"github.com/Nifri2/nup/internal/ui"
	"github.com/Nifri2/nup/internal/update"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// FallbackFlake is used when neither a flag, the config nor the working
// directory point at a flake.
const FallbackFlake = "/etc/nixos"

// App holds everything the commands share.
type App struct {
	Cfg      *config.Config
	FlakeDir string
	Host     string
	Format   output.Format
	Refresh  bool

	Runner nix.Runner
	Nix    *nix.Client
	Cache  *nix.Cache
	Git    *gitutil.Git

	Lock     *lock.File
	LockPath string
	MainRev  string

	Lister *pkgset.Lister
	Engine *update.Engine

	In  io.Reader
	Out io.Writer
	Err io.Writer

	// raw flag values
	flagFlake   string
	flagHost    string
	flagOutput  string
	flagNoColor bool
}

// ExecuteContext runs the root command and returns the process exit code.
func ExecuteContext(ctx context.Context) int {
	app := &App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
	root := NewRootCommand(app)
	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "interrupted")
			return 130
		}
		// cobra already prints usage errors; everything else is ours to report.
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		return 1
	}
	return 0
}

// NewRootCommand builds the command tree.
func NewRootCommand(app *App) *cobra.Command {
	root := &cobra.Command{
		Use:   "nup",
		Short: "Update single packages in a NixOS flake",
		Long: "nup pins individual packages to their own nixpkgs revision, so a single\n" +
			"package can be updated without moving the whole nixpkgs input.\n\n" +
			"Running nup without arguments starts the interactive TUI.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return app.setup(cmd)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI(cmd.Context(), app)
		},
	}

	f := root.PersistentFlags()
	f.StringVarP(&app.flagOutput, "output", "o", "", "output format: table, json or yaml")
	f.StringVar(&app.flagFlake, "flake", "", "path to the system flake (default: config, current directory or "+FallbackFlake+")")
	f.StringVar(&app.flagHost, "host", "", "nixosConfigurations attribute to use (default: hostname)")
	f.BoolVar(&app.Refresh, "refresh", false, "ignore cached evaluation results")
	f.BoolVar(&app.flagNoColor, "no-color", false, "disable colored output (NO_COLOR is also honored)")

	root.AddCommand(
		newInitCommand(app),
		newListCommand(app),
		newQueryCommand(app),
		newUpdateCommand(app),
		newOutdatedCommand(app),
		newPinCommand(app),
		newResetCommand(app),
		newCompletionCommand(),
	)
	return root
}

// setup resolves configuration and builds the shared objects. It runs before
// every command.
func (a *App) setup(cmd *cobra.Command) error {
	if a.Out == nil {
		a.Out = cmd.OutOrStdout()
	}
	if a.Err == nil {
		a.Err = cmd.ErrOrStderr()
	}

	ui.SetColor(ui.ShouldColor(a.flagNoColor, stdoutFile(a.Out)))

	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	a.Cfg = cfg

	format := cfg.Output
	if a.flagOutput != "" {
		format = a.flagOutput
	}
	a.Format, err = output.ParseFormat(format)
	if err != nil {
		return err
	}

	a.FlakeDir, err = resolveFlakeDir(a.flagFlake, cfg.Flake)
	if err != nil {
		return err
	}
	a.Host = resolveHost(a.flagHost, cfg.Host)

	if a.Runner == nil {
		a.Runner = nix.NewExecRunner(update.BuildTimeout)
	}
	a.Nix = nix.NewClient(a.Runner)
	a.Cache = nix.NewCache("", a.Refresh)
	a.Git = gitutil.New(a.Runner, a.FlakeDir)

	a.LockPath = lock.Path(a.FlakeDir)
	a.Lock, err = lock.Load(a.LockPath)
	if err != nil {
		return err
	}

	if in, err := nix.ReadFlakeLockInput(a.FlakeDir, "nixpkgs"); err == nil && in != nil {
		a.MainRev = in.Rev
	}

	hmUser := ""
	if cfg.HomeManager.Enable {
		hmUser = cfg.HomeManager.User
	}
	a.Lister = &pkgset.Lister{
		Nix:             a.Nix,
		Cache:           a.Cache,
		FlakeDir:        a.FlakeDir,
		Host:            a.Host,
		HomeManagerUser: hmUser,
	}
	a.Engine = &update.Engine{
		Nix:      a.Nix,
		Cache:    a.Cache,
		Git:      a.Git,
		FlakeDir: a.FlakeDir,
		Host:     a.Host,
		LockPath: a.LockPath,
		Lock:     a.Lock,
		Branch:   cfg.Branch,
		MainRev:  a.MainRev,
	}
	return nil
}

func stdoutFile(w io.Writer) *os.File {
	if f, ok := w.(*os.File); ok {
		return f
	}
	return nil
}

// resolveFlakeDir picks the flake directory: flag, then config, then the
// current directory if it holds a flake.nix, then /etc/nixos.
func resolveFlakeDir(flag, configured string) (string, error) {
	for _, candidate := range []string{flag, configured} {
		if candidate == "" {
			continue
		}
		abs, err := filepath.Abs(expandHome(candidate))
		if err != nil {
			return "", fmt.Errorf("resolving flake path %q: %w", candidate, err)
		}
		if _, err := os.Stat(filepath.Join(abs, "flake.nix")); err != nil {
			return "", fmt.Errorf("no flake.nix in %s", abs)
		}
		return abs, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(cwd, "flake.nix")); err == nil {
			return cwd, nil
		}
	}
	if _, err := os.Stat(filepath.Join(FallbackFlake, "flake.nix")); err != nil {
		return "", fmt.Errorf("could not find a flake: no --flake given, no flake path in %s, "+
			"no flake.nix in the current directory and none in %s", config.Path(), FallbackFlake)
	}
	return FallbackFlake, nil
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}

func resolveHost(flag, configured string) string {
	if flag != "" {
		return flag
	}
	if configured != "" {
		return configured
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "nixos"
}

// packages lists the installed packages with lock state merged in.
func (a *App) packages(ctx context.Context) ([]pkgset.Package, error) {
	return a.Lister.List(ctx, a.Lock, a.MainRev, pkgset.Options{Refresh: a.Refresh})
}

// warnUntracked tells the user when nup's files are invisible to a git flake,
// which would otherwise fail in a very confusing way.
func (a *App) warnUntracked(ctx context.Context) {
	if !a.Git.IsRepo(ctx) {
		return
	}
	var missing []string
	for _, f := range []string{lock.Name, overlay.Name} {
		full := filepath.Join(a.FlakeDir, f)
		if _, err := os.Stat(full); err != nil {
			continue
		}
		if !a.Git.IsTracked(ctx, f) {
			missing = append(missing, f)
		}
	}
	if len(missing) == 0 {
		return
	}
	s := ui.S()
	fmt.Fprintf(a.Err, "%s %s is not tracked by git. A git flake only sees tracked files, so nix will ignore it.\n",
		s.Warning.Render("warning:"), strings.Join(missing, " and "))
	fmt.Fprintf(a.Err, "  run: git -C %s add %s\n", a.FlakeDir, strings.Join(missing, " "))
}

// confirm asks a yes/no question, defaulting to no.
func (a *App) confirm(prompt string) (bool, error) {
	f := stdoutFile(a.Out)
	if f == nil || !isTerminal(f) {
		return false, errors.New("cannot ask for confirmation without a terminal; pass --yes to proceed non-interactively")
	}
	fmt.Fprintf(a.Out, "%s [y/N] ", prompt)
	var answer string
	if _, err := fmt.Fscanln(a.In, &answer); err != nil {
		// An empty line means "no", which Fscanln reports as an error.
		return false, nil
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

// commandContext bounds a command and cancels it on interrupt.
func commandContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithCancel(ctx)
}
