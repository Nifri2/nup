package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadMissingFileYieldsEmptyLock(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nup.lock.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f.Version != CurrentVersion || len(f.Packages) != 0 {
		t.Fatalf("got %+v", f)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nup.lock.json")
	when := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)

	in := New()
	in.Set(Package{Attr: "ripgrep", Rev: "a1b2c3d4e5f6", Sha256: "sha256-x", Version: "14.1.1",
		Branch: "nixos-unstable", PinnedAt: when})
	in.Set(Package{Attr: "python3Packages.requests", Rev: "a1b2c3d4e5f6", Sha256: "sha256-x",
		Version: "2.32.3", Branch: "nixos-unstable", PinnedAt: when})
	if err := in.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out.Packages) != 2 {
		t.Fatalf("got %d packages", len(out.Packages))
	}
	got, ok := out.Get("ripgrep")
	if !ok {
		t.Fatal("ripgrep missing")
	}
	if got.Version != "14.1.1" || got.Rev != "a1b2c3d4e5f6" || !got.PinnedAt.Equal(when) {
		t.Fatalf("got %+v", got)
	}
}

func TestSaveIsStableAndReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nup.lock.json")
	f := New()
	f.Set(Package{Attr: "b", Rev: "r", Sha256: "s"})
	f.Set(Package{Attr: "a", Rev: "r", Sha256: "s"})
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("lock file should end with a newline")
	}
	// The overlay reads this with builtins.fromJSON, so it must stay valid JSON
	// and the keys must be sorted for a stable diff.
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if ia, ib := strings.Index(string(data), `"a"`), strings.Index(string(data), `"b"`); ia > ib {
		t.Error("package keys should be sorted")
	}
	if info, err := os.Stat(path); err == nil && info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestSaveLeavesNoTempFilesBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nup.lock.json")
	f := New()
	f.Set(Package{Attr: "a", Rev: "r", Sha256: "s"})
	for i := 0; i < 3; i++ {
		if err := f.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d files, want only the lock file", len(entries))
	}
}

func TestSetFillsPinnedAt(t *testing.T) {
	f := New()
	f.Set(Package{Attr: "a", Rev: "r", Sha256: "s"})
	p, _ := f.Get("a")
	if p.PinnedAt.IsZero() {
		t.Error("PinnedAt should default to now")
	}
}

func TestRemove(t *testing.T) {
	f := New()
	f.Set(Package{Attr: "a", Rev: "r"})
	if !f.Remove("a") {
		t.Error("Remove should report the removal")
	}
	if f.Remove("a") {
		t.Error("removing twice should report false")
	}
}

func TestAttrsAndRevsAreSorted(t *testing.T) {
	f := New()
	f.Set(Package{Attr: "c", Rev: "z"})
	f.Set(Package{Attr: "a", Rev: "y"})
	f.Set(Package{Attr: "b", Rev: "y"})
	if got := f.Attrs(); got[0] != "a" || got[2] != "c" {
		t.Errorf("Attrs = %v", got)
	}
	// Two packages share a revision, so it must appear once: that is what makes
	// the overlay fetch each tarball only once.
	if got := f.Revs(); len(got) != 2 || got[0] != "y" || got[1] != "z" {
		t.Errorf("Revs = %v", got)
	}
}

func TestRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nup.lock.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"packages":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for a newer schema version")
	}
}

func TestLoadRejectsBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nup.lock.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestShortRev(t *testing.T) {
	if got := ShortRev("a1b2c3d4e5f6"); got != "a1b2c3d" {
		t.Errorf("got %q", got)
	}
	if got := ShortRev("abc"); got != "abc" {
		t.Errorf("got %q", got)
	}
}
