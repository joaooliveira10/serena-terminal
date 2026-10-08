package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// goldenDir is where the reviewed renderings live, below the repository root.
const goldenDir = "testdata/golden"

// regenerate is what a failure tells the developer to run.
const regenerate = "go run ./internal/tools/golden --out testdata/golden"

// TestGoldenFiles is T4: everything the renderer produces is compared, byte
// for byte, with the reviewed files of testdata/golden: the fragment in every
// case of T4, the three init files, PALETTE of the six variants and the 24
// export files. A file that the renderer no longer produces fails the test
// too. Nothing is written: a deliberate change is followed by the command in
// the message, and by reading the diff.
func TestGoldenFiles(t *testing.T) {
	want, err := tree()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(testenv.RepoRoot(t), filepath.FromSlash(goldenDir))

	seen := map[string]bool{}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if name == "README.txt" {
			return nil
		}
		seen[name] = true
		rendered, ok := want[name]
		if !ok {
			t.Errorf("%s/%s is not produced by the renderer any more: delete it", goldenDir, name)
			return nil
		}
		golden, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(golden, rendered) {
			t.Errorf("%s/%s differs from what the renderer produces (%s)\n%s", goldenDir, name, regenerate, firstDifference(golden, rendered))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for name := range want {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("%s/%s is missing (%s)", goldenDir, name, regenerate)
	}
}

// firstDifference describes where two files part, for the failure message.
func firstDifference(golden, rendered []byte) string {
	a, b := strings.Split(string(golden), "\n"), strings.Split(string(rendered), "\n")
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return "  line " + itoa(i+1) + "\n    golden:   " + clip(x) + "\n    rendered: " + clip(y)
		}
	}
	return "  the files differ in their line endings only"
}

func clip(line string) string {
	if len(line) > 160 {
		return line[:160] + "..."
	}
	return line
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

// TestTreeIsComplete counts what T4 lists, so that a case cannot silently
// drop out of the golden set.
func TestTreeIsComplete(t *testing.T) {
	files, err := tree()
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for name := range files {
		count[strings.SplitN(name, "/", 2)[0]]++
	}
	want := map[string]int{"fragment": 11, "init": 3, "palette": 7, "export": 24}
	for group, n := range want {
		if count[group] != n {
			t.Errorf("%d files under %s/, want %d", count[group], group, n)
		}
	}
	if len(count) != len(want) {
		t.Errorf("groups %v, want %v", count, want)
	}
	for _, name := range []string{
		"fragment/auto-soft.json", "fragment/auto-vivid.json", "fragment/dark-soft.json", "fragment/light-soft.json",
		"fragment/hc.json", "fragment/hc-light.json", "fragment/no-profile.json", "fragment/pwsh.json", "fragment/custom-root.json",
		"init/init.ps1", "init/init.bash", "init/init.zsh", "palette/hc-light.bin", "palette/reset.bin",
		"export/ghostty/Serena Dark", "export/alacritty/serena-hc-light.toml",
	} {
		if _, ok := files[name]; !ok {
			t.Errorf("%s is not rendered", name)
		}
	}
	if !bytes.Contains(files["fragment/custom-root.json"], []byte(`"D:\\Tools\\sereno home\\shell\\init.ps1\"`)) {
		t.Error("the custom-root fragment does not name init.ps1 by its absolute path")
	}
}

// TestRunWritesOnlyInsideTheSandbox renders into a temporary folder, twice,
// and proves the tool cannot write anywhere else while SERENO_SANDBOX is set.
func TestRunWritesOnlyInsideTheSandbox(t *testing.T) {
	home := testenv.New(t)
	out := filepath.Join(home.Dir, "rendered")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	want, err := tree()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range want {
		got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("%s was not written as rendered (%v)", name, err)
		}
	}
	before := testenv.Snapshot(t, out)
	stdout.Reset()
	if code := run([]string{"--out", out}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), ", 0 written") {
		t.Errorf("a second run should write nothing: exit %d, %q", code, stdout.String())
	}
	if after := testenv.Snapshot(t, out); len(after) != len(before) {
		t.Error("the second run changed the tree")
	}

	outside := testenv.TempDir(t)
	stderr.Reset()
	if code := run([]string{"--out", filepath.Join(outside, "x")}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "SERENO_SANDBOX") {
		t.Errorf("a write outside the sandbox must fail: exit %d, %q", code, stderr.String())
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("%d entries were created outside the sandbox", len(entries))
	}
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Errorf("no --out: exit %d, want 2", code)
	}
}
