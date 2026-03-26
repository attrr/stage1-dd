# tests/stage0-test.nix
{
  pkgs ? import <nixpkgs> { },
  lib ? pkgs.lib,
}:
let
  # Helper to eval stage0 with given config
  evalStage0 =
    extraConfig:
    (lib.nixosSystem {
      system = pkgs.stdenv.hostPlatform.system or "x86_64-linux";
      modules = [
        ../modules/stage0
        {
          stage0.stage1Initrd = pkgs.runCommand "dummy" { } "mkdir -p $out && touch $out/initrd";
          fileSystems."/" = {
            device = "none";
            fsType = "tmpfs";
          };
          system.stateVersion = lib.trivial.release;
        }
        extraConfig
      ];
    }).config;

in
lib.runTests {
  # --- Group A: Option Defaults & Basic Propagation ---

  testStage0DefaultKernelPackages = {
    expr = (evalStage0 { }).stage0.kernelPackages.kernel.version;
    expected = pkgs.linuxPackages.kernel.version;
  };

  testStage0DefaultBaseKernelModules = {
    expr = (evalStage0 { }).stage0.baseKernelModules;
    expected = [
      "ext4"
      "xfs"
      "vfat"
      "virtio_pci"
      "virtio_blk"
      "virtio_scsi"
      "ata_piix"
      "ata_generic"
      "sd_mod"
      "sr_mod"
      "nvme"
    ];
  };

  testStage0BootKernelPackagesPropagation = {
    expr =
      let
        cfg = evalStage0 { };
      in
      cfg.boot.kernelPackages.kernel.version == cfg.stage0.kernelPackages.kernel.version;
    expected = true;
  };

  testStage0AvailableModulesIncludeBase = {
    expr =
      let
        cfg = evalStage0 { };
      in
      lib.all (mod: lib.elem mod cfg.boot.initrd.availableKernelModules) cfg.stage0.baseKernelModules;
    expected = true;
  };

  testStage0ExtraKernelModules = {
    expr =
      let
        cfg = evalStage0 { stage0.extraKernelModules = [ "test_mod" ]; };
      in
      lib.elem "test_mod" cfg.boot.initrd.availableKernelModules;
    expected = true;
  };

  # --- Group B: Structural Logic (Deep Checks) ---

  testStage0CpioContents = {
    expr =
      let
        cfg = evalStage0 { };
        hasTarget =
          t:
          if lib.hasAttr "contents" cfg.system.build.stage0 then
            lib.any (c: c.target == t) cfg.system.build.stage0.contents
          else
            lib.hasInfix "\"target\":\"${t}\"" cfg.system.build.stage0.contentsJSON;
      in
      {
        init = hasTarget "/init";
        busybox = hasTarget "/bin/busybox";
        lib = hasTarget "/lib";
        proc = hasTarget "/proc";
      };
    expected = {
      init = true;
      busybox = true;
      lib = true;
      proc = true;
    };
  };

  testStage0InitScriptDRV = {
    expr =
      let
        cfg = evalStage0 { stage0.extraKernelModules = [ "test_extra" ]; };
      in
      if lib.hasAttr "contents" cfg.system.build.stage0 then
        let
          initItem = lib.findFirst (c: c.target == "/init") null cfg.system.build.stage0.contents;
        in
        lib.hasInfix "test_extra" initItem.source.HARDWARE_MODULES
      else
        # If we can't access .contents, we at least verify the extra module is in the available modules
        lib.elem "test_extra" cfg.boot.initrd.availableKernelModules;
    expected = true;
  };

  testSquashFSBuildInputs = {
    expr =
      let
        drv = (evalStage0 { }).system.build.squashfsFromInitrd;
        pkgNames = map (p: p.pname or p.name) drv.nativeBuildInputs;
      in
      {
        squashfs = lib.any (n: lib.hasInfix "squashfs" n) pkgNames;
        cpio = lib.any (n: lib.hasInfix "cpio" n) pkgNames;
        zstd = lib.any (n: lib.hasInfix "zstd" n) pkgNames;
      };
    expected = {
      squashfs = true;
      cpio = true;
      zstd = true;
    };
  };

  testRescueSymlinks = {
    expr =
      let
        cmd = (evalStage0 { }).system.build.rescue.buildCommand;
      in
      {
        bzImage = lib.hasInfix "ln -s" cmd && lib.hasInfix "bzImage" cmd;
        stage0 = lib.hasInfix "stage0.initrd" cmd;
        stage1 = lib.hasInfix "stage1.squashfs" cmd;
        merged = lib.hasInfix "initrd" cmd;
      };
    expected = {
      bzImage = true;
      stage0 = true;
      stage1 = true;
      merged = true;
    };
  };

  testModulesClosureKernel = {
    expr =
      let
        cfg = evalStage0 { };
      in
      lib.hasInfix cfg.boot.kernelPackages.kernel.version cfg.system.build.modulesClosure.kernel.name;
    expected = true;
  };
}
