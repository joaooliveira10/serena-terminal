//go:build !windows

package engine

import (
	"os"
	"path/filepath"
	"syscall"
)

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

// tryLock takes a lock on file without waiting (see Lock): an exclusive one,
// or a shared one that other shared locks may stand next to. locked is false
// when a lock of somebody else is in the way; err is set when the file system
// gives no locks at all. The lock belongs to this opening of the file: it
// ends with unlock, when the file is closed, and with the process.
func tryLock(file *os.File, exclusive bool) (locked bool, err error) {
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	err = syscall.Flock(int(file.Fd()), how|syscall.LOCK_NB)
	switch {
	case err == nil:
		return true, nil
	case err == syscall.EWOULDBLOCK:
		return false, nil
	}
	return false, err
}

// unlock gives back what tryLock took.
func unlock(file *os.File) {
	syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
