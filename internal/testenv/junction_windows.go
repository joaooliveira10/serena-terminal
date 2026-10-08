//go:build windows

package testenv

import (
	"encoding/binary"
	"errors"
	"os"
	"syscall"
	"testing"
)

// Values of the Windows API that package syscall does not name.
const (
	fsctlSetReparsePoint = 0x000900A4 // FSCTL_SET_REPARSE_POINT
	reparseTagMountPoint = 0xA0000003 // IO_REPARSE_TAG_MOUNT_POINT
	errorInvalidFunction = syscall.Errno(1)
	errorNotSupported    = syscall.Errno(50)
)

// Junction creates a directory junction at link that leads to the directory
// target, which must be an absolute path. Every Windows account may create a
// junction, which is not true of a symbolic link, and Go does not follow one
// when it resolves links: a guard that only resolves links is led out of its
// directory by it. The junction is made with the file system control code
// itself, so no test has to start a child process for it.
//
// The test is skipped where junctions do not exist: on macOS and Linux, and
// on a Windows volume without reparse points.
func Junction(t testing.TB, target, link string) {
	t.Helper()
	err := junction(target, link)
	if errors.Is(err, errorInvalidFunction) || errors.Is(err, errorNotSupported) {
		t.Skipf("testenv: this volume has no junctions: %v", err)
	}
	if err != nil {
		t.Fatalf("testenv: junction %s: %v", link, err)
	}
}

// junction makes an empty directory and turns it into a mount point. The
// buffer is a REPARSE_DATA_BUFFER for a mount point: the tag, the length of
// what follows the header, a reserved word, then offset and length of the
// substitute name and of the print name, and the two names, each followed by
// a NUL.
func junction(target, link string) error {
	substitute, err := syscall.UTF16FromString(`\??\` + target)
	if err != nil {
		return err
	}
	shown, err := syscall.UTF16FromString(target)
	if err != nil {
		return err
	}
	names := 2 * (len(substitute) + len(shown)) // both with their NUL
	buffer := make([]byte, 16+names)
	binary.LittleEndian.PutUint32(buffer[0:], reparseTagMountPoint)
	binary.LittleEndian.PutUint16(buffer[4:], uint16(8+names))
	binary.LittleEndian.PutUint16(buffer[8:], 0)
	binary.LittleEndian.PutUint16(buffer[10:], uint16(2*(len(substitute)-1)))
	binary.LittleEndian.PutUint16(buffer[12:], uint16(2*len(substitute)))
	binary.LittleEndian.PutUint16(buffer[14:], uint16(2*(len(shown)-1)))
	at := 16
	for _, unit := range append(substitute, shown...) {
		binary.LittleEndian.PutUint16(buffer[at:], unit)
		at += 2
	}

	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	name, err := syscall.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		os.Remove(link)
		return err
	}
	var returned uint32
	err = syscall.DeviceIoControl(handle, fsctlSetReparsePoint, &buffer[0], uint32(len(buffer)), nil, 0, &returned, nil)
	syscall.CloseHandle(handle)
	if err != nil {
		os.Remove(link)
	}
	return err
}
