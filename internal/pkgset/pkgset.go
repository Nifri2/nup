// Package pkgset determines which packages a NixOS configuration installs.
package pkgset

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Nifri2/nup/internal/lock"
	"github.com/Nifri2/nup/internal/nix"
	"github.com/Nifri2/nup/internal/overlay"
)

// Source says where a package comes from.
type Source string

const (
	// SourceSystem is environment.systemPackages.
	SourceSystem Source = "system"
	// SourceHome is home-manager's home.packages.
	SourceHome Source = "home"
)

// applyExpr maps a package list to the fields nup needs. Entries that are not
// derivations (plain paths or strings can legally appear in systemPackages) are
// handled instead of crashing the whole evaluation.
//
// The store path is returned as `out`, not `outPath`: nix serialises any
// attribute set containing an `outPath` attribute as a bare string, which would
// collapse each record into just its path.
const applyExpr = `ps:
let
  # meta.changelog and meta.homepage may be a string or a list of strings.
  first = v:
    if v == null then null
    else if builtins.isList v then (if v == [ ] then null else builtins.head v)
    else if builtins.isString v then v
    else null;
in
map (p:
  if !(builtins.isAttrs p) then {
    name = builtins.toString p;
    pname = null;
    version = null;
    out = builtins.toString p;
    changelog = null;
    homepage = null;
    description = null;
  } else {
    name = p.name or "";
    pname = p.pname or null;
    version = p.version or null;
    out = p.outPath or "";
    changelog = first (p.meta.changelog or null);
    homepage = first (p.meta.homepage or null);
    description = p.meta.description or null;
  }) ps`

// raw mirrors applyExpr.
type raw struct {
	Name        string  `json:"name"`
	Pname       *string `json:"pname"`
	Version     *string `json:"version"`
	Out         string  `json:"out"`
	Changelog   *string `json:"changelog"`
	Homepage    *string `json:"homepage"`
	Description *string `json:"description"`
}

