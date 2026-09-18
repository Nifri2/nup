// Package lock reads and writes nup.lock.json, the only file nup owns besides
// the generated overlay. It is plain JSON on purpose: the overlay reads it with
// builtins.fromJSON, so nup never has to parse or rewrite Nix code.
package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Name is the file name nup uses inside the flake directory.
const Name = "nup.lock.json"

// CurrentVersion is the schema version nup writes.
const CurrentVersion = 1

// Package is one pinned package.
type Package struct {
	// Attr is the attribute path inside nixpkgs, e.g. "ripgrep".
	Attr string `json:"attr"`
	// Rev is the nixpkgs commit the package is taken from.
	Rev string `json:"rev"`
	// Sha256 is the hash of the unpacked tarball, usable by fetchTarball.
	Sha256 string `json:"sha256"`
	// Version is informational: what nup saw when it wrote the pin.
	Version string `json:"version,omitempty"`
	// Branch records where Rev came from, so `nup update` can follow it again.
	Branch string `json:"branch,omitempty"`
	// PinnedAt is when the pin was written.
	PinnedAt time.Time `json:"pinnedAt"`
}

// File is the whole lock file.
type File struct {
	Version  int                `json:"version"`
	Packages map[string]Package `json:"packages"`
}

// New returns an empty lock file.
func New() *File {
	return &File{Version: CurrentVersion, Packages: map[string]Package{}}
}

// Path returns the lock file path for a flake directory.
func Path(flakeDir string) string { return filepath.Join(flakeDir, Name) }

// Load reads the lock file. A missing file is not an error; it yields an empty
// lock so every command works before `nup init` has run.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if f.Packages == nil {
		f.Packages = map[string]Package{}
	}
	if f.Version == 0 {
		f.Version = CurrentVersion
	}
	if f.Version > CurrentVersion {
		return nil, fmt.Errorf("%s has schema version %d, but this nup only understands up to %d; please upgrade nup", path, f.Version, CurrentVersion)
	}
	return &f, nil
}

// Save writes the lock file atomically: a temporary file in the same directory
// followed by a rename, so a crash can never leave a half-written lock behind.
func (f *File) Save(path string) error {
	if f.Packages == nil {
		f.Packages = map[string]Package{}
	}
	f.Version = CurrentVersion

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding lock file: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".nup.lock.*.json")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	// Match the permissions of a regular repository file rather than 0600.
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmpName, path, err)
	}
	return nil
}

// Get looks a package up by attribute path.
func (f *File) Get(attr string) (Package, bool) {
	p, ok := f.Packages[attr]
	return p, ok
}

// Set records or replaces a pin.
func (f *File) Set(p Package) {
	if f.Packages == nil {
		f.Packages = map[string]Package{}
	}
	if p.PinnedAt.IsZero() {
		p.PinnedAt = time.Now().UTC()
	}
	f.Packages[p.Attr] = p
}

// Remove drops a pin and reports whether it existed.
func (f *File) Remove(attr string) bool {
	if _, ok := f.Packages[attr]; !ok {
		return false
	}
	delete(f.Packages, attr)
	return true
}

// Attrs returns all pinned attribute paths in sorted order.
func (f *File) Attrs() []string {
	out := make([]string, 0, len(f.Packages))
	for a := range f.Packages {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// Revs returns the distinct revisions in use, sorted.
func (f *File) Revs() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range f.Packages {
		if !seen[p.Rev] {
			seen[p.Rev] = true
			out = append(out, p.Rev)
		}
	}
	sort.Strings(out)
	return out
}

// ShortRev shortens a commit hash for display.
func ShortRev(rev string) string {
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}
