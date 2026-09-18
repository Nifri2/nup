package pkgset

import (
	"context"
	"testing"
	"time"

	"github.com/Nifri2/nup/internal/lock"
	"github.com/Nifri2/nup/internal/nix"
)

func TestSplitName(t *testing.T) {
	cases := []struct{ in, pname, version string }{
		{"ripgrep-14.1.1", "ripgrep", "14.1.1"},
		{"go-task-3.38.0", "go-task", "3.38.0"},
		{"python3.12-requests-2.32.3", "python3.12-requests", "2.32.3"},
		{"hello", "hello", ""},
		{"nixos-system-nixos-26.11", "nixos-system-nixos", "26.11"},
		{"foo-bar", "foo-bar", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		p, v := SplitName(c.in)
		if p != c.pname || v != c.version {
			t.Errorf("SplitName(%q) = (%q, %q), want (%q, %q)", c.in, p, v, c.pname, c.version)
		}
	}
}

func TestAnnotateMarksPinsAndStaleness(t *testing.T) {
	lf := lock.New()
	lf.Set(lock.Package{Attr: "ripgrep", Rev: "aaaaaaaaaaaa", PinnedAt: time.Now()})
	lf.Set(lock.Package{Attr: "python3Packages.requests", Rev: "bbbbbbbbbbbb", PinnedAt: time.Now()})

	pkgs := []Package{
		{Name: "ripgrep", Attr: "ripgrep"},
		{Name: "requests", Attr: "requests"},
		{Name: "jq", Attr: "jq"},
	}
	got := annotate(pkgs, lf, "bbbbbbbbbbbb")

	if got[0].PinnedRev != "aaaaaaaaaaaa" || !got[0].Stale {
		t.Errorf("ripgrep: %+v — a pin on another rev than the flake input is stale", got[0])
	}
	// A pin recorded under a nested attribute path must still be found by the
	// package's plain name.
	if got[1].PinnedRev != "bbbbbbbbbbbb" || got[1].Attr != "python3Packages.requests" {
		t.Errorf("requests: %+v", got[1])
	}
	if got[1].Stale {
		t.Error("a pin on the same rev as the flake input is not stale")
	}
	if got[2].Pinned() {
		t.Errorf("jq should not be pinned: %+v", got[2])
	}
}

func TestDedupePrefersSystemSource(t *testing.T) {
	in := []Package{
		{Name: "jq", Version: "1.8", OutPath: "/nix/store/x", Source: SourceHome},
		{Name: "jq", Version: "1.8", OutPath: "/nix/store/x", Source: SourceSystem},
		{Name: "rg", Version: "14", OutPath: "/nix/store/y", Source: SourceSystem},
	}
	got := dedupe(in)
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Source != SourceSystem {
		t.Errorf("a package present in both should be reported as system, got %q", got[0].Source)
	}
}

// Multi-output packages appear once per output in systemPackages, but should
// only produce a single row.
func TestDedupeCollapsesMultipleOutputs(t *testing.T) {
	in := []Package{
		{Name: "glibc", Version: "2.42", OutPath: "/nix/store/a-glibc-2.42", Source: SourceSystem},
		{Name: "glibc", Version: "2.42", OutPath: "/nix/store/b-glibc-2.42-bin", Source: SourceSystem},
		{Name: "glibc", Version: "2.42", OutPath: "/nix/store/c-glibc-2.42-dev", Source: SourceSystem},
	}
	if got := dedupe(in); len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}
}

func TestFind(t *testing.T) {
	pkgs := []Package{
		{Name: "requests", Attr: "python3Packages.requests"},
		{Name: "jq", Attr: "jq"},
	}
	if got := Find(pkgs, "python3Packages.requests"); len(got) != 1 {
		t.Error("should be findable by attribute path")
	}
	if got := Find(pkgs, "jq"); len(got) != 1 {
		t.Error("should be findable by name")
	}
	if got := Find(pkgs, "nope"); len(got) != 0 {
		t.Error("unknown package should not match")
	}
}

func TestListPassesImpureThrough(t *testing.T) {
	fake := nix.NewFake(map[string]string{"eval": "[]"})
	l := &Lister{Nix: nix.NewClient(fake), FlakeDir: t.TempDir(), Host: "nixos", Impure: true}
	if _, err := l.List(context.Background(), lock.New(), "", Options{Refresh: true}); err != nil {
		t.Fatal(err)
	}
	if !fake.Called("--impure") {
		t.Errorf("the configuration should have been evaluated impurely: %v", fake.Calls())
	}
}
