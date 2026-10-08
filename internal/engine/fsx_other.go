//go:build !windows

package engine

import "path/filepath"

// patient is false where a rename or a removal does not fail because another
// program has the file open, and nothing puts files next to sereno's.
const patient = false

// transient reports whether an error is worth retrying: never, here.
func transient(error) bool { return false }

// pathKey returns the form in which the engine compares one path with
// another: the cleaned path. Letter case is ignored only on Windows; a macOS
// volume that ignores it is treated like any other, because sereno cannot
// tell without probing the disk.
func pathKey(path string) string { return filepath.Clean(path) }
