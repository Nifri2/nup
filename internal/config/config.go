// Package config loads ~/.config/nup/config.yaml. Every value can be
// overridden by a flag; the config only supplies defaults.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Action is what should happen after the lock file was updated.
type Action string

const (
	// ActionSwitch activates the new configuration immediately.
	ActionSwitch Action = "switch"
	// ActionBoot activates it on the next boot.
	ActionBoot Action = "boot"
	// ActionNone skips the rebuild entirely.
	ActionNone Action = "none"
	// ActionAsk means nup asks interactively.
	ActionAsk Action = "ask"
)

// DefaultBranch is the nixpkgs branch updates follow unless configured.
const DefaultBranch = "nixos-unstable"

// DefaultRebuildCommand is templated with {action}, {flake} and {host}.
var DefaultRebuildCommand = []string{"sudo", "nixos-rebuild", "{action}", "--flake", "{flake}#{host}"}

// RebuildTool is a rebuild front-end nup knows how to drive.
type RebuildTool struct {
	// Binary has to be on PATH for the tool to be chosen.
	Binary string
	// Command is the templated argv.
	Command []string
	// Note explains the choice to the user.
	Note string
}

// RebuildTools are tried in order when `nup init` writes a fresh config.
// nh comes first: someone who installed it wants to rebuild with it, and it
// elevates on its own, so no sudo prefix.
var RebuildTools = []RebuildTool{
	{
		Binary:  "nh",
		Command: []string{"nh", "os", "{action}", "{flake}", "--hostname", "{host}"},
		Note:    "nh handles privilege elevation itself, so there is no sudo in front",
	},
	{
		Binary:  "nixos-rebuild",
		Command: DefaultRebuildCommand,
	},
}

// LookPath resolves a binary, so tests can pretend a tool is or is not present.
type LookPath func(string) (string, error)

// DetectRebuildCommand returns the command for the first known tool on PATH,
// together with the tool. Detection happens once, in `nup init`, and the result
// is written to the config file: a tool that runs something under sudo should
// not silently change what it runs because PATH changed. When nothing is found
// it falls back to nixos-rebuild, which every NixOS system has.
func DetectRebuildCommand(look LookPath) ([]string, RebuildTool) {
	if look == nil {
		look = exec.LookPath
	}
	for _, t := range RebuildTools {
		if _, err := look(t.Binary); err == nil {
			return append([]string(nil), t.Command...), t
		}
	}
	fallback := RebuildTools[len(RebuildTools)-1]
	return append([]string(nil), fallback.Command...), fallback
}

// HomeManager configures the optional home-manager package source.
type HomeManager struct {
	Enable bool   `yaml:"enable"`
	User   string `yaml:"user"`
}

// Config mirrors config.yaml one-to-one.
type Config struct {
	Flake          string      `yaml:"flake"`
	Host           string      `yaml:"host"`
	Branch         string      `yaml:"branch"`
	HomeManager    HomeManager `yaml:"home-manager"`
	RebuildCommand []string    `yaml:"rebuild-command"`
	AfterUpdate    Action      `yaml:"after-update"`
	AutoCommit     bool        `yaml:"auto-commit"`
	Output         string      `yaml:"output"`
	// Impure evaluates the system configuration with --impure. Some flakes
	// genuinely need it, for example when a module reads an absolute path or
	// fetches without a hash.
	Impure bool `yaml:"impure"`
}

// Default returns the configuration used when no file exists.
func Default() *Config {
	return &Config{
		Branch:         DefaultBranch,
		RebuildCommand: append([]string(nil), DefaultRebuildCommand...),
		AfterUpdate:    ActionAsk,
		Output:         "table",
	}
}

// Dir is the directory holding config.yaml.
func Dir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "nup")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config/nup"
	}
	return filepath.Join(home, ".config", "nup")
}

// Path is the full path of config.yaml.
func Path() string { return filepath.Join(Dir(), "config.yaml") }

// Load reads the config file, filling in defaults for anything unset. A missing
// file yields the defaults without an error.
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	// Decode into a fresh value so defaults are not overwritten with zeroes.
	var file Config
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	merge(cfg, &file)
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func merge(dst, src *Config) {
	if src.Flake != "" {
		dst.Flake = src.Flake
	}
	if src.Host != "" {
		dst.Host = src.Host
	}
	if src.Branch != "" {
		dst.Branch = src.Branch
	}
	if len(src.RebuildCommand) > 0 {
		dst.RebuildCommand = src.RebuildCommand
	}
	if src.AfterUpdate != "" {
		dst.AfterUpdate = src.AfterUpdate
	}
	if src.Output != "" {
		dst.Output = src.Output
	}
	dst.HomeManager = src.HomeManager
	dst.AutoCommit = src.AutoCommit
	dst.Impure = src.Impure
}

// Validate rejects values that would only fail much later.
func (c *Config) Validate() error {
	switch c.AfterUpdate {
	case ActionSwitch, ActionBoot, ActionNone, ActionAsk:
	default:
		return fmt.Errorf("after-update: %q is not one of switch, boot, none, ask", c.AfterUpdate)
	}
	switch c.Output {
	case "table", "json", "yaml":
	default:
		return fmt.Errorf("output: %q is not one of table, json, yaml", c.Output)
	}
	if c.HomeManager.Enable && c.HomeManager.User == "" {
		return errors.New("home-manager.enable is true but home-manager.user is empty")
	}
	if len(c.RebuildCommand) == 0 {
		return errors.New("rebuild-command must not be empty")
	}
	return nil
}

// Save writes the config, creating the directory if needed.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	var buf bytes.Buffer
	buf.WriteString("# nup configuration. Flags on the command line override these values.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// RebuildArgs fills the placeholders in the configured rebuild command.
func (c *Config) RebuildArgs(action Action, flake, host string) []string {
	repl := strings.NewReplacer(
		"{action}", string(action),
		"{flake}", flake,
		"{host}", host,
	)
	out := make([]string, 0, len(c.RebuildCommand))
	for _, a := range c.RebuildCommand {
		out = append(out, repl.Replace(a))
	}
	return out
}
