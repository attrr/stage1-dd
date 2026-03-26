{
  description = "stage1-dd";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      # supportedSystems = [ "x86_64-linux" "aarch64-linux" ];
      # forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
      pkgsFor = system: nixpkgs.legacyPackages.${system};

      failoverPkgFor = system: (pkgsFor system).buildGoModule {
        pname = "failover";
        version = "1.0.0";
        src = ./src/failover;
        vendorHash = "sha256-gpcxRdFU3OelqH+cl5CjSFDx2FE0h2p7MutJv+O0FF0=";
      };
    in
    {
      nixosModules.stage0 = import ./modules/stage0;
      nixosModules.failover = import ./modules/failover;

      lib.mkRescue =
        {
          system ? "x86_64-linux",
          ssh ? { },
          kernelPackages ? null,
          extraModules ? [ ],
        }:
        let
          inherit (nixpkgs) lib;
          failoverPkg = failoverPkgFor system;
          stage1 = lib.nixosSystem {
            inherit system;
            specialArgs = { inherit failoverPkg; };
            modules = [
              ./modules/stage1
              {
                stage1 = {
                  enable = true;
                  inherit ssh;
                  kernel.packages = lib.mkIf (kernelPackages != null) kernelPackages;
                };
                system.stateVersion = lib.trivial.release;
              }
            ]
            ++ extraModules;
          };

          stage0 = lib.nixosSystem {
            inherit system;
            modules = [
              ./modules/stage0
              {
                stage0 = {
                  stage1Initrd = stage1.config.system.build.initialRamdisk;
                  kernelPackages = lib.mkIf (kernelPackages != null) kernelPackages;
                };
                system.stateVersion = lib.trivial.release;
              }
            ];
          };
        in
        stage0.config.system.build.rescue;

      checks.x86_64-linux = import ./tests {
        pkgs = nixpkgs.legacyPackages.x86_64-linux;
        lib = nixpkgs.lib;
        mkRescue = self.lib.mkRescue;
        failoverPkg = failoverPkgFor "x86_64-linux";
      };

      # ── Example packages ──────────────────────────────────────────
      packages.x86_64-linux =
        let
          testKeys = {
            ssh = {
              authorizedKeys = [
                "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDgFgJ0hFLOr8uaV1oTIzzHSObwG6VPzniEKxr+BYhyp"
              ];
            };
          };
        in
        {
          default = self.lib.mkRescue testKeys;
          failover = failoverPkgFor "x86_64-linux";
        };

    };
}
