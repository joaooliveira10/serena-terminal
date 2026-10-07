//go:build windows

package mode

import (
	"encoding/binary"
	"syscall"
	"unicode/utf16"
)

// System returns the Reader of this machine: on Windows, the three registry
// values of R28, read in-process and never written.
func System() Reader { return registryReader{} }

type registryReader struct{}

func (registryReader) Signals() (Signals, bool) { return FromRegistry(currentUser{}), true }

// currentUser reads HKEY_CURRENT_USER through package syscall.
type currentUser struct{}

func (currentUser) String(key, name string) (string, bool) {
	kind, data, ok := queryValue(key, name)
	if !ok || (kind != syscall.REG_SZ && kind != syscall.REG_EXPAND_SZ) {
		return "", false
	}
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		unit := binary.LittleEndian.Uint16(data[i:])
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	return string(utf16.Decode(units)), true
}

func (currentUser) Integer(key, name string) (uint32, bool) {
	kind, data, ok := queryValue(key, name)
	if !ok || kind != syscall.REG_DWORD || len(data) < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(data), true
}

// queryValue returns the type and the raw bytes of one registry value. The
// key is opened for querying only.
func queryValue(key, name string) (kind uint32, data []byte, ok bool) {
	keyPtr, err := syscall.UTF16PtrFromString(key)
	if err != nil {
		return 0, nil, false
	}
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, nil, false
	}
	var handle syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, keyPtr, 0, syscall.KEY_QUERY_VALUE, &handle); err != nil {
		return 0, nil, false
	}
	defer syscall.RegCloseKey(handle)

	buffer := make([]byte, 256)
	for attempt := 0; attempt < 2; attempt++ {
		size := uint32(len(buffer))
		err := syscall.RegQueryValueEx(handle, namePtr, nil, &kind, &buffer[0], &size)
		if err == nil {
			return kind, buffer[:size], true
		}
		if err != syscall.ERROR_MORE_DATA || size == 0 {
			return 0, nil, false
		}
		buffer = make([]byte, size)
	}
	return 0, nil, false
}
