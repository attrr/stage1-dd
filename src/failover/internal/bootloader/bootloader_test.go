package bootloader

import (
	"testing"

	"github.com/foo/failover/internal/config"
)

func TestNewBootloader(t *testing.T) {
	cfgSystemd := &config.Config{
		BootloaderType: "systemd-boot",
		ESPPath:        "/boot",
		RescueEntryID:  "rescue.conf",
	}

	bl, err := New(cfgSystemd, "/mnt")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	sbl, ok := bl.(*SystemdBoot)
	if !ok {
		t.Errorf("expected *SystemdBoot, got %T", bl)
	}
	if sbl.RootPrefix != "/mnt" {
		t.Errorf("expected RootPrefix /mnt, got %s", sbl.RootPrefix)
	}

	cfgGrub := &config.Config{
		BootloaderType: "grub",
		RescueEntryID:  "rescue.conf",
	}

	bl2, err := New(cfgGrub, "")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	gbl, ok := bl2.(*GRUB)
	if !ok {
		t.Errorf("expected *GRUB, got %T", bl2)
	}
	if gbl.MainEntry != "" {
		t.Errorf("expected MainEntry to be empty after New, got %s", gbl.MainEntry)
	}

	cfgInvalid := &config.Config{
		BootloaderType: "invalid",
	}

	_, err = New(cfgInvalid, "")
	if err == nil {
		t.Error("expected error for invalid bootloader type, got nil")
	}
}
