package bootloader

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var execCommand = exec.Command
var grubenvPathBase = "/boot/grub/grubenv"
var grubCfgPathBase = "/boot/grub/grub.cfg"

// Default Main regex for NixOS. It matches "NixOS" or "NixOS - Configuration X ..."
var defaultGrubMainRegex = regexp.MustCompile(`^NixOS.*`)

type GRUB struct {
	Env         map[string]string
	cfgEntries  []GrubEntry
	State       State
	RootPrefix  string
	MainEntry   string
	RescueEntry string
	MainRegex   *regexp.Regexp // Used to discover MainEntry if it's empty
}

func (g *GRUB) resolveEnvPath() string {
	if g.RootPrefix != "" {
		return filepath.Join(g.RootPrefix, grubenvPathBase)
	}
	return grubenvPathBase
}

func (g *GRUB) resolveCfgPath() string {
	if g.RootPrefix != "" {
		return filepath.Join(g.RootPrefix, grubCfgPathBase)
	}
	return grubCfgPathBase
}

func (g *GRUB) ParseEnv() error {
	path := g.resolveEnvPath()
	if _, err := os.Stat(path); err != nil {
		return err
	}

	cmd := execCommand("grub-editenv", path, "list")
	out, err := cmd.Output()
	if err != nil {
		return err
	}

	env := make(map[string]string)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	g.Env = env
	return nil
}

// GrubEntry represents a parsed menuentry from grub.cfg
type GrubEntry struct {
	ID    string // e.g., "0", "1>0", "1>1"
	Title string // The literal string between quotes
}

func extractGrubTitle(line string) string {
	var quoteChar rune
	start := -1
	for i, char := range line {
		if start == -1 {
			if char == '"' || char == '\'' {
				quoteChar = char
				start = i + 1
			}
		} else {
			if char == quoteChar {
				return line[start:i]
			}
		}
	}
	return ""
}

// parseGrubCfg completely parses grub.cfg to extract all menuentries and submenus,
// assigning them GRUB-compatible hierarchical IDs (e.g., "0", "1", "1>0").
func (g *GRUB) ParseCfgEntries() error {
	path := g.resolveCfgPath()
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	var entries []GrubEntry
	scanner := bufio.NewScanner(file)

	topIndex, subIndex := 0, 0
	depth := 0
	inSubmenu := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		depth += strings.Count(line, "{") - strings.Count(line, "}")

		if strings.HasPrefix(line, "submenu ") {
			title := extractGrubTitle(line)
			if title != "" {
				entries = append(entries, GrubEntry{
					ID:    strconv.Itoa(topIndex),
					Title: title,
				})
			}
			inSubmenu = true
			subIndex = 0
			continue
		}

		if strings.HasPrefix(line, "menuentry ") {
			title := extractGrubTitle(line)
			if title != "" {
				id := ""
				if inSubmenu {
					id = fmt.Sprintf("%d>%d", topIndex, subIndex)
					subIndex++
				} else {
					id = strconv.Itoa(topIndex)
					topIndex++
				}
				entries = append(entries, GrubEntry{
					ID:    id,
					Title: title,
				})
			}
			continue
		}

		if inSubmenu && depth == 0 {
			inSubmenu = false
			topIndex++ // Submenu consumes a top-level index
		}
	}

	g.cfgEntries = entries
	return scanner.Err()
}

func (g *GRUB) entryExists(title string) bool {
	for _, e := range g.cfgEntries {
		if e.Title == title {
			return true
		}
	}
	return false
}

func (g *GRUB) expandEntryTitle(entry string, isDefault bool) string {
	if isDefault && entry == "" {
		entry = "0"
	}
	if entry == "" {
		return ""
	}

	for _, e := range g.cfgEntries {
		if e.Title == entry || e.ID == entry {
			return e.Title
		}
	}
	return entry
}

