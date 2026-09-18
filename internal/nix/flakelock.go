package nix

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// FlakeLockInput is one resolved input of a flake.lock.
type FlakeLockInput struct {
	Rev          string
	NarHash      string
	LastModified time.Time
}

type flakeLockFile struct {
	Root  string `json:"root"`
	Nodes map[string]struct {
		Locked struct {
			Rev          string `json:"rev"`
			NarHash      string `json:"narHash"`
			LastModified int64  `json:"lastModified"`
		} `json:"locked"`
	} `json:"nodes"`
}

// ReadFlakeLockInput reads one input out of a flake.lock. It is used to learn
// which nixpkgs revision the flake itself is on, so nup can tell whether a pin
// is ahead of or behind the main input. A missing lock file is not an error.
func ReadFlakeLockInput(flakeDir, input string) (*FlakeLockInput, error) {
	path := filepath.Join(flakeDir, "flake.lock")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var f flakeLockFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	node, ok := f.Nodes[input]
	if !ok {
		return nil, nil
	}
	return &FlakeLockInput{
		Rev:          node.Locked.Rev,
		NarHash:      node.Locked.NarHash,
		LastModified: time.Unix(node.Locked.LastModified, 0),
	}, nil
}
