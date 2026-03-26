{ lib, config, ... }:
let
  cfg = config.stage1.zram;
in
{
  options.stage1.zram = {
    enable = lib.mkEnableOption "enable zram in stage1?";
    algorithm = lib.mkOption {
      type = lib.types.str;
      default = "lz4";
    };
    size = lib.mkOption {
      type = lib.types.str;
      default = "80M";
    };
    priority = lib.mkOption {
      type = lib.types.ints.between 1 100;
      default = 50;
    };
  };

  config = lib.mkIf (config.stage1.enable && cfg.enable) {
    boot.initrd.availableKernelModules = [ "zram" ];
    boot.initrd.systemd.services.zram = {
      description = "Setup ZRAM for Rescue";
      before = [
        "local-fs.target"
        "swap.target"
      ];
      wantedBy = [ "initrd.target" ];

      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
      };

      script = ''
        modprobe zram || true
        if [ -b /dev/zram0 ]; then
          echo ${cfg.algorithm} > /sys/class/block/zram0/comp_algorithm
          echo ${cfg.size} > /sys/class/block/zram0/disksize
          mkswap /dev/zram0
          swapon --priority ${toString cfg.priority} /dev/zram0
          echo "ZRAM activated: ${cfg.size} using ${cfg.algorithm}"
        fi
      '';
    };
  };
}
