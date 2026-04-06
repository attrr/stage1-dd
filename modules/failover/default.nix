{
  config,
  lib,
  pkgs,
  ...
}:
let
  failoverPkg = pkgs.buildGoModule {
    pname = "failover";
    version = "1.0.0";
    src = ../../src/failover;
    vendorHash = "sha256-gpcxRdFU3OelqH+cl5CjSFDx2FE0h2p7MutJv+O0FF0=";
  };

  cfg = config.services.failover;
  loader = config.boot.loader;
  bootloader = if loader.systemd-boot.enable then "systemd-boot" else "grub";

  # resolve bootloader specific dir
  rescueEntryName = "stage1-dd-rescue";
  rescueEntryID = if loader.systemd-boot.enable then rescueEntryName + ".conf" else rescueEntryName;

  # resolve artifacts placement location
  rescueDir = "stage1-dd";
  rescueKernelPath = "${rescueDir}/vmlinuz-rescue";
  rescueInitrdPath = "${rescueDir}/initramfs-rescue";

  rescueKernelParams = lib.concatStringsSep " " cfg.rescue.kernelParams;
  markerDir = "/var/lib/failover";

  # eval rescue
  evaluatedStage1 = import "${pkgs.path}/nixos/lib/eval-config.nix" {
    inherit (pkgs.stdenv.hostPlatform) system;
    modules = [
      ../stage1
      {
        stage1.enable = true;
        nixpkgs.pkgs = pkgs;
        stage1.kernel.extraModules = cfg.rescue.kernelModules;
        stage1.ssh.authorizedKeys = cfg.rescue.ssh.authorizedKeys;
        stage1.ssh.hostKeys = cfg.rescue.ssh.hostKeys;
        stage1.extraPackages = [ failoverPkg ] ++ cfg.rescue.extraPackages;
        boot.initrd.systemd.network = cfg.rescue.networkConfig;
      }
      cfg.rescue.extraConfig
    ];
  };

  evaluatedStage0 = import "${pkgs.path}/nixos/lib/eval-config.nix" {
    inherit (pkgs.stdenv.hostPlatform) system;
    modules = [
      ../stage0
      {
        stage0.stage1Initrd = evaluatedStage1.config.system.build.initialRamdisk;
        stage0.extraKernelModules = cfg.rescue.earlyKernelModules;
        nixpkgs.pkgs = pkgs;
      }
    ];
  };

  derivedRescueSystem = evaluatedStage0.config.system.build.rescue-merged;
  failoverConfig = builtins.toJSON {
    bootloader_type = bootloader;
    esp_path = loader.efi.efiSysMountPoint;
    rescue_entry_id = rescueEntryID;
    marker_dir = markerDir;
    watchdog_timeout_sec = cfg.watchdogTimeoutSec;
  };
in
{
  options.services.failover = {
    enable = lib.mkEnableOption "Failover subsystem for Stage1-DD";
    injectMethod = lib.mkOption {
      type = lib.types.enum [
        "disko"
        "script"
      ];
      default = "disko";
    };
    rescue = {
      earlyKernelModules = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
        description = "Extra kernel modules to append to stage0 (the squashfs loader).";
      };
      kernelModules = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
        description = "Extra kernel modules to append to stage1 (the rescue OS).";
      };
      ssh = {
        port = lib.mkOption {
          type = lib.types.port;
          default = 22;
        };
        authorizedKeys = lib.mkOption {
          type = lib.types.listOf lib.types.str;
          default = [ ];
        };
        hostKeys = lib.mkOption {
          type = lib.types.listOf lib.types.path;
          default = [ ];
        };
      };
      networkConfig = lib.mkOption {
        type = lib.types.attrs;
        default = { };
        description = "systemd.network configuration to drop into the rescue OS. Can be inherited from host for early-testing.";
      };
      kernelParams = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [
          "console=tty0"
          "console=ttyS0,115200n8"
          "systemd.journald.forward_to_console=1"
        ];
        description = "Kernel parameters for the rescue stage1 system. The last console= dictates /dev/console.";
      };
      extraPackages = lib.mkOption {
        type = lib.types.listOf lib.types.package;
        default = [ ];
      };
      extraConfig = lib.mkOption {
        type = lib.types.deferredModule;
        default = { };
        description = "Advanced: extra raw NixOS configuration for the inner stage1 rescue machine.";
      };
    };

    watchdogTimeoutSec = lib.mkOption {
      type = lib.types.int;
      default = 300;
      description = ''
        Timeout in seconds for the failover watchdog. If the system is not confirmed
        (via `failover confirm`) within this period after boot, the watchdog triggers
        a reboot into the rescue system.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    # prepare environment
    environment.systemPackages = [ failoverPkg ];
    environment.etc."failover/config.json".text = failoverConfig;

    # SDBOOT
    boot.loader.systemd-boot = lib.mkIf loader.systemd-boot.enable {
      extraFiles = {
        "${rescueInitrdPath}" = derivedRescueSystem + "/initrd";
        "${rescueKernelPath}" = derivedRescueSystem + "/bzImage";
      };
      extraEntries = {
        "${rescueEntryID}" = ''
          title NixOS Rescue (stage1-dd)
          linux ${rescueKernelPath}
          initrd ${rescueInitrdPath}
          options ${rescueKernelParams}
        '';
      };
    };

    # GRUB
    boot.loader.grub = lib.mkIf loader.grub.enable {
      default = "saved";
      extraFiles = {
        "${rescueInitrdPath}" = derivedRescueSystem + "/initrd";
        "${rescueKernelPath}" = derivedRescueSystem + "/bzImage";
      };
      extraConfig = ''
        function savedefault {
          true
        }
      '';
      extraEntries = ''
        menuentry "${rescueEntryID}" {
          linux  @bootRoot@/${rescueKernelPath} ${rescueKernelParams}
          initrd @bootRoot@/${rescueInitrdPath}
        }
      '';
    };

    # Hardware Watchdog, If kernel or PID 1 hangs, hardware watchdog forces power-cycle after 60s
    systemd.settings.Manager.RuntimeWatchdogSec = "60s";
    systemd.services.failover-watchdog = {
      description = "Failover Watchdog";
      wantedBy = [ "multi-user.target" ];
      path = lib.optional loader.grub.enable pkgs.grub2;
      after = [
        "network.target"
        "sshd.service"
      ];
      unitConfig = {
        FailureAction = "force-reboot";
      };
      serviceConfig = {
        Type = "notify";
        ExecStart = "${failoverPkg}/bin/failover watchdog";
        WatchdogSec = "10s";
        # never auto-restart this, watchdog down means system unavaliable
        Restart = "no";
      };
    };

    systemd.tmpfiles.rules = [ "d ${markerDir} 0755 root root" ];

    # inject first-boot.marker
    system.build.diskoImages.postInstallCommands = lib.mkIf (cfg.injectMethod == "disko") ''
      mkdir -p $out${markerDir}
      touch $out${markerDir}/first-boot.marker
    '';
    system.activationScripts.inject-firstboot-marker = lib.mkIf (cfg.injectMethod == "script") ''
      DONE_MARKER="${markerDir}/.initialized"

      if [ ! -f "$DONE_MARKER" ]; then
        echo "First time boot: Initializing..."
        mkdir -p ${markerDir}
        touch ${markerDir}/first-boot.marker
        touch "$DONE_MARKER"
      fi
    '';
    system.build.rescue = evaluatedStage0.config.system.build.rescue;
  };
}
