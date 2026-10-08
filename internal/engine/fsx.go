// Package engine holds the file mechanics of sereno: guarded atomic writes,
// the manifest of what sereno owns, plans that are computed before anything is
// written, and the editor for the one hook line a profile may receive.
//
// Every create, write, rename and delete in the program goes through this
// file, so the SERENO_SANDBOX guard of requirement R10 cannot be bypassed.
package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SandboxEnv names the environment variable that confines every write (R10).
// When it holds a directory, any create, write, rename or delete outside that
// directory fails before it happens.
const SandboxEnv = "SERENO_SANDBOX"

// Permissions of the directories and files sereno creates.
const (
	DirPerm  fs.FileMode = 0o755
	FilePerm fs.FileMode = 0o644
)

// SandboxError reports a write that was stopped by the SERENO_SANDBOX guard.
type SandboxError struct {
	Op      string // write, mkdir, remove or rmdir
	Path    string // the path that was about to change
	Sandbox string // the value of SERENO_SANDBOX
	Err     error  // set when the check itself could not be completed
}

// Error implements error.
func (e *SandboxError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s %s: %s=%s could not be checked: %v", e.Op, e.Path, SandboxEnv, e.Sandbox, e.Err)
	}
	return fmt.Sprintf("%s %s: outside %s=%s", e.Op, e.Path, SandboxEnv, e.Sandbox)
}

// Unwrap returns the reason the check could not be completed, if any.
func (e *SandboxError) Unwrap() error { return e.Err }

// guard returns a *SandboxError when SERENO_SANDBOX is set and path is not
// strictly inside it. It is the first thing every mutating function calls.
func guard(op, path string) error {
	sandbox := os.Getenv(SandboxEnv)
	if sandbox == "" {
		return nil
	}
	inside, err := insideSandbox(sandbox, path)
	if err != nil || !inside {
		return &SandboxError{Op: op, Path: path, Sandbox: sandbox, Err: err}
	}
	return nil
}

// insideSandbox reports whether path lies strictly below the sandbox
// directory. Symbolic links are resolved in the sandbox and in every existing
// directory above path, so a link inside the sandbox cannot lead a write out
// of it. Resolving does not follow the other things that make a directory
// lead elsewhere (a Windows junction above all), so every existing directory
// between the sandbox and path is also looked at, and the check fails when
// one of them is not a plain directory. The last element of path is not
// resolved: sereno replaces and removes directory entries, it never writes
// through them.
func insideSandbox(sandbox, path string) (bool, error) {
	if !filepath.IsAbs(sandbox) {
		return false, errors.New("the sandbox is not an absolute path")
	}
	root, err := filepath.EvalSymlinks(sandbox)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, errors.New("the sandbox is not a directory")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	resolved, err := resolveParent(abs)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		// Different volumes on Windows: certainly outside.
		return false, nil
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false, nil
	}
	if err := plainDirectories(root, filepath.Dir(rel)); err != nil {
		return false, err
	}
	return true, nil
}

// plainDirectories walks from root down the relative path below and returns
// an error for the first existing element that is a link or a reparse point.
// Go reports a Windows junction, and every other reparse point that is not a
// symbolic link, as an irregular file; filepath.EvalSymlinks leaves them in
// the path, so without this walk a junction inside the sandbox would carry a
// write to wherever it leads. Elements that do not exist yet end the walk:
// nothing can lead anywhere from there.
func plainDirectories(root, below string) error {
	if below == "." {
		return nil
	}
	dir := root
	for _, element := range strings.Split(below, string(filepath.Separator)) {
		dir = filepath.Join(dir, element)
		info, err := os.Lstat(dir)
		if isMissing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return fmt.Errorf("%s is a link or a junction", dir)
		}
	}
	return nil
}

// resolveParent resolves symbolic links in the longest existing ancestor of an
// absolute path and appends the remaining elements unchanged.
func resolveParent(abs string) (string, error) {
	rest := []string{filepath.Base(abs)}
	dir := filepath.Dir(abs)
	for {
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", err
		}
		rest = append([]string{filepath.Base(dir)}, rest...)
		dir = parent
	}
}

// plain returns the system's own words for a failed file operation, without
// the operation and the path that package os puts in front of them. Every
// function below names both itself, and the path package os knows may be that
// of a temporary file, which means nothing to the user.
func plain(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return linkErr.Err
	}
	return err
}

