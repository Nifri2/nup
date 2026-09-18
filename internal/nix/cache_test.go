package nix

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheRoundTrip(t *testing.T) {
	c := NewCache(t.TempDir(), false)
	type payload struct{ Name string }

	if c.Get("missing", 0, &payload{}) {
		t.Error("a missing entry must report false")
	}
	if err := c.Put("k", payload{Name: "ripgrep"}); err != nil {
		t.Fatal(err)
	}
	var back payload
	if !c.Get("k", 0, &back) || back.Name != "ripgrep" {
		t.Fatalf("got %+v", back)
	}
}

func TestCacheRespectsTTL(t *testing.T) {
	c := NewCache(t.TempDir(), false)
	if err := c.Put("k", 1); err != nil {
		t.Fatal(err)
	}
	var v int
	if !c.Get("k", time.Hour, &v) {
		t.Error("a fresh entry should be returned")
	}
	if c.Get("k", time.Nanosecond, &v) {
		t.Error("an expired entry must not be returned")
	}
}

// --refresh must bypass the cache without deleting it.
func TestDisabledCacheAlwaysMisses(t *testing.T) {
	dir := t.TempDir()
	if err := NewCache(dir, false).Put("k", 42); err != nil {
		t.Fatal(err)
	}
	var v int
	if NewCache(dir, true).Get("k", 0, &v) {
		t.Error("a disabled cache must miss")
	}
	if !NewCache(dir, false).Get("k", 0, &v) || v != 42 {
		t.Error("the entry should still be there")
	}
}

// The cache key must change when the flake lock, nup's lock or the host change,
// because those are exactly the inputs that change the result.
func TestKeyChangesWithInputs(t *testing.T) {
	base := Key("pkgs", "flakehash", "lockhash", "host")
	cases := map[string]string{
		"flake lock": Key("pkgs", "other", "lockhash", "host"),
		"nup lock":   Key("pkgs", "flakehash", "other", "host"),
		"host":       Key("pkgs", "flakehash", "lockhash", "other"),
	}
	for name, other := range cases {
		if base == other {
			t.Errorf("key did not change with %s", name)
		}
	}
	first := Key("pkgs", "a", "b", "c")
	second := Key("pkgs", "a", "b", "c")
	if first != second {
		t.Error("the same inputs must give the same key")
	}
}

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if got := HashFile(path); got != "missing" {
		t.Errorf("a missing file should hash to %q, got %q", "missing", got)
	}
	if err := os.WriteFile(path, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := HashFile(path)
	if err := os.WriteFile(path, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if HashFile(path) == first {
		t.Error("the hash should change with the content")
	}
}

func TestReadFlakeLockInput(t *testing.T) {
	dir := t.TempDir()
	if in, err := ReadFlakeLockInput(dir, "nixpkgs"); err != nil || in != nil {
		t.Errorf("a missing flake.lock is not an error: %v %v", in, err)
	}
	content := `{"nodes":{"nixpkgs":{"locked":{"rev":"deadbeef","narHash":"sha256-x","lastModified":1700000000}}},"root":"root"}`
	if err := os.WriteFile(filepath.Join(dir, "flake.lock"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := ReadFlakeLockInput(dir, "nixpkgs")
	if err != nil || in == nil || in.Rev != "deadbeef" {
		t.Fatalf("got %+v, %v", in, err)
	}
	if other, err := ReadFlakeLockInput(dir, "home-manager"); err != nil || other != nil {
		t.Errorf("an absent input should be nil: %+v %v", other, err)
	}
}
