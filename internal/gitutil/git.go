// Package gitutil covers the small amount of git nup needs: a git flake only
// sees files that git knows about, so nup must be able to tell whether its own
// files are tracked and offer to add them.
package gitutil

import (
	"context"
	"strings"

	"github.com/Nifri2/nup/internal/nix"
)

// Git runs git commands through a Runner so tests can fake them.
type Git struct {
	Runner nix.Runner
	Dir    string
}

// New returns a Git bound to a working directory.
func New(r nix.Runner, dir string) *Git { return &Git{Runner: r, Dir: dir} }

func (g *Git) run(ctx context.Context, args ...string) (*nix.Result, error) {
	return g.Runner.Run(ctx, "git", append([]string{"-C", g.Dir}, args...))
}

// IsRepo reports whether the directory is inside a git work tree.
func (g *Git) IsRepo(ctx context.Context) bool {
	res, err := g.run(ctx, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(res.Stdout) == "true"
}

// IsTracked reports whether git knows about a path. Untracked files are
// invisible to a git flake, which is the most common nup pitfall.
func (g *Git) IsTracked(ctx context.Context, path string) bool {
	_, err := g.run(ctx, "ls-files", "--error-unmatch", "--", path)
	return err == nil
}

// Add stages paths.
func (g *Git) Add(ctx context.Context, paths ...string) error {
	_, err := g.run(ctx, append([]string{"add", "--"}, paths...)...)
	return err
}

// HasStagedChanges reports whether anything is staged for the given paths.
func (g *Git) HasStagedChanges(ctx context.Context, paths ...string) bool {
	res, err := g.run(ctx, append([]string{"diff", "--cached", "--name-only", "--"}, paths...)...)
	return err == nil && strings.TrimSpace(res.Stdout) != ""
}

// Commit stages the given paths and commits them with the message.
func (g *Git) Commit(ctx context.Context, message string, paths ...string) error {
	if err := g.Add(ctx, paths...); err != nil {
		return err
	}
	args := append([]string{"commit", "-m", message, "--"}, paths...)
	_, err := g.run(ctx, args...)
	return err
}
