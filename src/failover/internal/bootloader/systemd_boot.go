package bootloader

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var defaultSdBootMainRegex = regexp.MustCompile(`^nixos-generation.*conf`)

var LoaderEntryOneShotPath = "/sys/firmware/efi/efivars/LoaderEntryOneShot-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"
var LoaderEntriesPath = "/sys/firmware/efi/efivars/LoaderEntries-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"

type SystemdBoot struct {
	State       State
	ESPPath     string
	RootPrefix  string
	MainEntry   string
	MainRegex   *regexp.Regexp
	RescueEntry string
}

func parseLoaderConf(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	conf := make(map[string]string)
	lines := strings.Split(string(raw), "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		if len(parts) == 1 {
			conf[parts[0]] = ""
		} else {
			conf[parts[0]] = parts[1]
		}
	}
	return conf, nil
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write to temp file: %w", err)
	}

	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	if err = tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to chmod temp file: %w", err)
	}

	if err = tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

func writeLoaderConf(path string, conf map[string]string, perm os.FileMode) error {
	keys := slices.Collect(maps.Keys(conf))
	sort.Strings(keys)

	var buffer strings.Builder
	for _, k := range keys {
		v := conf[k]
		if v == "" {
			buffer.WriteString(k + "\n")
		} else {
			buffer.WriteString(k + " " + v + "\n")
		}
	}
	return atomicWriteFile(path, []byte(buffer.String()), perm)
}

func fetchEntries(efivarPath string, entriesDir string) ([]string, error) {
	// read entries from efivars
	entries, err := readEFIVarSplitN(efivarPath, -1)
	if err == nil {
		return entries, nil
	}
	// fallback to */boot/loader/entries/*
	entries, err = fs.Glob(os.DirFS(entriesDir), "*.conf")
	if err != nil || entries == nil {
		return nil, fmt.Errorf("failed to fetch systemd-boot entries from dir")
	}
	return entries, err
}

func findMainEntry(mainRegex regexp.Regexp, entries []string) (string, error) {
	re := regexp.MustCompile(`\d+`)
	sort.Slice(entries, func(i, j int) bool {
		numI, _ := strconv.Atoi(re.FindString(entries[i]))
		numJ, _ := strconv.Atoi(re.FindString(entries[j]))
		// highest number first
		return numI > numJ
	})

	for _, entry := range entries {
		if mainRegex.MatchString(entry) {
			return entry, nil
		}
	}
	return "", fmt.Errorf("failed to find mainEntry")
}

func (s *SystemdBoot) resolvePath(path string) string {
	if strings.HasPrefix(path, "/sys/firmware/efi/efivars") {
		return path
	}
	return filepath.Join(s.RootPrefix, s.ESPPath, path)
}

func (s *SystemdBoot) Probe() error {
	if s.MainRegex == nil {
		s.MainRegex = defaultSdBootMainRegex
	}

	conf, err := parseLoaderConf(s.resolvePath("loader/loader.conf"))
	if err != nil {
		return fmt.Errorf("failed to parse systemd-boot loader.conf: %s", err)
	}
	oneshot, err := ReadEFIVar(LoaderEntryOneShotPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	entries, err := fetchEntries(LoaderEntriesPath, s.resolvePath("loader/entries"))
	if err != nil {
		return err
	}
	if !slices.Contains(entries, s.RescueEntry) {
		return fmt.Errorf("rescue entry is not in systemd-boot")
	}

	// map default
	defaultEntry, exist := conf["default"]
	if !exist || defaultEntry == "" {
		return fmt.Errorf("failed to get default entry from loader.conf")
	}
	if s.MainRegex.MatchString(defaultEntry) {
		s.State.currentDefault = DEFAULT_MAIN
		s.MainEntry = defaultEntry
	} else if defaultEntry == s.RescueEntry {
		s.State.currentDefault = DEFAULT_RESCUE
	} else {
		s.State.currentDefault = DEFAULT_UNKNOWN
	}

	// map rescue
	if oneshot == "" {
		s.State.currentOneshot = ONESHOT_NONE
	} else if oneshot == s.RescueEntry {
		s.State.currentOneshot = ONESHOT_RESCUE
	} else if s.MainRegex.MatchString(oneshot) {
		s.State.currentOneshot = ONESHOT_MAIN
		s.MainEntry = oneshot
	} else {
		s.State.currentOneshot = ONESHOT_UNKNOWN
	}

	if s.MainEntry == "" {
		s.MainEntry, err = findMainEntry(*s.MainRegex, entries)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *SystemdBoot) Arm() error {
	if s.State.IsArmed() {
		return nil
	}
	if !s.State.IsPristine() {
		return fmt.Errorf("illegal state")
	}

	// Establish S_default = Rescue
	if err := s.SetDefault(s.RescueEntry); err != nil {
		return fmt.Errorf("failed to set default to rescue: %w", err)
	}

	// Establish S_oneshot = Main
	err := s.SetOneShot(s.MainEntry)
	if err != nil {
		return fmt.Errorf("failed to set oneshot to main: %v, you next boot will be rescue", err)
	}
	return nil
}

func (s *SystemdBoot) Override() error {
	return s.SetOneShot(s.RescueEntry)
}

func (s *SystemdBoot) Confirm() error {
	if s.State.IsPristine() {
		return nil
	}
	// allow to cancel
	if !s.State.NeedConfirmation() {
		return fmt.Errorf("illegal state")
	}

	// Establish S_default = Main, S_oneshot = <None>
	if err := s.SetDefault(s.MainEntry); err != nil {
		return err
	}
	return s.ClearOneShot()
}

func (s *SystemdBoot) SetDefault(entry string) error {
	path := s.resolvePath("loader/loader.conf")
	conf, err := parseLoaderConf(path)
	if err != nil {
		return err
	}
	conf["default"] = entry
	return writeLoaderConf(path, conf, 0644)
}

func (s *SystemdBoot) SetOneShot(entry string) error {
	return WriteEFIVar(LoaderEntryOneShotPath, entry)
}

func (s *SystemdBoot) ClearOneShot() error {
	path := LoaderEntryOneShotPath
	removeImmutableFlag(path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear EFI OneShot variable: %w", err)
	}
	return nil
}

func (s *SystemdBoot) Status() map[string]string {
	return map[string]string{
		"state_default": s.State.currentDefault.String(),
		"state_oneshot": s.State.currentOneshot.String(),
		"main_entry":    s.MainEntry,
		"rescue_entry":  s.RescueEntry,
	}
}
