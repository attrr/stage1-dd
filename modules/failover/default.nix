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

  # resolve value from config
  efiMount = config.boot.loader.efi.efiSysMountPoint;
  sdbootEnabled = config.boot.loader.systemd-boot.enable;
  grubEnabled = config.boot.loader.grub.enable;
  bootloader = if sdbootEnabled then "systemd-boot" else "grub";

  # resolve bootloader specific dir
  rescueEntryName = "stage1-dd-rescue";
  rescueEntryID = if sdbootEnabled then rescueEntryName + ".conf" else rescueEntryName;
  rescueBaseDir = "stage-dd";
  rescueDir = if sdbootEnabled then "${efiMount}/${rescueBaseDir}" else "/boot/${rescueBaseDir}";

  # resolve artifacts placement location
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

  derivedRescueSystem = evaluatedStage0.config.system.build.rescue;
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

  config = lib.mkIf cfg.enable (
    lib.mkMerge [
      {
        # prepare environment
        environment.systemPackages = [ failoverPkg ];
        environment.etc."failover/config.json".text = builtins.toJSON {
          bootloader_type = bootloader;
          esp_path = efiMount;
          rescue_entry_id = rescueEntryID;
          marker_dir = markerDir;
          watchdog_timeout_sec = cfg.watchdogTimeoutSec;
        };

        system.activationScripts.failover-install = ''
          mkdir -p ${rescueDir}
          cp -f ${derivedRescueSystem}/bzImage ${rescueKernelPath}
          cp -f ${derivedRescueSystem}/initrd  ${rescueInitrdPath}
        '';

        # SDBOOT
        boot.loader.systemd-boot.extraEntries = lib.mkIf sdbootEnabled (
          let
            mkRelativePath = path: (lib.removePrefix efiMount path);
          in
          {
            "${rescueEntryID}" = ''
              title NixOS Rescue (stage1-dd)
              linux ${mkRelativePath rescueKernelPath}
              initrd ${mkRelativePath rescueInitrdPath}
              options ${rescueKernelParams}
            '';
          }
        );

        # Hardware Watchdog, If kernel or PID 1 hangs, hardware watchdog forces power-cycle after 60s
        systemd.settings.Manager.RuntimeWatchdogSec = "60s";
        systemd.services.failover-watchdog = {
          description = "Failover Watchdog";
          wantedBy = [ "multi-user.target" ];
          path = lib.optional grubEnabled pkgs.grub2;
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
      }

      (lib.mkIf grubEnabled {
        # GRUB
        boot.loader.grub.default = lib.mkIf grubEnabled "saved";
        boot.loader.grub.extraConfig = lib.mkIf grubEnabled ''
          function savedefault {
            true
          }
        '';
        boot.loader.grub.extraEntries = lib.mkIf grubEnabled ''
          menuentry "${rescueEntryID}" {
            linux  ${rescueKernelPath} ${rescueKernelParams}
            initrd ${rescueInitrdPath}
          }
        '';
      })
    ]
  );

}
