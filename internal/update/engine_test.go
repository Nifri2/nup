package update

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nifri2/nup/internal/config"
	"github.com/Nifri2/nup/internal/diff"
	"github.com/Nifri2/nup/internal/lock"
	"github.com/Nifri2/nup/internal/nix"
	"github.com/Nifri2/nup/internal/pkgset"
	"github.com/Nifri2/nup/internal/version"
)

const (
	testRev    = "b1b875982b17dabde9b4a37f3e229e74913e6db3"
	testHash   = "sha256-zVxLZiSnmaaPLwnhj7pwmqe3axBg/C6nG5JZsJMh2g4="
	oldOutPath = "/nix/store/old-ripgrep-14.1.0"
	newOutPath = "/nix/store/new-ripgrep-14.1.1"
)

// newTestEngine wires an Engine to a FakeRunner, so the whole pipeline runs
// without nix being installed.
func newTestEngine(t *testing.T, extra map[string]string) (*Engine, *nix.FakeRunner, string) {
	t.Helper()
	dir := t.TempDir()

	responses := map[string]string{
		"flake metadata github:NixOS/nixpkgs/nixos-unstable": `{"locked":{"rev":"` + testRev +
			`","narHash":"` + testHash + `","lastModified":1758016076}}`,
		// CheckTarball: the hash resolves, so no prefetch fallback is needed.
		"builtins.fetchTarball": "/nix/store/source",
		// AttrExists
		"--apply p: true": "true",
		// evalMeta
		"changelog = let c":                 `{"version":"14.1.1","changelog":"https://example.invalid/changelog","homepage":"https://example.invalid"}`,
		"build --no-link --print-out-paths": newOutPath + "\n",
		"store diff-closures": "pcre2: 10.43 → 10.44, 12.0 KiB\n" +
			"ripgrep: 14.1.0 → 14.1.1, 4.0 KiB\n",
		"path-info -S --json " + oldOutPath: `{"` + oldOutPath + `":{"closureSize":1000000}}`,
		"path-info -S --json " + newOutPath: `{"` + newOutPath + `":{"closureSize":1016384}}`,
	}
	for k, v := range extra {
		responses[k] = v
	}

	fake := nix.NewFake(responses)
	lf := lock.New()
	return &Engine{
		Nix:      nix.NewClient(fake),
		Cache:    nix.NewCache(filepath.Join(dir, "cache"), false),
		FlakeDir: dir,
		Host:     "testhost",
		LockPath: lock.Path(dir),
		Lock:     lf,
		Branch:   config.DefaultBranch,
		MainRev:  "0000000000000000000000000000000000000000",
	}, fake, dir
}

func TestPrepareFullPipeline(t *testing.T) {
	e, fake, _ := newTestEngine(t, nil)

	var steps []string
	plan, err := e.Prepare(context.Background(), Request{
		Name: "ripgrep",
		Current: pkgset.Package{
			Name: "ripgrep", Attr: "ripgrep", Version: "14.1.0", OutPath: oldOutPath,
		},
	}, func(msg string) { steps = append(steps, msg) })
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if plan.Unchanged {
		t.Fatal("14.1.0 -> 14.1.1 should not be reported as unchanged")
	}
	if plan.OldVersion != "14.1.0" || plan.NewVersion != "14.1.1" {
		t.Errorf("versions: %q -> %q", plan.OldVersion, plan.NewVersion)
	}
	if plan.Bump != version.BumpPatch {
		t.Errorf("Bump = %v, want patch", plan.Bump)
	}
	if plan.NewRev != testRev || plan.Sha256 != testHash {
		t.Errorf("rev/hash: %q %q", plan.NewRev, plan.Sha256)
	}
	if plan.OldRev != e.MainRev {
		t.Errorf("an unpinned package should compare against the flake's own nixpkgs, got %q", plan.OldRev)
	}
	if plan.NewOutPath != newOutPath {
		t.Errorf("NewOutPath = %q", plan.NewOutPath)
	}
	if len(plan.Entries) != 2 || plan.Entries[0].Name != "pcre2" {
		t.Errorf("closure entries = %+v", plan.Entries)
	}
	if plan.OldSize != 1000000 || plan.NewSize != 1016384 {
		t.Errorf("sizes: %d -> %d", plan.OldSize, plan.NewSize)
	}
	if plan.SizeDelta() != 16384 {
		t.Errorf("SizeDelta = %d", plan.SizeDelta())
	}
	if plan.Changelog != "https://example.invalid/changelog" {
		t.Errorf("Changelog = %q", plan.Changelog)
	}
	if len(steps) == 0 {
		t.Error("progress callback was never called")
	}
	if !fake.Called("build --no-link") {
		t.Error("the package should have been built")
	}
}

