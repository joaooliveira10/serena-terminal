package engine

import (
	"errors"
	"os"
	"path/filepath"
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
