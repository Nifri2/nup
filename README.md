# nup

Update **single packages** in a NixOS flake without moving the whole `nixpkgs` input.

`nix flake update` can only update a complete input. If you want a newer `ripgrep`
you have to rebuild against a newer nixpkgs for everything else too. nup keeps the
main nixpkgs where it is and pins individual packages to their own revision.

```
$ nup update ripgrep

ripgrep  14.1.1 → 15.2.0  MAJOR
  nixpkgs    ac62194 → b1b8759  (2 days ago)
  changelog  https://github.com/BurntSushi/ripgrep/releases/tag/15.2.0

  [U] glibc         2.40-66 → 2.42-84  +4.6 MiB
  [U] ripgrep       14.1.1 → 15.2.0  +1.1 MiB
  [A] libgit2       1.9.0
  [R] pcre2         10.43

  closure    49.2 MiB → 54.9 MiB  +5.7 MiB

Apply 1 change(s) to /etc/nixos/nup.lock.json? [y/N]
```

## How it works

nup owns exactly two files in your flake directory:

- **`nup.lock.json`** — plain JSON: for every pinned package its nixpkgs attribute
  path, the commit, a `fetchTarball`-compatible hash, the version and when it was
  pinned.
- **`nup-overlay.nix`** — a generated overlay that reads that JSON with
  `builtins.fromJSON`, imports nixpkgs once per distinct revision via
  `builtins.fetchTarball`, and replaces the affected attributes.

Because every fetch carries its hash, the overlay **evaluates in pure mode** — no
`--impure`, no `--no-pure-eval`. Packages you have not pinned are untouched and
still come from your own nixpkgs input.

When nup looks a candidate up in another revision it performs *the same import*
the overlay does — same tarball, same platform, same nixpkgs config subset
(`allowUnfree`, `permittedInsecurePackages`, …). What nup shows you is therefore
what your system will actually build, and unfree packages resolve exactly as they
do in your configuration.

nup reads and writes only JSON. It never parses Nix code and never edits your
`flake.nix` — you wire the overlay in once, by hand.

## Installation

### Run it without installing

```sh
nix run github:Nifri2/nup            # TUI
nix run github:Nifri2/nup -- list    # any subcommand
```

### Add it to your system

```nix
{
  inputs.nup.url = "github:Nifri2/nup";

  # in a NixOS module:
  environment.systemPackages = [ inputs.nup.packages.${pkgs.system}.default ];
}
```

There is also `overlays.default`, which adds `pkgs.nup`.

### From source

```sh
git clone https://github.com/Nifri2/nup && cd nup
task build     # or: go build -o nup .
```

## Setup

```sh
cd /etc/nixos      # or wherever your flake lives
nup init
```

`init` creates `nup.lock.json`, `nup-overlay.nix` and `~/.config/nup/config.yaml`,
and prints the snippet you need to add **once** to a NixOS module:

```nix
{
  nixpkgs.overlays = [ (import ./nup-overlay.nix) ];
}
```

> **Git flakes:** nix only sees files that git knows about. If your flake is a git
> repository, the generated files must be tracked or nix will silently ignore your
> pins. `nup init` detects this and offers to `git add` them; every other command
> warns if they are missing.

## Commands

| Command | What it does |
| --- | --- |
| `nup` | Start the TUI |
| `nup list` | List every installed package |
| `nup query <string>` | Search by name or attribute path, case-insensitive substring |
| `nup query -r <regex>` | Same, but as a regular expression |
| `nup update <pkg>...` | Update one or more packages to the head of the branch |
| `nup outdated` | Show packages with a newer version in nixpkgs |
| `nup pin <pkg> <rev>` | Pin a package to a specific nixpkgs commit |
| `nup reset <pkg>...` | Remove pins; the package comes from the main nixpkgs again |
| `nup init` | Create the lock file, the overlay and a config file |
| `nup completion <shell>` | Shell completions for bash, zsh, fish or powershell |

### Global flags

| Flag | Meaning |
| --- | --- |
| `-o, --output table\|json\|yaml` | Output format (default `table`) |
| `--flake <path>` | Flake directory. Default: config, else the current directory if it has a `flake.nix`, else `/etc/nixos` |
| `--host <name>` | `nixosConfigurations` attribute. Default: your hostname |
| `--refresh` | Ignore cached evaluation results |
| `--no-color` | Disable color (`NO_COLOR` is honored too, and color turns off automatically without a TTY) |
| `--impure` | Evaluate the configuration with `--impure` |

### Flags for `update` and `pin`

| Flag | Meaning |
| --- | --- |
| `-y, --yes` | No confirmation prompt |
| `--dry-run` | Show what would change, write nothing |
| `--switch` / `--boot` / `--no-rebuild` | What happens after the update (default from config, otherwise nup asks) |
| `--branch <name>` | nixpkgs branch to follow (default `nixos-unstable`) |
| `--attr <path>` | The nixpkgs attribute path, when it differs from the package name |
| `--force` | Update even when the version is unchanged |
| `--commit` | Commit the lock file change with git |

## Examples

```sh
# What is installed, and what is pinned?
nup list

# Find something
nup query rg
nup query '^(rip)?grep$' -r

# See what an update would do, without writing anything
nup update ripgrep --dry-run

# Update two packages: both diffs, one confirmation, one rebuild
nup update ripgrep jq

# Update and commit the lock file, no questions asked
nup update ripgrep --yes --commit --switch

# Pin to an exact commit, for example to roll back
nup pin ripgrep 11cb3517b3af6af300dd6c055aeda73c9bf52c48

# Back to the main nixpkgs
nup reset ripgrep

# Machine readable
nup list -o json | jq '.[] | select(.stale)'
```

