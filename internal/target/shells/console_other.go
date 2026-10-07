//go:build !windows

package shells

// decodeConsole converts what a console program wrote to a pipe into a Go
// string. PowerShell on macOS and Linux writes UTF-8.
func decodeConsole(output []byte) string { return string(output) }
