//go:build !windows

package testenv

import "testing"

// Junction skips the test: directory junctions are a Windows notion. See the
// Windows file for what it does there.
func Junction(t testing.TB, target, link string) {
	t.Helper()
	t.Skip("testenv: directory junctions exist only on Windows")
}
