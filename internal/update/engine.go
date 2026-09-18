// Package update contains the whole update pipeline. Both the CLI and the TUI
// drive it; neither of them contains update logic of its own.
package update

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Nifri2/nup/internal/config"
	"github.com/Nifri2/nup/internal/diff"
	"github.com/Nifri2/nup/internal/gitutil"
	"github.com/Nifri2/nup/internal/lock"
	"github.com/Nifri2/nup/internal/nix"
	"github.com/Nifri2/nup/internal/pkgset"
	"github.com/Nifri2/nup/internal/version"
)

// Timeouts for the individual phases. A build can legitimately take a long
// time; an evaluation should not.
const (
	MetadataTimeout = 5 * time.Minute
	EvalTimeout     = 15 * time.Minute
	BuildTimeout    = 4 * time.Hour
	// BranchTTL is how long a resolved branch head stays cached.
	BranchTTL = time.Hour
)

// Engine performs updates against a flake.
type Engine struct {
	Nix      *nix.Client
	Cache    *nix.Cache
	Git      *gitutil.Git
	FlakeDir string
	Host     string
	LockPath string
	Lock     *lock.File
	// Branch is the default nixpkgs branch pins follow.
	Branch string
	// MainRev is the nixpkgs revision of the flake's own input, used to decide
	// whether a pin is stale.
	MainRev string
}

// Request describes one package to update or pin.
type Request struct {
	// Name is what the user typed, used in messages.
	Name string
	// Attr is the nixpkgs attribute path. Empty means "derive it from Name".
	Attr string
	// Branch overrides the engine default.
	Branch string
	// Rev pins to an explicit revision instead of following a branch.
	Rev string
	// Force updates even when the version is unchanged.
	Force bool
	// Current is the installed package, if known.
	Current pkgset.Package
}

// Plan is the fully resolved result of preparing one update. It carries
// everything both frontends need to render a summary and to commit the change.
type Plan struct {
	Name string
	Attr string

	OldVersion string
	NewVersion string
	Bump       version.Bump

	OldRev string
	NewRev string
	Sha256 string
	Branch string
	// RevDate is when the target revision was committed.
	RevDate time.Time

	OldOutPath string
	NewOutPath string

	Entries []diff.Entry
	OldSize int64
	NewSize int64

	Changelog string
	Homepage  string

	// Unchanged is true when the target version equals the installed one.
	Unchanged bool
}

// SizeDelta is the change in closure size in bytes.
func (p *Plan) SizeDelta() int64 { return p.NewSize - p.OldSize }

// LockEntry is the lock file record this plan would write.
func (p *Plan) LockEntry() lock.Package {
	return lock.Package{
		Attr:     p.Attr,
		Rev:      p.NewRev,
		Sha256:   p.Sha256,
		Version:  p.NewVersion,
		Branch:   p.Branch,
		PinnedAt: time.Now().UTC(),
	}
}

// AttrError is returned when an attribute path does not exist in nixpkgs. It
// carries the hint the user needs to fix it.
type AttrError struct {
	Name string
	Attr string
	Rev  string
}

func (e *AttrError) Error() string {
	return fmt.Sprintf("no attribute %q in nixpkgs at %s\n"+
		"The package name in your configuration does not always match the nixpkgs attribute.\n"+
		"Pass the attribute explicitly, for example: nup update %s --attr <attrpath>",
		e.Attr, lock.ShortRev(e.Rev), e.Name)
}

// branchHead is the cached result of resolving a branch.
type branchHead struct {
	Rev          string    `json:"rev"`
	Sha256       string    `json:"sha256"`
	LastModified time.Time `json:"lastModified"`
}

// ResolveBranch returns the current head of a nixpkgs branch together with a
// hash usable by fetchTarball.
func (e *Engine) ResolveBranch(ctx context.Context, branch string) (rev, sha256 string, date time.Time, err error) {
	var head branchHead
	key := nix.Key("branch", branch)
	if e.Cache.Get(key, BranchTTL, &head) && head.Rev != "" && head.Sha256 != "" {
		return head.Rev, head.Sha256, head.LastModified, nil
	}
	rev, sha256, date, err = e.resolveRef(ctx, branch)
	if err != nil {
		return "", "", time.Time{}, err
	}
	_ = e.Cache.Put(key, branchHead{Rev: rev, Sha256: sha256, LastModified: date})
	return rev, sha256, date, nil
}

// ResolveRev resolves an explicit revision to a hash.
func (e *Engine) ResolveRev(ctx context.Context, rev string) (string, string, time.Time, error) {
	return e.resolveRef(ctx, rev)
}

// revInfo is the per-revision cache entry.
type revInfo struct {
	Sha256       string    `json:"sha256"`
	LastModified time.Time `json:"lastModified"`
}

