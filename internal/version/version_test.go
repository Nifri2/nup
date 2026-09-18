package version

import "testing"

// The expected values come from `builtins.compareVersions` in nix 2.34, so nup
// orders versions exactly like nixpkgs does.
func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0", "2.3", -1},
		{"2.3", "2.3", 0},
		{"2.1.1", "2.1", 1},
		{"2.3pre1", "2.3", -1},
		{"2.3", "2.3.0", -1},
		{"1.0", "1.0-1", -1},
		{"1.0-1", "1.0", 1},
		{"14.1.0", "14.1.1", -1},
		{"1.9", "1.10", -1},
		{"2.12.1", "2.12.3", -1},
		{"1.0rc1", "1.0", 1},
		{"1.0", "1.0rc1", -1},
		{"abc", "1", -1},
		{"1", "abc", 1},
		{"", "1", -1},
		{"1.0.0", "1.0", 1},
		{"3.38.0", "3.38.0", 0},
		{"0.9", "0.10", -1},
		{"1.2.3-beta", "1.2.3", 1},
		{"2024-01-01", "2024-02-01", -1},
		{"r100", "r99", 1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCompareIsAntisymmetric(t *testing.T) {
	versions := []string{"", "1", "1.0", "1.0.1", "1.1", "2.0", "2.0pre1", "2.0rc1", "abc"}
	for _, a := range versions {
		for _, b := range versions {
			if got, rev := Compare(a, b), Compare(b, a); got != -rev {
				t.Errorf("Compare(%q,%q)=%d but Compare(%q,%q)=%d", a, b, got, b, a, rev)
			}
		}
	}
}

func TestKind(t *testing.T) {
	cases := []struct {
		old, new string
		want     Bump
	}{
		{"1.0.0", "2.0.0", BumpMajor},
		{"1.0.0", "1.1.0", BumpMinor},
		{"1.0.0", "1.0.1", BumpPatch},
		{"14.1.0", "14.1.1", BumpPatch},
		{"3.38.0", "3.38.0", BumpSame},
		{"2.0.0", "1.9.0", BumpDowngrade},
		{"", "1.0.0", BumpOther},
		{"1.0.0", "", BumpOther},
		{"1.0", "1.0-2", BumpPatch},
		{"2.12.1", "2.12.3", BumpPatch},
		{"0.9", "0.10", BumpMinor},
		{"abc", "def", BumpOther},
	}
	for _, c := range cases {
		if got := Kind(c.old, c.new); got != c.want {
			t.Errorf("Kind(%q, %q) = %v, want %v", c.old, c.new, got, c.want)
		}
	}
}

func TestBumpString(t *testing.T) {
	for b, want := range map[Bump]string{
		BumpSame: "same", BumpPatch: "patch", BumpMinor: "minor",
		BumpMajor: "major", BumpDowngrade: "downgrade", BumpOther: "other",
	} {
		if got := b.String(); got != want {
			t.Errorf("Bump(%d).String() = %q, want %q", b, got, want)
		}
	}
}
