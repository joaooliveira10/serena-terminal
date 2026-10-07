//go:build windows

package engine

import (
	"errors"
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
