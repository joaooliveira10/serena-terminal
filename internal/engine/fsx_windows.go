//go:build windows

package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// patient is true on Windows: other programs get a moment to let go of files
// and directories before sereno gives up on them (see patience).
const patient = true

// Error codes of Windows that package syscall does not name.
const (
	errorSharingViolation = syscall.Errno(32)  // ERROR_SHARING_VIOLATION
	errorLockViolation    = syscall.Errno(33)  // ERROR_LOCK_VIOLATION
	errorIOPending        = syscall.Errno(997) // ERROR_IO_PENDING
)

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

// The two procedures behind the lock of sereno's folder (see Lock). They lock
// and unlock a range of bytes of a file that is already open; neither opens,
// creates or changes a file.
var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

// Flags of LockFileEx.
const (
	lockfileFailImmediately = 0x1 // do not wait for the lock
	lockfileExclusiveLock   = 0x2 // nobody else may hold one
)

// tryLock takes a lock on the first byte of file without waiting: an
// exclusive one, or a shared one that other shared locks may stand next to.
// locked is false when a lock of somebody else is in the way; err is set when
// the file system gives no locks at all. The lock ends with unlock, with the
// last handle of the file, and with the process.
func tryLock(file *os.File, exclusive bool) (locked bool, err error) {
	flags := uintptr(lockfileFailImmediately)
	if exclusive {
		flags |= lockfileExclusiveLock
	}
	var overlapped syscall.Overlapped
	result, _, errno := procLockFileEx.Call(file.Fd(), flags, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	switch {
	case result != 0:
		return true, nil
	case errno == errorLockViolation || errno == errorIOPending:
		return false, nil
	}
	return false, errno
}

// unlock gives back what tryLock took.
func unlock(file *os.File) {
	var overlapped syscall.Overlapped
	procUnlockFileEx.Call(file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
}
