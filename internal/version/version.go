// Package version implements nixpkgs-compatible version comparison.
package version

import (
	"strconv"
	"strings"
)

// Compare compares two version strings the same way builtins.compareVersions
// does. It returns -1 if a < b, 0 if a == b and 1 if a > b.
func Compare(a, b string) int {
	pa, pb := 0, 0
	for pa < len(a) || pb < len(b) {
		ca, na := nextComponent(a, &pa)
		cb, nb := nextComponent(b, &pb)
		if d := compareComponent(ca, na, cb, nb); d != 0 {
			return d
		}
	}
	return 0
}

// nextComponent consumes the next component starting at *p: separators ('.' and
// '-') are skipped, then either a run of digits or a run of non-digits is taken.
func nextComponent(s string, p *int) (string, int) {
	for *p < len(s) && (s[*p] == '.' || s[*p] == '-') {
		*p++
	}
	start := *p
	if *p < len(s) && isDigit(s[*p]) {
		for *p < len(s) && isDigit(s[*p]) {
			*p++
		}
	} else {
		for *p < len(s) && !isDigit(s[*p]) && s[*p] != '.' && s[*p] != '-' {
			*p++
		}
	}
	c := s[start:*p]
	n := -1
	if c != "" && isDigit(c[0]) {
		if v, err := strconv.Atoi(c); err == nil {
			n = v
		}
	}
	return c, n
}

func compareComponent(c1 string, n1 int, c2 string, n2 int) int {
	switch {
	case n1 >= 0 && n2 >= 0:
		switch {
		case n1 < n2:
			return -1
		case n1 > n2:
			return 1
		default:
			return 0
		}
	case c1 == "" && n2 >= 0:
		return -1
	case n1 >= 0 && c2 == "":
		return 1
	case c1 == "pre" && c2 != "pre":
		return -1
	case c2 == "pre" && c1 != "pre":
		return 1
	case n1 >= 0:
		// A numeric component sorts above an alphabetic one.
		return 1
	case n2 >= 0:
		return -1
	case c1 < c2:
		return -1
	case c1 > c2:
		return 1
	}
	return 0
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// Bump describes how far apart two versions are.
type Bump int

// The kinds of step between two versions, from smallest to least classifiable.
const (
	// BumpSame means the versions are equal.
	BumpSame Bump = iota
	// BumpPatch is a change in the third or later component.
	BumpPatch
	// BumpMinor is a change in the second component.
	BumpMinor
	// BumpMajor is a change in the first component.
	BumpMajor
	// BumpDowngrade means the new version sorts below the old one.
	BumpDowngrade
	// BumpOther covers versions that do not compare numerically.
	BumpOther
)

func (b Bump) String() string {
	switch b {
	case BumpSame:
		return "same"
	case BumpPatch:
		return "patch"
	case BumpMinor:
		return "minor"
	case BumpMajor:
		return "major"
	case BumpDowngrade:
		return "downgrade"
	default:
		return "other"
	}
}

// Kind classifies the step from old to new.
func Kind(oldV, newV string) Bump {
	switch {
	case oldV == "" || newV == "":
		return BumpOther
	case oldV == newV:
		return BumpSame
	}
	switch Compare(oldV, newV) {
	case 0:
		return BumpSame
	case 1:
		return BumpDowngrade
	}

	no, ok1 := numericPrefix(oldV)
	nn, ok2 := numericPrefix(newV)
	if !ok1 || !ok2 {
		return BumpOther
	}
	for i := 0; i < max(len(no), len(nn)); i++ {
		a, b := 0, 0
		if i < len(no) {
			a = no[i]
		}
		if i < len(nn) {
			b = nn[i]
		}
		if a == b {
			continue
		}
		switch i {
		case 0:
			return BumpMajor
		case 1:
			return BumpMinor
		default:
			return BumpPatch
		}
	}
	// Numeric parts are identical, only a suffix changed.
	return BumpPatch
}

// numericPrefix returns the leading dot-separated numeric components.
func numericPrefix(v string) ([]int, bool) {
	var out []int
	for _, part := range strings.Split(v, ".") {
		// Stop at the first component that is not purely numeric, e.g. "1.2.3rc1".
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}
