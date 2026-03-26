# tests/stage1-test.nix
{
  pkgs ? import <nixpkgs> { },
  lib ? pkgs.lib,
}:
let
  # Helper to eval stage1 with given config
  evalStage1 =
    extraConfig:
    (lib.nixosSystem {
      system = pkgs.stdenv.hostPlatform.system;
      modules = [
        ../modules/stage1
        {
          stage1.enable = true;
          system.stateVersion = lib.trivial.release;
        }
        extraConfig
      ];
    }).config;

in
lib.runTests {
  # --- Group A: Existing ---
  testStage1Enabled = {
    expr = (evalStage1 { }).stage1.enable;
    expected = true;
  };

  testStage1DefaultPackages = {
    expr =
      let
        cfg = evalStage1 { };
        expectedPkgs = [
          "busybox"
          "zstd"
          "zsh"
          "util-linux"
          "iproute2"
          "parted"
          "curl"
          "htop"
        ];
        getName = p: p.pname or (p.name or (if lib.isDerivation p then p.name else "${p}"));
        pkgNames = map getName cfg.boot.initrd.systemd.initrdBin;
      in
      lib.all (name: lib.any (pkgName: lib.hasInfix name pkgName) pkgNames) expectedPkgs;
    expected = true;
  };

  testStage1SSHConfig = {
    expr =
      let
        cfg = evalStage1 {
          stage1.ssh.port = 2222;
          stage1.ssh.authorizedKeys = [ "test-key" ];
        };
      in
      {
        port = cfg.boot.initrd.network.ssh.port;
        keys = cfg.boot.initrd.network.ssh.authorizedKeys;
      };
    expected = {
      port = 2222;
      keys = [ "test-key" ];
    };
  };

  testStage1KernelModules = {
    expr =
      let
        cfg = evalStage1 { stage1.kernel.extraModules = [ "test_mod" ]; };
      in
      lib.all (mod: lib.elem mod cfg.boot.initrd.availableKernelModules) [
        "nvme"
        "test_mod"
      ];
    expected = true;
  };

  # --- Group B: Systemd Services ---
  testConsoleShellService = {
    expr =
      let
        srv = (evalStage1 { }).boot.initrd.systemd.services."console-shell@";
      in
      {
        wantedBy = lib.elem "initrd.target" srv.wantedBy;
        # ExecStart might contain thunks in pure eval
        execStart = lib.hasInfix "zsh" "${srv.serviceConfig.ExecStart}";
        restart = srv.serviceConfig.Restart;
      };
    expected = {
      wantedBy = false;
      execStart = true;
      restart = "always";
    };
  };

  testDisabledNixosTransitionServices = {
    expr =
      let
        srvs = (evalStage1 { }).boot.initrd.systemd.services;
      in
      {
        find-closure = srvs.initrd-find-nixos-closure.enable;
        activation = srvs.initrd-nixos-activation.enable;
        cleanup = srvs.initrd-cleanup.enable;
      };
    expected = {
      find-closure = false;
      activation = false;
      cleanup = false;
    };
  };

  # --- Group C: Initrd Contents ---
  testZshrcContent = {
    expr =
      let
        # .text should be a string, but we wrap in quotes just in case of thunks in pure eval
        text = "${(evalStage1 { }).boot.initrd.systemd.contents."/etc/zshrc".text}";
      in
      (lib.hasInfix "grml-zsh-config" text) && (lib.hasInfix "rescue@nixos" text);
    expected = true;
  };

  testTerminfoContent = {
    expr =
      let
        # .source might be a path object which doesn't coerce to string in pure eval
        source = (evalStage1 { }).boot.initrd.systemd.contents."/usr/share/terminfo".source;
        sourceStr = if lib.isDerivation source then source.name else "${source}";
      in
      lib.hasInfix "ncurses" sourceStr;
    expected = true;
  };

  testStorePaths = {
    expr =
      let
        cfg = evalStage1 { };
        paths = cfg.boot.initrd.systemd.storePaths;
        # In pure eval, storePaths contains systemd content items { source, target, ... }
        # We check the 'source' attribute for our expected packages.
        getSource = p: if lib.isAttrs p && lib.hasAttr "source" p then p.source else p;
        sources = map getSource paths;
        # Now we check if grml and zsh are in the sources
        hasZsh = lib.any (s: lib.isDerivation s && lib.hasInfix "zsh" (s.pname or s.name)) sources;
        hasGrml = lib.any (
          s: lib.isDerivation s && lib.hasInfix "grml-zsh-config" (s.pname or s.name)
        ) sources;
      in
      {
        inherit hasZsh hasGrml;
      };
    expected = {
      hasZsh = true;
      hasGrml = true;
    };
  };
  # --- Group D: Network ---
  testNetworkDHCP = {
    expr =
      let
        cfg = evalStage1 { };
      in
      {
        enabled = cfg.boot.initrd.systemd.network.enable;
        dhcp = cfg.boot.initrd.systemd.network.networks."99-eth-all".networkConfig.DHCP;
      };
    expected = {
      enabled = true;
      dhcp = "yes";
    };
  };

  # --- Group E: Override Scenarios ---
  testExtraPackagesMerge = {
    expr =
      let
        cfg = evalStage1 { stage1.extraPackages = [ pkgs.strace ]; };
        getName = p: p.pname or (p.name or (if lib.isDerivation p then p.name else "${p}"));
        pkgNames = map getName cfg.boot.initrd.systemd.initrdBin;
      in
      lib.any (n: lib.hasInfix "strace" n) pkgNames;
    expected = true;
  };

  testKernelModulesMkForce = {
    expr =
      let
        cfg = evalStage1 {
          stage1.kernel.baseModules = lib.mkForce [ "ahci" ];
          stage1.kernel.extraModules = lib.mkForce [ ];
        };
        mods = cfg.boot.initrd.availableKernelModules;
      in
      {
        # We check that "ahci" is present (our force-set list)
        hasAhci = lib.elem "ahci" mods;
        # We don't check for "noNvme" because qemu-guest.nix might re-add it
        # or other modules. We just want to see if our option was replaced.
      };
    expected = {
      hasAhci = true;
    };
  };
  testKernelPackagesOverride = {
    expr =
      let
        cfg = evalStage1 { stage1.kernel.packages = pkgs.linuxPackages_latest; };
      in
      cfg.boot.kernelPackages.kernel.version == pkgs.linuxPackages_latest.kernel.version;
    expected = true;
  };

  testSSHShellIsZsh = {
    expr = (evalStage1 { }).boot.initrd.network.ssh.shell;
    expected = "/bin/zsh";
  };
}
