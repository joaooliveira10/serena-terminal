package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// outside returns a directory that exists and is not inside the sandbox of
// the test. It is itself temporary, so a failing guard would still do no harm.
func outside(t *testing.T) string {
	t.Helper()
	return testenv.TempDir(t)
}

func mustSandboxError(t *testing.T, err error) {
	t.Helper()
	var sandboxErr *SandboxError
	if !errors.As(err, &sandboxErr) {
		t.Fatalf("got error %v, want a *SandboxError", err)
	}
}

func TestWriteFileInsideSandbox(t *testing.T) {
	home := testenv.New(t)
	path := filepath.Join(home.Dir, "file.txt")

	if err := WriteFile(path, []byte("one"), FilePerm); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("two"), FilePerm); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "two" {
		t.Errorf("content = %q, want %q", got, "two")
	}
	entries, err := os.ReadDir(home.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the file (no temporary left behind)", len(entries))
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != FilePerm {
			t.Errorf("mode = %v, want %v", info.Mode().Perm(), FilePerm)
		}
	}
}

func TestWriteFileNeedsTheDirectory(t *testing.T) {
	home := testenv.New(t)
	err := WriteFile(filepath.Join(home.Dir, "missing", "file.txt"), []byte("x"), FilePerm)
	if err == nil {
		t.Fatal("writing into a missing directory succeeded")
	}
	if _, statErr := os.Stat(filepath.Join(home.Dir, "missing")); !os.IsNotExist(statErr) {
		t.Error("the missing directory was created")
	}
}