// resolveRef turns a branch name or revision into (rev, sha256, date).
//
// The narHash reported by `nix flake metadata` is the hash of the unpacked
// source tree, which is exactly what builtins.fetchTarball expects, so it can
// normally be reused as-is. The value is verified once per revision and falls
// back to `nix store prefetch-file` if it ever does not match.
func (e *Engine) resolveRef(ctx context.Context, ref string) (string, string, time.Time, error) {
	mctx, cancel := context.WithTimeout(ctx, MetadataTimeout)
	defer cancel()

	meta, err := e.Nix.FlakeMetadata(mctx, nix.FlakeRef(ref))
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("resolving nixpkgs %s: %w", ref, err)
	}

	var cached revInfo
	if e.Cache.Get(nix.Key("rev", meta.Revision), 0, &cached) && cached.Sha256 != "" {
		return meta.Revision, cached.Sha256, meta.LastModified, nil
	}

	url := nix.TarballURL(meta.Revision)
	sha := meta.NarHash
	if sha == "" || e.Nix.CheckTarball(ctx, url, sha) != nil {
		sha, err = e.Nix.PrefetchTarball(ctx, url)
		if err != nil {
			return "", "", time.Time{}, fmt.Errorf("hashing %s: %w", url, err)
		}
	}
	_ = e.Cache.Put(nix.Key("rev", meta.Revision), revInfo{Sha256: sha, LastModified: meta.LastModified})
	return meta.Revision, sha, meta.LastModified, nil
}

// ResolveAttr determines the nixpkgs attribute path for a request and verifies
// that it exists at the target revision.
func (e *Engine) ResolveAttr(ctx context.Context, req Request, rev string) (string, error) {
	candidates := []string{}
	switch {
	case req.Attr != "":
		candidates = append(candidates, req.Attr)
	default:
		// An existing pin already knows the right attribute path.
		if p, ok := e.Lock.Get(req.Name); ok {
			candidates = append(candidates, p.Attr)
		}
		if req.Current.Attr != "" {
			candidates = append(candidates, req.Current.Attr)
		}
		candidates = append(candidates, req.Name)
	}

	ectx, cancel := context.WithTimeout(ctx, EvalTimeout)
	defer cancel()

	seen := map[string]bool{}
	for _, c := range candidates {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		ok, err := e.Nix.AttrExists(ectx, nix.FlakeRef(rev), c)
		if err != nil {
			return "", err
		}
		if ok {
			return c, nil
		}
	}
	attr := req.Attr
	if attr == "" {
		attr = req.Name
	}
	return "", &AttrError{Name: req.Name, Attr: attr, Rev: rev}
}

// Prepare resolves a single request into a Plan: it picks the target revision,
// evaluates the new version, builds the package and diffs the closures.
func (e *Engine) Prepare(ctx context.Context, req Request, progress func(string)) (*Plan, error) {
	report := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}

	branch := req.Branch
	if branch == "" {
		branch = e.Branch
	}

	var (
		rev, sha string
		date     time.Time
		err      error
	)
	if req.Rev != "" {
		report("resolving revision")
		rev, sha, date, err = e.ResolveRev(ctx, req.Rev)
		branch = ""
	} else {
		report("resolving " + branch)
		rev, sha, date, err = e.ResolveBranch(ctx, branch)
	}
	if err != nil {
		return nil, err
	}

	report("resolving attribute")
	attr, err := e.ResolveAttr(ctx, req, rev)
	if err != nil {
		return nil, err
	}

	plan := &Plan{
		Name:       req.Name,
		Attr:       attr,
		OldVersion: req.Current.Version,
		NewRev:     rev,
		Sha256:     sha,
		Branch:     branch,
		RevDate:    date,
		OldOutPath: req.Current.OutPath,
		Changelog:  req.Current.Changelog,
		Homepage:   req.Current.Homepage,
	}
	if p, ok := e.Lock.Get(attr); ok {
		plan.OldRev = p.Rev
		if plan.OldVersion == "" {
			plan.OldVersion = p.Version
		}
	} else {
		plan.OldRev = e.MainRev
	}

	report("evaluating new version")
	ectx, cancel := context.WithTimeout(ctx, EvalTimeout)
	meta, err := e.evalMeta(ectx, rev, attr)
	cancel()
	if err != nil {
		return nil, err
	}
	plan.NewVersion = meta.Version
	if meta.Changelog != "" {
		plan.Changelog = meta.Changelog
	}
	if meta.Homepage != "" {
		plan.Homepage = meta.Homepage
	}
	plan.Bump = version.Kind(plan.OldVersion, plan.NewVersion)

	// An explicit `nup pin <pkg> <rev>` is still worth recording even when the
	// version happens to be identical -- unless it is already pinned there.
	sameRev := plan.OldRev != "" && plan.OldRev == rev
	sameVersion := plan.OldVersion != "" && plan.OldVersion == plan.NewVersion
	if !req.Force && sameVersion && (req.Rev == "" || sameRev) {
		plan.Unchanged = true
		return plan, nil
	}

	report("building " + attr)
	bctx, cancel := context.WithTimeout(ctx, BuildTimeout)
	defer cancel()
	paths, err := e.Nix.Build(bctx, fmt.Sprintf("%s#%s", nix.FlakeRef(rev), attr))
	if err != nil {
		return nil, fmt.Errorf("building %s from nixpkgs %s: %w", attr, lock.ShortRev(rev), err)
	}
	plan.NewOutPath = paths[0]

	if plan.OldOutPath == "" {
		// Nothing installed to compare against (a fresh pin of a package that
		// is not in the configuration yet): the diff is simply skipped.
		return plan, nil
	}

	report("diffing closures")
	if out, err := e.Nix.DiffClosures(bctx, plan.OldOutPath, plan.NewOutPath); err == nil {
		plan.Entries = diff.Parse(out)
	}
	if n, err := e.Nix.ClosureSize(bctx, plan.OldOutPath); err == nil {
		plan.OldSize = n
	}
	if n, err := e.Nix.ClosureSize(bctx, plan.NewOutPath); err == nil {
		plan.NewSize = n
	}
	return plan, nil
}

