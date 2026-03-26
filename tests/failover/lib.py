import time
import json


class Text(str):
    def __new__(cls, text: str):
        return super().__new__(cls, text)

    def hex_escape(self):
        return "".join(f"\\\\x{ord(c):02x}" for c in self)


def assert_status(cmd, expected_marker_state, expected_default, expected_oneshot):
    out = machine.succeed(f"{cmd} --json")
    data = json.loads(out)
    assert (
        data["marker_state"] == expected_marker_state
    ), f"Expected marker {expected_marker_state}, got {data['marker_state']}"
    bstate = data["bootloader_state"]
    if expected_default:
        assert (
            bstate["state_default"] == expected_default
        ), f"Expected default {expected_default}, got {bstate['state_default']}"
    if expected_oneshot:
        assert (
            bstate["state_oneshot"] == expected_oneshot
        ), f"Expected oneshot {expected_oneshot}, got {bstate['state_oneshot']}"


def validate_cli_behaviour_in_main():
    machine.start()
    machine.wait_for_unit("multi-user.target")

    # Initial State (Armed due to first-boot.marker)
    assert_status("failover status", "ARMED", "MAIN", "RESCUE")

    # mock for --root flags
    machine.succeed("mkdir -p /tmp/etc/failover /tmp/var/lib /tmp/boot")
    machine.succeed("cp /etc/failover/config.json /tmp/etc/failover/")
    machine.succeed("mount --bind /var/lib /tmp/var/lib")
    machine.succeed("mount --bind /boot /tmp/boot")

    # Pristine
    machine.succeed("failover --root /tmp confirm")
    assert_status("failover --root /tmp status", "CLEAR", "MAIN", "NONE")

    # Armed + trigger automatic reboot
    machine.succeed("failover --root /tmp arm")
    assert_status("failover --root /tmp status", "ARMED", "RESCUE", "MAIN")
    machine.succeed("systemctl start failover-watchdog.service")

    # Monitor watchdog reboot
    machine.wait_for_console_text("TIMEOUT REACHED! Rebooting!")
    machine.wait_for_shutdown()
    machine.start()


def boot_into_rescue_from_main(bootloader: str):
    machine.start()
    machine.wait_for_unit("multi-user.target")
    machine.succeed("failover confirm")
    if bootloader == "grub":
        machine.succeed(
            "grub-editenv /boot/grub/grubenv set next_entry='stage1-dd-rescue'"
        )
    else:
        machine.succeed("bootctl set-oneshot stage1-dd-rescue.conf")
    machine.succeed("sync")
    machine.shutdown()
    machine.start()


# assume kernel params (nixos_mode=stage1-rescue) is set
# and stage1.debug is enabled
def wait_for_stage1_rescue():
    # wait for rescue
    machine.wait_for_console_text("RESCUE_READY")
    time.sleep(3)
    # verify rescue
    in_rescue = Text("IN_RESCUE")
    machine.send_console(
        f"grep -q 'nixos_mode=stage1-rescue' /proc/cmdline && echo -e {in_rescue.hex_escape()}\n"
    )
    machine.wait_for_console_text(in_rescue)


# inside rescue -> init -> reboot
def init_in_resuce(bootloader):
    # mount main os
    machine.send_console("mkdir -p /mnt /mnt/boot\n")
    machine.send_console("mount /dev/disk/by-label/nixos /mnt\n")
    machine.send_console("mount /dev/disk/by-label/ESP /mnt/boot\n")  # UEFI ESP
    if bootloader == "sdboot":
        machine.send_console(
            "mount -o remount,rw /sys/firmware/efi/efivars 2>/dev/null || mount -t efivarfs efivarfs /sys/firmware/efi/efivars\n"
        )

    machine.send_console("failover --root /mnt confirm\n")
    init_done = Text("INIT_DONE")
    machine.send_console(
        f"failover --root /mnt init && echo -e {init_done.hex_escape()}\n"
    )
    machine.wait_for_console_text(init_done)

    machine.send_console("umount /mnt/boot /mnt\n")
    machine.send_console("sync && reboot -f\n")
    machine.wait_for_shutdown()
    machine.start()


def verify_init_result_in_main():
    # call start() anyway
    machine.start()
    machine.wait_for_unit("multi-user.target")

    # Wait for watchdog to start, confrm override
    machine.wait_for_console_text("Started Failover Watchdog.")
    assert_status("failover status", "ARMED", "RESCUE", "RESCUE")

    # Final Confirm
    machine.succeed("failover confirm")
    assert_status("failover status", "CLEAR", "MAIN", "NONE")
