// Package overlay owns the generated nup-overlay.nix.
package overlay

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// Name is the file name nup writes into the flake directory.
const Name = "nup-overlay.nix"

//go:embed nup-overlay.nix
var source string

// Source returns the overlay text nup generates.
func Source() string { return source }

// Path returns the overlay path for a flake directory.
func Path(flakeDir string) string { return filepath.Join(flakeDir, Name) }

// Write writes the overlay. Without force an existing file is left alone so a
// user's local edits are never clobbered silently.
func Write(path string, force bool) (written bool, err error) {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return false, nil
		} else if !os.IsNotExist(err) {
			return false, fmt.Errorf("checking %s: %w", path, err)
		}
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// Snippet is what `nup init` prints for the user to paste into their config.
const Snippet = `  nixpkgs.overlays = [ (import ./nup-overlay.nix) ];`

// InheritedConfigKeys are the nixpkgs config options carried from the main
// nixpkgs into a pinned import. They are the single source of truth: the
// generated overlay lists exactly these, and nup uses the same set when it
// evaluates or builds a candidate package, so what nup shows is what the
// system will get.
//
// `pkgs.config` is the *evaluated* config and also contains module outputs,
// nulls and functions, none of which a fresh `import nixpkgs` accepts, so it
// can never be forwarded wholesale.
var InheritedConfigKeys = []string{
	"allowAliases",
	"allowBroken",
	"allowInsecurePredicate",
	"allowUnfree",
	"allowUnfreePredicate",
	"allowUnsupportedSystem",
	"android_sdk",
	"checkMeta",
	"cudaCapabilities",
	"cudaForwardCompat",
	"cudaSupport",
	"joypixels",
	"permittedInsecurePackages",
	"rocmSupport",
	"segger-jlink",
}
