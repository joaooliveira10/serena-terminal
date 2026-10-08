package testenv

import (
	"io/fs"
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
	// The last three are stored under short alias names in the repository
	// and must arrive under the real, long ones.
	for _, want := range []string{
		".bashrc",
		"Documents/WindowsPowerShell/Microsoft.PowerShell_profile.ps1",
		"AppData/Local/Packages/Microsoft.WindowsTerminal_8wekyb3d8bbwe/LocalState/settings.json",
		"AppData/Local/Packages/Microsoft.WindowsTerminal_8wekyb3d8bbwe/LocalState/state.json",
		"AppData/Local/Microsoft/Windows Terminal/Fragments/Git/git-bash.json",
	} {
		if !strings.HasPrefix(tree[want], "file:") {
			t.Errorf("%s was not copied (%q)", want, tree[want])
		}
	}
	if tree["AppData/Local"] != "dir" || tree["AppData/Local/Microsoft/Windows Terminal/Fragments/Git"] != "dir" {
		t.Errorf("directories are not in the snapshot: %v", tree)
	}
	for key := range tree {
		if strings.Contains(key, "@") {
			t.Errorf("an alias name reached the home: %s", key)
		}
	}
	if len(tree) != 16 {
		t.Errorf("the home holds %d entries, want the 5 files and their 11 directories: %v", len(tree), tree)
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

func TestExpand(t *testing.T) {
	tests := map[string]string{
		"@wt-state":                    "AppData/Local/Packages/Microsoft.WindowsTerminal_8wekyb3d8bbwe/LocalState",
		"@wt-state/settings.json":      "AppData/Local/Packages/Microsoft.WindowsTerminal_8wekyb3d8bbwe/LocalState/settings.json",
		"@wt-fragments/Git/git.json":   "AppData/Local/Microsoft/Windows Terminal/Fragments/Git/git.json",
		"Documents/@wt-state/x":        "Documents/@wt-state/x", // only at the top level
		"@unknown/x":                   "@unknown/x",
		".bashrc":                      ".bashrc",
		"AppData/Local/sereno/mode":    "AppData/Local/sereno/mode",
		"@wt-state-and-more/file.json": "@wt-state-and-more/file.json",
	}
	for in, want := range tests {
		if got := filepath.ToSlash(expand(filepath.FromSlash(in))); got != want {
			t.Errorf("expand(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestJunction checks the helper the sandbox tests rely on: what it makes
// must really lead to the target, or those tests would prove nothing.
func TestJunction(t *testing.T) {
	home := New(t)
	target := TempDir(t)
	if err := os.WriteFile(filepath.Join(target, "inside.txt"), []byte("reached"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home.Dir, "junction")
	Junction(t, target, link)

	data, err := os.ReadFile(filepath.Join(link, "inside.txt"))
	if err != nil || string(data) != "reached" {
		t.Fatalf("reading through the junction: %q, %v", data, err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&fs.ModeSymlink != 0 || info.Mode()&fs.ModeIrregular == 0 {
		t.Errorf("a junction should be neither a symbolic link nor a plain directory to Go: %v, %v", info.Mode(), err)
	}
	if resolved, err := filepath.EvalSymlinks(link); err != nil || resolved != link {
		t.Errorf("resolving links is expected to leave a junction in the path: %q, %v", resolved, err)
	}
	// Removing the junction removes the entry, not what it leads to.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "inside.txt")); err != nil {
		t.Errorf("the target lost its file: %v", err)
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