// The narHash from `nix flake metadata` is normally usable as fetchTarball's
// sha256; when it is not, nup must fall back to prefetching.
func TestPrepareFallsBackToPrefetch(t *testing.T) {
	e, fake, _ := newTestEngine(t, map[string]string{
		"store prefetch-file": `{"hash":"sha256-fallbackfallbackfallbackfallbackfallbackfal="}`,
	})
	// Make the hash check fail.
	e.Nix.Runner.(*nix.FakeRunner).Matches["builtins.fetchTarball"] = nix.Response{
		Result: &nix.Result{ExitCode: 1, Stderr: "hash mismatch"},
		Err:    &nix.Error{Name: "nix", Code: 1, Stderr: "hash mismatch"},
	}

	plan, err := e.Prepare(context.Background(), Request{
		Name:    "ripgrep",
		Current: pkgset.Package{Name: "ripgrep", Attr: "ripgrep", Version: "14.1.0", OutPath: oldOutPath},
	}, nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if plan.Sha256 != "sha256-fallbackfallbackfallbackfallbackfallbackfal=" {
		t.Errorf("Sha256 = %q, want the prefetched hash", plan.Sha256)
	}
	if !fake.Called("store prefetch-file") {
		t.Error("prefetch fallback was not used")
	}
}

func TestPrepareReportsUnchanged(t *testing.T) {
	e, fake, _ := newTestEngine(t, nil)
	plan, err := e.Prepare(context.Background(), Request{
		Name:    "ripgrep",
		Current: pkgset.Package{Name: "ripgrep", Attr: "ripgrep", Version: "14.1.1", OutPath: oldOutPath},
	}, nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !plan.Unchanged {
		t.Fatal("an identical version should be reported as unchanged")
	}
	if fake.Called("build --no-link") {
		t.Error("nothing should be built when the version is unchanged")
	}
}

func TestPrepareForceBuildsAnyway(t *testing.T) {
	e, fake, _ := newTestEngine(t, nil)
	plan, err := e.Prepare(context.Background(), Request{
		Name:    "ripgrep",
		Force:   true,
		Current: pkgset.Package{Name: "ripgrep", Attr: "ripgrep", Version: "14.1.1", OutPath: oldOutPath},
	}, nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if plan.Unchanged {
		t.Fatal("--force must not short-circuit")
	}
	if !fake.Called("build --no-link") {
		t.Error("--force should build")
	}
}

func TestPrepareUnknownAttributeGivesHint(t *testing.T) {
	e, _, _ := newTestEngine(t, map[string]string{
		"--apply p: true": "",
	})
	e.Nix.Runner.(*nix.FakeRunner).Matches["--apply p: true"] = nix.Response{
		Result: &nix.Result{ExitCode: 1, Stderr: "error: flake output attribute 'missingpkg' does not exist"},
		Err:    &nix.Error{Name: "nix", Code: 1, Stderr: "error: flake output attribute 'missingpkg' does not exist"},
	}

	_, err := e.Prepare(context.Background(), Request{Name: "missingpkg"}, nil)
	if err == nil {
		t.Fatal("expected an error for a missing attribute")
	}
	var ae *AttrError
	if !errorsAs(err, &ae) {
		t.Fatalf("expected an *AttrError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "--attr") {
		t.Errorf("the error should point at --attr, got: %v", err)
	}
}

// An existing pin already knows the attribute path, so a later update must not
// fall back to guessing from the package name.
func TestResolveAttrUsesExistingPin(t *testing.T) {
	e, _, _ := newTestEngine(t, nil)
	e.Lock.Set(lock.Package{Attr: "python3Packages.requests", Rev: testRev})
	got, err := e.ResolveAttr(context.Background(), Request{Name: "python3Packages.requests"}, testRev)
	if err != nil {
		t.Fatal(err)
	}
	if got != "python3Packages.requests" {
		t.Errorf("got %q", got)
	}
}

func TestApplyWritesLock(t *testing.T) {
	e, _, dir := newTestEngine(t, nil)
	plan := &Plan{Name: "ripgrep", Attr: "ripgrep", NewRev: testRev, Sha256: testHash,
		NewVersion: "14.1.1", Branch: "nixos-unstable"}
	if err := e.Apply([]*Plan{plan}); err != nil {
		t.Fatal(err)
	}
	back, err := lock.Load(lock.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := back.Get("ripgrep")
	if !ok {
		t.Fatal("ripgrep was not written to the lock file")
	}
	if p.Rev != testRev || p.Sha256 != testHash || p.Version != "14.1.1" {
		t.Errorf("got %+v", p)
	}
	if p.PinnedAt.IsZero() {
		t.Error("PinnedAt should be set")
	}
}

func TestApplySkipsUnchangedPlans(t *testing.T) {
	e, _, dir := newTestEngine(t, nil)
	if err := e.Apply([]*Plan{{Name: "x", Attr: "x", Unchanged: true}}); err != nil {
		t.Fatal(err)
	}
	back, _ := lock.Load(lock.Path(dir))
	if len(back.Packages) != 0 {
		t.Errorf("an unchanged plan must not be written: %+v", back.Packages)
	}
}

func TestUnpin(t *testing.T) {
	e, _, _ := newTestEngine(t, nil)
	e.Lock.Set(lock.Package{Attr: "ripgrep", Rev: testRev})
	e.Lock.Set(lock.Package{Attr: "python3Packages.requests", Rev: testRev})

	removed, err := e.Unpin([]string{"ripgrep", "requests", "nothere"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 {
		t.Fatalf("removed = %v, want two entries (one matched by its leaf name)", removed)
	}
	if len(e.Lock.Packages) != 0 {
		t.Errorf("lock should be empty, got %+v", e.Lock.Packages)
	}
}

func TestCommitMessage(t *testing.T) {
	one := []*Plan{{Name: "ripgrep", OldVersion: "14.1.0", NewVersion: "14.1.1", NewRev: testRev}}
	if got := CommitMessage(one); got != "nup: ripgrep: 14.1.0 -> 14.1.1" {
		t.Errorf("got %q", got)
	}

	two := append(one, &Plan{Name: "jq", OldVersion: "1.7", NewVersion: "1.8", NewRev: testRev})
	got := CommitMessage(two)
	if !strings.HasPrefix(got, "nup: update 2 packages") || !strings.Contains(got, "- jq: 1.7 -> 1.8") {
		t.Errorf("got %q", got)
	}
}

func TestLatest(t *testing.T) {
	e, _, _ := newTestEngine(t, map[string]string{
		"eval --raw github:NixOS/nixpkgs/" + testRev + "#ripgrep.version": "14.1.1",
	})
	v, rev, err := e.Latest(context.Background(), "ripgrep", "")
	if err != nil {
		t.Fatal(err)
	}
	if v != "14.1.1" || rev != testRev {
		t.Errorf("got %q at %q", v, rev)
	}
}

func TestRebuildUsesConfiguredCommand(t *testing.T) {
	e, fake, dir := newTestEngine(t, nil)
	cfg := config.Default()
	cfg.RebuildCommand = []string{"nh", "os", "{action}", "{flake}"}

	var out strings.Builder
	if err := e.Rebuild(context.Background(), cfg, config.ActionSwitch, nil, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !fake.Called("nh os switch " + dir) {
		t.Errorf("calls: %v", fake.Calls())
	}
	if !strings.Contains(out.String(), "nh os switch") {
		t.Errorf("the command should be echoed, got %q", out.String())
	}
}

// diff.Entry values must survive the trip through the engine unchanged.
func TestPlanEntriesAreParsed(t *testing.T) {
	e, _, _ := newTestEngine(t, nil)
	plan, err := e.Prepare(context.Background(), Request{
		Name:    "ripgrep",
		Current: pkgset.Package{Name: "ripgrep", Attr: "ripgrep", Version: "14.1.0", OutPath: oldOutPath},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Entries[1].Kind != diff.KindUpgrade {
		t.Errorf("got %v", plan.Entries[1].Kind)
	}
}