func TestGuardStopsEveryOperationOutsideTheSandbox(t *testing.T) {
	testenv.New(t)
	other := outside(t)
	victim := filepath.Join(other, "victim.txt")
	victimDir := filepath.Join(other, "victim-dir")
	if err := os.WriteFile(victim, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(victimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	before := testenv.Snapshot(t, other)

	mustSandboxError(t, WriteFile(victim, []byte("overwritten"), FilePerm))
	mustSandboxError(t, WriteFile(filepath.Join(other, "new.txt"), []byte("new"), FilePerm))
	mustSandboxError(t, Mkdir(filepath.Join(other, "new-dir")))
	mustSandboxError(t, RemoveFile(victim))
	_, err := RemoveEmptyDir(victimDir)
	mustSandboxError(t, err)

	after := testenv.Snapshot(t, other)
	if len(after) != len(before) {
		t.Fatalf("the directory outside the sandbox changed: %v", after)
	}
	for key, value := range before {
		if after[key] != value {
			t.Errorf("%s changed outside the sandbox", key)
		}
	}
}

func TestGuardPaths(t *testing.T) {
	home := testenv.New(t)
	other := outside(t)
	if err := os.MkdirAll(filepath.Join(home.Dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	sibling := home.Dir + "-sibling"

	tests := []struct {
		name   string
		path   string
		inside bool
	}{
		{"file in the sandbox", filepath.Join(home.Dir, "a.txt"), true},
		{"file in a subdirectory", filepath.Join(home.Dir, "sub", "a.txt"), true},
		{"below directories that do not exist yet", filepath.Join(home.Dir, "x", "y", "z.txt"), true},
		{"dot-dot that stays inside", filepath.Join(home.Dir, "sub", "..", "a.txt"), true},
		{"the sandbox itself", home.Dir, false},
		{"parent of the sandbox", filepath.Dir(home.Dir), false},
		{"dot-dot that leaves", home.Dir + string(filepath.Separator) + ".." + string(filepath.Separator) + "a.txt", false},
		{"another directory", filepath.Join(other, "a.txt"), false},
		{"a sibling whose name starts like the sandbox", filepath.Join(sibling, "a.txt"), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := guard("write", test.path)
			if test.inside && err != nil {
				t.Errorf("guard refused %s: %v", test.path, err)
			}
			if !test.inside {
				mustSandboxError(t, err)
			}
		})
	}
}

// attempt runs every mutating primitive against path and returns those the
// guard did not stop.
func attempt(path string) []string {
	var leaked []string
	check := func(name string, err error) {
		var sandboxErr *SandboxError
		if !errors.As(err, &sandboxErr) {
			leaked = append(leaked, fmt.Sprintf("%s: %v", name, err))
		}
	}
	check("WriteFile", WriteFile(path, []byte("ESCAPED"), FilePerm))
	check("Mkdir", Mkdir(path))
	check("RemoveFile", RemoveFile(path))
	_, err := RemoveEmptyDir(path)
	check("RemoveEmptyDir", err)
	check("removeEmptiedDir", removeEmptiedDir(path))
	return leaked
}

// TestGuardSpellings tries the ways a path can be written so that it looks
// like one thing and is another. Every spelling of a place outside the
// sandbox must be stopped for every operation, every spelling of a place
// inside must be let through, and so must every spelling of the sandbox
// variable itself.
func TestGuardSpellings(t *testing.T) {
	home := testenv.New(t)
	other := outside(t)
	if err := os.WriteFile(filepath.Join(other, "victim.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(other, "victim-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home.Dir, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	toOther, err := filepath.Rel(home.Dir, other)
	if err != nil {
		t.Fatal(err)
	}
	before := testenv.Snapshot(t, other)

	outsideSpellings := map[string]string{
		"existing file elsewhere":             filepath.Join(other, "victim.txt"),
		"existing directory elsewhere":        filepath.Join(other, "victim-dir"),
		"new name elsewhere":                  filepath.Join(other, "new.txt"),
		"dot-dot from the sandbox":            home.Dir + sep + toOther + sep + "victim.txt",
		"dot-dot from a subdirectory":         home.Dir + sep + "sub" + sep + "deep" + sep + ".." + sep + ".." + sep + toOther + sep + "victim.txt",
		"dot-dot through a missing directory": home.Dir + sep + "nope" + sep + ".." + sep + toOther + sep + "victim.txt",
		"the sandbox itself":                  home.Dir,
		"the sandbox with a separator":        home.Dir + sep,
		"the sandbox with a dot":              home.Dir + sep + ".",
		"the parent of the sandbox":           filepath.Dir(home.Dir),
		"a sibling with the same beginning":   home.Dir + "x" + sep + "a.txt",
		"elsewhere with a separator":          filepath.Join(other, "victim-dir") + sep,
	}
	for name, path := range windowsOutsideSpellings(home.Dir, other) {
		outsideSpellings[name] = path
	}
	for name, path := range outsideSpellings {
		if leaked := attempt(path); len(leaked) > 0 {
			t.Errorf("outside, %s (%s): not stopped: %v", name, path, leaked)
		}
	}
	// Other drives and devices are only shown to the guard itself: this test
	// must not be able to write there even if the guard were wrong.
	for name, path := range windowsFarAway(home.Dir, other) {
		var sandboxErr *SandboxError
		if err := guard("write", path); !errors.As(err, &sandboxErr) {
			t.Errorf("outside, %s (%s): guard = %v", name, path, err)
		}
	}
	if after := testenv.Snapshot(t, other); !reflect.DeepEqual(after, before) {
		t.Fatalf("the directory outside the sandbox changed: %v", after)
	}

	insideSpellings := map[string]string{
		"plain":                 filepath.Join(home.Dir, "a.txt"),
		"dot-dot staying in":    home.Dir + sep + "sub" + sep + ".." + sep + "b.txt",
		"double separator":      home.Dir + sep + sep + "c.txt",
		"dot element":           home.Dir + sep + "." + sep + "d.txt",
		"in a subdirectory":     filepath.Join(home.Dir, "sub", "deep", "e.txt"),
		"out and back in again": home.Dir + sep + ".." + sep + filepath.Base(home.Dir) + sep + "f.txt",
	}
	if runtime.GOOS == "windows" {
		insideSpellings["upper case"] = filepath.Join(strings.ToUpper(home.Dir), "G.TXT")
		insideSpellings["lower case"] = filepath.Join(strings.ToLower(home.Dir), "h.txt")
		insideSpellings["forward slashes"] = filepath.ToSlash(home.Dir) + "/i.txt"
	}
	for name, path := range insideSpellings {
		if err := WriteFile(path, []byte("in"), FilePerm); err != nil {
			t.Errorf("inside, %s (%s): refused: %v", name, path, err)
		}
	}

	sandboxSpellings := map[string]string{
		"with a separator at the end": home.Dir + sep,
		"with dot-dot":                home.Dir + sep + "sub" + sep + "..",
	}
	if runtime.GOOS == "windows" {
		sandboxSpellings["in upper case"] = strings.ToUpper(home.Dir)
	}
	for name, value := range sandboxSpellings {
		t.Setenv(SandboxEnv, value)
		if err := WriteFile(filepath.Join(home.Dir, "z.txt"), []byte("in"), FilePerm); err != nil {
			t.Errorf("the sandbox spelled %s: a write inside was refused: %v", name, err)
		}
		if leaked := attempt(filepath.Join(other, "victim.txt")); len(leaked) > 0 {
			t.Errorf("the sandbox spelled %s: a write outside was not stopped: %v", name, leaked)
		}
	}
	if after := testenv.Snapshot(t, other); !reflect.DeepEqual(after, before) {
		t.Fatalf("the directory outside the sandbox changed: %v", after)
	}
}

func TestGuardIgnoresCaseOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("file names are case-sensitive here")
	}
	home := testenv.New(t)
	if err := guard("write", filepath.Join(strings.ToUpper(home.Dir), "a.txt")); err != nil {
		t.Errorf("an upper-case spelling of the sandbox was refused: %v", err)
	}
}

func TestGuardFollowsLinksOutOfTheSandbox(t *testing.T) {
	home := testenv.New(t)
	other := outside(t)
	link := filepath.Join(home.Dir, "link")
	testenv.Symlink(t, other, link)

	mustSandboxError(t, WriteFile(filepath.Join(link, "escaped.txt"), []byte("x"), FilePerm))
	mustSandboxError(t, Mkdir(filepath.Join(link, "escaped")))
	mustSandboxError(t, Mkdir(filepath.Join(link, "a", "b")))
	if entries, _ := os.ReadDir(other); len(entries) != 0 {
		t.Errorf("a write followed the link out of the sandbox: %v", entries)
	}
	// The link itself is an entry of the sandbox and may be removed.
	if err := RemoveFile(link); err != nil {
		t.Errorf("removing the link itself: %v", err)
	}
}

// TestGuardStopsAtJunctions is the Windows case the link test cannot cover: a
// directory junction needs no privilege, and resolving symbolic links does not
// follow it, so the path stays inside the sandbox letter by letter while the
// write would land outside.
func TestGuardStopsAtJunctions(t *testing.T) {
	home := testenv.New(t)
	other := outside(t)
	victim := filepath.Join(other, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(other, "victim-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home.Dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := testenv.Snapshot(t, other)
	unchanged := func(when string) {
		t.Helper()
		if after := testenv.Snapshot(t, other); !reflect.DeepEqual(after, before) {
			t.Fatalf("%s: the directory outside the sandbox changed: %v", when, after)
		}
	}

	for _, link := range []string{filepath.Join(home.Dir, "junction"), filepath.Join(home.Dir, "sub", "junction")} {
		testenv.Junction(t, other, link)
		if data, err := os.ReadFile(filepath.Join(link, "victim.txt")); err != nil || string(data) != "keep me" {
			t.Fatalf("the junction does not lead out of the sandbox: %q, %v", data, err)
		}

		mustSandboxError(t, WriteFile(filepath.Join(link, "victim.txt"), []byte("overwritten"), FilePerm))
		mustSandboxError(t, WriteFile(filepath.Join(link, "new.txt"), []byte("new"), FilePerm))
		mustSandboxError(t, WriteFile(filepath.Join(link, "a", "b", "new.txt"), []byte("new"), FilePerm))
		mustSandboxError(t, Mkdir(filepath.Join(link, "new-dir")))
		mustSandboxError(t, Mkdir(filepath.Join(link, "a", "b")))
		mustSandboxError(t, RemoveFile(filepath.Join(link, "victim.txt")))
		_, err := RemoveEmptyDir(filepath.Join(link, "victim-dir"))
		mustSandboxError(t, err)
		mustSandboxError(t, removeEmptiedDir(filepath.Join(link, "victim-dir")))
		unchanged("writes through " + link)

		// The junction itself is an entry of the sandbox: it may be removed,
		// and removing it does not touch what it leads to.
		if _, err := RemoveEmptyDir(link); err == nil {
			t.Error("RemoveEmptyDir took a junction for a directory of the sandbox")
		}
		if err := RemoveFile(link); err != nil {
			t.Errorf("removing the junction itself: %v", err)
		}
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Errorf("the junction is still there: %v", err)
		}
		unchanged("removing " + link)
	}
}

// TestSandboxStopsAnApplyThroughAJunction puts the junction where a real
// installation would meet one: LOCALAPPDATA itself leads somewhere else.
func TestSandboxStopsAnApplyThroughAJunction(t *testing.T) {
	s := newSite(t, "")
	other := outside(t)
	if err := os.Mkdir(filepath.Dir(s.home.LocalAppData), 0o755); err != nil {
		t.Fatal(err)
	}
	testenv.Junction(t, other, s.home.LocalAppData)

	plan := mustPlan(t, s.request("init v1\n"))
	mustSandboxError(t, plan.Apply())
	if entries, _ := os.ReadDir(other); len(entries) != 0 {
		t.Errorf("the apply wrote through the junction: %v", entries)
	}
	if err := os.Remove(s.home.LocalAppData); err != nil {
		t.Fatal(err)
	}
}

func TestGuardFailsClosedOnABrokenSandbox(t *testing.T) {
	home := testenv.New(t)
	target := filepath.Join(home.Dir, "a.txt")
	file := filepath.Join(home.Dir, "plain-file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, sandbox := range map[string]string{
		"relative":        "relative-sandbox",
		"missing":         filepath.Join(home.Dir, "does-not-exist"),
		"not a directory": file,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(SandboxEnv, sandbox)
			mustSandboxError(t, WriteFile(target, []byte("x"), FilePerm))
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Error("the file was written although the sandbox is unusable")
			}
		})
	}
}

func TestNoSandboxMeansNoGuard(t *testing.T) {
	home := testenv.New(t)
	t.Setenv(SandboxEnv, "")
	// Still a temporary directory: only the guard is switched off.
	if err := WriteFile(filepath.Join(home.Dir, "a.txt"), []byte("x"), FilePerm); err != nil {
		t.Fatal(err)
	}
}

func TestMkdirRemoveFileRemoveEmptyDir(t *testing.T) {
	home := testenv.New(t)
	dir := filepath.Join(home.Dir, "d")
	file := filepath.Join(dir, "f")

	if err := Mkdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := Mkdir(dir); err == nil {
		t.Error("creating an existing directory succeeded")
	}
	if err := WriteFile(file, []byte("x"), FilePerm); err != nil {
		t.Fatal(err)
	}
	if err := RemoveFile(dir); err == nil {
		t.Error("RemoveFile removed a directory")
	}
	if removed, err := RemoveEmptyDir(dir); err != nil || removed {
		t.Errorf("RemoveEmptyDir on a non-empty directory = %v, %v; want false, nil", removed, err)
	}
	if _, err := RemoveEmptyDir(file); err == nil {
		t.Error("RemoveEmptyDir accepted a file")
	}
	if err := RemoveFile(file); err != nil {
		t.Fatal(err)
	}
	if err := RemoveFile(file); err == nil {
		t.Error("removing a missing file succeeded")
	}
	if removed, err := RemoveEmptyDir(dir); err != nil || !removed {
		t.Errorf("RemoveEmptyDir on an empty directory = %v, %v; want true, nil", removed, err)
	}
	if removed, err := RemoveEmptyDir(dir); err != nil || removed {
		t.Errorf("RemoveEmptyDir on a missing directory = %v, %v; want false, nil", removed, err)
	}
}

// TestRemoveEmptiedDirIsPatientOnWindows puts a short-lived file into a
// directory, the way scanners do on Windows. There the removal waits for it;
// elsewhere nothing waits and the directory simply stays.
func TestRemoveEmptiedDirIsPatientOnWindows(t *testing.T) {
	home := testenv.New(t)
	dir := filepath.Join(home.Dir, "emptied")
	visitor := filepath.Join(dir, "SCRIPT.PS1.tmp")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(visitor, []byte("copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	gone := make(chan error, 1)
	go func() {
		time.Sleep(40 * time.Millisecond)
		gone <- os.Remove(visitor)
	}()

	if err := removeEmptiedDir(dir); err != nil {
		t.Fatal(err)
	}
	_, statErr := os.Stat(dir)
	if err := <-gone; err != nil {
		t.Fatal(err)
	}
	if removed := os.IsNotExist(statErr); removed != (runtime.GOOS == "windows") {
		t.Errorf("directory removed = %v on %s", removed, runtime.GOOS)
	}

	if err := removeEmptiedDir(filepath.Join(home.Dir, "missing")); err != nil {
		t.Errorf("a missing directory: %v", err)
	}
	if err := removeEmptiedDir(outside(t)); err == nil {
		t.Error("removeEmptiedDir worked outside the sandbox")
	}
}

func TestSHA256AndHashFile(t *testing.T) {
	home := testenv.New(t)
	const abc = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := SHA256([]byte("abc")); got != abc {
		t.Errorf("SHA256(abc) = %s", got)
	}
	path := filepath.Join(home.Dir, "abc")
	if err := WriteFile(path, []byte("abc"), FilePerm); err != nil {
		t.Fatal(err)
	}
	got, err := HashFile(path)
	if err != nil || got != abc {
		t.Errorf("HashFile = %s, %v", got, err)
	}
	if _, err := HashFile(filepath.Join(home.Dir, "missing")); err == nil {
		t.Error("HashFile of a missing file succeeded")
	}
}
