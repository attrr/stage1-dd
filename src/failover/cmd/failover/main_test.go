package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/foo/failover/internal/bootloader"
	"github.com/foo/failover/internal/config"
	"github.com/foo/failover/internal/markers"
)

// mockBootloader implements bootloader.Bootloader for testing
type mockBootloader struct {
	armCalled        bool
	armRootPrefix    string
	setDefaultVal    string
	setDefaultRoot   string
	oneShotVal       string
	oneShotRoot      string
	clearOneShotRoot string
	confirmCalled    bool
	overrideCalled   bool
}

func (m *mockBootloader) Probe() error {
	return nil
}

func (m *mockBootloader) Arm() error {
	m.armCalled = true
	return nil
}
func (m *mockBootloader) Confirm() error {
	m.confirmCalled = true
	return nil
}
func (m *mockBootloader) SetDefault(entry string) error {
	m.setDefaultVal = entry
	return nil
}
func (m *mockBootloader) SetOneShot(entry string) error {
	m.oneShotVal = entry
	return nil
}
func (m *mockBootloader) ClearOneShot() error {
	m.clearOneShotRoot = "called" // reuse existing field for simple flag
	return nil
}
func (m *mockBootloader) Override() error {
	m.overrideCalled = true
	// To satisfy TestCmdWatchdog for now, though the interface changed
	m.oneShotVal = "rescue.conf"
	m.setDefaultVal = "rescue.conf"
	return nil
}

func (m *mockBootloader) Status() map[string]string {
	return map[string]string{}
}

// verify it implements bootloader.Bootloader
var _ bootloader.Bootloader = (*mockBootloader)(nil)

func setupTestCfg(t *testing.T) (*config.Config, string) {
	t.Helper()
	tmp := t.TempDir()

	// Setup etc/failover dir
	cfgDir := filepath.Join(tmp, "etc", "failover")
	os.MkdirAll(cfgDir, 0755)

	markerDir := filepath.Join(tmp, "var", "lib", "failover")
	os.MkdirAll(markerDir, 0755)

	cfg := &config.Config{
		BootloaderType:     "systemd-boot",
		RescueEntryID:      "rescue.conf",
		MarkerDir:          "var/lib/failover",
		WatchdogTimeoutSec: 1,
	}

	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(filepath.Join(cfgDir, "config.json"), cfgData, 0644)

	// Setup loader.conf and entries for Probe()
	loaderDir := filepath.Join(tmp, "loader")
	os.MkdirAll(loaderDir, 0755)
	os.WriteFile(filepath.Join(loaderDir, "loader.conf"), []byte("default rescue.conf\n"), 0644)

	entriesDir := filepath.Join(loaderDir, "entries")
	os.MkdirAll(entriesDir, 0755)
	os.WriteFile(filepath.Join(entriesDir, "rescue.conf"), []byte("title Rescue\n"), 0644)
	os.WriteFile(filepath.Join(entriesDir, "nixos-generation-1.conf"), []byte("title NixOS\n"), 0644)

	return cfg, tmp
}

// fakeExecCommand is used to mock os/exec.Command calls
func fakeExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// Extract the actual command
	args := os.Args[3:]
	if len(args) == 0 {
		os.Exit(0)
	}

	command := args[0]
	switch command {
	case "systemctl":
		if len(args) > 1 && args[1] == "is-active" {
			fmt.Print("inactive")
		}
		os.Exit(0)
	case "/run/current-system/sw/bin/reboot":
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %s", command)
		os.Exit(1)
	}
}

func TestCLI_Init(t *testing.T) {
	_, tmp := setupTestCfg(t)

	os.Setenv("FAILOVER_CONFIG_PATH", filepath.Join(tmp, "etc", "failover", "config.json"))
	defer os.Unsetenv("FAILOVER_CONFIG_PATH")

	// Override EFI paths to avoid accessing host /sys
	oldOneshot := bootloader.LoaderEntryOneShotPath
	oldEntries := bootloader.LoaderEntriesPath
	bootloader.LoaderEntryOneShotPath = filepath.Join(tmp, "efi-oneshot")
	bootloader.LoaderEntriesPath = filepath.Join(tmp, "efi-entries")
	defer func() {
		bootloader.LoaderEntryOneShotPath = oldOneshot
		bootloader.LoaderEntriesPath = oldEntries
	}()

	cli, err := Init(tmp)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if cli.rootPrefix != tmp {
		t.Errorf("expected rootPrefix %s, got %s", tmp, cli.rootPrefix)
	}
}

