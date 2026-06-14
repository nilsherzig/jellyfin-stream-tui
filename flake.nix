{
  description = "jellyfin-stream-tui – TUI to stream Jellyfin media via mpv";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        # `nix build` / `nix profile install` – builds the binary and wraps mpv
        # (a runtime dependency) onto its PATH.
        packages.default = pkgs.buildGoModule {
          pname = "jellyfin-stream-tui";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-cpZVWNgH/SoTu117Iby4QExgP0ROPzWju6A0iUkyQ1o=";

          subPackages = [ "cmd/jellyfin-stream-tui" ];

          nativeBuildInputs = [ pkgs.makeWrapper ];
          postInstall = ''
            wrapProgram $out/bin/jellyfin-stream-tui \
              --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.mpv ]}
          '';

          meta = {
            description = "TUI to browse and play Jellyfin media with mpv";
            mainProgram = "jellyfin-stream-tui";
          };
        };

        # `nix run`
        apps.default = flake-utils.lib.mkApp {
          drv = self.packages.${system}.default;
        };

        # `nix develop` – dev environment with go + mpv.
        devShells.default = pkgs.mkShell {
          buildInputs = [ pkgs.go pkgs.mpv ];
        };
      });
}
