package bootloader

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// helper to fake execCommand
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

	if args[0] == "grub-editenv" {
		if len(args) < 4 {
			fmt.Fprintf(os.Stderr, "not enough arguments")
			os.Exit(1)
		}

		filePath := args[1]
		command := args[2]
		setting := args[3]

		if command != "set" && command != "unset" {
			fmt.Fprintf(os.Stderr, "unknown command %s", command)
			os.Exit(1)
		}

		if command == "set" {
			parts := strings.SplitN(setting, "=", 2)
			if len(parts) != 2 {
				fmt.Fprintf(os.Stderr, "invalid setting %s", setting)
				os.Exit(1)
			}
		}

		// In a real scenario grub-editenv writes to the 1024-byte block
		// We'll simulate modifying the file by just appending to it for testing purposes
		f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open grubenv: %v", err)
			os.Exit(1)
		}
		defer f.Close()

		if command == "set" {
			fmt.Fprintf(f, "\n%s", setting)
		} else {
			// for unset, we just append a marker for testing
			fmt.Fprintf(f, "\n%s=", setting)
		}
		os.Exit(0)
	}

	fmt.Fprintf(os.Stderr, "unknown command %s", args[0])
	os.Exit(1)
}

func TestGRUBArm(t *testing.T) {
	tempDir := t.TempDir()

	grubDir := filepath.Join(tempDir, "boot", "grub")
	if err := os.MkdirAll(grubDir, 0755); err != nil {
		t.Fatalf("failed to setup grub dir: %v", err)
	}

	grubenvPath := filepath.Join(grubDir, "grubenv")
	if err := os.WriteFile(grubenvPath, []byte("# GRUB\n"), 0644); err != nil {
		t.Fatalf("failed to write grubenv: %v", err)
	}

	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	oldGrubenvPathBase := grubenvPathBase
	grubenvPathBase = "/boot/grub/grubenv"
	defer func() { grubenvPathBase = oldGrubenvPathBase }()

	g := &GRUB{
		RootPrefix:  tempDir,
		MainEntry:   "main.conf",
		RescueEntry: "rescue.conf",
		State: State{
			currentDefault: DEFAULT_MAIN,
			currentOneshot: ONESHOT_NONE,
		},
	}

	err := g.Arm()
	if err != nil {
		t.Fatalf("Arm failed: %v", err)
	}

	// Verify both saved_entry and next_entry were set
	data, err := os.ReadFile(grubenvPath)
	if err != nil {
		t.Fatalf("failed to read grubenv: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "saved_entry=rescue.conf") {
		t.Errorf("expected saved_entry=rescue.conf, got: %s", content)
	}
	if !strings.Contains(content, "next_entry=main.conf") {
		t.Errorf("expected next_entry=main.conf, got: %s", content)
	}

	// Test Arm with missing file
	os.Remove(grubenvPath)
	err = g.Arm()
	if err == nil {
		t.Error("Arm should fail if grubenv is missing")
	}
}

func TestGRUBArmAndSetDefault(t *testing.T) {
	tempDir := t.TempDir()

	grubenvFile := filepath.Join(tempDir, "grubenv")
	if err := os.WriteFile(grubenvFile, []byte("# GRUB\n"), 0644); err != nil {
		t.Fatalf("failed to write initial grubenv: %v", err)
	}

	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	oldGrubenvPathBase := grubenvPathBase
	grubenvPathBase = grubenvFile
	defer func() { grubenvPathBase = oldGrubenvPathBase }()

	g := &GRUB{
		MainEntry:   "main.conf",
		RescueEntry: "rescue.conf",
		State: State{
			currentDefault: DEFAULT_MAIN,
			currentOneshot: ONESHOT_NONE,
		},
	}

	err := g.Arm()
	if err != nil {
		t.Fatalf("Arm failed: %v", err)
	}

	// Test Arm failure (SetDefault fails)
	execCommand = func(command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", "grub-editenv", "fail"}
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
		return cmd
	}
	err = g.Arm()
	if err == nil {
		t.Error("Arm should fail if SetDefault fails")
	}
	execCommand = fakeExecCommand // restore

	// Test Arm failure (setEnv fails for next_entry)
	execCommand = func(command string, args ...string) *exec.Cmd {
		// When command is exec.Command("grub-editenv", "/boot/...", "set", "next_entry=main.conf")
		// The args here are actually those passed to fakeExecCommand if we intercept it,
		// but since we are replacing execCommand, args are ["grubenvPath", "set", "next_entry=..."]
		if len(args) > 2 && args[1] == "set" && strings.HasPrefix(args[2], "next_entry") {
			cs := []string{"-test.run=TestHelperProcess", "--", "grub-editenv", "fail"}
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
			return cmd
		}
		return fakeExecCommand(command, args...)
	}
	err = g.Arm()
	if err == nil {
		t.Error("Arm should fail if setEnv fails for next_entry")
	}
	execCommand = fakeExecCommand // restore

}

func TestGRUBSetDefault(t *testing.T) {
	tempDir := t.TempDir()
	grubenvFile := filepath.Join(tempDir, "grubenv")
	os.WriteFile(grubenvFile, []byte("# GRUB\n"), 0644)

	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()
	oldGrubenvPathBase := grubenvPathBase
	grubenvPathBase = grubenvFile
	defer func() { grubenvPathBase = oldGrubenvPathBase }()

	g := &GRUB{}
	if err := g.SetDefault("rescue.conf"); err != nil {
		t.Fatalf("SetDefault failed: %v", err)
	}
	data, _ := os.ReadFile(grubenvFile)
	if !strings.Contains(string(data), "saved_entry=rescue.conf") {
		t.Error("SetDefault did not set saved_entry")
	}
}

func TestGRUBSetEnvError(t *testing.T) {
	// Mock execCommand to fail
	execCommand = func(command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", "grub-editenv", "fail"}
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
		return cmd
	}
	defer func() { execCommand = exec.Command }()

	g := &GRUB{}
	err := g.SetDefault("main.conf")
	if err == nil {
		t.Error("SetDefault should fail when grub-editenv fails")
	}
}

func TestGRUBSetOneShot(t *testing.T) {
	tempDir := t.TempDir()

	grubenvFile := filepath.Join(tempDir, "grubenv")
	if err := os.WriteFile(grubenvFile, []byte("# GRUB\n"), 0644); err != nil {
		t.Fatalf("failed to write initial grubenv: %v", err)
	}

	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	oldGrubenvPathBase := grubenvPathBase
	grubenvPathBase = grubenvFile
	defer func() { grubenvPathBase = oldGrubenvPathBase }()

	g := &GRUB{}

	err := g.SetOneShot("main.conf")
	if err != nil {
		t.Fatalf("SetOneShot failed: %v", err)
	}

	data, err := os.ReadFile(grubenvFile)
	if err != nil {
		t.Fatalf("failed to read grubenv: %v", err)
	}
	if !strings.Contains(string(data), "next_entry=main.conf") {
		t.Errorf("expected next_entry=main.conf, got: %s", string(data))
	}
}

func TestGRUBClearOneShot(t *testing.T) {
	tempDir := t.TempDir()

	grubenvFile := filepath.Join(tempDir, "grubenv")
	if err := os.WriteFile(grubenvFile, []byte("# GRUB\n"), 0644); err != nil {
		t.Fatalf("failed to write initial grubenv: %v", err)
	}

	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	oldGrubenvPathBase := grubenvPathBase
	grubenvPathBase = grubenvFile
	defer func() { grubenvPathBase = oldGrubenvPathBase }()

	g := &GRUB{}

	err := g.ClearOneShot()
	if err != nil {
		t.Fatalf("ClearOneShot failed: %v", err)
	}

	data, err := os.ReadFile(grubenvFile)
	if err != nil {
		t.Fatalf("failed to read grubenv: %v", err)
	}
	if !strings.Contains(string(data), "next_entry=") {
		t.Errorf("expected next_entry= (empty), got: %s", string(data))
	}
}
