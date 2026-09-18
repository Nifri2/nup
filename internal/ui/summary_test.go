package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/Nifri2/nup/internal/diff"
	"github.com/Nifri2/nup/internal/update"
	"github.com/Nifri2/nup/internal/version"
)

func plan() *update.Plan {
	return &update.Plan{
		Name: "ripgrep", Attr: "ripgrep",
		OldVersion: "14.1.0", NewVersion: "14.1.1", Bump: version.BumpPatch,
		OldRev: "aaaaaaaaaaaa", NewRev: "bbbbbbbbbbbb",
		RevDate:   time.Now().Add(-48 * time.Hour),
		OldSize:   1000000,
		NewSize:   1016384,
		Changelog: "https://example.invalid/CHANGELOG",
		Entries: []diff.Entry{
			{Name: "pcre2", Old: []string{"10.43"}, New: []string{"10.44"}, Kind: diff.KindUpgrade, SizeDelta: 12288, HasSize: true},
			{Name: "gone", Old: []string{"1.0"}, Kind: diff.KindRemoved},
		},
	}
}

func TestRenderSummaryPlain(t *testing.T) {
	SetColor(false)
	out := RenderSummary([]*update.Plan{plan()}, SummaryOptions{})
	for _, want := range []string{
		"ripgrep", "14.1.0", "14.1.1", "patch",
		"aaaaaaa", "bbbbbbb", "2 days ago",
		"[U] pcre2", "[R] gone",
		"https://example.invalid/CHANGELOG",
		"closure", "+16.0 KiB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary is missing %q:\n%s", want, out)
		}
	}
}

// A major bump has to stand out, so it is rendered differently from a patch.
func TestMajorBumpIsHighlighted(t *testing.T) {
	SetColor(false)
	p := plan()
	p.OldVersion, p.NewVersion, p.Bump = "1.0.0", "2.0.0", version.BumpMajor
	out := RenderSummary([]*update.Plan{p}, SummaryOptions{})
	if !strings.Contains(out, "MAJOR") {
		t.Errorf("a major bump should be flagged:\n%s", out)
	}
}

func TestLongClosureIsTruncated(t *testing.T) {
	SetColor(false)
	p := plan()
	for i := 0; i < 50; i++ {
		p.Entries = append(p.Entries, diff.Entry{Name: "dep", Old: []string{"1"}, New: []string{"2"}, Kind: diff.KindUpgrade})
	}
	out := RenderSummary([]*update.Plan{p}, SummaryOptions{MaxEntries: 5})
	if !strings.Contains(out, "and 47 more") {
		t.Errorf("expected a truncation notice:\n%s", out)
	}
}

func TestMultiplePlansGetTotals(t *testing.T) {
	SetColor(false)
	a, b := plan(), plan()
	b.Name = "jq"
	out := RenderSummary([]*update.Plan{a, b}, SummaryOptions{})
	if !strings.Contains(out, "2 package(s) to update") {
		t.Errorf("expected a totals line:\n%s", out)
	}
}

func TestColorCanBeDisabled(t *testing.T) {
	SetColor(false)
	if strings.Contains(RenderSummary([]*update.Plan{plan()}, SummaryOptions{}), "\x1b[") {
		t.Error("no escape sequences should be emitted with color off")
	}
}

func TestHumanAge(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second:     "just now",
		5 * time.Minute:      "5 minutes ago",
		1 * time.Hour:        "1 hour ago",
		50 * time.Hour:       "2 days ago",
		60 * 24 * time.Hour:  "2 months ago",
		400 * 24 * time.Hour: "1 year ago",
	}
	for d, want := range cases {
		if got := HumanAge(d); got != want {
			t.Errorf("HumanAge(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestShouldColorRespectsNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if ShouldColor(false, nil) {
		t.Error("NO_COLOR must disable color")
	}
}

// A wrapper such as vscode-with-extensions is a different derivation from the
// bare attribute, so nup must say so instead of showing a closure diff in which
// every extension looks removed.
func TestWrappedPackageExplainsTheMissingDiff(t *testing.T) {
	SetColor(false)
	p := plan()
	p.Name = "vscode"
	p.InstalledName = "vscode-with-extensions"
	p.CandidateName = "vscode"
	p.Entries = nil
	p.OldSize, p.NewSize = 0, 0

	out := RenderSummary([]*update.Plan{p}, SummaryOptions{})
	if !strings.Contains(out, "vscode-with-extensions") || !strings.Contains(out, "No closure diff") {
		t.Errorf("expected an explanation:\n%s", out)
	}
	if strings.Contains(out, "closure  ") {
		t.Errorf("no closure line should be shown:\n%s", out)
	}
}