func TestCmdInit(t *testing.T) {
	cfg, tmp := setupTestCfg(t)
	bl := &mockBootloader{}
	cli := &CLI{
		rootPrefix: tmp,
		cfg:        *cfg,
		bootloader: bl,
		markers:    *markers.New(tmp, cfg.MarkerDir),
	}

	err := cli.commandInit()
	if err != nil {
		t.Fatalf("commandInit failed: %v", err)
	}
	if !bl.armCalled {
		t.Error("expected bl.Arm to be called (init is unified with arm)")
	}
}

func TestCmdArm(t *testing.T) {
	cfg, tmp := setupTestCfg(t)
	bl := &mockBootloader{}
	cli := &CLI{
		rootPrefix: tmp,
		cfg:        *cfg,
		bootloader: bl,
		markers:    *markers.New(tmp, cfg.MarkerDir),
	}

	err := cli.commandArm()
	if err != nil {
		t.Fatalf("commandArm failed: %v", err)
	}
	if !bl.armCalled {
		t.Error("expected bl.Arm to be called")
	}

	has, err := cli.markers.HasMarker("armed.marker")
	if err != nil || !has {
		t.Errorf("expected armed.marker to be created")
	}
}

func TestCmdConfirm(t *testing.T) {
	cfg, tmp := setupTestCfg(t)
	bl := &mockBootloader{}

	// Mock execCommand
	oldExecCommand := execCommand
	execCommand = fakeExecCommand
	defer func() { execCommand = oldExecCommand }()

	cli := &CLI{
		rootPrefix: tmp,
		cfg:        *cfg,
		bootloader: bl,
		markers:    *markers.New(tmp, cfg.MarkerDir),
	}

	err := cli.markers.CreateMarker("armed.marker")
	if err != nil {
		t.Fatalf("failed to setup marker: %v", err)
	}

	err = cli.commandConfirm()
	if err != nil {
		t.Errorf("commandConfirm failed: %v", err)
	}

	if !bl.confirmCalled {
		t.Error("expected bl.Confirm to be called")
	}

	has, _ := cli.markers.HasMarker("armed.marker")
	if has {
		t.Error("expected marker to be deleted")
	}
}

func TestCmdWatchdog(t *testing.T) {
	cfg, tmp := setupTestCfg(t)
	bl := &mockBootloader{}

	// Mock execCommand
	oldExecCommand := execCommand
	execCommand = fakeExecCommand
	defer func() { execCommand = oldExecCommand }()

	cli := &CLI{
		rootPrefix: tmp,
		cfg:        *cfg,
		bootloader: bl,
		markers:    *markers.New(tmp, cfg.MarkerDir),
	}

	cli.cfg.WatchdogTimeoutSec = 0

	// Case 1: No markers
	err := cli.commandWatchdog()
	if err != nil {
		t.Fatalf("commandWatchdog failed: %v", err)
	}
	if bl.oneShotVal != "" {
		t.Error("expected no oneshot to be set when no markers")
	}

	// Case 2: Marker present
	err = cli.markers.CreateMarker("armed.marker")
	if err != nil {
		t.Fatalf("failed to setup marker: %v", err)
	}

	cli.cfg.WatchdogTimeoutSec = 1
	go func() {
		time.Sleep(100 * time.Millisecond)
		syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()

	err = cli.commandWatchdog()
	if err != nil {
		t.Fatalf("commandWatchdog failed: %v", err)
	}

	if bl.oneShotVal != cfg.RescueEntryID {
		t.Errorf("expected oneshot to be %s, got %s", cfg.RescueEntryID, bl.oneShotVal)
	}
	if bl.setDefaultVal != cfg.RescueEntryID {
		t.Errorf("expected default to be %s, got %s", cfg.RescueEntryID, bl.setDefaultVal)
	}
}

func TestCmdStatus(t *testing.T) {
	cfg, tmp := setupTestCfg(t)
	bl := &mockBootloader{}
	cli := &CLI{
		rootPrefix: tmp,
		cfg:        *cfg,
		bootloader: bl,
		markers:    *markers.New(tmp, cfg.MarkerDir),
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := cli.commandStatus()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()

	if err != nil {
		t.Fatalf("commandStatus failed: %v", err)
	}
	if !strings.Contains(out, "Failover Status") {
		t.Error("expected output to contain 'Failover Status'")
	}
}
