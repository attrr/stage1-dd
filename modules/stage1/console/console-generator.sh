#!/bin/sh
outdir="$1/initrd.target.wants"
mkdir -p "$outdir"

has_console=0
for arg in $(cat /proc/cmdline); do
    case "$arg" in
    console=*)
        has_console=1
        val="${arg#console=}"
        tty="${val%%,*}"
        ln -sf "/etc/systemd/system/console-shell@.service" "$outdir/console-shell@$tty.service"
        ;;
    esac
done

if [ "$has_console" -eq 0 ]; then
    ln -sf "/etc/systemd/system/console-shell@.service" "$outdir/console-shell@ttyS0.service"
    ln -sf "/etc/systemd/system/console-shell@.service" "$outdir/console-shell@tty0.service"
fi