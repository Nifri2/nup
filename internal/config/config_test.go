package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Branch != DefaultBranch || cfg.AfterUpdate != ActionAsk || cfg.Output != "table" {
		t.Fatalf("got %+v", cfg)
	}
}

// A config that only sets one key must keep the defaults for everything else.
func TestLoadPartialKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("host: workstation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "workstation" {
		t.Errorf("Host = %q", cfg.Host)
	}
	if cfg.Branch != DefaultBranch {
		t.Errorf("Branch = %q, want the default", cfg.Branch)
	}
	if !reflect.DeepEqual(cfg.RebuildCommand, DefaultRebuildCommand) {
		t.Errorf("RebuildCommand = %v", cfg.RebuildCommand)
	}
}

func TestLoadFull(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `flake: /etc/nixos
host: nixos
branch: nixos-25.05
home-manager:
  enable: true
  user: niklas
rebuild-command: ["nh", "os", "{action}", "{flake}"]
after-update: switch
auto-commit: true
output: json
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Branch != "nixos-25.05" || !cfg.HomeManager.Enable || cfg.HomeManager.User != "niklas" {
		t.Fatalf("got %+v", cfg)
	}
	if cfg.AfterUpdate != ActionSwitch || !cfg.AutoCommit || cfg.Output != "json" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestImpureDefaultsOffAndCanBeEnabled(t *testing.T) {
	if Default().Impure {
		t.Error("impure must default to off")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("impure: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Impure {
		t.Error("impure: true was not picked up")
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	cases := map[string]func(*Config){
		"bad action": func(c *Config) { c.AfterUpdate = "reboot" },
		"bad output": func(c *Config) { c.Output = "xml" },
		"hm no user": func(c *Config) { c.HomeManager.Enable = true },
		"no rebuild": func(c *Config) { c.RebuildCommand = nil },
	}
	for name, mutate := range cases {
		cfg := Default()
		mutate(cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestRebuildArgs(t *testing.T) {
	cfg := Default()
	got := cfg.RebuildArgs(ActionSwitch, "/etc/nixos", "nixos")
	want := []string{"sudo", "nixos-rebuild", "switch", "--flake", "/etc/nixos#nixos"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// The rebuild command is configurable so nh or nom can be used instead.
func TestRebuildArgsCustomCommand(t *testing.T) {
	cfg := Default()
	cfg.RebuildCommand = []string{"nh", "os", "{action}", "{flake}", "--hostname", "{host}"}
	got := cfg.RebuildArgs(ActionBoot, "/etc/nixos", "laptop")
	want := []string{"nh", "os", "boot", "/etc/nixos", "--hostname", "laptop"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSaveThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	cfg := Default()
	cfg.Host = "nixos"
	cfg.Flake = "/etc/nixos"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Host != "nixos" || back.Flake != "/etc/nixos" || back.Branch != DefaultBranch {
		t.Fatalf("got %+v", back)
	}
}

func TestDetectRebuildCommand(t *testing.T) {
	found := func(names ...string) LookPath {
		set := map[string]bool{}
		for _, n := range names {
			set[n] = true
		}
		return func(name string) (string, error) {
			if set[name] {
				return "/run/current-system/sw/bin/" + name, nil
			}
			return "", errors.New("not found")
		}
	}

	// nh wins when it is installed: that is what the user rebuilds with.
	cmd, tool := DetectRebuildCommand(found("nh", "nixos-rebuild"))
	if tool.Binary != "nh" {
		t.Fatalf("tool = %q, want nh", tool.Binary)
	}
	want := []string{"nh", "os", "{action}", "{flake}", "--hostname", "{host}"}
	if !reflect.DeepEqual(cmd, want) {
		t.Errorf("cmd = %v, want %v", cmd, want)
	}
	// nh elevates on its own, so prefixing sudo would be wrong.
	if cmd[0] == "sudo" {
		t.Error("nh must not be run under sudo")
	}

	cmd, tool = DetectRebuildCommand(found("nixos-rebuild"))
	if tool.Binary != "nixos-rebuild" || !reflect.DeepEqual(cmd, DefaultRebuildCommand) {
		t.Errorf("got %q %v", tool.Binary, cmd)
	}

	// Nothing found: still produce something a NixOS system can run.
	cmd, tool = DetectRebuildCommand(found())
	if tool.Binary != "nixos-rebuild" || !reflect.DeepEqual(cmd, DefaultRebuildCommand) {
		t.Errorf("fallback: got %q %v", tool.Binary, cmd)
	}
}

// The detected command must survive templating into a runnable argv.
func TestDetectedNhCommandTemplates(t *testing.T) {
	cmd, _ := DetectRebuildCommand(func(n string) (string, error) {
		if n == "nh" {
			return "/bin/nh", nil
		}
		return "", errors.New("no")
	})
	cfg := Default()
	cfg.RebuildCommand = cmd
	got := cfg.RebuildArgs(ActionBoot, "/home/u/nix", "laptop")
	want := []string{"nh", "os", "boot", "/home/u/nix", "--hostname", "laptop"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Default() must stay pure: it is the in-memory fallback for every command, and
// must not depend on what happens to be on PATH.
func TestDefaultDoesNotDetect(t *testing.T) {
	if !reflect.DeepEqual(Default().RebuildCommand, DefaultRebuildCommand) {
		t.Error("Default() should always be the plain nixos-rebuild command")
	}
}
