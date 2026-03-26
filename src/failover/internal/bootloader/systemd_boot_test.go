package bootloader

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestSystemdBootArm(t *testing.T) {
	tempDir := t.TempDir()

	loaderDir := filepath.Join(tempDir, "boot", "loader")
	if err := os.MkdirAll(loaderDir, 0755); err != nil {
		t.Fatalf("failed to setup loader dir: %v", err)
	}

	confPath := filepath.Join(loaderDir, "loader.conf")
	confData := "timeout 3\ndefault main.conf\n"
	if err := os.WriteFile(confPath, []byte(confData), 0644); err != nil {
		t.Fatalf("failed to write loader.conf: %v", err)
	}

	efiFile := filepath.Join(tempDir, "LoaderEntryOneShot-xxx")
	oldOneshotPath := LoaderEntryOneShotPath
	LoaderEntryOneShotPath = efiFile
	defer func() { LoaderEntryOneShotPath = oldOneshotPath }()

	sb := &SystemdBoot{
		ESPPath:     "/boot",
		RootPrefix:  tempDir,
		MainEntry:   "main.conf",
		RescueEntry: "rescue.conf",
		State: State{
			currentDefault: DEFAULT_MAIN,
			currentOneshot: ONESHOT_NONE,
		},
	}

	err := sb.Arm()
	if err != nil {
		t.Fatalf("Arm failed: %v", err)
	}

	// Verify loader.conf default was set to rescue
	newData, _ := os.ReadFile(confPath)
	if !strings.Contains(string(newData), "default rescue.conf") {
		t.Errorf("expected default rescue.conf in loader.conf, got: %s", string(newData))
	}

	// Verify EFI One-Shot variable set to main
	efiData, err := os.ReadFile(efiFile)
	if err != nil {
		t.Fatalf("failed to read efi var: %v", err)
	}
	// (Binary check omitted for brevity, string check follows)
	if !bytes.Contains(efiData, []byte("m\x00a\x00i\x00n\x00.\x00c\x00o\x00n\x00f\x00")) {
		t.Error("EFI var does not contain main.conf")
	}

	// Test Arm missing loader.conf
	os.Remove(confPath)
	err = sb.Arm()
	if err == nil {
		t.Error("Arm should fail if loader.conf is missing")
	}
}

func TestSystemdBootSetDefault(t *testing.T) {
	tempDir := t.TempDir()
	confPath := filepath.Join(tempDir, "loader", "loader.conf")
	os.MkdirAll(filepath.Dir(confPath), 0755)
	os.WriteFile(confPath, []byte("default main.conf\n"), 0644)

	sb := &SystemdBoot{ESPPath: tempDir}
	if err := sb.SetDefault("rescue.conf"); err != nil {
		t.Fatalf("SetDefault failed: %v", err)
	}
	data, _ := os.ReadFile(confPath)
	if !strings.Contains(string(data), "default rescue.conf") {
		t.Error("SetDefault did not update loader.conf")
	}
}

func TestSystemdBootSetOneShot(t *testing.T) {
	tempDir := t.TempDir()
	efiFile := filepath.Join(tempDir, "efi-oneshot")

	oldOneshotPath := LoaderEntryOneShotPath
	LoaderEntryOneShotPath = efiFile
	defer func() { LoaderEntryOneShotPath = oldOneshotPath }()

	sb := &SystemdBoot{}

	err := sb.SetOneShot("rescue.conf")
	if err != nil {
		t.Fatalf("SetOneShot failed: %v", err)
	}

	// Verify file was written
	data, err := os.ReadFile(efiFile)
	if err != nil {
		t.Fatalf("failed to read EFI var: %v", err)
	}

	// Parse the UTF-16LE string (skip 4-byte attribute header, skip 2-byte null terminator)
	var utf16Chars []uint16
	for i := 4; i < len(data)-2; i += 2 {
		var char uint16
		binary.Read(bytes.NewReader(data[i:i+2]), binary.LittleEndian, &char)
		utf16Chars = append(utf16Chars, char)
	}
	decoded := string(utf16.Decode(utf16Chars))
	if decoded != "rescue.conf" {
		t.Errorf("expected 'rescue.conf', got %q", decoded)
	}
}

func TestSystemdBootClearOneShot(t *testing.T) {
	tempDir := t.TempDir()
	efiFile := filepath.Join(tempDir, "efi-oneshot")

	oldOneshotPath := LoaderEntryOneShotPath
	LoaderEntryOneShotPath = efiFile
	defer func() { LoaderEntryOneShotPath = oldOneshotPath }()

	sb := &SystemdBoot{
		ESPPath: tempDir,
	}

	// Write a variable first
	sb.SetOneShot("main.conf")

	// Clear it
	err := sb.ClearOneShot()
	if err != nil {
		t.Fatalf("ClearOneShot failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(efiFile); !os.IsNotExist(err) {
		t.Error("EFI var file still exists after ClearOneShot")
	}

	// Clear again (idempotent — file already gone)
	err = sb.ClearOneShot()
	if err != nil {
		t.Errorf("ClearOneShot should be idempotent, got: %v", err)
	}
}
