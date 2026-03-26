{
  pkgs ? import <nixpkgs> { },
  lib ? pkgs.lib,
  mkRescue ? null,
}:
let
  # Replicate mkRescue's internal structure so we can inspect both configs
  mkRescueWithConfigs =
    {
      ssh ? { },
      kernelPackages ? null,
      extraModules ? [ ],
    }:
    let
      stage1Eval = lib.nixosSystem {
        system = pkgs.stdenv.hostPlatform.system or "x86_64-linux";
        modules = [
          ../modules/stage1
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

      stage0Eval = lib.nixosSystem {
        system = pkgs.stdenv.hostPlatform.system or "x86_64-linux";
        modules = [
          ../modules/stage0
          {
            stage0 = {
              stage1Initrd = stage1Eval.config.system.build.initialRamdisk;
              kernelPackages = lib.mkIf (kernelPackages != null) kernelPackages;
            };
            system.stateVersion = lib.trivial.release;
          }
        ];
      };
    in
    {
      stage0 = stage0Eval.config;
      stage1 = stage1Eval.config;
      rescue = stage0Eval.config.system.build.rescue;
    };

in
lib.runTests {
  # Group A: Wiring Correctness
  testSSHWiring = {
    expr =
      let
        configs = mkRescueWithConfigs {
          ssh = {
            port = 2222;
            authorizedKeys = [ "test-key" ];
          };
        };
      in
      {
        port = configs.stage1.boot.initrd.network.ssh.port;
        keys = configs.stage1.boot.initrd.network.ssh.authorizedKeys;
      };
    expected = {
      port = 2222;
      keys = [ "test-key" ];
    };
  };

  testInitrdWiring = {
    expr =
      let
        configs = mkRescueWithConfigs { };
      in
      configs.stage0.stage0.stage1Initrd.outPath == configs.stage1.system.build.initialRamdisk.outPath;
    expected = true;
  };

  testExtraModulesApplyToStage1Only = {
    expr =
      let
        configs = mkRescueWithConfigs {
          extraModules = [ { stage1.ssh.port = 3333; } ];
        };
      in
      {
        stage1Port = configs.stage1.boot.initrd.network.ssh.port;
        stage0ExtraModules = configs.stage0.stage0.extraKernelModules;
      };
    expected = {
      stage1Port = 3333;
      stage0ExtraModules = [ ];
    };
  };

  # Group B: kernelPackages Propagation
  testKernelPackagesNullUseDefaults = {
    expr =
      let
        configs = mkRescueWithConfigs { };
      in
      configs.stage0.boot.kernelPackages.kernel.version == pkgs.linuxPackages.kernel.version
      && configs.stage1.boot.kernelPackages.kernel.version == pkgs.linuxPackages.kernel.version;
    expected = true;
  };

  testKernelPackagesOverrideBoth = {
    expr =
      let
        configs = mkRescueWithConfigs { kernelPackages = pkgs.linuxPackages_latest; };
      in
      configs.stage0.boot.kernelPackages.kernel.version == pkgs.linuxPackages_latest.kernel.version
      && configs.stage1.boot.kernelPackages.kernel.version == pkgs.linuxPackages_latest.kernel.version;
    expected = true;
  };

  # Group C: Rescue Derivation Structure
  testRescueBuildCommandStructure = {
    expr =
      let
        configs = mkRescueWithConfigs { };
        cmd = configs.rescue.buildCommand;
      in
      {
        hasBzImage = lib.strings.hasInfix "bzImage" cmd;
        hasStage0Initrd = lib.strings.hasInfix "stage0.initrd" cmd;
        hasStage1Squashfs = lib.strings.hasInfix "stage1.squashfs" cmd;
        hasInitrd = lib.strings.hasInfix "initrd" cmd;
      };
    expected = {
      hasBzImage = true;
      hasStage0Initrd = true;
      hasStage1Squashfs = true;
      hasInitrd = true;
    };
  };

  testRescueIsDerivation = {
    expr =
      let
        configs = mkRescueWithConfigs { };
      in
      lib.isDerivation configs.rescue;
    expected = true;
  };

  # Group D: Edge Cases
  testDefaultMkRescueEvaluates = {
    expr = lib.isDerivation (mkRescue { });
    expected = true;
  };

  testExtraModulesMkForceKernelModules = {
    expr =
      let
        configs = mkRescueWithConfigs {
          extraModules = [ { boot.initrd.availableKernelModules = lib.mkForce [ "ahci" ]; } ];
        };
      in
      builtins.elem "ahci" configs.stage1.boot.initrd.availableKernelModules;
    expected = true;
  };
}
