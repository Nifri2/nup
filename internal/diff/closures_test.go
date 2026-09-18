package diff

import (
	"reflect"
	"testing"
)

// realOutput is verbatim output of `nix store diff-closures` (nix 2.34),
// including the SGR colouring nix emits, the unicode arrow and the empty-set
// glyph for added and removed paths.
const realOutput = "X11: \x1b[32;1m-9.8 KiB\x1b[0m\n" +
	"ada: 3.4.4 → 4.0.0, \x1b[32;1m-152.2 KiB\x1b[0m\n" +
	"apr-util: 1.6.3 → 1.6.5\n" +
	"binaryen: 131 → 132, \x1b[31;1m468.3 KiB\x1b[0m\n" +
	"blender: 5.2.0, 5.2.0_fish → 5.2.1, 5.2.1_fish, \x1b[32;1m-126.9 KiB\x1b[0m\n" +
	"clang-wrapper: 21.1.8 → ∅, \x1b[32;1m-73.8 KiB\x1b[0m\n" +
	"hello: ∅ → 2.12.1, \x1b[31;1m100.0 KiB\x1b[0m\n" +
	"deno: 2.9.5 → 2.9.4, \x1b[32;1m-111.9 MiB\x1b[0m\n" +
	"\n"

func TestParse(t *testing.T) {
	got := Parse(realOutput)
	if len(got) != 8 {
		t.Fatalf("got %d entries, want 8: %+v", len(got), got)
	}

	want := []Entry{
		{Name: "X11", SizeDelta: -10035, HasSize: true, Kind: KindResized},
		{Name: "ada", Old: []string{"3.4.4"}, New: []string{"4.0.0"}, SizeDelta: -155852, HasSize: true, Kind: KindUpgrade},
		{Name: "apr-util", Old: []string{"1.6.3"}, New: []string{"1.6.5"}, Kind: KindUpgrade},
		{Name: "binaryen", Old: []string{"131"}, New: []string{"132"}, SizeDelta: 479539, HasSize: true, Kind: KindUpgrade},
		{
			Name: "blender",
			Old:  []string{"5.2.0", "5.2.0_fish"},
			New:  []string{"5.2.1", "5.2.1_fish"},
			// The trailing size must not be mistaken for another version.
			SizeDelta: -129945, HasSize: true, Kind: KindUpgrade,
		},
		{Name: "clang-wrapper", Old: []string{"21.1.8"}, SizeDelta: -75571, HasSize: true, Kind: KindRemoved},
		{Name: "hello", New: []string{"2.12.1"}, SizeDelta: 102400, HasSize: true, Kind: KindAdded},
		{Name: "deno", Old: []string{"2.9.5"}, New: []string{"2.9.4"}, SizeDelta: -117335654, HasSize: true, Kind: KindDowngrade},
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("entry %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestParseASCIIArrow(t *testing.T) {
	got := Parse("foo: 1.0 -> 2.0\n")
	if len(got) != 1 || got[0].Kind != KindUpgrade || got[0].New[0] != "2.0" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestParseSkipsGarbage(t *testing.T) {
	got := Parse("not a diff line\n\n   \nfoo: 1.0 → 2.0\n: 1.0 → 2.0\n")
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}
	if got[0].Name != "foo" {
		t.Errorf("got name %q", got[0].Name)
	}
}

func TestMarkers(t *testing.T) {
	for k, want := range map[Kind]string{
		KindUpgrade: "U", KindDowngrade: "D", KindAdded: "A", KindRemoved: "R", KindResized: "S",
	} {
		if got := k.Marker(); got != want {
			t.Errorf("Kind(%d).Marker() = %q, want %q", k, got, want)
		}
	}
}

func TestStripANSI(t *testing.T) {
	if got := StripANSI("\x1b[31;1mred\x1b[0m"); got != "red" {
		t.Errorf("got %q", got)
	}
}

func TestFormatSize(t *testing.T) {
	cases := map[int64]string{
		0: "0 B", 512: "512 B", 1024: "1.0 KiB", -1024: "-1.0 KiB",
		1536: "1.5 KiB", 1 << 20: "1.0 MiB", 1 << 30: "1.0 GiB",
	}
	for in, want := range cases {
		if got := FormatSize(in); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatDelta(t *testing.T) {
	if got := FormatDelta(1024); got != "+1.0 KiB" {
		t.Errorf("got %q", got)
	}
	if got := FormatDelta(-1024); got != "-1.0 KiB" {
		t.Errorf("got %q", got)
	}
	if got := FormatDelta(0); got != "±0 B" {
		t.Errorf("got %q", got)
	}
}

func TestVersions(t *testing.T) {
	e := Entry{Name: "x", Old: []string{"1.0"}, New: []string{"2.0"}, Kind: KindUpgrade}
	if got := e.Versions(); got != "1.0 -> 2.0" {
		t.Errorf("got %q", got)
	}
	if got := (Entry{New: []string{"1.0"}, Kind: KindAdded}).Versions(); got != "1.0" {
		t.Errorf("got %q", got)
	}
}
