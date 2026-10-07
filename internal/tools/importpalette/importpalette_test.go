package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

const (
	fixtureThemes = "testdata/themes"
	fixtureCommit = "0123456789abcdef0123456789abcdef01234567"
	otherCommit   = "89abcdef0123456789abcdef0123456789abcdef"
	tagObject     = "fedcba9876543210fedcba9876543210fedcba98"
)

// importInto runs the tool with the fixture themes and returns the exit code
// and what it printed.
func importInto(t *testing.T, out string, extra ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args := append([]string{"--themes", fixtureThemes, "--tag", "v9.9.9", "--out", out}, extra...)
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// TestImportIsReproducible is the byte-for-byte requirement of R6: two runs on
// the same input give the same file, and that file is the reviewed golden one.
func TestImportIsReproducible(t *testing.T) {
	home := testenv.New(t)
	first := filepath.Join(home.Dir, "first.json")
	second := filepath.Join(home.Dir, "second.json")

	for _, out := range []string{first, second} {
		if code, _, stderr := importInto(t, out, "--commit", fixtureCommit); code != 0 {
			t.Fatalf("exit %d: %s", code, stderr)
		}
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two runs on the same input produced different bytes")
	}
	golden, err := os.ReadFile("testdata/serena.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, golden) {
		t.Errorf("the generated file differs from testdata/serena.golden.json:\n%s", a)
	}

	// A third run over the existing file changes nothing and says so.
	code, stdout, _ := importInto(t, first, "--commit", fixtureCommit)
	if code != 0 || !strings.Contains(stdout, "up to date") {
		t.Errorf("rerun: exit %d, output %q", code, stdout)
	}
	if again, _ := os.ReadFile(first); !bytes.Equal(again, a) {
		t.Error("the rerun changed the file")
	}
}

// TestImportedFileFollowsR6 reads the generated file back and checks every
// rule of R6 against the fixture themes.
func TestImportedFileFollowsR6(t *testing.T) {
	home := testenv.New(t)
	out := filepath.Join(home.Dir, "serena.json")
	if code, _, stderr := importInto(t, out, "--commit", fixtureCommit, "--repo", "example.org/theme"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	p, err := palette.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != (palette.Source{Repo: "example.org/theme", Tag: "v9.9.9", Commit: fixtureCommit}) {
		t.Errorf("source = %+v", p.Source)
	}

	dark, _ := p.Variant("dark")
	if dark.Background != "#101010" || dark.Foreground != "#e0e0e0" || dark.Cursor != "#c0c0c0" {
		t.Errorf("dark: background, foreground, cursor = %s %s %s (upper-case input must come out in lower case)",
			dark.Background, dark.Foreground, dark.Cursor)
	}
	if dark.SelectionBackground != "#304050" {
		t.Errorf("dark: selectionBackground = %s, want the first 7 characters of editor.selectionBackground", dark.SelectionBackground)
	}
	if dark.SelectionForeground != dark.Foreground {
		t.Errorf("dark: selectionForeground = %s, want the foreground", dark.SelectionForeground)
	}
	// Slots: Black..White then BrightBlack..BrightWhite.
	for n, c := range dark.ANSI {
		want := palette.Color(fmt.Sprintf("#a1%x%x%x%x", n, n, n, n))
		if c != want {
			t.Errorf("dark: ansi[%d] = %s, want %s", n, c, want)
		}
	}

	hc, _ := p.Variant("hc")
	if hc.Kind != palette.KindHC || hc.SelectionBackground != "#ccbbff" || hc.SelectionForeground != "#050505" {
		t.Errorf("hc: kind %s, selection %s on %s; the terminal.selection* keys must win", hc.Kind, hc.SelectionForeground, hc.SelectionBackground)
	}
	hcLight, _ := p.Variant("hc-light")
	if hcLight.Kind != palette.KindHCLight || hcLight.Name != "Serena High Contrast Light" {
		t.Errorf("hc-light = %+v", hcLight)
	}
}

func TestRunUsageErrors(t *testing.T) {
	home := testenv.New(t)
	out := filepath.Join(home.Dir, "serena.json")
	tests := map[string][]string{
		"no arguments":   {},
		"no tag":         {"--themes", fixtureThemes, "--out", out},
		"no themes":      {"--tag", "v9.9.9", "--out", out},
		"unknown flag":   {"--themes", fixtureThemes, "--tag", "v9.9.9", "--out", out, "--force"},
		"stray argument": {"--themes", fixtureThemes, "--tag", "v9.9.9", "--out", out, "extra"},
		"short commit":   {"--themes", fixtureThemes, "--tag", "v9.9.9", "--out", out, "--commit", "0123abc"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != 2 {
				t.Errorf("exit %d, want 2 (%s)", code, stderr.String())
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Error("a file was written despite the usage error")
			}
		})
	}
}

// TestRunWithoutMetadataAsksForCommit covers an export made with git archive:
// there is no .git above the themes, so the tool must stop and say what to do.
func TestRunWithoutMetadataAsksForCommit(t *testing.T) {
	home := testenv.New(t)
	themes := filepath.Join(home.Dir, "export", "themes")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	testenv.CopyTree(t, fixtureThemes, themes)
	out := filepath.Join(home.Dir, "serena.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--themes", themes, "--tag", "v9.9.9", "--out", out}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "--commit") {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("a file was written without a commit")
	}
}

