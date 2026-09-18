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

type flakeLockNode struct {
	Inputs map[string]json.RawMessage `json:"inputs"`
	Locked struct {
		Rev          string `json:"rev"`
		NarHash      string `json:"narHash"`
		LastModified int64  `json:"lastModified"`
	} `json:"locked"`
}

type flakeLockFile struct {
	Root  string                   `json:"root"`
	Nodes map[string]flakeLockNode `json:"nodes"`
}

// ReadFlakeLockInput reads one of the flake's own inputs out of flake.lock. It
// is used to learn which nixpkgs revision the flake is on, so nup can tell
// whether a pin is ahead of or behind the main input. A missing lock file or a
// missing input is not an error.
//
// The input is resolved through the root node's input map, not by node name: a
// flake whose own nixpkgs is node "nixpkgs_2" (because a transitive input
// already claimed "nixpkgs") would otherwise be read from the wrong node.
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

	root := f.Root
	if root == "" {
		root = "root"
	}
	name, ok := f.resolve(root, input)
	if !ok {
		// Older lock files, or a hand-written one, may not list root inputs.
		name = input
	}
	node, ok := f.Nodes[name]
	if !ok {
		return nil, nil
	}
	return &FlakeLockInput{
		Rev:          node.Locked.Rev,
		NarHash:      node.Locked.NarHash,
		LastModified: time.Unix(node.Locked.LastModified, 0),
	}, nil
}

// resolve maps an input of a node to the node that holds it. An input is either
// a node name, or a "follows" path to walk from the root.
func (f *flakeLockFile) resolve(from, input string) (string, bool) {
	node, ok := f.Nodes[from]
	if !ok {
		return "", false
	}
	raw, ok := node.Inputs[input]
	if !ok {
		return "", false
	}

	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		return name, true
	}

	var path []string
	if err := json.Unmarshal(raw, &path); err != nil || len(path) == 0 {
		return "", false
	}
	root := f.Root
	if root == "" {
		root = "root"
	}
	current := root
	for i, step := range path {
		next, ok := f.resolve(current, step)
		if !ok {
			return "", false
		}
		if i == len(path)-1 {
			return next, true
		}
		current = next
	}
	return "", false
}
