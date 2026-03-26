package bootloader

import (
	"bytes"
	"encoding/binary"
	"log"
	"os"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/unix"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

func ReadEFIVar(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) <= 4 {
		return "", err
	}

	decoder := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder()
	bytes, _, err := transform.Bytes(decoder, raw[4:])
	if err != nil {
		return "", err
	}
	return strings.Trim(string(bytes), "\x00"), nil
}

func readEFIVarSplitN(path string, n int) ([]string, error) {
	s, err := ReadEFIVar(path)
	if err != nil {
		return nil, err
	}
	return strings.SplitN(s, "\x00", n), nil
}

func WriteEFIVar(path, entry string) error {
	entry = strings.TrimRight(entry, "\x00") // Prevent double-termination
	var buf bytes.Buffer

	// Attribute header: EFI_VARIABLE_NON_VOLATILE | EFI_VARIABLE_BOOTSERVICE_ACCESS | EFI_VARIABLE_RUNTIME_ACCESS
	attr := uint32(0x00000007)
	if err := binary.Write(&buf, binary.LittleEndian, attr); err != nil {
		return err
	}

	// UTF-16LE string
	runes := []rune(entry)
	encoded := utf16.Encode(runes)
	if err := binary.Write(&buf, binary.LittleEndian, encoded); err != nil {
		return err
	}

	// Null terminator
	if err := binary.Write(&buf, binary.LittleEndian, uint16(0)); err != nil {
		return err
	}

	removeImmutableFlag(path)
	return os.WriteFile(path, buf.Bytes(), 0644)
}

// EFI variable flag constants from linux/fs.h.
// FS_IMMUTABLE_FL is not exported by golang.org/x/sys/unix.
const (
	FS_IMMUTABLE_FL = 0x00000010 // FS_IMMUTABLE_FL
)

// removeImmutableFlag clears the FS_IMMUTABLE_FL flag on an EFI variable file.
// Linux kernel sets this flag on efivarfs entries by default; without clearing
// it, write/remove operations will fail with EPERM on real hardware.
// Errors are logged but not returned since this is best-effort (e.g. in test
// environments where the file is on tmpfs, not efivarfs).
func removeImmutableFlag(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // file doesn't exist yet, nothing to clear
	}
	defer f.Close()

	var flags int
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(),
		uintptr(unix.FS_IOC_GETFLAGS), uintptr(unsafe.Pointer(&flags)))
	if errno != 0 {
		log.Printf("Warning: FS_IOC_GETFLAGS on %s: %v", path, errno)
		return
	}

	if flags&FS_IMMUTABLE_FL == 0 {
		return // not immutable, nothing to do
	}

	flags &^= FS_IMMUTABLE_FL
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, f.Fd(),
		uintptr(unix.FS_IOC_SETFLAGS), uintptr(unsafe.Pointer(&flags)))
	if errno != 0 {
		log.Printf("Warning: FS_IOC_SETFLAGS on %s: %v", path, errno)
	}
}
