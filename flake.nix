{
  description = "Site Reliability Clanker: pinned toolchain for the dev shell";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            opentofu
            kubectl
            kubernetes-helm
            open-policy-agent
            git
            bash
            coreutils
            cacert
            kind
            go
            nodejs_22
            pnpm
            shellcheck
            shfmt
            ffmpeg
          ];
        };
      });
    };
}
