package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Nifri2/nup/internal/output"
	"github.com/Nifri2/nup/internal/pkgset"
	"github.com/Nifri2/nup/internal/ui"
)

func pkgs() []pkgset.Package {
	return []pkgset.Package{
		{Name: "go-task", Attr: "go-task", Version: "3.38.0", Source: pkgset.SourceSystem},
		{Name: "ripgrep", Attr: "ripgrep", Version: "14.1.1", PinnedRev: "a1b2c3d4e5", Source: pkgset.SourceSystem},
		{Name: "requests", Attr: "python3Packages.requests", Version: "2.32.3", Source: pkgset.SourceHome},
	}
}

func TestFilterSubstringIsCaseInsensitive(t *testing.T) {
	got, err := Filter(pkgs(), "RIPG", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "ripgrep" {
		t.Fatalf("got %+v", got)
	}
}

func TestFilterMatchesAttributePath(t *testing.T) {
	got, err := Filter(pkgs(), "python3packages", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "requests" {
		t.Fatalf("got %+v", got)
	}
}

func TestFilterRegex(t *testing.T) {
	got, err := Filter(pkgs(), "^r", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d matches, want ripgrep and requests: %+v", len(got), got)
	}

	if _, err := Filter(pkgs(), "[unclosed", true); err == nil {
		t.Error("an invalid regex should be reported")
	}
}

func TestFilterNoMatch(t *testing.T) {
	got, err := Filter(pkgs(), "definitely-not-here", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestPackageTableLayout(t *testing.T) {
	ui.SetColor(false)
	var b strings.Builder
	if err := output.Render(&b, output.FormatTable, PackageTable(pkgs()), nil); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if lines[0] != "NAME       VERSION   PINNED    SOURCE" {
		t.Errorf("header = %q", lines[0])
	}
	// A pinned package shows the short revision, an unpinned one a dash.
	if !strings.Contains(lines[1], "-") || !strings.Contains(lines[2], "a1b2c3d") {
		t.Errorf("rows:\n%s", b.String())
	}
	if !strings.Contains(lines[3], "home") {
		t.Errorf("the home-manager source should be shown: %q", lines[3])
	}
}

// The LATEST column only appears once a version is actually known.
func TestPackageTableLatestColumn(t *testing.T) {
	ui.SetColor(false)
	p := pkgs()
	if strings.Contains(headerOf(t, p), "LATEST") {
		t.Error("LATEST should be hidden while unknown")
	}
	p[0].Latest = "3.39.0"
	if !strings.Contains(headerOf(t, p), "LATEST") {
		t.Error("LATEST should appear once known")
	}
}

func headerOf(t *testing.T, p []pkgset.Package) string {
	t.Helper()
	var b strings.Builder
	if err := output.Render(&b, output.FormatTable, PackageTable(p), nil); err != nil {
		t.Fatal(err)
	}
	return strings.SplitN(b.String(), "\n", 2)[0]
}

func TestResolveFlakeDirPrefersFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "flake.nix"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveFlakeDir(dir, "/somewhere/else")
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestResolveFlakeDirRejectsDirWithoutFlake(t *testing.T) {
	if _, err := resolveFlakeDir(t.TempDir(), ""); err == nil {
		t.Fatal("expected an error when there is no flake.nix")
	} else if !strings.Contains(err.Error(), "flake.nix") {
		t.Errorf("the error should mention flake.nix: %v", err)
	}
}

func TestResolveFlakeDirFallsBackToCwd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "flake.nix"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	got, err := resolveFlakeDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	// macOS and some setups symlink temp dirs, so compare resolved paths.
	want, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != want {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestResolveHost(t *testing.T) {
	if got := resolveHost("flaghost", "confighost"); got != "flaghost" {
		t.Errorf("the flag should win, got %q", got)
	}
	if got := resolveHost("", "confighost"); got != "confighost" {
		t.Errorf("the config should be used, got %q", got)
	}
	if resolveHost("", "") == "" {
		t.Error("there should always be a fallback host")
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := expandHome("~/nixos"); got != filepath.Join(home, "nixos") {
		t.Errorf("got %q", got)
	}
	if got := expandHome("/etc/nixos"); got != "/etc/nixos" {
		t.Errorf("an absolute path must be left alone, got %q", got)
	}
}

func TestRootCommandHasEveryDocumentedCommand(t *testing.T) {
	root := NewRootCommand(&App{})
	want := []string{"init", "list", "query", "update", "outdated", "pin", "reset", "completion"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("command %q is missing", w)
		}
	}
}

func TestGlobalFlagsExist(t *testing.T) {
	root := NewRootCommand(&App{})
	for _, name := range []string{"output", "flake", "host", "refresh", "no-color"} {
		if root.PersistentFlags().Lookup(name) == nil {
			t.Errorf("global flag --%s is missing", name)
		}
	}
	if root.PersistentFlags().ShorthandLookup("o") == nil {
		t.Error("-o shorthand is missing")
	}
}

func TestUpdateFlagsExist(t *testing.T) {
	root := NewRootCommand(&App{})
	var upd *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "update" {
			upd = c
		}
	}
	if upd == nil {
		t.Fatal("update command missing")
	}
	for _, name := range []string{"yes", "dry-run", "switch", "boot", "no-rebuild", "branch", "attr", "force", "commit"} {
		if upd.Flags().Lookup(name) == nil {
			t.Errorf("update flag --%s is missing", name)
		}
	}
}

func TestRebuildFlagsAreMutuallyExclusive(t *testing.T) {
	root := NewRootCommand(&App{})
	root.SetArgs([]string{"update", "hello", "--switch", "--boot"})
	root.SetOut(&strings.Builder{})
	root.SetErr(&strings.Builder{})
	if err := root.Execute(); err == nil {
		t.Fatal("--switch and --boot must not be combinable")
	}
}
