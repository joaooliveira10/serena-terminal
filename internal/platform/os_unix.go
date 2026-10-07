//go:build darwin || linux

package platform

import (
	"os"
	"syscall"
	"unsafe"
)

// IsTerminal reports whether f is a terminal: the terminal attributes of a
// tty can be read, those of a file, a pipe or /dev/null cannot.
func IsTerminal(f *os.File) bool {
	var attributes syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), ioctlGetTermios, uintptr(unsafe.Pointer(&attributes)))
	return errno == 0
}

// EnableVT exists for Windows, where a console must be told to interpret
// escape sequences. Terminals on macOS and Linux always do, so this only
// reports whether f is one; restore does nothing.
func EnableVT(f *os.File) (restore func(), ok bool) {
	return func() {}, IsTerminal(f)
}

// uiLanguageID is a Windows notion; elsewhere the locale variables decide.
func uiLanguageID() uint16 { return 0 }
