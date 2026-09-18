package nix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// TarballURL is the archive nup pins against. GitHub's archive tarball of a
// commit unpacks to the same tree that `nix flake metadata` hashes, which is
// why the flake's narHash can be reused verbatim as fetchTarball's sha256.
func TarballURL(rev string) string {
	return fmt.Sprintf("https://github.com/NixOS/nixpkgs/archive/%s.tar.gz", rev)
}

// FlakeRef builds a nixpkgs flake reference for a revision or branch.
func FlakeRef(ref string) string { return "github:NixOS/nixpkgs/" + ref }

// Client is the typed interface to the nix CLI.
type Client struct {
	Runner Runner
	// Bin is the nix executable, "nix" unless overridden.
	Bin string
}

// NewClient returns a client using the given runner.
func NewClient(r Runner) *Client { return &Client{Runner: r, Bin: "nix"} }

func (c *Client) bin() string {
	if c.Bin == "" {
		return "nix"
	}
	return c.Bin
}

// args prefixes every invocation with the flags nup relies on, so nup works
// even if the user has not enabled flakes globally.
func (c *Client) args(rest ...string) []string {
	return append([]string{
		"--extra-experimental-features", "nix-command flakes",
	}, rest...)
}

func (c *Client) run(ctx context.Context, rest ...string) (*Result, error) {
	return c.Runner.Run(ctx, c.bin(), c.args(rest...))
}

// EvalOption tunes a single evaluation.
type EvalOption func(*evalOpts)

type evalOpts struct{ impure bool }

// Impure adds --impure. Some configurations genuinely need it, for example when
// a module reads an absolute path or fetches without a hash; nup only applies it
// to evaluations of the user's own flake, never to nixpkgs lookups.
func Impure(on bool) EvalOption {
	return func(o *evalOpts) { o.impure = on }
}

