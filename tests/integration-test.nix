# tests/integration-test.nix
{
  pkgs ? import <nixpkgs> { },
  rescueSystem,
  ...
}:
let
  makeTest =
    pkgs.testers.runNixOSTest or (import (pkgs.path + "/nixos/tests/make-test-python.nix") {
      inherit pkgs;
      system = pkgs.stdenv.hostPlatform.system or "x86_64-linux";
    });

  # Common test script
  #
  # IMPORTANT: We must NEVER call methods that trigger connect(), because our
  # custom initrd does not run the NixOS backdoor.service on /dev/hvc0.
  # Forbidden: machine.succeed(), machine.execute(), machine.sleep(),
  #            machine.wait_for_unit(), machine.wait_until_succeeds(), etc.
  # Safe:      machine.start(), machine.wait_for_console_text(),
  #            machine.send_console(), machine.send_key()
  testScript = ''
    import time

    machine.start()

    # ── Stage 0 (busybox bootstrap) ──────────────────────────────
    machine.wait_for_console_text("Mounting essential filesystems")
    machine.wait_for_console_text("Setting up overlayfs")
    machine.wait_for_console_text("Handing off to /init inside squashfs")

    # ── Stage 1 (systemd initrd boot) ────────────────────────────
    machine.wait_for_console_text("RESCUE_READY")

    # ── Interactive shell ready — neutralize ZLE ─────────────────
    # Give the idle-type console-shell a moment to attach to /dev/console
    time.sleep(0.5)
    machine.send_console("\n")
    time.sleep(0.3)
    machine.send_console("unsetopt zle; export PS1='# '\n")
    time.sleep(0.3)

    # ── Verify functionality via beacon strings ──────────────────
    machine.send_console("mount | grep squashfs && echo SQUASHFS_OK\n")
    machine.wait_for_console_text("SQUASHFS_OK")

    machine.send_console("curl --version && echo CURL_OK\n")
    machine.wait_for_console_text("CURL_OK")

    machine.send_console("parted --version && echo PARTED_OK\n")
    machine.wait_for_console_text("PARTED_OK")

    # ── Verify SSH daemon is running ─────────────────────────────
    machine.send_console("ss -tlnp | grep ':22' && echo SSH_OK\n")
    machine.wait_for_console_text("SSH_OK")
  '';

  # Factory to create a test with specific RAM
  mkIntegrationTest =
    name: memorySize: useSeparated:
    makeTest {
      inherit name;

      nodes.machine =
        { lib, ... }:
        {
          virtualisation.memorySize = memorySize;

          # Prevent the framework from injecting its own -kernel/-initrd/-append.
          # Do NOT use useBootLoader (it generates a disk image + bootloader that
          # conflicts with our manual -kernel/-initrd in qemu.options).
          virtualisation.directBoot.enable = false;

          # Force QEMU to boot our mkRescue artifacts directly
          virtualisation.qemu.options = [
            "-kernel ${rescueSystem}/bzImage"
            "-initrd ${if useSeparated then "${rescueSystem}/stage0.initrd" else "${rescueSystem}/initrd"}"
            "-append \"console=ttyS0 panic=30 systemd.journald.forward_to_console=1 systemd.log_level=debug\""
          ]
          ++ lib.optionals useSeparated [
            "-drive file=${rescueSystem}/stage1.squashfs,if=virtio,format=raw,readonly=on"
          ];

          # Minimal dummy config just to satisfy the eval of the dummy node
          boot.loader.grub.enable = false;
          fileSystems."/" = {
            device = "tmpfs";
            fsType = "tmpfs";
          };
        };

      inherit testScript;
    };

in
{
  ram-256mb = mkIntegrationTest "rescue-256mb" 256 false;
  ram-512mb = mkIntegrationTest "rescue-512mb" 512 false;
  ram-1gb = mkIntegrationTest "rescue-1gb" 1024 false;
  ram-2gb = mkIntegrationTest "rescue-2gb" 2048 false;
  ram-separated-128mb = mkIntegrationTest "rescue-separated-128mb" 128 true;
  ram-separated-256mb = mkIntegrationTest "rescue-separated-256mb" 256 true;
  ram-separated-512mb = mkIntegrationTest "rescue-separated-512mb" 512 true;
}
