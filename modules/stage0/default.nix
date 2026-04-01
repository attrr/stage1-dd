# stage0.nix — Generic squashfs boot wrapper
#
# A self-contained NixOS module that takes any NixOS initrd, repacks it
# as squashfs, and provides a tiny busybox-based bootstrap cpio to mount
# and switch_root into it.
#
# Builds its own kernel modules closure — independent of the stage1 initrd.
#
# Outputs:
#   system.build.stage0            — bootstrap cpio (busybox + modules + init)
#   system.build.squashfsFromInitrd — repacked NixOS initrd as squashfs
#   system.build.stage1            — assembled result dir (kernel + all pieces)
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.stage0;

  busyboxStatic = pkgs.pkgsStatic.busybox;

  # Directories that makeInitrdNG doesn't create but we need
  stage0Dirs = pkgs.runCommand "stage0-dirs" { } ''
    mkdir -p $out/{proc,sys,dev,mnt/ro,mnt/rw,mnt/root,mnt/part}
  '';

  stage0Init =
    pkgs.runCommand "stage0-init"
      {
        src = pkgs.replaceVars ./stage0-init.sh {
          HARDWARE_MODULES = lib.concatStringsSep " " (cfg.baseKernelModules ++ cfg.extraKernelModules);
        };
      }
      ''
        cp $src $out
        chmod +x $out
      '';

  # Build the squashfs by repacking the existing NixOS initrd
  squashfsFromInitrd =
    pkgs.runCommand "stage1-squashfs"
      {
        nativeBuildInputs = [
          pkgs.squashfsTools
          pkgs.cpio
          pkgs.zstd
        ];
      }
      ''
        mkdir -p unpack
        cd unpack
        zstd -d < ${cfg.stage1Initrd}/initrd | cpio -idm --no-preserve-owner 2>/dev/null
        cd ..
        mksquashfs unpack $out \
          -comp ${cfg.squashfsCompression} \
          -no-xattrs -all-root -b 1048576 \
          -processors $NIX_BUILD_CORES
      '';

  # Base contents for stage0 cpio — shared between separate and merged modes
  stage0BaseContents = [
    {
      source = stage0Init;
      target = "/init";
    }
    {
      source = "${busyboxStatic}/bin/busybox";
      target = "/bin/busybox";
    }
    {
      source = "${config.system.build.modulesClosure}/lib";
      target = "/lib";
    }
    {
      source = "${stage0Dirs}/proc";
      target = "/proc";
    }
    {
      source = "${stage0Dirs}/sys";
      target = "/sys";
    }
    {
      source = "${stage0Dirs}/dev";
      target = "/dev";
    }
    {
      source = "${stage0Dirs}/mnt";
      target = "/mnt";
    }
  ];

  stage1SquashfsCpio =
    pkgs.runCommand "stage1.squashfs"
      {
        nativeBuildInputs = [ pkgs.cpio ];
      }
      ''
        mkdir -p target-root
        cp ${squashfsFromInitrd} target-root/stage1.squashfs

        cd target-root
        find . -mindepth 1 -printf "%P\n" | cpio -o -H newc -R 0:0 > $out
      '';

  # Merged mode: stage0 + squashfs in one cpio
  stage0MergedInitrd = pkgs.makeInitrdNG {
    inherit (config.boot.initrd) compressor compressorArgs;
    contents = stage0BaseContents;
    prepend = [
      stage1SquashfsCpio
    ];
  };

in
{
  options.stage0 = {
    stage1Initrd = lib.mkOption {
      type = lib.types.path;
      description = "Path to the initrd to repack as squashfs (e.g. config.system.build.initialRamdisk).";
    };
    squashfsCompression = lib.mkOption {
      type = lib.types.str;
      default = "zstd -Xcompression-level 19";
      description = "Compression algorithm and flags for the squashfs image.";
    };

    kernelPackages = lib.mkOption {
      type = lib.types.attrs;
      default = pkgs.linuxPackages;
      description = "Kernel packages set shared between stage0 and stage1.";
    };
    baseKernelModules = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [
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
      description = "Base kernel modules to include in initrd.";
    };
    extraKernelModules = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      description = "Extra kernel modules to append to the base list.";
    };
  };

  config = {
    # Stage0's kernel module requirements
    boot.kernelPackages = cfg.kernelPackages;
    boot.initrd.availableKernelModules = [
      "loop"
      "squashfs"
      "overlay"
    ]
    ++ cfg.baseKernelModules
    ++ cfg.extraKernelModules;

    # Separate mode: stage0 cpio without squashfs embedded
    system.build.stage0 = pkgs.makeInitrdNG {
      inherit (config.boot.initrd) compressor compressorArgs;
      contents = stage0BaseContents;
    };

    system.build.squashfsFromInitrd = squashfsFromInitrd;

    # Assembled result dir
    #   bzImage          - kernel
    #   stage0.initrd    - tiny cpio (busybox + modules + init), no squashfs
    #   stage1.squashfs  - repacked initrd as squashfs
    #   initrd           - merged (stage0 + squashfs in one cpio)
    system.build.rescue = pkgs.runCommand "stage1-squashfs" { } ''
      mkdir -p $out
      ln -s ${stage0MergedInitrd}/initrd $out/initrd
      ln -s ${config.system.build.kernel}/${config.system.boot.loader.kernelFile} $out/bzImage
      ln -s ${config.system.build.stage0}/initrd $out/stage0.initrd
      ln -s ${squashfsFromInitrd} $out/stage1.squashfs
    '';

    system.build.rescue-merged = pkgs.runCommand "stage1-merged" { } ''
      mkdir -p $out
      ln -s ${stage0MergedInitrd}/initrd $out/initrd
      ln -s ${config.system.build.kernel}/${config.system.boot.loader.kernelFile} $out/bzImage
    '';

    system.build.rescue-split = pkgs.runCommand "stage1-split" { } ''
      mkdir -p $out
      ln -s ${config.system.build.kernel}/${config.system.boot.loader.kernelFile} $out/bzImage
      ln -s ${config.system.build.stage0}/initrd $out/stage0.initrd
      ln -s ${squashfsFromInitrd} $out/stage1.squashfs
    '';

    system.stateVersion = lib.trivial.release;
  };
}
