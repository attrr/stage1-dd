package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"

	securejoin "github.com/cyphar/filepath-securejoin"
)

type Config struct {
	BootloaderType     string `json:"bootloader_type"` // "systemd-boot" or "grub"
	ESPPath            string `json:"esp_path"`
	RescueEntryID      string `json:"rescue_entry_id"`
	MarkerDir          string `json:"marker_dir"`
	WatchdogTimeoutSec int    `json:"watchdog_timeout_sec"`
	EFIVarPath         string `json:"efi_var_path"` // optional override for EFI variable path
}

const DefaultWatchdogTimeoutSec = 300
const defaultConfigDirName = "failover"
const defaultConfigName = "config.json"

func gatheringConfigDir() []string {
	dirs := []string{}
	if envDir := os.Getenv("FAILOVER_CONFIG_DIR"); envDir != "" {
		dirs = append(dirs, path.Join(envDir))
	}
	// default search dirs
	return slices.Concat(dirs, []string{
		filepath.Join("/etc", defaultConfigDirName),
		filepath.Join("/nix/var/nix/profiles/system/etc", defaultConfigDirName),
	})
}

func ResolveConfigPath(rootPrefix string) (string, error) {
	searchPaths := []string{}
	env := os.Getenv("FAILOVER_CONFIG_PATH")
	if env != "" {
		searchPaths = append(searchPaths, env)
	}

	configDirs := gatheringConfigDir()
	for _, dir := range configDirs {
		searchPaths = append(searchPaths, filepath.Join(dir, defaultConfigName))
	}

	var fullpath string
	var err error
	for _, path := range searchPaths {
		fullpath, err = securejoin.SecureJoin(rootPrefix, path)
		if err != nil {
			return "", err
		}
		if fi, err := os.Stat(fullpath); err == nil && !fi.IsDir() {
			return fullpath, nil
		}
	}
	return "", fmt.Errorf("failed to resolve a valid config")
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.WatchdogTimeoutSec <= 0 {
		cfg.WatchdogTimeoutSec = DefaultWatchdogTimeoutSec
	}

	return &cfg, nil
}

func LoadResolvedConfig(rootPrefix string) (*Config, error) {
	path, err := ResolveConfigPath(rootPrefix)
	if err != nil {
		return nil, err
	}
	return LoadConfig(path)
}
