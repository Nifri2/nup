// Command nup updates single packages in a NixOS flake without moving the whole
// nixpkgs input.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Nifri2/nup/cmd"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cmd.Version = version

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(cmd.ExecuteContext(ctx))
}
