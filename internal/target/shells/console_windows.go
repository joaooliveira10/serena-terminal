//go:build windows

package shells

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleOutputCP  = kernel32.NewProc("GetConsoleOutputCP")
	procGetOEMCP            = kernel32.NewProc("GetOEMCP")
	procMultiByteToWideChar = kernel32.NewProc("MultiByteToWideChar")
)

// decodeConsole converts what a console program wrote to a pipe into a Go
// string. On Windows, PowerShell encodes redirected output in the console's
// output code page (850 on a Brazilian system, 437 on a US one), so a profile
// path with an accented user name is not UTF-8.
func decodeConsole(output []byte) string {
	codePage, _, _ := procGetConsoleOutputCP.Call()
	if codePage == 0 {
		// No console attached: a child creates its own, with the OEM page.
		codePage, _, _ = procGetOEMCP.Call()
	}
	return decodeCodePage(output, uint32(codePage))
}

// decodeCodePage converts bytes in a Windows code page to a string. Bytes
// that cannot be converted come back as they are, which ParseProbe accepts
// only if they are plain ASCII.
func decodeCodePage(data []byte, codePage uint32) string {
	if len(data) == 0 {
		return ""
	}
	// First call: the number of UTF-16 units needed. Second call: convert.
	size, _, _ := procMultiByteToWideChar.Call(uintptr(codePage), 0,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 0, 0)
	if size == 0 {
		return string(data)
	}
	wide := make([]uint16, size)
	size, _, _ = procMultiByteToWideChar.Call(uintptr(codePage), 0,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)),
		uintptr(unsafe.Pointer(&wide[0])), uintptr(len(wide)))
	if size == 0 {
		return string(data)
	}
	return string(utf16.Decode(wide[:size]))
}