func TestRunStaysInsideTheSandbox(t *testing.T) {
	testenv.New(t)
	out := filepath.Join(testenv.TempDir(t), "serena.json")
	code, _, stderr := importInto(t, out, "--commit", fixtureCommit)
	if code != 1 || !strings.Contains(stderr, "SERENO_SANDBOX") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("the file was written outside the sandbox")
	}
}

// editedThemes copies the fixture themes into the temporary home and applies
// one edit to one file.
func editedThemes(t *testing.T, home *testenv.Home, file string, edit func(string) string) string {
	t.Helper()
	dir := filepath.Join(home.Dir, "themes")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	testenv.CopyTree(t, fixtureThemes, dir)
	if file == "" {
		return dir
	}
	path := filepath.Join(dir, file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := edit(string(data))
	if edited == string(data) {
		t.Fatal("the edit did not change the theme")
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBuildRejectsBrokenThemes(t *testing.T) {
	const dark = "serena-dark-color-theme.json"
	replace := func(old, new string) func(string) string {
		return func(s string) string { return strings.Replace(s, old, new, 1) }
	}
	tests := []struct {
		name string
		file string
		edit func(string) string
		want string
	}{
		{"missing slot", dark, replace(`    "terminal.ansiCyan": "#a16666",`+"\n", ""), "terminal.ansiCyan"},
		{"missing background", dark, replace(`    "terminal.background": "#101010",`+"\n", ""), "terminal.background"},
		{"missing selection", dark, replace(`    "editor.selectionBackground": "#30405080",`+"\n", ""), "editor.selectionBackground"},
		{"malformed colour", dark, replace(`"#C0C0C0"`, `"silver"`), "terminalCursor.foreground"},
		{"alpha where none is allowed", dark, replace(`"terminal.background": "#101010"`, `"terminal.background": "#10101080"`), "terminal.background"},
		{"unknown type", dark, replace(`"type": "dark"`, `"type": "sepia"`), "sepia"},
		{"kind does not match the name", dark, replace(`"type": "dark"`, `"type": "light"`), "Serena Dark"},
		{"renamed theme", dark, replace(`"name": "Serena Dark"`, `"name": "Serena Night"`), "Serena Dark"},
		{"duplicate name", dark, replace(`"name": "Serena Dark"`, `"name": "Serena Light"`), "Serena Light"},
		{"not json", dark, func(string) string { return "{" }, dark},
	}
	source := palette.Source{Repo: "example.org/theme", Tag: "v9.9.9", Commit: fixtureCommit}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := testenv.New(t)
			dir := editedThemes(t, home, test.file, test.edit)
			_, err := build(dir, source)
			if err == nil {
				t.Fatal("build accepted the themes")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error %q does not mention %q", err, test.want)
			}
		})
	}

	t.Run("five files", func(t *testing.T) {
		home := testenv.New(t)
		dir := editedThemes(t, home, "", nil)
		if err := os.Remove(filepath.Join(dir, dark)); err != nil {
			t.Fatal(err)
		}
		if _, err := build(dir, source); err == nil {
			t.Error("build accepted five theme files")
		}
	})
}

