{
  pkgs,
  config,
  lib,
  ...
}:
let
  cfg = config.stage1.ssh;
in
{
  options.stage1.ssh = {
    port = lib.mkOption {
      type = lib.types.port;
      default = 22;
      description = "SSH port for the rescue environment.";
    };
    authorizedKeys = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      description = "SSH authorized keys for root.";
    };
    hostKeys = lib.mkOption {
      type = lib.types.listOf lib.types.path;
      default = [ ];
      description = "SSH host keys.";
    };
  };

  config = lib.mkIf config.stage1.enable {
    boot.initrd.network.ssh = {
      enable = true;
      port = cfg.port;
      shell = "/bin/zsh";
      inherit (cfg) hostKeys authorizedKeys;
    };

    boot.initrd.systemd.initrdBin = [ pkgs.openssh ];
    boot.initrd.systemd.services."ssh-key-injection" = {
      description = "Inject SSH keys from kernel args and generate HostKey";
      wantedBy = [
        "sshd.service"
        "initrd.target"
      ];
      before = [ "sshd.service" ];
      after = [ "initrd-nixos-copy-secrets.service" ];

      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
      };

      script = ''
        #!/bin/sh
        mkdir -p /root/.ssh /etc/ssh
        chmod 700 /root/.ssh
        touch /root/.ssh/authorized_keys

        # inject host key if not exist
        if ! ls /etc/ssh/ssh_host_*_key >/dev/null 2>&1; then
          ${pkgs.openssh}/bin/ssh-keygen -t ed25519 -N "" -f /etc/ssh/ssh_host_ed25519_key
        fi

        # read cmdline for sshkey
        eval "set -- $(cat /proc/cmdline)"
        for arg do
          case "$arg" in
            ssh_key=*)
              key="''${arg#ssh_key=}"
              echo "$key" >> /root/.ssh/authorized_keys
              echo "[Rescue] Injected SSH Key from kernel cmdline."
              ;;
          esac
        done
        chmod 600 /root/.ssh/authorized_keys
      '';
    };
  };
}
