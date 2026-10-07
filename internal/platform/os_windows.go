//go:build windows

package platform

import (
	"os"
	"syscall"
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleMode           = kernel32.NewProc("SetConsoleMode")
	procGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")
)

// enableVirtualTerminalProcessing is the console output mode flag that makes
// the console interpret escape sequences.
const enableVirtualTerminalProcessing = 0x0004

// IsTerminal reports whether f is a console. Redirected output, pipes and the
// NUL device are not.
func IsTerminal(f *os.File) bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(f.Fd()), &mode) == nil
}

// EnableVT makes the console behind f interpret escape sequences (SGR colours
// and the OSC palette strings) and returns a function that puts the previous
// mode back. ok is false when f is not a console or refuses the mode; restore
// is then a no-op and nothing should be painted.
func EnableVT(f *os.File) (restore func(), ok bool) {
	handle := syscall.Handle(f.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(handle, &mode); err != nil {
		return func() {}, false
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return func() {}, true
	}
	if r, _, _ := procSetConsoleMode.Call(uintptr(handle), uintptr(mode|enableVirtualTerminalProcessing)); r == 0 {
		return func() {}, false
	}
	return func() { procSetConsoleMode.Call(uintptr(handle), uintptr(mode)) }, true
}

// uiLanguageID returns the language identifier of the user interface.
func uiLanguageID() uint16 {
	id, _, _ := procGetUserDefaultUILanguage.Call()
	return uint16(id)
}
