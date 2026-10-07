// Package testenv gives every automated test the hermetic home of test-plan
// rule T1: a temporary directory that HOME, USERPROFILE, LOCALAPPDATA,
// XDG_DATA_HOME and the two SERENO_*_PROFILE_PATH variables point into, with
// SERENO_SANDBOX set to it so that a write anywhere else fails the test.
//
// It is test support only: it imports package testing and is never linked into
// the sereno binary (a test in internal/policy enforces that).
package testenv

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Home is the temporary home directory of one test.
type Home struct {
	Dir          string // HOME and USERPROFILE
	LocalAppData string // LOCALAPPDATA
	DataHome     string // XDG_DATA_HOME
	PSProfile    string // SERENO_PS_PROFILE_PATH
	PwshProfile  string // SERENO_PWSH_PROFILE_PATH
}

// cleared lists the variables that must not leak from the developer's session
// into a test: each is set to the empty string, which sereno treats as unset.
var cleared = []string{
	"SERENO_HOME", "SERENO_LANG", "SERENO_OS_MODE", "SERENO_SESSION", "SERENO_REG_ROOT",
	"ZDOTDIR", "WT_SESSION", "WT_PROFILE_ID", "TERM_PROGRAM", "TMUX", "NO_COLOR",
	"LC_ALL", "LC_MESSAGES", "LANG",
}

// New creates an empty temporary home, points the environment of the test at
// it and sets SERENO_SANDBOX to it. Nothing inside it is created: a test that
// needs LOCALAPPDATA or a profile to exist makes them itself or uses NewFrom.
func New(t testing.TB) *Home {
	t.Helper()
	h := homeAt(TempDir(t))
	for key, value := range h.Env() {
		t.Setenv(key, value)
	}
	return h
}

// homeAt lays out a home in a directory without creating anything.
func homeAt(dir string) *Home {
	return &Home{
		Dir:          dir,
		LocalAppData: filepath.Join(dir, "AppData", "Local"),
		DataHome:     filepath.Join(dir, ".local", "share"),
		PSProfile:    filepath.Join(dir, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"),
		PwshProfile:  filepath.Join(dir, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1"),
	}
}

// Main is the TestMain of every test package of the module:
//
//	func TestMain(m *testing.M) { testenv.Main(m) }
//
// Before any test runs it points the whole test binary at an empty temporary
// home, exactly as New does for one test. A test that forgets to call New
// therefore still cannot see the developer's real home or write outside the
// sandbox. A test in internal/policy checks that no package leaves this out.
func Main(m *testing.M) {
	created, err := os.MkdirTemp("", "sereno-test-")
	if err != nil {
		panic("testenv: " + err.Error())
	}
	dir, err := filepath.EvalSymlinks(created)
	if err != nil {
		panic("testenv: " + err.Error())
	}
	for key, value := range homeAt(dir).Env() {
		if err := os.Setenv(key, value); err != nil {
			panic("testenv: " + err.Error())
		}
	}
	code := m.Run()
	removeAll(created)
	os.Exit(code)
}

// removeAll deletes a temporary directory, trying again for a few seconds
// when another program still holds something in it.
func removeAll(dir string) error {
	var err error
	for pause := 5 * time.Millisecond; pause < 5*time.Second; pause *= 2 {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		time.Sleep(pause)
	}
	return err
}

// TempDir returns a new empty temporary directory that is removed when the
// test ends. It differs from t.TempDir in two ways that matter here. The path
// is resolved, so it has one spelling (no Windows short names, no /var link
// on macOS). And the removal is patient: on Windows, scanners may put a
// short-lived copy next to a script a test has just written, which makes the
// immediate removal of t.TempDir fail the test with "directory not empty".
func TempDir(t testing.TB) string {
	t.Helper()
	created, err := os.MkdirTemp("", "sereno-test-")
	if err != nil {
		t.Fatalf("testenv: %v", err)
	}
	t.Cleanup(func() {
		if err := removeAll(created); err != nil {
			t.Logf("testenv: temporary directory left behind: %v", err)
		}
	})
	dir, err := filepath.EvalSymlinks(created)
	if err != nil {
		t.Fatalf("testenv: %v", err)
	}
	return dir
}

// NewFrom is New followed by a copy of the fixture tree testdata/homes/<name>
// of the repository into the temporary home.
func NewFrom(t testing.TB, name string) *Home {
	t.Helper()
	h := New(t)
	CopyTree(t, Fixture(t, name), h.Dir)
	return h
}

// Env returns the environment New installs: the variables of T1 and the
// cleared ones. Tests that inject an environment instead of reading the
// process one can start from this map.
func (h *Home) Env() map[string]string {
	env := map[string]string{
		"HOME":                     h.Dir,
		"USERPROFILE":              h.Dir,
		"LOCALAPPDATA":             h.LocalAppData,
		"XDG_DATA_HOME":            h.DataHome,
		"SERENO_PS_PROFILE_PATH":   h.PSProfile,
		"SERENO_PWSH_PROFILE_PATH": h.PwshProfile,
		"SERENO_SANDBOX":           h.Dir,
	}
	for _, key := range cleared {
		env[key] = ""
	}
	return env
}

// Getenv turns a map into a lookup function with the signature of os.Getenv.
func Getenv(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

// RepoRoot returns the directory that holds go.mod, found from the working
// directory of the test.
func RepoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("testenv: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("testenv: go.mod not found above the working directory")
		}
		dir = parent
	}
}

// Fixture returns the path of the fixture tree testdata/homes/<name>.
func Fixture(t testing.TB, name string) string {
	t.Helper()
	dir := filepath.Join(RepoRoot(t), "testdata", "homes", filepath.FromSlash(name))
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("testenv: fixture %q not found at %s", name, dir)
	}
	return dir
}

// CopyTree copies the directories and regular files below src into dst, which
// must already exist. Files named .gitkeep are skipped: they only make Git
// keep an otherwise empty fixture directory.
func CopyTree(t testing.TB, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Name() == ".gitkeep" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("testenv: copy %s: %v", src, err)
	}
}

// Snapshot describes every entry below dir: "dir" for a directory,
// "link:<target>" for a symbolic link and "file:<sha256>" for anything else.
// Keys are slash-separated paths relative to dir. Two snapshots are equal
// exactly when the trees are byte-identical.
//
// The tree is read until two consecutive readings agree, so that a file some
// other program keeps there for a few milliseconds (see TempDir) does not end
// up in the snapshot.
func Snapshot(t testing.TB, dir string) map[string]string {
	t.Helper()
	previous := snapshot(t, dir)
	for attempt := 0; attempt < 50; attempt++ {
		time.Sleep(2 * time.Millisecond)
		current := snapshot(t, dir)
		if reflect.DeepEqual(previous, current) {
			return current
		}
		previous = current
	}
	t.Fatalf("testenv: %s keeps changing", dir)
	return nil
}

// snapshot reads the tree once. An entry that vanishes while it is being read
// is left out; the caller reads again.
func snapshot(t testing.TB, dir string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		key := filepath.ToSlash(rel)
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			tree[key] = "link:" + target
		case entry.IsDir():
			tree[key] = "dir"
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				tree[key] = "unreadable:" + err.Error()
				return nil
			}
			sum := sha256.Sum256(data)
			tree[key] = "file:" + hex.EncodeToString(sum[:])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("testenv: snapshot %s: %v", dir, err)
	}
	return tree
}

// Symlink creates a symbolic link for a fixture and skips the test when the
// system does not let this account create one (Windows without Developer
// Mode).
func Symlink(t testing.TB, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("testenv: symbolic links cannot be created here: %v", err)
	}
}
