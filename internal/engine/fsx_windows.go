//go:build windows

package engine

import (
	"errors"
	"path/filepath"
	"strings"
	"syscall"
)

// patient is true on Windows: other programs get a moment to let go of files
// and directories before sereno gives up on them (see patience).
const patient = true

// errorSharingViolation is ERROR_SHARING_VIOLATION, which package syscall
// does not name.
const errorSharingViolation = syscall.Errno(32)

// transient reports whether err is what Windows returns while another program
// has the file open: the operation is worth retrying for a moment.
func transient(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) || errors.Is(err, errorSharingViolation)
}

// pathKey returns the form in which the engine compares one path with
// another. File names ignore letter case on Windows, so a folder reached
// through LOCALAPPDATA one day and through a hand-typed SERENO_HOME the next
// is the same folder, and what the manifest records for it still counts.
func pathKey(path string) string { return strings.ToLower(filepath.Clean(path)) }
