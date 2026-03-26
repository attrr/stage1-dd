package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/foo/failover/internal/bootloader"
	"github.com/foo/failover/internal/config"
	"github.com/foo/failover/internal/markers"
	"golang.org/x/sys/unix"
)

var execCommand = exec.Command

func cliUsage() {
	fmt.Printf("Usage: %s <command> [flags]", os.Args[0])
	flag.PrintDefaults()
	fmt.Println("Commands:")
	fmt.Println("  init [--root=/mnt]    Offline: verify Rescue default, set OneShot to Main, verify first-boot marker")
	fmt.Println("  arm                   Online: set Default to Rescue, OneShot to Main, create marker")
	fmt.Println("  confirm               Online: stop watchdog, restore Default to Main, clear oneshot, delete markers")
	fmt.Println("  watchdog              Service: check markers, set Rescue as oneshot+default, wait and reboot")
	fmt.Println("  status [--root=/mnt]  Diagnostic: show current state vector [S_default, S_oneshot, S_marker]")
}

func main() {
	// global level flags
	rootPtr := flag.String("root", "", "Target root prefix")
	flag.Usage = cliUsage
	flag.Parse()

	cli, err := Init(*rootPtr)
	if err != nil {
		log.Fatal(err)
	}
	if flag.NArg() < 1 {
		cliUsage()
		os.Exit(1)
	}

	subcommand := flag.Arg(0)
	cli.args = flag.Args()[1:]
	err = cli.commandDispatcher(subcommand)
	if err != nil {
		log.Fatal(err)
	}
}

type CLI struct {
	rootPrefix string
	cfg        config.Config
	bootloader bootloader.Bootloader
	markers    markers.Markers
	args       []string
}

func Init(rootPrefix string) (*CLI, error) {
	cfg, err := config.LoadResolvedConfig(rootPrefix)
	if err != nil || cfg == nil {
		return nil, fmt.Errorf("failed to load config from %s: %v", rootPrefix, err)
	}
	markers := markers.New(rootPrefix, cfg.MarkerDir)

	bl, err := bootloader.New(cfg, rootPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize bootloader: %w", err)
	}

	if err = bl.Probe(); err != nil {
		return nil, fmt.Errorf("failed to probe current system state: %w", err)
	}
	return &CLI{
		rootPrefix: rootPrefix,
		cfg:        *cfg,
		bootloader: bl,
		markers:    *markers,
	}, nil
}

type CLISubcommand func() error

func (cli *CLI) commandDispatcher(subcommand string) error {
	table := map[string]CLISubcommand{
		"init":     cli.commandInit,
		"arm":      cli.commandArm,
		"confirm":  cli.commandConfirm,
		"watchdog": cli.commandWatchdog,
		"status":   cli.commandStatus,
	}

	subfunc, ok := table[subcommand]
	if !ok {
		return fmt.Errorf("unknown command: %s", subcommand)
	}

	if err := subfunc(); err != nil {
		return err
	}
	return nil
}

// commandInit implements `failover init`:
// Runs from the rescue environment with --root pointing to the mounted target disk.
func (cli *CLI) commandInit() error {
	// ensure first-boot.marker exists
	hasFirstBoot, err := cli.markers.HasMarker("first-boot.marker")
	if err != nil {
		return fmt.Errorf("failed to check first-boot.marker: %v", err)
	}
	if !hasFirstBoot {
		log.Printf("Warning: first-boot.marker not found in %s. Initializing anyway.", cli.cfg.MarkerDir)
		if err := cli.markers.CreateMarker("first-boot.marker"); err != nil {
			return fmt.Errorf("failed to init first-boot.marker due to: %v, exit....", err)
		}
	}
	// arm bootloader, complete transition to [Rescue, Main, True]
	if err := cli.bootloader.Arm(); err != nil {
		return fmt.Errorf("failed to initialize bootloader state: %w, degrating to watchdog only", err)
	}
	fmt.Printf("Initialization complete. Target system (root=%s) is now ARMED.\n", cli.rootPrefix)
	return nil
}

// commandArm implements `failover arm`:
// Runs from the live main system (online).
func (cli *CLI) commandArm() error {
	// prepond S_marker to handle degradation
	if err := cli.markers.CreateMarker("armed.marker"); err != nil {
		return fmt.Errorf("failed to create marker: %v", err)
	}

	// Arm the bootloader (S = [Rescue, Main, True])
	// if degraded, (S = [Main, None, True])
	if err := cli.bootloader.Arm(); err != nil {
		return fmt.Errorf("failed to arm bootloader: %w, degrating to watchdog only", err)
	}
	return nil
}

// commandConfirm implements `failover confirm` with atomic protocol (§2.5):
// Runs from the live main system after SSH login.
func (cli *CLI) commandConfirm() error {
	// stop the watchdog service synchronously
	stopCmd := execCommand("systemctl", "stop", "failover-watchdog.service")
	if out, err := stopCmd.CombinedOutput(); err != nil {
		log.Printf("Warning: failed to stop watchdog service: %v (output: %s)", err, string(out))
	}
	// verify service is inactive/dead
	verifyCmd := execCommand("systemctl", "is-active", "failover-watchdog.service")
	out, _ := verifyCmd.CombinedOutput()
	statusStr := strings.TrimSpace(string(out))
	if statusStr != "inactive" && statusStr != "dead" && statusStr != "" {
		log.Printf("Warning: watchdog service status is '%s', expected inactive/dead", statusStr)
	}

	// confirm Bootloader State (S_default = Main, S_oneshot = None)
	if err := cli.bootloader.Confirm(); err != nil {
		log.Printf("Warning: failed to confirm bootloader state: %v", err)
	}

	// clear markers
	if err := cli.markers.DeleteAllMarkers(); err != nil {
		return fmt.Errorf("failed to delete markers: %v", err)
	}
	return nil
}