type pkgMeta struct {
	Version   string `json:"version"`
	Changelog string `json:"changelog"`
	Homepage  string `json:"homepage"`
}

// evalMeta reads version and metadata in a single evaluation.
func (e *Engine) evalMeta(ctx context.Context, rev, attr string) (*pkgMeta, error) {
	const apply = `p: {
  version = p.version or "";
  changelog = let c = p.meta.changelog or ""; in if builtins.isList c then (if c == [] then "" else builtins.head c) else c;
  homepage = let h = p.meta.homepage or ""; in if builtins.isList h then (if h == [] then "" else builtins.head h) else h;
}`
	var m pkgMeta
	if err := e.Nix.EvalJSON(ctx, fmt.Sprintf("%s#%s", nix.FlakeRef(rev), attr), apply, &m); err != nil {
		return nil, fmt.Errorf("evaluating %s from nixpkgs %s: %w", attr, lock.ShortRev(rev), err)
	}
	return &m, nil
}

// Apply writes the plans into the lock file and saves it atomically.
func (e *Engine) Apply(plans []*Plan) error {
	for _, p := range plans {
		if p == nil || p.Unchanged {
			continue
		}
		e.Lock.Set(p.LockEntry())
	}
	return e.Lock.Save(e.LockPath)
}

// Unpin removes pins and saves the lock file. It returns the attributes that
// were actually pinned.
func (e *Engine) Unpin(names []string) ([]string, error) {
	var removed []string
	for _, n := range names {
		attr := n
		if _, ok := e.Lock.Get(attr); !ok {
			// Allow removing by package name when the pin used a longer path.
			for _, a := range e.Lock.Attrs() {
				if a == n || strings.HasSuffix(a, "."+n) {
					attr = a
					break
				}
			}
		}
		if e.Lock.Remove(attr) {
			removed = append(removed, attr)
		}
	}
	if len(removed) == 0 {
		return nil, nil
	}
	if err := e.Lock.Save(e.LockPath); err != nil {
		return nil, err
	}
	return removed, nil
}

// CommitMessage describes a set of applied plans.
func CommitMessage(plans []*Plan) string {
	var parts []string
	for _, p := range plans {
		if p == nil || p.Unchanged {
			continue
		}
		if p.OldVersion != "" && p.NewVersion != "" && p.OldVersion != p.NewVersion {
			parts = append(parts, fmt.Sprintf("%s: %s -> %s", p.Name, p.OldVersion, p.NewVersion))
		} else {
			parts = append(parts, fmt.Sprintf("%s: %s", p.Name, lock.ShortRev(p.NewRev)))
		}
	}
	switch len(parts) {
	case 0:
		return "nup: no changes"
	case 1:
		return "nup: " + parts[0]
	default:
		return "nup: update " + fmt.Sprint(len(parts)) + " packages\n\n" + strings.Join(prefixAll(parts, "- "), "\n")
	}
}

func prefixAll(in []string, prefix string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = prefix + s
	}
	return out
}

// Rebuild runs the configured rebuild command, streaming its output.
func (e *Engine) Rebuild(ctx context.Context, cfg *config.Config, action config.Action, stdin io.Reader, stdout, stderr io.Writer) error {
	argv := cfg.RebuildArgs(action, e.FlakeDir, e.Host)
	if _, err := io.WriteString(stdout, "$ "+strings.Join(argv, " ")+"\n"); err != nil {
		return err
	}
	return e.Nix.Rebuild(ctx, argv, stdin, stdout, stderr)
}

// Latest reports the version of an attribute at the head of a branch. It is
// what `nup outdated` and the TUI's LATEST column use.
func (e *Engine) Latest(ctx context.Context, attr, branch string) (string, string, error) {
	if branch == "" {
		branch = e.Branch
	}
	rev, _, _, err := e.ResolveBranch(ctx, branch)
	if err != nil {
		return "", "", err
	}
	ectx, cancel := context.WithTimeout(ctx, EvalTimeout)
	defer cancel()
	v, err := e.Nix.EvalRaw(ectx, fmt.Sprintf("%s#%s.version", nix.FlakeRef(rev), attr))
	if err != nil {
		return "", rev, err
	}
	return v, rev, nil
}
