{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.stage1.kernel;
in
{
  options.stage1.kernel = {
    packages = lib.mkOption {
      type = lib.types.attrs;
      default = pkgs.linuxPackages;
      description = "Kernel packages set shared between stage0 and stage1.";
    };
    baseModules = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [
        "ext4"
        "xfs"
        "vfat"
        "nls_cp437"
        "nls_iso8859_1"
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
    extraModules = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      description = "Extra kernel modules to append to the base list.";
    };

  };
  config = lib.mkIf config.stage1.enable {
    boot.kernelPackages = cfg.packages;
    boot.initrd.availableKernelModules = cfg.baseModules ++ cfg.extraModules;
    boot.kernel.sysctl."vm.overcommit_memory" = "1";

    # sample kernelParams, take no effects
    boot.kernelParams = [
      "console=ttyS0"
      "console=tty0"
      "panic=30"
    ];
  };
}
