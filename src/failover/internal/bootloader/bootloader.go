package bootloader

import (
	"fmt"
	"slices"

	"github.com/foo/failover/internal/config"
)

type Default int

const (
	DEFAULT_UNKNOWN Default = iota
	DEFAULT_MAIN
	DEFAULT_RESCUE
)

func (d Default) String() string {
	switch d {
	case DEFAULT_MAIN:
		return "MAIN"
	case DEFAULT_RESCUE:
		return "RESCUE"
	default:
		return "UNKNOWN"
	}
}

type Oneshot int

const (
	ONESHOT_UNKNOWN Oneshot = iota
	ONESHOT_NONE
	ONESHOT_MAIN
	ONESHOT_RESCUE
)

func (o Oneshot) String() string {
	switch o {
	case ONESHOT_NONE:
		return "NONE"
	case ONESHOT_MAIN:
		return "MAIN"
	case ONESHOT_RESCUE:
		return "RESCUE"
	default:
		return "UNKNOWN"
	}
}

type State struct {
	currentDefault Default
	currentOneshot Oneshot
}

func (s *State) String() string {
	return fmt.Sprintf("[S_default=%s, S_oneshot=%s]", s.currentDefault, s.currentOneshot)
}

func (s *State) IsPristine() bool {
	return s.currentDefault == DEFAULT_MAIN && s.currentOneshot == ONESHOT_NONE
}
func (s *State) IsArmed() bool {
	return s.currentDefault == DEFAULT_RESCUE && s.currentOneshot == ONESHOT_MAIN
}

func (s *State) IsDegradedOrComsumed() bool {
	return s.currentDefault == DEFAULT_RESCUE && s.currentOneshot == ONESHOT_NONE
}

func (s *State) NeedConfirmation() bool {
	if s.IsArmed() || s.IsDegradedOrComsumed() {
		return true
	}
	// overrided
	if slices.Contains([]Default{DEFAULT_MAIN, DEFAULT_RESCUE}, s.currentDefault) && s.currentOneshot == ONESHOT_RESCUE {
		return true
	}
	return false
}

type Bootloader interface {
	// Probe discovers the current state of the bootloader.
	Probe() error
	// Status returns a formatted string containing the state vector and underlying physical details.
	Status() map[string]string
	// S_default=main, S_oneshot=none -> S_default=rescue, S_oneshot=main
	// degraded: -> S_default=rescue, S_oneshot=none
	Arm() error
	// S_default=rescue, S_oneshot=none -> S_default=main, S_oneshot=none
	Confirm() error
	// S_default=main/rescue, S_oneshot=none -> S_default=main/rescue, S_oneshot=rescue
	Override() error
}

func New(cfg *config.Config, rootPrefix string) (Bootloader, error) {
	switch cfg.BootloaderType {
	case "systemd-boot":
		return &SystemdBoot{
			ESPPath:     cfg.ESPPath,
			RootPrefix:  rootPrefix,
			RescueEntry: cfg.RescueEntryID,
		}, nil
	case "grub":
		return &GRUB{
			RootPrefix:  rootPrefix,
			RescueEntry: cfg.RescueEntryID,
		}, nil
	default:
		return nil, fmt.Errorf("unknown bootloader type: %s", cfg.BootloaderType)
	}
}