func forceReboot() {
	log.Println("Attempting reboot via /run/current-system/sw/bin/reboot...")
	if err := execCommand("/run/current-system/sw/bin/reboot").Run(); err != nil {
		log.Printf("reboot command failed: %v, falling back to syscall", err)
	}
	log.Println("Attempting reboot via unix.Reboot syscall...")
	if err := unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART); err != nil {
		log.Printf("unix.Reboot failed: %v, falling back to sysrq-trigger", err)
	}
	log.Println("Attempting reboot via /proc/sysrq-trigger...")
	os.WriteFile("/proc/sysrq-trigger", []byte("b"), 0200)
}

// commandWatchdog implements `failover watchdog`:
// Runs as a systemd service on the main system.
func (cli *CLI) commandWatchdog() error {
	cfg := cli.cfg
	// check markers
	hasMarker, err := cli.markers.HasAnyMarker()
	if err != nil {
		return fmt.Errorf("failed to check markers: %v", err)
	}
	if !hasMarker {
		fmt.Println("No markers found. System is safe.")
		daemon.SdNotify(false, daemon.SdNotifyReady)
		daemon.SdNotify(false, daemon.SdNotifyStopping)
		return nil
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)

	countdown := time.NewTimer(time.Duration(cfg.WatchdogTimeoutSec) * time.Second)
	defer countdown.Stop()

	heartbeat := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()

	fmt.Println("Marker found. Waiting for confirmation...")
	// Set OS_oneshot = Rescue as backup
	if err := cli.bootloader.Override(); err != nil {
		log.Printf("failed to set rescue one-shot: %v", err)
	}
	if sent, _ := daemon.SdNotify(false, daemon.SdNotifyReady); !sent {
		log.Println("WARNING: Failed to send READY=1 to systemd. Is Type=notify configured?")
	}

	log.Printf("Shutdown in %d seconds...\n", cfg.WatchdogTimeoutSec)
	for {
		select {
		case <-sigs:
			log.Println("Received SIGTERM. system is stable.")
			// Confirm should clear oneshot here
			daemon.SdNotify(false, daemon.SdNotifyStopping)
			return nil

		case <-heartbeat.C:
			daemon.SdNotify(false, daemon.SdNotifyWatchdog)

		case <-countdown.C:
			log.Println("TIMEOUT REACHED! Rebooting!")
			forceReboot()
			// shouldn't reach here
			log.Fatal("FATAL: reboot failed")
		}
	}
}

// runStatus implements `failover status`:
// Supports --root for inspecting target system state from rescue.
func (cli *CLI) commandStatus() error {
	subset := flag.NewFlagSet("status", flag.ExitOnError)
	jsonOutput := subset.Bool("json", false, "Enable json output")
	if err := subset.Parse(cli.args); err != nil {
		return err
	}

	cfg := cli.cfg
	hasMarker, err := cli.markers.HasAnyMarker()

	if *jsonOutput {
		out := struct {
			MarkerState     string            `json:"marker_state"`
			MarkerError     string            `json:"marker_error,omitempty"`
			Config          config.Config     `json:"config"`
			RootPrefix      string            `json:"root_prefix,omitempty"`
			PresentMarkers  []string          `json:"present_markers"`
			BootloaderState map[string]string `json:"bootloader_state"`
		}{
			Config:          cfg,
			RootPrefix:      cli.rootPrefix,
			BootloaderState: cli.bootloader.Status(),
		}

		if err != nil {
			out.MarkerState = "ERROR"
			out.MarkerError = err.Error()
		} else if hasMarker {
			out.MarkerState = "ARMED"
		} else {
			out.MarkerState = "CLEAR"
		}

		out.PresentMarkers = []string{}
		for _, name := range []string{"first-boot.marker", "armed.marker"} {
			if has, _ := cli.markers.HasMarker(name); has {
				out.PresentMarkers = append(out.PresentMarkers, name)
			}
		}

		b, marshalErr := json.MarshalIndent(out, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(b))
		return nil
	}

	fmt.Println("=== Failover Status ===")
	if err != nil {
		fmt.Printf("S_marker:  ERROR (%v)\n", err)
	} else if hasMarker {
		fmt.Println("S_marker:  ARMED (markers present)")
	} else {
		fmt.Println("S_marker:  CLEAR (no markers)")
	}

	c, _ := config.ResolveConfigPath(cli.rootPrefix)
	fmt.Printf("\nConfig (%s):\n", c)
	fmt.Printf("  bootloader_type:      %s\n", cfg.BootloaderType)
	// fmt.Printf("  main_entry_id:        %s\n", cfg.MainEntryID)
	fmt.Printf("  rescue_entry_id:      %s\n", cfg.RescueEntryID)
	fmt.Printf("  marker_dir:           %s\n", cfg.MarkerDir)
	fmt.Printf("  watchdog_timeout_sec: %d\n", cfg.WatchdogTimeoutSec)
	if cli.rootPrefix != "" {
		fmt.Printf("  root_prefix:          %s\n", cli.rootPrefix)
	}

	for _, name := range []string{"first-boot.marker", "armed.marker"} {
		has, _ := cli.markers.HasMarker(name)
		if has {
			fmt.Printf("           └─ %s: present\n", name)
		}
	}
	fmt.Printf("\nBootloader State:\n")
	bState := cli.bootloader.Status()
	keys := make([]string, 0, len(bState))
	for k := range bState {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %s: %s\n", k, bState[k])
	}

	return nil
}