The `PINNED` column shows the short revision, or `-`. A pin on a different
revision than your flake's own nixpkgs is highlighted in yellow and carries
`"stale": true` in JSON and YAML.

### Attribute paths

The name a package reports is not always its attribute in nixpkgs. nup tries the
name first and, if that does not resolve, tells you to be explicit:

```sh
nup update ripgrep --attr ripgrep
nup update requests --attr python3Packages.requests
```

The attribute path is stored in the lock file, so you only have to say it once.

## TUI

`nup` with no arguments opens the interactive interface. The package list loads in
the background with a spinner; the UI never blocks.

| Key | Action |
| --- | --- |
| `/` | Open the filter, `esc` closes it |
| `j` `k` `↑` `↓` | Move |
| `g` `G` | Jump to start / end |
| `space` | Toggle the selection of a row |
| `a` / `A` | Select all visible rows / clear the selection |
| `enter` | Update the selection, or the current row if nothing is selected |
| `u` | Check in the background which packages are outdated and fill `LATEST` |
| `r` | Remove the pin of the selection |
| `R` | Reload the package list |
| `?` | Help |
| `q` | Quit |

After `enter` you get the same diff the CLI shows, a `y`/`n` confirmation, and then
the rebuild log in a scrollable viewport.

## Configuration

`~/.config/nup/config.yaml` (respects `XDG_CONFIG_HOME`). Flags always win.

```yaml
flake: /etc/nixos
host: nixos
branch: nixos-unstable

home-manager:
  enable: false
  user: ""

# {action} is switch or boot, {flake} the flake path, {host} the configuration
rebuild-command: [sudo, nixos-rebuild, "{action}", --flake, "{flake}#{host}"]

# switch | boot | none | ask
after-update: ask

auto-commit: false
output: table

# Some configurations genuinely need impure evaluation, for example when a
# module reads an absolute path or fetches without a hash.
impure: false
```

Using [`nh`](https://github.com/nix-community/nh) instead:

```yaml
rebuild-command: [nh, os, "{action}", "{flake}", --hostname, "{host}"]
```

With `nix-output-monitor`:

```yaml
rebuild-command: [sudo, nixos-rebuild, "{action}", --flake, "{flake}#{host}", --log-format, internal-json, -v]
```

### home-manager

Turn it on to include `home.packages` as well:

```yaml
home-manager:
  enable: true
  user: niklas
```

The `SOURCE` column then shows `system` or `home`. This reads
`config.home-manager.users.<user>.home.packages`, so it only works when
home-manager is a NixOS module of the same configuration.

## Caching

Evaluating a NixOS configuration takes tens of seconds, so nup caches the result in
`$XDG_CACHE_HOME/nup`. The key is built from the hash of `flake.lock`, the hash of
`nup.lock.json` and the host — exactly the inputs that can change the answer.
Resolved revision hashes and branch heads are cached too. `--refresh` skips all of
it.

## Caveats

- **Binary cache.** A pinned package is built from a different nixpkgs revision.
  If `cache.nixos.org` has no build for that exact revision, it is compiled
  locally. `nup update` shows the closure diff before you commit to it.
- **Mixed closures.** A pinned package brings along its own glibc and friends when
  its revision differs from yours. The summary shows exactly how much that costs.
- **Nested attributes.** `python3Packages.requests` works, but overriding inside a
  package set does not propagate into things built from that set, such as
  `python3.withPackages`. Pin the top-level package where you can.
- **GitHub only.** The overlay fetches from `github.com/NixOS/nixpkgs`. Forks and
  other mirrors are not supported yet.
- **Wrapped packages.** If your configuration installs an override or wrapper —
  `vscode-with-extensions.override { ... }` rather than plain `vscode` — nup
  reports the version bump but skips the closure diff, because comparing a
  wrapper against the bare attribute would list every wrapped-in dependency as
  removed. The pin still reaches the wrapper as long as it takes the attribute
  from the package set, which `callPackage`-based wrappers in nixpkgs do.
- **Config predicates.** `allowUnfreePredicate` and friends are functions and
  cannot be carried into the pinned import; plain `allowUnfree` and
  `permittedInsecurePackages` are.
- **Impure configurations.** If your flake needs `--impure` — an `IFD`, a module
  reading an absolute path, `builtins.fetchTarball` without a hash — set
  `impure: true` in the config or pass `--impure`. nup only applies it to your own
  flake; the nixpkgs lookups it does stay pure.

## Development

```sh
nix develop          # go, gopls, go-task, golangci-lint
task                 # list the tasks
task build
task test
task test:cover
task ci              # fmt, vet, test, nix build
task nix:vendor-hash # after changing dependencies
```

Layout:

```
cmd/              cobra commands, no update logic of their own
internal/config   config.yaml
internal/lock     nup.lock.json, written atomically
internal/nix      every external command, behind a Runner interface
internal/overlay  the generated nup-overlay.nix
internal/pkgset   which packages a configuration installs
internal/version  nixpkgs-compatible version comparison
internal/diff     parsing nix store diff-closures
internal/update   the update engine, shared by the CLI and the TUI
internal/output   table, JSON and YAML rendering
internal/ui       lipgloss styles and the update summary
internal/tui      bubbletea interface
```

Every call to an external command goes through `nix.Runner`, so the tests drive the
whole pipeline against a fake and need neither nix nor a network.

## License

MIT