func TestDropCommentLines(t *testing.T) {
	in := "\xef\xbb\xbf// header\r\n{\n  // indented\n  \"a\": \"// kept\",\n\t// tab\n  \"b\": 1\n}\n// trailer"
	want := "{\n  \"a\": \"// kept\",\n  \"b\": 1\n}\n"
	if got := string(dropCommentLines([]byte(in))); got != want {
		t.Errorf("dropCommentLines = %q, want %q", got, want)
	}
}

// repository builds the smallest .git directory the tool can read: HEAD plus
// whatever refs the test adds. It returns the themes directory inside the
// work tree and the .git directory.
func repository(t *testing.T, head string) (themes, gitDir string) {
	t.Helper()
	home := testenv.New(t)
	gitDir = filepath.Join(home.Dir, "theme", ".git")
	themes = filepath.Join(home.Dir, "theme", "themes")
	for _, dir := range []string{themes, filepath.Join(gitDir, "refs", "tags"), filepath.Join(gitDir, "refs", "heads")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(gitDir, "HEAD"), head+"\n")
	return themes, gitDir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeObject stores a loose Git object the way Git does: zlib over
// "<type> <size>\x00<body>".
func writeObject(t *testing.T, gitDir, id, kind, body string) {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	fmt.Fprintf(w, "%s %d\x00%s", kind, len(body), body)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(gitDir, "objects", id[:2], id[2:]), buf.String())
}

