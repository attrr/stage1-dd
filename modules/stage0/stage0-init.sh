#!/bin/busybox ash
/bin/busybox --install -s /bin

mount_essential_fs() {
    echo "Mounting essential filesystems..."
    mount -t proc proc /proc
    mount -t sysfs sysfs /sys
    mount -t devtmpfs devtmpfs /dev
}

load_modules() {
    echo "Loading core kernel modules..."
    modprobe loop 2>/dev/null
    modprobe squashfs 2>/dev/null
    modprobe overlay 2>/dev/null

    echo "Loading hardware modules..."
    for mod in @HARDWARE_MODULES@; do
        modprobe "$mod" 2>/dev/null
    done
}

wait_for_devices() {
    echo "Waiting for block devices to settle..."
    local settle_timeout=50
    local i=0
    while [ $i -lt $settle_timeout ]; do
        ls /sys/class/block/*/device 2>/dev/null | grep -q . && return 0
        i=$((i + 1))
        sleep 0.1
    done
}

parse_cmdline() {
    local cmdline=$(cat /proc/cmdline)
    for param in $cmdline; do
        case "$param" in
            root=*) kern_root="${param#root=}" ;;
            stage1.path=*) stage1_path="${param#stage1.path=}" ;;
        esac
    done
}

mount_squashfs() {
    local mounted=0

    # 1. Embedded in cpio
    if [ -f /stage1.squashfs ] && [ -z "$kern_root" ]; then
        mount -t squashfs -o loop /stage1.squashfs /mnt/ro && mounted=1
    fi

    # 2. root= kernel param: mount the partition, then loopback the squashfs file
    if [ "$mounted" = "0" ] && [ -n "$kern_root" ]; then
        mkdir -p /mnt/part
        mount "$kern_root" /mnt/part 2>/dev/null
        if [ -f "/mnt/part$stage1_path" ]; then
            mount -t squashfs -o loop "/mnt/part$stage1_path" /mnt/ro && mounted=1
        else
            echo "WARN: root=$kern_root mounted but $stage1_path not found"
            umount /mnt/part 2>/dev/null
        fi
    fi

    # 3. Raw block device scan (all virtio, scsi, ide, nvme disks)
    if [ "$mounted" = "0" ]; then
        for dev in /dev/vd* /dev/sd* /dev/sr* /dev/hd* /dev/nvme*n*; do
            [ -b "$dev" ] || continue
            echo "Scanning $dev for squashfs..."
            mount -t squashfs "$dev" /mnt/ro 2>/dev/null && mounted=1 && return 0
        done
    fi

	# final check
    if [ "$mounted" = "0" ]; then
        echo "FATAL: Cannot find stage1.squashfs"
        echo "  Tried: embedded cpio, root=$kern_root, block device scan"
        echo "Dropping to emergency shell..."
        exec /bin/ash
    fi
}

setup_overlay_and_switch() {
    echo "Setting up overlayfs..."
    mount -t tmpfs tmpfs /mnt/rw
    mkdir -p /mnt/rw/upper /mnt/rw/work
    mount -t overlay overlay \
        -o lowerdir=/mnt/ro,upperdir=/mnt/rw/upper,workdir=/mnt/rw/work \
        /mnt/root

    # Clean up before switch
    umount /proc /sys /dev 2>/dev/null

    # Hand off to the real init (systemd) inside the squashfs
    echo "Handing off to /init inside squashfs..."
    exec switch_root /mnt/root /init
}

main() {
    # Initialize globals defaults
    kern_root=""
    stage1_path="/stage1.squashfs"

    mount_essential_fs
    load_modules
    wait_for_devices
    
    parse_cmdline
    mount_squashfs
    setup_overlay_and_switch
}

main
