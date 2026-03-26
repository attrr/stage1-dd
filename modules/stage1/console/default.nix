{
  pkgs,
  lib,
  config,
  ...
}:
{
  config = lib.mkIf config.stage1.enable {
    # console generators
    boot.initrd.systemd.services = {
      "console-shell@" = {
        description = "Started Console Shell on %I";
        serviceConfig = {
          Type = "simple";
          StandardInput = "tty";
          StandardOutput = "tty";
          StandardError = "tty";
          TTYPath = "/dev/%I";
          ExecStart = "${pkgs.zsh}/bin/zsh";
          Restart = "always";
          DefaultDependencies = false;
        };
      };
    };
    boot.initrd.systemd.contents."/etc/systemd/system-generators/console-generator".source =
      pkgs.writeShellScript "console-generator" (builtins.readFile ./console-generator.sh);

    # zsh
    boot.initrd.systemd.storePaths = [
      pkgs.zsh
      pkgs.grml-zsh-config
    ];
    boot.initrd.systemd.contents = {
      "/usr/share/terminfo".source = "${pkgs.ncurses}/share/terminfo";
      "/etc/zshrc".text = ''
        source ${pkgs.grml-zsh-config}/etc/zsh/zshrc
        export TERM=''${TERM:-linux}
        export TERMINFO_DIRS=/usr/share/terminfo
        export SYSTEMD_COLORS=1
        export PATH=/bin
        PS1='[rescue@nixos:%~]%# '
      '';
    };
  };
}
