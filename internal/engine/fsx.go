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
// of it. The last element of path is not resolved: sereno replaces and removes
// directory entries, it never writes through them.
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
	return true, nil
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

// WriteFile writes data to path atomically: the bytes go to a temporary file
// in the same directory, which is then renamed over path. The directory must
// exist. An existing file is replaced as a whole and never written in place.
func WriteFile(path string, data []byte, perm fs.FileMode) (err error) {
	if err := guard("write", path); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".sereno-tmp-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(name)
			err = fmt.Errorf("write %s: %w", path, err)
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
	return retry(func() error { return os.Rename(name, path) })
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
		return fmt.Errorf("mkdir %s: %w", path, err)
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
		return fmt.Errorf("remove %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("remove %s: is a directory", path)
	}
	if err := retry(func() error { return os.Remove(path) }); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
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
		return false, fmt.Errorf("rmdir %s: %w", path, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("rmdir %s: not a directory", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, fmt.Errorf("rmdir %s: %w", path, err)
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
		return false, fmt.Errorf("rmdir %s: %w", path, err)
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
