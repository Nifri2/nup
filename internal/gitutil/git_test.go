package gitutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nifri2/nup/internal/nix"
)

func repo(t *testing.T) (*Git, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.invalid")
	run("config", "user.name", "test")
	return New(nix.NewExecRunner(30*time.Second), dir), dir
}

func TestIsRepo(t *testing.T) {
	g, _ := repo(t)
	if !g.IsRepo(context.Background()) {
		t.Error("should detect a git repository")
	}
	outside := New(nix.NewExecRunner(30*time.Second), t.TempDir())
	if outside.IsRepo(context.Background()) {
		t.Error("a plain directory is not a repository")
	}
}

// A git flake only sees tracked files, so this check is what keeps nup from
// silently writing files nix will ignore.
func TestIsTrackedAndAdd(t *testing.T) {
	g, dir := repo(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "nup.lock.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if g.IsTracked(ctx, "nup.lock.json") {
		t.Fatal("a new file must not be reported as tracked")
	}
	if err := g.Add(ctx, "nup.lock.json"); err != nil {
		t.Fatal(err)
	}
	if !g.IsTracked(ctx, "nup.lock.json") {
		t.Error("the file should be tracked after git add")
	}
}

func TestCommit(t *testing.T) {
	g, dir := repo(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "nup.lock.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.Commit(ctx, "nup: test", "nup.lock.json"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	out, err := exec.Command("git", "-C", dir, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Error("expected a commit")
	}
}
