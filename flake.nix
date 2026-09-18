{
  description = "nup - update single packages in a NixOS flake without moving all of nixpkgs";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      version = "0.2.0";
    in
    {
      packages = forAllSystems (pkgs: rec {
        nup = pkgs.buildGoModule {
          pname = "nup";
          inherit version;
          src = self;

          vendorHash = "sha256-9cYTeoffRGMt3NlN1K0NjOAebye/MCYanb28KeUKKc4=";

          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];

          # The unit tests need no network and no nix, so they run in the
          # sandbox as a build-time check.
          doCheck = true;

          meta = {
            description = "Update single packages in a NixOS flake without moving all of nixpkgs";
            homepage = "https://github.com/Nifri2/nup";
            license = pkgs.lib.licenses.mit;
            mainProgram = "nup";
            platforms = pkgs.lib.platforms.unix;
          };
        };
        default = nup;
      });

      # `nix run github:Nifri2/nup` works because of this.
      apps = forAllSystems (pkgs: rec {
        nup = {
          type = "app";
          program = "${self.packages.${pkgs.stdenv.hostPlatform.system}.nup}/bin/nup";
          meta.description = "Update single packages in a NixOS flake";
        };
        default = nup;
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
            pkgs.gotools
            pkgs.go-task
            pkgs.golangci-lint
            pkgs.nixfmt
          ];
        };
      });

      overlays.default = final: prev: {
        nup = self.packages.${prev.stdenv.hostPlatform.system}.nup;
      };

      formatter = forAllSystems (pkgs: pkgs.nixfmt);
    };
}