// WriteFile writes data to path atomically: the bytes go to a temporary file
// in the same directory, which is then renamed over path. The directory must
// exist. An existing file is replaced as a whole and never written in place.
func WriteFile(path string, data []byte, perm fs.FileMode) (err error) {
	if err := guard("write", path); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".sereno-tmp-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, plain(err))
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(name)
			err = fmt.Errorf("write %s: %w", path, plain(err))
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(name, perm); err != nil {
		return err
	}
	return retry(func() error {
		err := os.Rename(name, path)
		if err != nil && readOnly(path) {
			// Windows does not replace a file that is marked read-only, and
			// waiting does not change the mark. Planning leaves such a file
			// alone; this is for one that was marked after planning.
			return errReadOnly
		}
		return err
	})
}

// errReadOnly is why a file could not be replaced; see WriteFile.
var errReadOnly = errors.New("the file is read-only")

// readOnly reports whether path is a regular file that its owner may not
// write, which is also how Go reports the read-only attribute of Windows.
func readOnly(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o200 == 0
}

// patience bounds how long sereno waits for other programs on Windows, where
// virus scanners and indexers open files right after they are written and may
// even put a copy next to them for a moment. A rename or a removal that fails
// because of that is retried, and a directory that should be empty is given
// this long to become so. Nothing waits on macOS and Linux (see patient).
const patience = 2 * time.Second

// retry runs op again, with growing pauses, while it fails with an error that
// transient recognises and patience lasts.
func retry(op func() error) error {
	err := op()
	deadline := time.Now().Add(patience)
	for pause := time.Millisecond; err != nil && transient(err) && time.Now().Before(deadline); pause *= 2 {
		time.Sleep(pause)
		err = op()
	}
	return err
}

// Mkdir creates one directory. Its parent must exist; plans create
// directories one level at a time so that each can be recorded.
func Mkdir(path string) error {
	if err := guard("mkdir", path); err != nil {
		return err
	}
	if err := os.Mkdir(path, DirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, plain(err))
	}
	return nil
}

// RemoveFile deletes one file or symbolic link. It refuses directories.
func RemoveFile(path string) error {
	if err := guard("remove", path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("remove %s: %w", path, plain(err))
	}
	if info.IsDir() {
		return fmt.Errorf("remove %s: is a directory", path)
	}
	if err := retry(func() error { return os.Remove(path) }); err != nil {
		return fmt.Errorf("remove %s: %w", path, plain(err))
	}
	return nil
}

// RemoveEmptyDir deletes a directory only when it is empty. It reports whether
// the directory was removed; a missing or non-empty directory is not an error.
func RemoveEmptyDir(path string) (bool, error) {
	if err := guard("rmdir", path); err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("rmdir %s: %w", path, plain(err))
	}
	if !info.IsDir() {
		return false, fmt.Errorf("rmdir %s: not a directory", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, fmt.Errorf("rmdir %s: %w", path, plain(err))
	}
	if len(entries) > 0 {
		return false, nil
	}
	err = retry(func() error { return os.Remove(path) })
	if errors.Is(err, fs.ErrExist) {
		// "Directory not empty": something appeared after the listing.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("rmdir %s: %w", path, plain(err))
	}
	return true, nil
}

// removeEmptiedDir removes a directory that a plan has just emptied. It is
// RemoveEmptyDir with patience: on Windows a file that another program put
// there for a moment gets time to go away. A directory that stays occupied is
// left in place, which is not an error.
func removeEmptiedDir(path string) error {
	deadline := time.Now().Add(patience)
	for pause := 2 * time.Millisecond; ; pause *= 2 {
		removed, err := RemoveEmptyDir(path)
		if err != nil || removed || !patient || !time.Now().Before(deadline) {
			return err
		}
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		time.Sleep(pause)
	}
}

// LockName is the file in ROOT that a command holds locked while it writes
// (R10, R13). It is empty, it is there only for that time, and the manifest
// does not list it. A command that is killed may leave it behind without a
// holder; the next command that writes takes it, and takes it away.
const LockName = "lock"

// ErrBusy says that a command had to make way for another one: another
// sereno holds the lock of the folder, or has changed the folder since this
// command looked at it. Nothing was written; running the command again, once
// the other one has finished, is all there is to do.
var ErrBusy = errors.New("another sereno is working on this folder")

