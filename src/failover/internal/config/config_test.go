package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	// Test successful load
	validJSON := `{
		"bootloader_type": "systemd-boot",
		"esp_path": "/boot",
		"main_entry_id": "main.conf",
		"rescue_entry_id": "rescue.conf",
		"marker_dir": "/var/lib/failover"
	}`
	if err := os.WriteFile(configPath, []byte(validJSON), 0644); err != nil {
		t.Fatalf("failed to write valid config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.BootloaderType != "systemd-boot" {
		t.Errorf("expected bootloader_type systemd-boot, got %s", cfg.BootloaderType)
	}

	// Test non-existent file
	_, err = LoadConfig(filepath.Join(tempDir, "missing.json"))
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}

	// Test invalid JSON
	invalidJSONPath := filepath.Join(tempDir, "invalid.json")
	if err := os.WriteFile(invalidJSONPath, []byte(`{ invalid }`), 0644); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}
	_, err = LoadConfig(invalidJSONPath)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}

	// Test WatchdogTimeoutSec default
	if cfg.WatchdogTimeoutSec != DefaultWatchdogTimeoutSec {
		t.Errorf("expected default watchdog timeout %d, got %d", DefaultWatchdogTimeoutSec, cfg.WatchdogTimeoutSec)
	}

	// Test explicit WatchdogTimeoutSec
	customCfgPath := filepath.Join(tempDir, "custom.json")
	customJSON := `{
		"bootloader_type": "systemd-boot",
		"esp_path": "/boot",
		"main_entry_id": "main.conf",
		"rescue_entry_id": "rescue.conf",
		"marker_dir": "/var/lib/failover",
		"watchdog_timeout_sec": 300
	}`
	if err := os.WriteFile(customCfgPath, []byte(customJSON), 0644); err != nil {
		t.Fatalf("failed to write custom config: %v", err)
	}
	customCfg, err := LoadConfig(customCfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed for custom config: %v", err)
	}
	if customCfg.WatchdogTimeoutSec != 300 {
		t.Errorf("expected watchdog timeout 300, got %d", customCfg.WatchdogTimeoutSec)
	}
}
