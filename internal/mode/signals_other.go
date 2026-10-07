//go:build !windows

package mode

// System returns the Reader of this machine. macOS and Linux have no
// detection in 0.1 (R28): the signals are unknown.
func System() Reader { return Unknown{} }