func TestTagCommit(t *testing.T) {
	annotated := "object " + fixtureCommit + "\ntype commit\ntag v9.9.9\ntagger A Tagger 0 +0000\n\nrelease\n"
	tests := []struct {
		name  string
		head  string
		setup func(t *testing.T, gitDir string)
		want  string // "" means an error is expected
		hint  string // text the error must contain
	}{
		{
			name: "detached at a lightweight tag",
			head: fixtureCommit,
			setup: func(t *testing.T, gitDir string) {
				write(t, filepath.Join(gitDir, "refs", "tags", "v9.9.9"), fixtureCommit+"\n")
				writeObject(t, gitDir, fixtureCommit, "commit", "tree 0\n")
			},
			want: fixtureCommit,
		},
		{
			name: "branch at an annotated tag stored loose",
			head: "ref: refs/heads/main",
			setup: func(t *testing.T, gitDir string) {
				write(t, filepath.Join(gitDir, "refs", "heads", "main"), fixtureCommit+"\n")
				write(t, filepath.Join(gitDir, "refs", "tags", "v9.9.9"), tagObject+"\n")
				writeObject(t, gitDir, tagObject, "tag", annotated)
				writeObject(t, gitDir, fixtureCommit, "commit", "tree 0\n")
			},
			want: fixtureCommit,
		},
		{
			name: "packed refs with a peeled line, as in a clone",
			head: "ref: refs/heads/main",
			setup: func(t *testing.T, gitDir string) {
				write(t, filepath.Join(gitDir, "packed-refs"),
					"# pack-refs with: peeled fully-peeled sorted \n"+
						fixtureCommit+" refs/heads/main\n"+
						otherCommit+" refs/tags/v9.9.8\n"+
						tagObject+" refs/tags/v9.9.9\n"+
						"^"+fixtureCommit+"\n")
			},
			want: fixtureCommit,
		},
		{
			name: "packed lightweight tag in a fully peeled file",
			head: fixtureCommit,
			setup: func(t *testing.T, gitDir string) {
				write(t, filepath.Join(gitDir, "packed-refs"),
					"# pack-refs with: peeled fully-peeled sorted \n"+fixtureCommit+" refs/tags/v9.9.9\n")
			},
			want: fixtureCommit,
		},
		{
			name: "annotated tag whose object is packed: HEAD is trusted",
			head: fixtureCommit,
			setup: func(t *testing.T, gitDir string) {
				write(t, filepath.Join(gitDir, "refs", "tags", "v9.9.9"), tagObject+"\n")
			},
			want: fixtureCommit,
		},
		{
			name: "work tree on another commit",
			head: otherCommit,
			setup: func(t *testing.T, gitDir string) {
				write(t, filepath.Join(gitDir, "packed-refs"),
					"# pack-refs with: peeled fully-peeled sorted \n"+tagObject+" refs/tags/v9.9.9\n^"+fixtureCommit+"\n")
			},
			hint: "check out the tag",
		},
		{
			name:  "tag does not exist",
			head:  fixtureCommit,
			setup: func(*testing.T, string) {},
			hint:  "--commit",
		},
		{
			name:  "branch without a ref",
			head:  "ref: refs/heads/main",
			setup: func(*testing.T, string) {},
			hint:  "--commit",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			themes, gitDir := repository(t, test.head)
			test.setup(t, gitDir)
			got, err := tagCommit(themes, "v9.9.9")
			if test.want == "" {
				if err == nil {
					t.Fatalf("tagCommit = %s, want an error", got)
				}
				if !strings.Contains(err.Error(), test.hint) {
					t.Errorf("error %q does not contain %q", err, test.hint)
				}
				return
			}
			if err != nil || got != test.want {
				t.Errorf("tagCommit = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

// TestRunReadsTheCommitFromGitMetadata is the command line of R6 as written:
// only --themes and --tag.
func TestRunReadsTheCommitFromGitMetadata(t *testing.T) {
	themes, gitDir := repository(t, fixtureCommit)
	write(t, filepath.Join(gitDir, "refs", "tags", "v9.9.9"), fixtureCommit+"\n")
	writeObject(t, gitDir, fixtureCommit, "commit", "tree 0\n")
	testenv.CopyTree(t, fixtureThemes, themes)
	out := filepath.Join(filepath.Dir(themes), "serena.json")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--themes", themes, "--tag", "v9.9.9", "--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	p, err := palette.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.Source.Commit != fixtureCommit {
		t.Errorf("commit = %s, want %s", p.Source.Commit, fixtureCommit)
	}
}

func TestLinkedWorkTree(t *testing.T) {
	themes, gitDir := repository(t, fixtureCommit)
	// Turn the work tree into a linked one: .git becomes a file that points
	// at a per-work-tree directory whose commondir is the main repository.
	main := filepath.Join(filepath.Dir(gitDir), "main.git")
	if err := os.Rename(gitDir, main); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(main, "worktrees", "wt")
	write(t, filepath.Join(linked, "HEAD"), "ref: refs/heads/topic\n")
	write(t, filepath.Join(linked, "commondir"), "../..\n")
	write(t, gitDir, "gitdir: "+linked+"\n")
	write(t, filepath.Join(main, "refs", "heads", "topic"), fixtureCommit+"\n")
	write(t, filepath.Join(main, "packed-refs"), tagObject+" refs/tags/v9.9.9\n^"+fixtureCommit+"\n")

	got, err := tagCommit(themes, "v9.9.9")
	if err != nil || got != fixtureCommit {
		t.Errorf("tagCommit = %q, %v", got, err)
	}
}
