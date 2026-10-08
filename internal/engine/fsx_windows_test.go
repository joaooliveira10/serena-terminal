//go:build windows

package engine

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// windowsOutsideSpellings returns the spellings of places outside the sandbox
// that only Windows has. All of them lead to the temporary directory other,
// so even a guard that failed would do no harm.
func windowsOutsideSpellings(sandbox, other string) map[string]string {
	victim := filepath.Join(other, "victim.txt")
	volume := filepath.VolumeName(sandbox) // "C:"
	toOther, _ := filepath.Rel(sandbox, other)
	return map[string]string{
		"mixed case with dot-dot":      strings.ToUpper(sandbox) + `\` + strings.ToLower(toOther) + `\VICTIM.TXT`,
		"upper case elsewhere":         strings.ToUpper(victim),
		"forward slashes elsewhere":    filepath.ToSlash(victim),
		"forward slashes with dot-dot": filepath.ToSlash(sandbox) + "/../" + filepath.Base(other) + "/victim.txt",
		"extended-length spelling":     `\\?\` + victim,
		"rooted path without a drive":  strings.TrimPrefix(victim, volume),
		"a stream of the sandbox":      sandbox + ":stream",
	}
}

// windowsFarAway returns paths the guard must refuse and that the test does
// not dare to hand to a write: other drives, devices, a network spelling.
func windowsFarAway(sandbox, other string) map[string]string {
	volume := filepath.VolumeName(sandbox)
	victim := filepath.Join(other, "victim.txt")
	far := map[string]string{
		"the NUL device":           `NUL`,
		"a device path":            `\\.\NUL`,
		"the administrative share": `\\localhost\` + strings.TrimSuffix(volume, ":") + `$` + strings.TrimPrefix(victim, volume),
	}
	for _, drive := range []string{"A", "B", "D", "E", "Q", "Z"} {
		if !strings.EqualFold(drive+":", volume) {
			far["drive "+drive] = drive + `:\sereno-test\a.txt`
			far["drive "+drive+", relative"] = drive + `:a.txt`
		}
	}
	return far
}

var procGetShortPathName = syscall.NewLazyDLL("kernel32.dll").NewProc("GetShortPathNameW")

// shortPath returns the 8.3 spelling of a path, or the path itself where the
// volume keeps no short names.
func shortPath(t *testing.T, long string) string {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(long)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 1024)
	n, _, _ := procGetShortPathName.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 || int(n) > len(buffer) {
		return long
	}
	return syscall.UTF16ToString(buffer[:n])
}

// TestGuardShortNames uses the 8.3 spelling of a directory, which is a second
// name for the same directory. The temporary directory of a Windows account
// is often given in that spelling.
func TestGuardShortNames(t *testing.T) {
	home := testenv.New(t)
	other := testenv.TempDir(t)
	long := filepath.Join(home.Dir, "a long directory name")
	if err := os.Mkdir(long, 0o755); err != nil {
		t.Fatal(err)
	}
	if short := shortPath(t, long); short != long {
		if err := WriteFile(filepath.Join(short, "x.txt"), []byte("in"), FilePerm); err != nil {
			t.Errorf("the short spelling %s of a directory inside the sandbox was refused: %v", short, err)
		}
	}
	if short := shortPath(t, other); short != other {
		if leaked := attempt(filepath.Join(short, "y.txt")); len(leaked) > 0 {
			t.Errorf("the short spelling %s of a directory outside was not stopped: %v", short, leaked)
		}
	}
	// os.TempDir may itself be the short spelling of the parent.
	if err := WriteFile(filepath.Join(os.TempDir(), filepath.Base(home.Dir), "z.txt"), []byte("in"), FilePerm); err != nil {
		t.Errorf("the sandbox reached through os.TempDir was refused: %v", err)
	}
	if leaked := attempt(filepath.Join(os.TempDir(), filepath.Base(other), "y.txt")); len(leaked) > 0 {
		t.Errorf("a directory outside reached through os.TempDir was not stopped: %v", leaked)
	}
	if entries, _ := os.ReadDir(other); len(entries) != 0 {
		t.Errorf("the directory outside the sandbox changed: %v", entries)
	}
}