// lockAttempts bounds how often Lock starts over when the file it has just
// locked turns out to be one that its last holder was taking away, and how
// often it tries again because somebody was only looking (see Busy).
const lockAttempts = 8

// Lock takes the lock of sereno's folder for a command that is about to
// write: the file LockName in root, created when it is missing and locked
// through the operating system, so that the lock ends with the process that
// holds it, however that process ends. Lock does not wait: when another
// command holds the lock, the error is ErrBusy. root must exist.
//
// The lock of a command at work is exclusive. A command that only asks
// whether the folder is busy takes a shared lock for an instant (see Busy);
// that must not make a command give way, so Lock tells the two apart and
// tries again when it met a shared one.
//
// release gives the lock back and deletes the file.
//
// A file system that offers no locks is not a reason to refuse the work: the
// command then goes on without one, as every command did before there was a
// lock.
func Lock(root string) (release func(), err error) {
	path := filepath.Join(root, LockName)
	if err := guard("write", path); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < lockAttempts; attempt++ {
		// Planning has refused a foreign file under this name already (see
		// lockInTheWay); this is for one that appeared since. release would
		// delete it.
		if lockInTheWay(root) {
			return nil, fmt.Errorf("lock %s: is in the way and is not the lock file of sereno", path)
		}
		var file *os.File
		err = retry(func() error {
			var err error
			file, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE, FilePerm)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("lock %s: %w", path, plain(err))
		}
		locked, err := tryLock(file, true)
		for again := 0; err == nil && !locked && again < lockAttempts; again++ {
			// Somebody holds a lock on the file. If a shared lock can be had
			// next to it, it is a shared one: nobody is at work.
			if shared, _ := tryLock(file, false); !shared {
				break
			}
			unlock(file)
			time.Sleep(time.Millisecond)
			locked, err = tryLock(file, true)
		}
		if err == nil && !locked {
			file.Close()
			return nil, ErrBusy
		}
		// The file that is locked must be the one that has the name: the last
		// holder deletes the file when it lets go, and a lock on a file
		// without a name keeps nobody out.
		if sameFile(file, path) {
			return func() { unlockAndRemove(file, path) }, nil
		}
		unlock(file)
		file.Close()
	}
	return nil, ErrBusy
}

// lockInTheWay reports whether something that is not sereno's lock file has
// the name of the lock in root. sereno's is a regular file and empty; a
// folder, a link or a file with content is somebody else's, and is in the way
// of every command that writes, like a foreign file where an owned one
// belongs (R15).
func lockInTheWay(root string) bool {
	info, err := os.Lstat(filepath.Join(root, LockName))
	return err == nil && (!info.Mode().IsRegular() || info.Size() > 0)
}

// sameFile reports whether the open file is the one that path names now.
func sameFile(file *os.File, path string) bool {
	held, err := file.Stat()
	if err != nil {
		return false
	}
	named, err := os.Lstat(path)
	return err == nil && os.SameFile(held, named)
}

// unlockAndRemove ends a lock. Where an open file can be deleted, the file
// goes first and the lock after it, so that nobody can take the lock of a
// file that is about to lose its name. Windows does not delete a file that is
// open; there the lock goes first, and deleting waits a moment for whoever
// looks at the file just then. A command that has taken the lock in that
// moment keeps the file, and deletes it when it is done.
func unlockAndRemove(file *os.File, path string) {
	err := os.Remove(path)
	unlock(file)
	file.Close()
	if err == nil {
		return
	}
	retry(func() error {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	})
}

// Busy reports whether another command holds the lock of sereno's folder at
// this moment. It changes nothing: a lock file without a holder stays where
// it is, and none is created. The question is asked with a shared lock, which
// can be had exactly when no command holds the exclusive one, and which does
// not stand in the way of a command that takes the lock in the same instant
// (see Lock).
func Busy(root string) bool {
	path := filepath.Join(root, LockName)
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	shared, err := tryLock(file, false)
	if err != nil {
		return false
	}
	if shared {
		unlock(file)
	}
	return !shared
}

// samePath reports whether two paths name the same file as far as spelling
// can tell (see pathKey).
func samePath(a, b string) bool { return pathKey(a) == pathKey(b) }

// SHA256 returns the lower-case hexadecimal SHA-256 of data.
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashFile returns the SHA-256 of the file at path.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return SHA256(data), nil
}