// Package is one installed package, enriched with nup's own lock state.
type Package struct {
	Name        string `json:"name" yaml:"name"`
	Version     string `json:"version" yaml:"version"`
	Attr        string `json:"attr" yaml:"attr"`
	PinnedRev   string `json:"pinnedRev,omitempty" yaml:"pinnedRev,omitempty"`
	Stale       bool   `json:"stale" yaml:"stale"`
	Source      Source `json:"source" yaml:"source"`
	OutPath     string `json:"outPath" yaml:"outPath"`
	Changelog   string `json:"changelog,omitempty" yaml:"changelog,omitempty"`
	Homepage    string `json:"homepage,omitempty" yaml:"homepage,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Latest is filled in by `nup outdated` and the TUI, never by List.
	Latest string `json:"latest,omitempty" yaml:"latest,omitempty"`
}

// Pinned reports whether nup manages this package's revision.
func (p Package) Pinned() bool { return p.PinnedRev != "" }

// Lister evaluates a flake and returns its packages.
type Lister struct {
	Nix      *nix.Client
	Cache    *nix.Cache
	FlakeDir string
	Host     string
	// HomeManagerUser enables the home-manager source when non-empty.
	HomeManagerUser string
	// Impure evaluates the configuration with --impure. Needed by flakes that
	// read absolute paths or fetch without a hash.
	Impure bool
}

// Options controls one List call.
type Options struct {
	// Refresh bypasses the cache.
	Refresh bool
}

// systemInstallable is the attribute path of environment.systemPackages.
func (l *Lister) systemInstallable() string {
	return fmt.Sprintf("%s#nixosConfigurations.%s.config.environment.systemPackages", l.FlakeDir, l.Host)
}

func (l *Lister) homeInstallable() string {
	return fmt.Sprintf("%s#nixosConfigurations.%s.config.home-manager.users.%s.home.packages",
		l.FlakeDir, l.Host, l.HomeManagerUser)
}

// CacheKey identifies an evaluation result. It changes whenever the flake lock,
// nup's own lock or the host changes, which is exactly when the result can
// differ.
func (l *Lister) CacheKey() string {
	return nix.Key("pkgs",
		l.FlakeDir,
		l.Host,
		l.HomeManagerUser,
		nix.HashFile(l.FlakeDir+"/flake.lock"),
		nix.HashFile(lock.Path(l.FlakeDir)),
	)
}

// List evaluates the configuration and returns its packages, sorted by name.
// Results are cached because a NixOS evaluation takes tens of seconds.
func (l *Lister) List(ctx context.Context, lf *lock.File, mainRev string, opts Options) ([]Package, error) {
	key := l.CacheKey()
	cache := l.Cache
	if opts.Refresh && cache != nil {
		cache = nix.NewCache(cache.Dir, true)
	}

	var pkgs []Package
	if cache != nil && cache.Get(key, 0, &pkgs) && len(pkgs) > 0 {
		return annotate(pkgs, lf, mainRev), nil
	}

	system, err := l.eval(ctx, l.systemInstallable(), SourceSystem)
	if err != nil {
		return nil, fmt.Errorf("evaluating environment.systemPackages for host %q: %w", l.Host, err)
	}
	pkgs = system

	if l.HomeManagerUser != "" {
		home, err := l.eval(ctx, l.homeInstallable(), SourceHome)
		if err != nil {
			return nil, fmt.Errorf("evaluating home-manager packages for user %q: %w "+
				"(if this host has no home-manager module, set home-manager.enable: false in your nup config)",
				l.HomeManagerUser, err)
		}
		pkgs = append(pkgs, home...)
	}

	pkgs = dedupe(pkgs)
	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].Name != pkgs[j].Name {
			return pkgs[i].Name < pkgs[j].Name
		}
		return pkgs[i].Version < pkgs[j].Version
	})

	if l.Cache != nil {
		_ = l.Cache.Put(key, pkgs)
	}
	return annotate(pkgs, lf, mainRev), nil
}

func (l *Lister) eval(ctx context.Context, installable string, src Source) ([]Package, error) {
	var rows []raw
	if err := l.Nix.EvalJSON(ctx, installable, applyExpr, &rows, nix.Impure(l.Impure)); err != nil {
		return nil, err
	}
	out := make([]Package, 0, len(rows))
	for _, r := range rows {
		var name, version string
		if r.Pname != nil && *r.Pname != "" {
			name = *r.Pname
		} else {
			name, version = SplitName(r.Name)
		}
		if r.Version != nil && *r.Version != "" {
			version = *r.Version
		}
		if name == "" {
			continue
		}
		out = append(out, Package{
			Name:        name,
			Version:     version,
			Attr:        name,
			Source:      src,
			OutPath:     r.Out,
			Changelog:   deref(r.Changelog),
			Homepage:    deref(r.Homepage),
			Description: deref(r.Description),
		})
	}
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// annotate merges lock state into a package list. It runs after the cache so a
// pin change is visible immediately without re-evaluating.
func annotate(pkgs []Package, lf *lock.File, mainRev string) []Package {
	out := make([]Package, len(pkgs))
	copy(out, pkgs)
	if lf == nil {
		return out
	}
	// Index pins by their last attribute component so `python3Packages.requests`
	// still matches the package named `requests`.
	byLeaf := map[string]lock.Package{}
	for attr, p := range lf.Packages {
		byLeaf[leaf(attr)] = p
	}
	for i := range out {
		p, ok := lf.Get(out[i].Attr)
		if !ok {
			p, ok = byLeaf[out[i].Name]
		}
		if !ok {
			continue
		}
		out[i].Attr = p.Attr
		out[i].PinnedRev = p.Rev
		out[i].Stale = mainRev != "" && p.Rev != mainRev
	}
	return out
}

func leaf(attr string) string {
	if i := strings.LastIndex(attr, "."); i >= 0 {
		return attr[i+1:]
	}
	return attr
}

// dedupe keeps one entry per name and version. Multi-output packages appear in
// systemPackages once per output (glibc.bin, glibc.out, ...), and a package can
// be installed from both the system and home-manager; either way the user wants
// a single row. The system source wins when both are present.
func dedupe(pkgs []Package) []Package {
	seen := map[string]int{}
	var out []Package
	for _, p := range pkgs {
		k := p.Name + "\x00" + p.Version
		if i, ok := seen[k]; ok {
			if out[i].Source == SourceHome && p.Source == SourceSystem {
				out[i].Source = SourceSystem
			}
			continue
		}
		seen[k] = len(out)
		out = append(out, p)
	}
	return out
}

// SplitName splits a derivation name like "ripgrep-14.1.1" into its pname and
// version. The version starts at the last dash that is followed by a digit.
func SplitName(name string) (pname, version string) {
	for i := len(name) - 1; i > 0; i-- {
		if name[i] != '-' {
			continue
		}
		if i+1 < len(name) && name[i+1] >= '0' && name[i+1] <= '9' {
			return name[:i], name[i+1:]
		}
	}
	return name, ""
}

// Find returns the packages whose name matches exactly.
func Find(pkgs []Package, name string) []Package {
	var out []Package
	for _, p := range pkgs {
		if p.Name == name || p.Attr == name {
			out = append(out, p)
		}
	}
	return out
}

// DefaultTimeout bounds a single evaluation; NixOS configurations are slow but
// not unbounded.
const DefaultTimeout = 10 * time.Minute

// Settings is the platform and the nixpkgs configuration a flake evaluates
// with. nup reuses them when it looks a candidate package up in another nixpkgs
// revision, so the lookup sees the same allowUnfree, permittedInsecurePackages
// and friends the system does.
type Settings struct {
	System string         `json:"system"`
	Config map[string]any `json:"config"`
}

// settingsExpr keeps only JSON-representable values. Predicates such as
// allowUnfreePredicate are functions and cannot cross the process boundary;
// they are dropped rather than guessed at.
func settingsExpr() string {
	quoted := make([]string, 0, len(overlay.InheritedConfigKeys))
	for _, k := range overlay.InheritedConfigKeys {
		quoted = append(quoted, strconv.Quote(k))
	}
	return `p: {
  system = p.stdenv.hostPlatform.system;
  config = builtins.listToAttrs (builtins.concatMap (k:
    let v = p.config.${k} or null; in
    if v != null && (builtins.isBool v || builtins.isString v || builtins.isInt v
                     || (builtins.isList v && builtins.all builtins.isString v))
    then [ { name = k; value = v; } ]
    else [ ]) [ ` + strings.Join(quoted, " ") + ` ]);
}`
}

// Settings evaluates the flake's platform and nixpkgs config. The result is
// cached: it only changes when the flake's inputs change.
func (l *Lister) Settings(ctx context.Context, opts Options) (*Settings, error) {
	key := nix.Key("settings", l.FlakeDir, l.Host, nix.HashFile(filepath.Join(l.FlakeDir, "flake.lock")))

	var s Settings
	if !opts.Refresh && l.Cache.Get(key, 0, &s) && s.System != "" {
		return &s, nil
	}
	installable := fmt.Sprintf("%s#nixosConfigurations.%s.pkgs", l.FlakeDir, l.Host)
	if err := l.Nix.EvalJSON(ctx, installable, settingsExpr(), &s, nix.Impure(l.Impure)); err != nil {
		return nil, fmt.Errorf("reading the nixpkgs configuration of host %q: %w", l.Host, err)
	}
	if s.Config == nil {
		s.Config = map[string]any{}
	}
	if l.Cache != nil {
		_ = l.Cache.Put(key, s)
	}
	return &s, nil
}
