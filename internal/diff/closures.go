// Package diff parses the output of `nix store diff-closures` and formats
// closure sizes.
package diff

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Nifri2/nup/internal/version"
)

// Kind is the type of change a closure entry describes.
type Kind int

const (
	// KindUpgrade means at least one version went up.
	KindUpgrade Kind = iota
	// KindDowngrade means at least one version went down.
	KindDowngrade
	// KindAdded means the package is new in the closure.
	KindAdded
	// KindRemoved means the package left the closure.
	KindRemoved
	// KindResized means only the size changed, not the version.
	KindResized
)

// Marker is the single letter nup shows in front of an entry.
func (k Kind) Marker() string {
	switch k {
	case KindUpgrade:
		return "U"
	case KindDowngrade:
		return "D"
	case KindAdded:
		return "A"
	case KindRemoved:
		return "R"
	default:
		return "S"
	}
}

// String is the stable machine-readable name of a change kind.
func (k Kind) String() string {
	switch k {
	case KindUpgrade:
		return "upgrade"
	case KindDowngrade:
		return "downgrade"
	case KindAdded:
		return "added"
	case KindRemoved:
		return "removed"
	default:
		return "resized"
	}
}

// Entry is one line of `nix store diff-closures`.
type Entry struct {
	Name string
	Old  []string
	New  []string
	// SizeDelta is in bytes and only meaningful when HasSize is true.
	SizeDelta int64
	HasSize   bool
	Kind      Kind
}

// Versions renders the version transition, e.g. "1.2 -> 1.3".
func (e Entry) Versions() string {
	switch e.Kind {
	case KindAdded:
		return strings.Join(e.New, ", ")
	case KindRemoved:
		return strings.Join(e.Old, ", ")
	case KindResized:
		return strings.Join(e.Old, ", ")
	default:
		return strings.Join(e.Old, ", ") + " -> " + strings.Join(e.New, ", ")
	}
}

var (
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	sizeRe = regexp.MustCompile(`^([+-]?[0-9]+(?:\.[0-9]+)?) (B|KiB|MiB|GiB|TiB)$`)
)

// StripANSI removes SGR escape sequences. nix colours its diff output even when
// stdout is not a terminal in some versions, so we always strip.
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// Parse turns `nix store diff-closures` output into entries. Unparseable lines
// are skipped rather than failing the whole update.
func Parse(out string) []Entry {
	var entries []Entry
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(StripANSI(raw), " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		e, ok := parseLine(line)
		if !ok {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}

func parseLine(line string) (Entry, bool) {
	idx := strings.Index(line, ": ")
	if idx <= 0 {
		return Entry{}, false
	}
	e := Entry{Name: strings.TrimSpace(line[:idx])}
	rest := strings.TrimSpace(line[idx+2:])
	if rest == "" {
		return Entry{}, false
	}

	left, right, hasArrow := splitArrow(rest)
	if !hasArrow {
		// Only a size delta, e.g. "X11: -9.8 KiB".
		delta, ok := parseSize(rest)
		if !ok {
			return Entry{}, false
		}
		e.SizeDelta, e.HasSize, e.Kind = delta, true, KindResized
		return e, true
	}

	// A trailing size delta lives in the last comma-separated field of the
	// right-hand side: "5.2.0 -> 5.2.1, 5.2.1_fish, 16.4 MiB".
	if i := strings.LastIndex(right, ", "); i >= 0 {
		if delta, ok := parseSize(strings.TrimSpace(right[i+2:])); ok {
			e.SizeDelta, e.HasSize = delta, true
			right = right[:i]
		}
	} else if delta, ok := parseSize(right); ok {
		// "foo: 1.0 -> , 12 KiB" never happens, but a bare size would mean the
		// right-hand side carries no versions at all.
		e.SizeDelta, e.HasSize = delta, true
		right = ""
	}

	e.Old = parseVersions(left)
	e.New = parseVersions(right)

	switch {
	case len(e.Old) == 0 && len(e.New) == 0:
		return Entry{}, false
	case len(e.Old) == 0:
		e.Kind = KindAdded
	case len(e.New) == 0:
		e.Kind = KindRemoved
	default:
		switch version.Compare(maxVersion(e.Old), maxVersion(e.New)) {
		case -1:
			e.Kind = KindUpgrade
		case 1:
			e.Kind = KindDowngrade
		default:
			e.Kind = KindResized
		}
	}
	return e, true
}

// splitArrow splits on the unicode arrow nix uses, tolerating an ASCII "->".
func splitArrow(s string) (left, right string, ok bool) {
	for _, sep := range []string{" → ", " -> "} {
		if i := strings.Index(s, sep); i >= 0 {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(sep):]), true
		}
	}
	return "", "", false
}

// parseVersions splits "1.0, 1.0-fish" and maps the empty-set glyph to nothing.
func parseVersions(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "∅" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == "∅" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func maxVersion(vs []string) string {
	best := ""
	for _, v := range vs {
		if best == "" || version.Compare(best, v) < 0 {
			best = v
		}
	}
	return best
}

var unitFactor = map[string]int64{
	"B":   1,
	"KiB": 1 << 10,
	"MiB": 1 << 20,
	"GiB": 1 << 30,
	"TiB": 1 << 40,
}

// parseSize turns "-50.7 MiB" into bytes.
func parseSize(s string) (int64, bool) {
	m := sizeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return int64(f * float64(unitFactor[m[2]])), true
}

// FormatSize renders a byte count the way nix does.
func FormatSize(b int64) string {
	neg := b < 0
	v := float64(b)
	if neg {
		v = -v
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	s := fmt.Sprintf("%.1f %s", v, units[i])
	if i == 0 {
		s = fmt.Sprintf("%.0f %s", v, units[i])
	}
	if neg {
		s = "-" + s
	}
	return s
}

// FormatDelta always carries an explicit sign, so growth and shrinkage read
// unambiguously in the summary.
func FormatDelta(b int64) string {
	if b > 0 {
		return "+" + FormatSize(b)
	}
	if b == 0 {
		return "±0 B"
	}
	return FormatSize(b)
}
