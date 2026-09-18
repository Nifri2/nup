package nix

import (
	"context"
	"strings"
	"testing"
	"time"
)

// nix has used two JSON shapes for path-info; nup must read both.
func TestParseClosureSizeBothFormats(t *testing.T) {
	const path = "/nix/store/abc-hello"

	v1 := []byte(`{"` + path + `":{"closureSize":123,"narSize":10}}`)
	if got, err := parseClosureSize(v1, path); err != nil || got != 123 {
		t.Errorf("format 1: got %d, %v", got, err)
	}

	v2 := []byte(`[{"path":"` + path + `","closureSize":456}]`)
	if got, err := parseClosureSize(v2, path); err != nil || got != 456 {
		t.Errorf("format 2: got %d, %v", got, err)
	}

	if _, err := parseClosureSize([]byte(`"nope"`), path); err == nil {
		t.Error("expected an error for unparseable output")
	}
}

func TestFlakeMetadata(t *testing.T) {
	fake := NewFake(map[string]string{
		"flake metadata": `{"locked":{"rev":"abc123","narHash":"sha256-x","lastModified":1758016076}}`,
	})
	m, err := NewClient(fake).FlakeMetadata(context.Background(), "github:NixOS/nixpkgs/nixos-unstable")
	if err != nil {
		t.Fatal(err)
	}
	if m.Revision != "abc123" || m.NarHash != "sha256-x" {
		t.Fatalf("got %+v", m)
	}
	if !m.LastModified.Equal(time.Unix(1758016076, 0)) {
		t.Errorf("LastModified = %v", m.LastModified)
	}
}

func TestBuildParsesPaths(t *testing.T) {
	fake := NewFake(map[string]string{
		"build --no-link": "/nix/store/a\n/nix/store/b\n",
	})
	paths, err := NewClient(fake).Build(context.Background(), "github:NixOS/nixpkgs#hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/nix/store/a" {
		t.Fatalf("got %v", paths)
	}
}

func TestBuildWithoutOutputIsAnError(t *testing.T) {
	fake := NewFake(map[string]string{"build --no-link": "\n"})
	if _, err := NewClient(fake).Build(context.Background(), "x#y"); err == nil {
		t.Fatal("expected an error")
	}
}

// Every nix invocation must carry the experimental feature flags, so nup works
// on systems where the user has not enabled flakes globally.
func TestEveryCallEnablesFlakes(t *testing.T) {
	fake := NewFake(map[string]string{"eval": "{}"})
	var out map[string]any
	_ = NewClient(fake).EvalJSON(context.Background(), "x#y", "", &out)
	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls", len(calls))
	}
	if !strings.Contains(calls[0].String(), "--extra-experimental-features nix-command flakes") {
		t.Errorf("got %q", calls[0].String())
	}
}

func TestAttrExistsHandlesMissingAttribute(t *testing.T) {
	fake := &FakeRunner{Matches: map[string]Response{
		"--apply p: true": {
			Result: &Result{ExitCode: 1, Stderr: "error: flake output attribute 'nope' does not exist"},
			Err:    &Error{Name: "nix", Code: 1, Stderr: "error: flake output attribute 'nope' does not exist"},
		},
	}}
	ok, err := NewClient(fake).AttrExists(context.Background(), "github:NixOS/nixpkgs/abc", "nope")
	if err != nil {
		t.Fatalf("a missing attribute is an answer, not an error: %v", err)
	}
	if ok {
		t.Error("should report false")
	}
}

func TestAttrExistsPropagatesRealErrors(t *testing.T) {
	fake := &FakeRunner{Matches: map[string]Response{
		"--apply p: true": {
			Result: &Result{ExitCode: 1, Stderr: "error: unable to download: connection refused"},
			Err:    &Error{Name: "nix", Code: 1, Stderr: "error: unable to download: connection refused"},
		},
	}}
	if _, err := NewClient(fake).AttrExists(context.Background(), "github:NixOS/nixpkgs/abc", "hello"); err == nil {
		t.Fatal("a network failure must not be swallowed")
	}
}

// The error must surface nix's own stderr, which is where the explanation is.
func TestErrorIncludesStderr(t *testing.T) {
	e := &Error{Name: "nix", Args: []string{"build", "x"}, Code: 1, Stderr: "error: undefined variable 'godot'"}
	msg := e.Error()
	if !strings.Contains(msg, "undefined variable 'godot'") || !strings.Contains(msg, "nix build x") {
		t.Errorf("got %q", msg)
	}
}

func TestErrorTruncatesLongStderr(t *testing.T) {
	long := strings.Repeat("line\n", 200)
	e := &Error{Name: "nix", Args: []string{"eval"}, Code: 1, Stderr: long}
	if !strings.Contains(e.Error(), "earlier lines omitted") {
		t.Error("a very long trace should be truncated")
	}
}

func TestTarballURLAndFlakeRef(t *testing.T) {
	if got := TarballURL("abc"); got != "https://github.com/NixOS/nixpkgs/archive/abc.tar.gz" {
		t.Errorf("got %q", got)
	}
	if got := FlakeRef("nixos-unstable"); got != "github:NixOS/nixpkgs/nixos-unstable" {
		t.Errorf("got %q", got)
	}
}

// A whole Nix expression passed to --apply must not drown the error message.
func TestErrorAbbreviatesLongArguments(t *testing.T) {
	long := strings.Repeat("x", 500)
	e := &Error{Name: "nix", Args: []string{"eval", "--apply", long}, Code: 1, Stderr: "error: boom"}
	msg := e.Error()
	if strings.Contains(msg, long) {
		t.Error("a very long argument should be abbreviated")
	}
	if !strings.Contains(msg, "…") || !strings.Contains(msg, "nix eval --apply") {
		t.Errorf("got %q", msg)
	}
	if !strings.Contains(msg, "error: boom") {
		t.Error("nix's own stderr must still be shown")
	}
}

// --impure is opt-in and must only appear when asked for: nup applies it to the
// user's own flake, never to nixpkgs lookups.
func TestEvalImpureIsOptIn(t *testing.T) {
	fake := NewFake(map[string]string{"eval": "{}"})
	c := NewClient(fake)
	var out map[string]any

	_ = c.EvalJSON(context.Background(), "x#y", "", &out)
	if strings.Contains(fake.Calls()[0].String(), "--impure") {
		t.Error("--impure must not be added by default")
	}

	_ = c.EvalJSON(context.Background(), "x#y", "", &out, Impure(true))
	if !strings.Contains(fake.Calls()[1].String(), "--impure") {
		t.Errorf("--impure was not passed: %q", fake.Calls()[1])
	}

	_, _ = c.EvalRaw(context.Background(), "x#y", Impure(true))
	if !strings.Contains(fake.Calls()[2].String(), "--impure") {
		t.Errorf("--impure was not passed to EvalRaw: %q", fake.Calls()[2])
	}

	_ = c.EvalJSON(context.Background(), "x#y", "", &out, Impure(false))
	if strings.Contains(fake.Calls()[3].String(), "--impure") {
		t.Error("Impure(false) must not add the flag")
	}
}
