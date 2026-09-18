package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Nifri2/nup/internal/diff"
	"github.com/Nifri2/nup/internal/lock"
	"github.com/Nifri2/nup/internal/update"
	"github.com/Nifri2/nup/internal/version"
	"github.com/charmbracelet/lipgloss"
)

// MaxClosureEntries caps how many dependency changes are listed before the rest
// is summarised; a stdenv bump can change thousands of paths.
const MaxClosureEntries = 25

// SummaryOptions tunes the rendering.
type SummaryOptions struct {
	// MaxEntries overrides MaxClosureEntries; 0 uses the default, -1 shows all.
	MaxEntries int
}

// RenderSummary renders the nvd-style summary for a set of plans.
func RenderSummary(plans []*update.Plan, opts SummaryOptions) string {
	var b strings.Builder
	limit := opts.MaxEntries
	if limit == 0 {
		limit = MaxClosureEntries
	}
	for i, p := range plans {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(renderPlan(p, limit))
	}
	if len(plans) > 1 {
		b.WriteByte('\n')
		b.WriteString(renderTotals(plans))
	}
	return b.String()
}

// WriteSummary writes the summary to w.
func WriteSummary(w io.Writer, plans []*update.Plan, opts SummaryOptions) error {
	_, err := io.WriteString(w, RenderSummary(plans, opts))
	return err
}

func renderPlan(p *update.Plan, maxEntries int) string {
	s := S()
	var b strings.Builder

	arrow := s.Dim.Render("→")
	header := s.Bold.Render(p.Name)
	switch {
	case p.Unchanged:
		header += "  " + s.Dim.Render(p.NewVersion) + "  " + s.Dim.Render("(already up to date)")
	case p.OldVersion == "":
		header += "  " + s.Green.Render(p.NewVersion) + "  " + bumpBadge(version.BumpOther)
	default:
		header += "  " + s.Red.Render(p.OldVersion) + " " + arrow + " " +
			s.Green.Render(p.NewVersion) + "  " + bumpBadge(p.Bump)
	}
	b.WriteString(header + "\n")

	b.WriteString(field("nixpkgs", fmt.Sprintf("%s %s %s%s",
		s.Dim.Render(lock.ShortRev(p.OldRev)), arrow, s.Bold.Render(lock.ShortRev(p.NewRev)),
		revAge(p.RevDate))))

	if p.Attr != p.Name {
		b.WriteString(field("attribute", p.Attr))
	}
	if link := p.Changelog; link != "" {
		b.WriteString(field("changelog", s.Blue.Render(link)))
	} else if p.Homepage != "" {
		b.WriteString(field("homepage", s.Blue.Render(p.Homepage)))
	}

	if len(p.Entries) > 0 {
		b.WriteByte('\n')
		b.WriteString(renderEntries(p.Entries, maxEntries))
	}
	if p.OldSize > 0 || p.NewSize > 0 {
		b.WriteByte('\n')
		b.WriteString(field("closure", fmt.Sprintf("%s %s %s  %s",
			diff.FormatSize(p.OldSize), arrow, diff.FormatSize(p.NewSize),
			deltaStyle(p.SizeDelta()).Render(diff.FormatDelta(p.SizeDelta())))))
	}
	return b.String()
}

func renderEntries(entries []diff.Entry, maxEntries int) string {
	s := S()
	shown := entries
	hidden := 0
	if maxEntries >= 0 && len(entries) > maxEntries {
		shown = entries[:maxEntries]
		hidden = len(entries) - maxEntries
	}

	nameWidth := 0
	for _, e := range shown {
		if n := lipgloss.Width(e.Name); n > nameWidth {
			nameWidth = n
		}
	}

	var b strings.Builder
	for _, e := range shown {
		st := kindStyle(e.Kind)
		pad := strings.Repeat(" ", nameWidth-lipgloss.Width(e.Name))
		line := fmt.Sprintf("  %s %s%s  %s",
			st.Render("["+e.Kind.Marker()+"]"), e.Name, pad, versions(e))
		if e.HasSize && e.SizeDelta != 0 {
			line += "  " + deltaStyle(e.SizeDelta).Render(diff.FormatDelta(e.SizeDelta))
		}
		b.WriteString(line + "\n")
	}
	if hidden > 0 {
		b.WriteString(s.Dim.Render(fmt.Sprintf("  … and %d more", hidden)) + "\n")
	}
	return b.String()
}

func versions(e diff.Entry) string {
	s := S()
	switch e.Kind {
	case diff.KindAdded:
		return s.Green.Render(strings.Join(e.New, ", "))
	case diff.KindRemoved:
		return s.Red.Render(strings.Join(e.Old, ", "))
	case diff.KindResized:
		return s.Dim.Render(strings.Join(e.Old, ", "))
	default:
		return s.Red.Render(strings.Join(e.Old, ", ")) + " " + s.Dim.Render("→") + " " +
			s.Green.Render(strings.Join(e.New, ", "))
	}
}

func renderTotals(plans []*update.Plan) string {
	var oldSize, newSize int64
	changed := 0
	for _, p := range plans {
		if p.Unchanged {
			continue
		}
		changed++
		oldSize += p.OldSize
		newSize += p.NewSize
	}
	s := S()
	delta := newSize - oldSize
	line := fmt.Sprintf("%s %s", s.Bold.Render(fmt.Sprintf("%d package(s) to update", changed)),
		s.Dim.Render("·"))
	if oldSize > 0 || newSize > 0 {
		line += fmt.Sprintf(" total closure %s %s %s  %s",
			diff.FormatSize(oldSize), s.Dim.Render("→"), diff.FormatSize(newSize),
			deltaStyle(delta).Render(diff.FormatDelta(delta)))
	}
	return line + "\n"
}

// field renders an indented "label  value" line with a stable label column.
func field(label, value string) string {
	s := S()
	return fmt.Sprintf("  %s  %s\n", s.Dim.Render(fmt.Sprintf("%-9s", label)), value)
}

func bumpBadge(b version.Bump) string {
	s := S()
	switch b {
	case version.BumpMajor:
		return s.Warning.Render("MAJOR")
	case version.BumpMinor:
		return s.Dim.Render("minor")
	case version.BumpPatch:
		return s.Dim.Render("patch")
	case version.BumpDowngrade:
		return s.Warning.Render("DOWNGRADE")
	case version.BumpSame:
		return s.Dim.Render("same version")
	default:
		return s.Dim.Render("")
	}
}

func kindStyle(k diff.Kind) lipgloss.Style {
	s := S()
	switch k {
	case diff.KindUpgrade:
		return s.Green
	case diff.KindDowngrade:
		return s.Yellow
	case diff.KindAdded:
		return s.Blue
	case diff.KindRemoved:
		return s.Red
	default:
		return s.Dim
	}
}

// deltaStyle colours a size change: growth is red, shrinkage green.
func deltaStyle(delta int64) lipgloss.Style {
	s := S()
	switch {
	case delta > 0:
		return s.Red
	case delta < 0:
		return s.Green
	default:
		return s.Dim
	}
}

// revAge renders how old a revision is, e.g. " (2 days ago)".
func revAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return "  " + S().Dim.Render("("+HumanAge(time.Since(t))+")")
}

// HumanAge renders a duration as a coarse, readable age.
func HumanAge(d time.Duration) string {
	switch {
	case d < 0:
		return "in the future"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24), "day") + " ago"
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/24/30), "month") + " ago"
	default:
		return plural(int(d.Hours()/24/365), "year") + " ago"
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
