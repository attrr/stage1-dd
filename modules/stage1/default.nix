# stage1.nix — Default rescue/deployment environment
#
# A NixOS initrd module with SSH, zsh, and deployment tools.
# Imports stage0.nix for squashfs boot support.
#
# This is the default stage1 shipped by this project. Users can replace
# it entirely with their own NixOS initrd config while still using stage0.
{
  config,
  lib,
  pkgs,
  modulesPath,
  ...
}:
let
  cfg = config.stage1;
in
{
  imports = [
    (modulesPath + "/profiles/qemu-guest.nix")
    (modulesPath + "/profiles/minimal.nix")
    ./console
    ./ssh.nix
    ./kernel.nix
    ./zram.nix
    ./debug.nix
  ];

  options.stage1 = {
    enable = lib.mkEnableOption "Enable stage1";
    extraPackages = lib.mkOption {
      type = lib.types.listOf lib.types.package;
      default = [ ];
      description = "Extra packages to append to the base list.";
    };
  };

  config = lib.mkIf cfg.enable {
    # boot.initrd.network.ssh.hostKeys will failed without grub disabled
    boot.loader.grub.enable = false;

    system.stateVersion = lib.trivial.release;
    boot.initrd.systemd.enable = true;
    boot.initrd.systemd.emergencyAccess = true;

    boot.initrd.systemd.services = {
      # Prevent NixOS from trying to transition out of initrd
      initrd-find-nixos-closure.enable = false;
      initrd-nixos-activation.enable = false;
      initrd-cleanup.enable = false;
      initrd-parse-etc.enable = false;
      initrd-switch-root.enable = false;
    };

    # default networking config
    boot.initrd.systemd.network.enable = true;
    boot.initrd.systemd.network.networks."99-eth-all" = {
      matchConfig.Name = lib.mkDefault "en* eth*";
      networkConfig.DHCP = lib.mkDefault "yes";
    };

    # extra tools
    boot.initrd.systemd.initrdBin =
      with pkgs;
      [
        util-linux
        busybox
        zstd
        zsh
        grub2
        # utils
        iproute2
        parted
        curl
        htop
      ]
      ++ cfg.extraPackages;
  };
}
