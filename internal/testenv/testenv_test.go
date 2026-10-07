package testenv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestNewIsHermetic checks rule T1: every variable a command could take a
// path from points into the temporary home, and the sandbox is that home.
func TestNewIsHermetic(t *testing.T) {
	realHome, _ := os.UserHomeDir()
	t.Setenv("SERENO_HOME", filepath.Join(os.TempDir(), "leaked-from-the-developer"))
	t.Setenv("SERENO_OS_MODE", "light")
	t.Setenv("ZDOTDIR", filepath.Join(os.TempDir(), "leaked-zdotdir"))

	home := New(t)
	for _, key := range []string{"HOME", "USERPROFILE", "LOCALAPPDATA", "XDG_DATA_HOME", "SERENO_PS_PROFILE_PATH", "SERENO_PWSH_PROFILE_PATH"} {
		value := os.Getenv(key)
		if value != home.Dir && !strings.HasPrefix(value, home.Dir+string(filepath.Separator)) {
			t.Errorf("%s = %q is not inside the temporary home %q", key, value, home.Dir)
		}
	}
	if got := os.Getenv("SERENO_SANDBOX"); got != home.Dir {
		t.Errorf("SERENO_SANDBOX = %q, want %q", got, home.Dir)
	}
	for _, key := range cleared {
		if value := os.Getenv(key); value != "" {
			t.Errorf("%s = %q leaked into the test", key, value)
		}
	}
	if realHome != "" && (home.Dir == realHome || strings.HasPrefix(realHome, home.Dir+string(filepath.Separator))) {
		t.Errorf("the temporary home %q contains the real one", home.Dir)
	}
	if entries, err := os.ReadDir(home.Dir); err != nil || len(entries) != 0 {
		t.Errorf("a new home must be empty: %v, %v", entries, err)
	}
	if resolved, err := filepath.EvalSymlinks(home.Dir); err != nil || resolved != home.Dir {
		t.Errorf("the home path %q is not fully resolved (%q, %v)", home.Dir, resolved, err)
	}
	if !reflect.DeepEqual(Getenv(home.Env())("HOME"), home.Dir) || Getenv(nil)("HOME") != "" {
		t.Error("Getenv does not read the map")
	}
}

func TestNewFromCopiesTheFixture(t *testing.T) {
	home := NewFrom(t, "wt-present")
	tree := Snapshot(t, home.Dir)
	for _, want := range []string{
		".bashrc",
		"Documents/WindowsPowerShell/Microsoft.PowerShell_profile.ps1",
		"AppData/Local/Packages/Microsoft.WindowsTerminal_8wekyb3d8bbwe/LocalState/settings.json",
		"AppData/Local/Microsoft/Windows Terminal/Fragments/Git/git-bash.json",
	} {
		if !strings.HasPrefix(tree[want], "file:") {
			t.Errorf("%s was not copied (%q)", want, tree[want])
		}
	}
	if tree["AppData/Local"] != "dir" {
		t.Errorf("directories are not in the snapshot: %v", tree)
	}

	// The copy is byte-exact: the CRLF profile keeps its line endings.
	profile, err := os.ReadFile(home.PSProfile)
	if err != nil || !strings.HasSuffix(string(profile), "\r\n") {
		t.Errorf("the profile lost its CRLF: %q, %v", profile, err)
	}

	empty := NewFrom(t, "empty")
	if left := Snapshot(t, empty.Dir); len(left) != 0 {
		t.Errorf("the empty fixture copied something: %v", left)
	}
}

func TestSnapshotSeesEveryChange(t *testing.T) {
	home := NewFrom(t, "lf")
	before := Snapshot(t, home.Dir)
	if !reflect.DeepEqual(before, Snapshot(t, home.Dir)) {
		t.Fatal("two snapshots of an unchanged tree differ")
	}
	rc := filepath.Join(home.Dir, ".bashrc")
	original, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}

	changes := map[string]func(){
		"a changed byte": func() { os.WriteFile(rc, append([]byte("X"), original[1:]...), 0o644) },
		"a new file":     func() { os.WriteFile(filepath.Join(home.Dir, "new"), nil, 0o644) },
		"a new folder":   func() { os.Mkdir(filepath.Join(home.Dir, "folder"), 0o755) },
		"a removed file": func() { os.Remove(rc) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			change()
			if reflect.DeepEqual(before, Snapshot(t, home.Dir)) {
				t.Error("the snapshot did not notice")
			}
			// Put the tree back for the next case.
			os.Remove(filepath.Join(home.Dir, "new"))
			os.Remove(filepath.Join(home.Dir, "folder"))
			if err := os.WriteFile(rc, original, 0o644); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, Snapshot(t, home.Dir)) {
				t.Fatal("the tree could not be restored")
			}
		})
	}

	t.Run("a link", func(t *testing.T) {
		link := filepath.Join(home.Dir, "link")
		Symlink(t, ".bashrc", link)
		if got := Snapshot(t, home.Dir)["link"]; got != "link:.bashrc" {
			t.Errorf("a link is recorded as %q", got)
		}
	})
}

func TestRepoRootAndFixture(t *testing.T) {
	root := RepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Errorf("RepoRoot = %q has no go.mod", root)
	}
	if got := Fixture(t, "empty"); got != filepath.Join(root, "testdata", "homes", "empty") {
		t.Errorf("Fixture = %q", got)
	}
}