func apply(opts []EvalOption) evalOpts {
	var o evalOpts
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

func (o evalOpts) extend(args []string) []string {
	if o.impure {
		return append(args, "--impure")
	}
	return args
}

// EvalJSON evaluates an installable and decodes the JSON result into v.
// apply may be empty.
func (c *Client) EvalJSON(ctx context.Context, installable, applyExpr string, v any, opts ...EvalOption) error {
	args := []string{"eval", "--json", installable}
	if applyExpr != "" {
		args = append(args, "--apply", applyExpr)
	}
	res, err := c.run(ctx, apply(opts).extend(args)...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(res.Stdout), v); err != nil {
		return fmt.Errorf("decoding `nix eval --json %s`: %w", installable, err)
	}
	return nil
}

// EvalExprJSON evaluates a Nix expression and decodes the JSON result.
// applyExpr may be empty.
func (c *Client) EvalExprJSON(ctx context.Context, expr, applyExpr string, v any, opts ...EvalOption) error {
	args := []string{"eval", "--json", "--expr", expr}
	if applyExpr != "" {
		args = append(args, "--apply", applyExpr)
	}
	res, err := c.run(ctx, apply(opts).extend(args)...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(res.Stdout), v); err != nil {
		return fmt.Errorf("decoding `nix eval --json --expr`: %w", err)
	}
	return nil
}

// EvalRaw evaluates an installable to a bare string, e.g. a version.
func (c *Client) EvalRaw(ctx context.Context, installable string, opts ...EvalOption) (string, error) {
	res, err := c.run(ctx, apply(opts).extend([]string{"eval", "--raw", installable})...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// String renders s as a Nix double-quoted string literal. It is used to pass
// JSON into an expression without another round of quoting rules.
func String(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"$", `\$`,
		"\n", `\n`,
		"\t", `\t`,
	)
	return `"` + r.Replace(s) + `"`
}

// Metadata is the subset of `nix flake metadata --json` nup needs.
type Metadata struct {
	Revision     string
	NarHash      string
	LastModified time.Time
}

type flakeMetadata struct {
	Locked struct {
		Rev          string `json:"rev"`
		NarHash      string `json:"narHash"`
		LastModified int64  `json:"lastModified"`
	} `json:"locked"`
	Revision string `json:"revision"`
}

// FlakeMetadata resolves a flake reference to a concrete revision and hash.
func (c *Client) FlakeMetadata(ctx context.Context, ref string) (*Metadata, error) {
	res, err := c.run(ctx, "flake", "metadata", ref, "--json")
	if err != nil {
		return nil, err
	}
	var m flakeMetadata
	if err := json.Unmarshal([]byte(res.Stdout), &m); err != nil {
		return nil, fmt.Errorf("decoding `nix flake metadata %s --json`: %w", ref, err)
	}
	rev := m.Locked.Rev
	if rev == "" {
		rev = m.Revision
	}
	if rev == "" {
		return nil, fmt.Errorf("`nix flake metadata %s` returned no revision", ref)
	}
	return &Metadata{
		Revision:     rev,
		NarHash:      m.Locked.NarHash,
		LastModified: time.Unix(m.Locked.LastModified, 0),
	}, nil
}

// PrefetchTarball downloads a tarball and returns its unpacked SRI hash. This is
// the fallback for the rare case where a flake's narHash does not match the
// GitHub archive.
func (c *Client) PrefetchTarball(ctx context.Context, url string) (string, error) {
	res, err := c.run(ctx, "store", "prefetch-file", "--unpack", "--json", "--hash-type", "sha256", url)
	if err != nil {
		return "", err
	}
	var out struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		return "", fmt.Errorf("decoding `nix store prefetch-file --json`: %w", err)
	}
	if out.Hash == "" {
		return "", fmt.Errorf("`nix store prefetch-file %s` returned no hash", url)
	}
	return out.Hash, nil
}

// CheckTarball verifies that a url/hash pair actually resolves, which is what
// the generated overlay will do at evaluation time.
func (c *Client) CheckTarball(ctx context.Context, url, sha256 string) error {
	expr := fmt.Sprintf(`builtins.fetchTarball { url = %q; sha256 = %q; }`, url, sha256)
	_, err := c.run(ctx, "eval", "--raw", "--expr", expr)
	return err
}

// Build realises an installable and returns its output paths.
func (c *Client) Build(ctx context.Context, installable string) ([]string, error) {
	return c.build(ctx, installable, []string{"build", "--no-link", "--print-out-paths", installable})
}

// BuildExpr realises a Nix expression and returns its output paths.
func (c *Client) BuildExpr(ctx context.Context, expr string) ([]string, error) {
	return c.build(ctx, "--expr", []string{"build", "--no-link", "--print-out-paths", "--expr", expr})
}

// build runs a build command and collects the printed store paths. what only
// names the target in the error message.
func (c *Client) build(ctx context.Context, what string, args []string) ([]string, error) {
	res, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, l := range strings.Split(res.Stdout, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			paths = append(paths, l)
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("`nix build %s` produced no output path", what)
	}
	return paths, nil
}

// DiffClosures returns the raw output of `nix store diff-closures`.
func (c *Client) DiffClosures(ctx context.Context, before, after string) (string, error) {
	res, err := c.run(ctx, "store", "diff-closures", before, after)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// ClosureSize returns the total closure size of a store path in bytes.
func (c *Client) ClosureSize(ctx context.Context, path string) (int64, error) {
	res, err := c.run(ctx, "path-info", "-S", "--json", path)
	if err != nil {
		return 0, err
	}
	return parseClosureSize([]byte(res.Stdout), path)
}

// parseClosureSize understands both JSON shapes nix has used: a map keyed by
// store path (format 1) and a list of objects (format 2).
func parseClosureSize(data []byte, path string) (int64, error) {
	type entry struct {
		Path        string `json:"path"`
		ClosureSize int64  `json:"closureSize"`
	}

	var asMap map[string]entry
	if err := json.Unmarshal(data, &asMap); err == nil {
		if e, ok := asMap[path]; ok {
			return e.ClosureSize, nil
		}
		for _, e := range asMap {
			return e.ClosureSize, nil
		}
		return 0, fmt.Errorf("`nix path-info -S` returned no entry for %s", path)
	}

	var asList []entry
	if err := json.Unmarshal(data, &asList); err == nil && len(asList) > 0 {
		for _, e := range asList {
			if e.Path == path {
				return e.ClosureSize, nil
			}
		}
		return asList[0].ClosureSize, nil
	}
	return 0, fmt.Errorf("could not decode `nix path-info -S --json` output for %s", path)
}

// AttrExists reports whether an attribute path resolves in a flake.
func (c *Client) AttrExists(ctx context.Context, flakeRef, attr string) (bool, error) {
	res, err := c.Runner.Run(ctx, c.bin(), c.args("eval", "--json",
		fmt.Sprintf("%s#%s", flakeRef, attr), "--apply", "p: true"))
	if err == nil {
		return strings.Contains(res.Stdout, "true"), nil
	}
	// A missing attribute is an expected answer, not a failure worth reporting.
	if isMissingAttr(res, err) {
		return false, nil
	}
	return false, err
}

func isMissingAttr(res *Result, err error) bool {
	var s string
	if res != nil {
		s = res.Stderr
	}
	var ne *Error
	if s == "" && errors.As(err, &ne) {
		s = ne.Stderr
	}
	return strings.Contains(s, "does not provide attribute") ||
		strings.Contains(s, "does not exist") ||
		(strings.Contains(s, "attribute '") && strings.Contains(s, "' missing"))
}

// Rebuild streams an arbitrary command (nixos-rebuild, nh, nixos-rebuild piped
// into nom, ...) straight through to the user's terminal.
func (c *Client) Rebuild(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty rebuild command")
	}
	return c.Runner.Stream(ctx, argv[0], argv[1:], stdin, stdout, stderr)
}
