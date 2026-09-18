package nix

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Cache is a small JSON file cache under $XDG_CACHE_HOME/nup. Evaluating a
// NixOS configuration takes tens of seconds, so results are keyed by the inputs
// that can change them and reused until one of those changes.
type Cache struct {
	Dir string
	// Disabled short-circuits every read, for --refresh.
	Disabled bool
}

// DefaultDir is $XDG_CACHE_HOME/nup, falling back to ~/.cache/nup.
func DefaultDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "nup")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "nup-cache")
	}
	return filepath.Join(home, ".cache", "nup")
}

// NewCache returns a cache rooted at dir; an empty dir uses DefaultDir.
func NewCache(dir string, disabled bool) *Cache {
	if dir == "" {
		dir = DefaultDir()
	}
	return &Cache{Dir: dir, Disabled: disabled}
}

type envelope struct {
	StoredAt time.Time       `json:"storedAt"`
	Payload  json.RawMessage `json:"payload"`
}

func (c *Cache) path(key string) string { return filepath.Join(c.Dir, key+".json") }

// Get decodes a cached value. ok is false when the entry is missing, unreadable
// or older than ttl (ttl <= 0 means it never expires).
func (c *Cache) Get(key string, ttl time.Duration, v any) bool {
	if c == nil || c.Disabled {
		return false
	}
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return false
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return false
	}
	if ttl > 0 && time.Since(env.StoredAt) > ttl {
		return false
	}
	return json.Unmarshal(env.Payload, v) == nil
}

// Put stores a value. Cache failures are never fatal, so callers may ignore the
// error, but it is returned for tests.
func (c *Cache) Put(key string, v any) error {
	if c == nil {
		return nil
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("creating cache dir %s: %w", c.Dir, err)
	}
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding cache entry %s: %w", key, err)
	}
	data, err := json.Marshal(envelope{StoredAt: time.Now(), Payload: payload})
	if err != nil {
		return fmt.Errorf("encoding cache entry %s: %w", key, err)
	}
	tmp, err := os.CreateTemp(c.Dir, "."+key+".*")
	if err != nil {
		return fmt.Errorf("creating cache temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.path(key))
}

// Clear removes the whole cache directory.
func (c *Cache) Clear() error {
	err := os.RemoveAll(c.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Key hashes the given parts into a short, filename-safe cache key.
func Key(prefix string, parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return prefix + "-" + hex.EncodeToString(h.Sum(nil))[:16]
}

// HashFile returns a hash of a file's contents, or a marker when it is absent.
func HashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "missing"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
