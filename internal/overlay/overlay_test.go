package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceContract(t *testing.T) {
	src := Source()
	// These are the properties the design depends on; a regeneration that loses
	// one of them would silently break pure evaluation or refetch tarballs.
	musts := []string{
		"builtins.fetchTarball", // pinning mechanism
		"sha256 = src.sha256",   // a hash on every fetch keeps pure eval working
		"builtins.fromJSON",     // nup never writes Nix code, only JSON
		"nup.lock.json",
		"builtins.listToAttrs", // dedupes revisions
		"prev.stdenv.hostPlatform.system",
		"inheritedConfig",
		"final: prev:",
	}
	for _, m := range musts {
		if !strings.Contains(src, m) {
			t.Errorf("the generated overlay no longer contains %q", m)
		}
	}
	// prev.config must never be passed through verbatim: it is the evaluated
	// config and contains nulls that a fresh import rejects.
	if strings.Contains(src, "config = prev.config") {
		t.Error("the overlay must not forward prev.config verbatim")
	}
}

func TestWriteCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	written, err := Write(path, false)
	if err != nil || !written {
		t.Fatalf("Write: %v, written=%v", err, written)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != Source() {
		t.Error("the written file should match the embedded source")
	}
}

// Without --force an existing overlay must be left alone, so local edits are
// never clobbered.
func TestWriteKeepsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	if err := os.WriteFile(path, []byte("# edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, err := Write(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if written {
		t.Error("Write should have reported that it kept the file")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "# edited by hand\n" {
		t.Error("the existing file was overwritten")
	}

	if written, err := Write(path, true); err != nil || !written {
		t.Fatalf("--force should overwrite: %v %v", written, err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != Source() {
		t.Error("--force did not regenerate the overlay")
	}
}

func TestPath(t *testing.T) {
	if got := Path("/etc/nixos"); got != "/etc/nixos/nup-overlay.nix" {
		t.Errorf("got %q", got)
	}
}
