{
  description = "Site Reliability Clanker: pinned toolchain for the dev shell and the runner image";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # The tools that execute jobs against real environments. An approval covers these
      # versions, because flake.lock pins them and the image digest is recorded on each job.
      runnerTools = pkgs: with pkgs; [
        opentofu
        kubectl
        kubernetes-helm
        open-policy-agent
        git
        bash
        coreutils
        cacert
      ];
    in
    {
      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = (runnerTools pkgs) ++ (with pkgs; [
            kind
            go
            nodejs_22
            pnpm
            shellcheck
            shfmt
            ffmpeg
          ]);
        };
      });

      packages = forAllSystems (pkgs: {
        runner-image = pkgs.dockerTools.buildLayeredImage {
          name = "clanker-runner";
          tag = "latest";
          contents = runnerTools pkgs;
          config = {
            Env = [
              "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
              "TF_IN_AUTOMATION=1"
            ];
            Entrypoint = [ "${pkgs.bash}/bin/bash" ];
          };
        };
      });
    };
}
