{ lib, config, ... }:
{
  options.stage1.debug = lib.mkEnableOption "debug?";
  config = lib.mkIf config.stage1.debug {
    boot.initrd.systemd.services.rescue-ready-beacon = {
      description = "Print rescue ready beacon to console";
      wantedBy = [ "initrd.target" ];
      after = [ "initrd.target" ];
      script = ''
        echo RESCUE_READY > /dev/console
      '';
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
      };
    };
  };
}
