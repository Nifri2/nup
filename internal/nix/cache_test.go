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

func writeLock(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "flake.lock"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadFlakeLockInput(t *testing.T) {
	if in, err := ReadFlakeLockInput(t.TempDir(), "nixpkgs"); err != nil || in != nil {
		t.Errorf("a missing flake.lock is not an error: %v %v", in, err)
	}

	dir := writeLock(t, `{
	  "root": "root",
	  "nodes": {
	    "root": {"inputs": {"nixpkgs": "nixpkgs"}},
	    "nixpkgs": {"locked": {"rev": "deadbeef", "narHash": "sha256-x", "lastModified": 1700000000}}
	  }
	}`)
	in, err := ReadFlakeLockInput(dir, "nixpkgs")
	if err != nil || in == nil || in.Rev != "deadbeef" {
		t.Fatalf("got %+v, %v", in, err)
	}
	if other, err := ReadFlakeLockInput(dir, "home-manager"); err != nil || other != nil {
		t.Errorf("an absent input should be nil: %+v %v", other, err)
	}
}

// When a transitive input already claims the name "nixpkgs", the flake's own
// nixpkgs is stored under "nixpkgs_2". Reading by node name would pick the
// wrong revision and mis-report which pins are stale.
func TestReadFlakeLockInputResolvesThroughRoot(t *testing.T) {
	dir := writeLock(t, `{
	  "root": "root",
	  "nodes": {
	    "root": {"inputs": {"nixpkgs": "nixpkgs_2", "yeetmouse": "yeetmouse"}},
	    "nixpkgs": {"locked": {"rev": "transitive", "lastModified": 1}},
	    "nixpkgs_2": {"locked": {"rev": "mine", "lastModified": 2}},
	    "yeetmouse": {"inputs": {"nixpkgs": "nixpkgs"}, "locked": {"rev": "ym", "lastModified": 3}}
	  }
	}`)
	in, err := ReadFlakeLockInput(dir, "nixpkgs")
	if err != nil {
		t.Fatal(err)
	}
	if in == nil || in.Rev != "mine" {
		t.Fatalf("got %+v, want the root's own nixpkgs", in)
	}
}

// A "follows" input is a path to walk from the root, not a node name.
func TestReadFlakeLockInputFollowsPath(t *testing.T) {
	dir := writeLock(t, `{
	  "root": "root",
	  "nodes": {
	    "root": {"inputs": {"nixpkgs": "nixpkgs_2", "nup": "nup"}},
	    "nixpkgs_2": {"locked": {"rev": "mine", "lastModified": 2}},
	    "nup": {"inputs": {"nixpkgs": ["nixpkgs"]}, "locked": {"rev": "nuprev", "lastModified": 4}}
	  }
	}`)
	in, err := ReadFlakeLockInput(dir, "nup")
	if err != nil || in == nil || in.Rev != "nuprev" {
		t.Fatalf("got %+v, %v", in, err)
	}
}

// A lock file without a root input map still resolves by node name.
func TestReadFlakeLockInputFallsBackToNodeName(t *testing.T) {
	dir := writeLock(t, `{"nodes":{"nixpkgs":{"locked":{"rev":"deadbeef","lastModified":1}}}}`)
	in, err := ReadFlakeLockInput(dir, "nixpkgs")
	if err != nil || in == nil || in.Rev != "deadbeef" {
		t.Fatalf("got %+v, %v", in, err)
	}
}