func (g *GRUB) Probe() error {
	if g.MainRegex == nil {
		g.MainRegex = defaultGrubMainRegex
	}

	if err := g.ParseEnv(); err != nil {
		return fmt.Errorf("failed to parse grubenv: %s", err)
	}
	if err := g.ParseCfgEntries(); err != nil {
		return fmt.Errorf("failed to parse grub.cfg: %s", err)
	}

	// Validate RescueEntry exists in grub.cfg
	rescue := g.expandEntryTitle(g.RescueEntry, false)
	if rescue == "" || rescue == g.RescueEntry && !g.entryExists(g.RescueEntry) {
		return fmt.Errorf("rescue entry '%s' not found in grub.cfg", g.RescueEntry)
	}
	g.RescueEntry = rescue

	// Probe state
	savedEntry := g.Env["saved_entry"]
	nextEntry := g.Env["next_entry"]
	if nextEntry == "" {
		g.State.currentOneshot = ONESHOT_NONE
	}
	if nextEntry == "" && savedEntry == "" {
		// default NixOS state, as caution verify grub.cfg(idx=1) is main
		savedEntry = g.expandEntryTitle(savedEntry, true)
		if !g.MainRegex.MatchString(savedEntry) {
			return fmt.Errorf("saved_entry is empty, but grub.cfg first entry is not main")
		}
		g.State.currentDefault = DEFAULT_MAIN
		g.MainEntry = savedEntry
		return nil
	}

	// reach here means savedEntry has to exist
	var potentialMain string
	savedEntry = g.expandEntryTitle(savedEntry, true)
	if savedEntry == g.RescueEntry {
		// match exactly from config
		g.State.currentDefault = DEFAULT_RESCUE
	} else if g.MainRegex.MatchString(savedEntry) {
		g.State.currentDefault = DEFAULT_MAIN
		potentialMain = savedEntry
	} else {
		g.State.currentDefault = DEFAULT_UNKNOWN
	}

	if nextEntry != "" {
		nextEntry = g.expandEntryTitle(nextEntry, false)
		if nextEntry == g.RescueEntry {
			g.State.currentOneshot = ONESHOT_RESCUE
		} else if g.MainRegex.MatchString(nextEntry) {
			g.State.currentOneshot = ONESHOT_MAIN
			potentialMain = nextEntry
		} else {
			g.State.currentOneshot = ONESHOT_UNKNOWN
		}
	}

	// probe Main entry
	if potentialMain != "" {
		g.MainEntry = potentialMain
		return nil
	}
	for _, e := range g.cfgEntries {
		if g.MainRegex.MatchString(e.Title) && e.Title != g.RescueEntry {
			g.MainEntry = e.Title
			break
		}
	}
	if g.MainEntry == "" {
		return fmt.Errorf("could not discover a valid Main entry matching regex '%s'", g.MainRegex.String())
	}
	return nil
}

func (g *GRUB) Arm() error {
	if g.State.IsArmed() {
		return nil
	}
	if !g.State.IsPristine() {
		return fmt.Errorf("illegal state")
	}

	path := g.resolveEnvPath()
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("grubenv not found at %s: %w", path, err)
	}

	// Establish S_default = Rescue
	if err := g.SetDefault(g.RescueEntry); err != nil {
		return fmt.Errorf("failed to set default to rescue: %w", err)
	}

	// Establish S_oneshot = Main
	if err := g.SetOneShot(g.MainEntry); err != nil {
		return fmt.Errorf("failed to set oneshot to main: %v, you next boot will be rescue", err)
	}
	return nil
}

func (g *GRUB) Override() error {
	return g.SetOneShot(g.RescueEntry)
}

func (g *GRUB) Confirm() error {
	if g.State.IsPristine() {
		return nil
	}
	// allow to cancel
	if !g.State.NeedConfirmation() {
		return fmt.Errorf("illegal state")
	}

	// Establish S_default = Main, S_oneshot = <None>
	if err := g.ClearDefault(); err != nil {
		return err
	}
	return g.ClearOneShot()
}

func (g *GRUB) SetDefault(entry string) error {
	return g.setEnv("saved_entry", entry)
}

func (g *GRUB) SetOneShot(entry string) error {
	return g.setEnv("next_entry", entry)
}

func (g *GRUB) ClearDefault() error {
	return g.unsetEnv("saved_entry")
}

func (g *GRUB) ClearOneShot() error {
	return g.unsetEnv("next_entry")
}

func (g *GRUB) unsetEnv(key string) error {
	args := []string{"unset", key}
	return g.editenv(args)
}

func (g *GRUB) setEnv(key, value string) error {
	args := []string{"set", key + "=" + value}
	return g.editenv(args)
}

func (g *GRUB) editenv(args []string) error {
	path := g.resolveEnvPath()
	args = slices.Insert(args, 0, path)
	cmd := execCommand("grub-editenv", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("grub-editenv failed: %v, output: %s", err, string(out))
	}
	return nil
}

func (g *GRUB) Status() map[string]string {
	return map[string]string{
		"state_default": g.State.currentDefault.String(),
		"state_oneshot": g.State.currentOneshot.String(),
		"main_entry":    g.MainEntry,
		"rescue_entry":  g.RescueEntry,
	}
}
