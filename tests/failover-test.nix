# tests/failover-grub-test.nix
{
  pkgs ? import <nixpkgs> { },
  failoverModule,
  ...
}:
let
  makeTest =
    pkgs.testers.runNixOSTest or (import (pkgs.path + "/nixos/tests/make-test-python.nix") {
      inherit pkgs;
      system = pkgs.stdenv.hostPlatform.system or "x86_64-linux";
    });

  grubConfig = {
    virtualisation.useEFIBoot = false;
    boot.loader.grub.enable = true;
    boot.loader.grub.device = "/dev/vda";
  };
  sdbootConfig = {
    boot.loader.systemd-boot.enable = true;

    virtualisation.useEFIBoot = true;
    boot.loader.efi.canTouchEfiVariables = true;
    boot.kernelModules = [ "efivarfs" ];
  };
  commonConfig =
    { lib, ... }:
    {
      imports = [ failoverModule ];
      virtualisation.memorySize = 4096;
      virtualisation.cores = 2;
      virtualisation.useBootLoader = true;

      services.failover = {
        enable = true;
        watchdogTimeoutSec = lib.mkDefault 60;
        rescue.kernelParams = [
          "console=tty0"
          "console=ttyS0,115200n8"
          "systemd.journald.forward_to_console=1"
          "rd.systemd.gpt_auto=0"
          "panic=1"
          "nixos_mode=stage1-rescue"
        ];
        rescue.extraConfig = {
          stage1.debug = true;
        };
      };

      system.activationScripts.test-marker = ''
        DONE_MARKER="/var/lib/failover/.initialized"

        if [ ! -f "$DONE_MARKER" ]; then
          echo "First time boot: Initializing..."
          mkdir -p /var/lib/failover
          touch /var/lib/failover/first-boot.marker
          
          touch "$DONE_MARKER"
        fi
      '';

      boot.initrd.availableKernelModules = [
        "virtio_pci"
        "virtio_blk"
      ];
    };

in
{
  # 1. Trigger Test
  failover-trigger = makeTest {
    name = "failover-trigger";
    nodes.machine = {
      imports = [
        commonConfig
        sdbootConfig
      ];
      services.failover.watchdogTimeoutSec = 30;
    };
    testScript = ''
      ${builtins.readFile ./failover/lib.py}
      validate_cli_behaviour_in_main()
      wait_for_stage1_rescue()
    '';
  };

  failover-grub-trigger = makeTest {
    name = "failover-grub-trigger";
    nodes.machine = {
      imports = [
        commonConfig
        grubConfig
      ];
      services.failover.watchdogTimeoutSec = 30;
    };
    testScript = ''
      ${builtins.readFile ./failover/lib.py}
      validate_cli_behaviour_in_main()
      wait_for_stage1_rescue()
    '';
  };

  # 2. Recovery Test
  failover-recovery = makeTest {
    name = "failover-recovery";
    nodes.machine.imports = [
      commonConfig
      sdbootConfig
    ];
    testScript = ''
      ${builtins.readFile ./failover/lib.py}
      bootloader = "sdboot"
      boot_into_rescue_from_main(bootloader)
      wait_for_stage1_rescue()
      init_in_resuce(bootloader)
      verify_init_result_in_main()
    '';
  };

  failover-grub-recovery = makeTest {
    name = "failover-grub-recovery";
    nodes.machine.imports = [
      commonConfig
      grubConfig
    ];
    testScript = ''
      ${builtins.readFile ./failover/lib.py}
      bootloader = "grub"
      boot_into_rescue_from_main(bootloader)
      wait_for_stage1_rescue()
      init_in_resuce(bootloader)
      verify_init_result_in_main()
    '';
  };
}
