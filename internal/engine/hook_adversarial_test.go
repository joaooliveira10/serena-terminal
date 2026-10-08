package engine

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// This file holds the hook editor's hostile cases: profiles in every shape a
// user's editor leaves them in, what users do to the hook line afterwards,
// and lines of the user's own that merely mention the marker. They were
// added by an independent review of the hook editor.

// describe renders a plan for failure messages, with note keys instead of
// translated texts.
func describe(plan *Plan) string {
	var b strings.Builder
	for _, op := range plan.Ops {
		fmt.Fprintf(&b, "  %-8s%s", op.Verb, op.Path)
		if !op.Note.IsZero() {
			fmt.Fprintf(&b, " (%s)", op.Note.Key)
		}
		b.WriteString("\n")
		for _, text := range op.Removed {
			b.WriteString("          - " + text + "\n")
		}
		for _, text := range op.Added {
			b.WriteString("          + " + text + "\n")
		}
	}
	for _, r := range plan.Refusals {
		fmt.Fprintf(&b, "  REFUSAL %s: %s\n", r.Path, r.Reason.Key)
	}
	return b.String()
}

// bom16 is the UTF-16 little endian byte order mark followed by the text.
func bom16(text string) []byte {
	return append([]byte{0xFF, 0xFE}, utf16le(text)...)
}

// profileSite is a site whose home holds one profile with the given bytes,
// and the request that asks for a hook line in it. shell is "bash" (the
// profile is ~/.bashrc, the line is bashLine) or "powershell" (the profile is
// the Windows PowerShell one, the line is psLine).
func profileSite(t *testing.T, shell string, original []byte) (site, string, Request) {
	t.Helper()
	s := newSite(t, "")
	target, line, crlf := filepath.Join(s.home.Dir, bashrc), bashLine, false
	if shell == "powershell" {
		target, line, crlf = s.home.PSProfile, psLine, true
	}
	if original != nil {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, original, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	req := s.request("init v1\n")
	req.Hooks = []Hook{{Shell: shell, Path: target, Line: line, NewFileCRLF: crlf}}
	return s, target, req
}

// appended is an independent reading of R22 for an append: the line goes to
// the end, in the encoding of the file, after a line ending when the last
// line has none, with CRLF when the file has any CRLF.
func appended(original []byte, line string) []byte {
	wide := bytes.HasPrefix(original, []byte{0xFF, 0xFE})
	encode := func(text string) []byte {
		if wide {
			return utf16le(text)
		}
		return []byte(text)
	}
	body := original
	switch {
	case wide:
		body = original[2:]
	case bytes.HasPrefix(original, []byte{0xEF, 0xBB, 0xBF}):
		body = original[3:]
	}
	var units []uint16
	if wide {
		for i := 0; i+1 < len(body); i += 2 {
			units = append(units, uint16(body[i])|uint16(body[i+1])<<8)
		}
	} else {
		for _, b := range body {
			units = append(units, uint16(b))
		}
	}
	eol := "\n"
	for i := 1; i < len(units); i++ {
		if units[i] == '\n' && units[i-1] == '\r' {
			eol = "\r\n"
		}
	}
	out := append([]byte{}, original...)
	if len(units) > 0 && units[len(units)-1] != '\n' {
		out = append(out, encode(eol)...)
	}
	out = append(out, encode(line)...)
	return append(out, encode(eol)...)
}

// TestHookRoundTrips applies and uninstalls against profiles in many shapes.
// Each case asserts the exact bytes after apply, that a second apply changes
// nothing, and that the whole home is byte-identical after uninstall.
func TestHookRoundTrips(t *testing.T) {
	cases := []struct {
		name     string
		shell    string
		original []byte
	}{
		{"LF", "bash", []byte("export A=1\nexport B=2\n")},
		{"CRLF", "bash", []byte("export A=1\r\nexport B=2\r\n")},
		{"CRLF no final newline", "bash", []byte("export A=1\r\nexport B=2")},
		{"LF no final newline", "bash", []byte("export A=1\nexport B=2")},
		{"one line no newline", "bash", []byte("x")},
		{"empty file", "bash", []byte{}},
		{"only a newline", "bash", []byte("\n")},
		{"only CRLF", "bash", []byte("\r\n")},
		{"blank lines at the end", "bash", []byte("a\n\n\n")},
		{"mixed endings CRLF first", "bash", []byte("a\r\nb\nc\n")},
		{"mixed endings LF first, no final", "bash", []byte("a\nb\r\nc")},
		{"lone CR at the end", "bash", []byte("a\nb\r")},
		{"UTF-8 mark LF", "powershell", append([]byte{0xEF, 0xBB, 0xBF}, "$a = 1\n"...)},
		{"UTF-8 mark CRLF no final newline", "powershell", append([]byte{0xEF, 0xBB, 0xBF}, "$a = 1\r\n$b = 'ção'"...)},
		{"UTF-8 mark only", "powershell", []byte{0xEF, 0xBB, 0xBF}},
		{"UTF-8 accents without a mark", "bash", []byte("# configuração\nexport A='ç'\n")},
		{"Latin-1 bytes (invalid UTF-8)", "bash", []byte("# configura\xe7\xe3o\r\nexport A=1\r\n")},
		{"UTF-16LE CRLF", "powershell", bom16("$a = 1\r\n$b = 'ção'\r\n")},
		{"UTF-16LE LF no final newline", "powershell", bom16("$a = 1\n$b = 2")},
		{"UTF-16LE mark only", "powershell", bom16("")},
		{"UTF-16LE with the units U+0A0D and U+0D0A", "powershell", bom16("# ਍ ഊ\n$a = 1\n")},
		{"UTF-16LE ending in U+0A0A without newline", "powershell", bom16("# x\n# ਊ")},
		{"UTF-16LE with a surrogate pair, CRLF, no final", "powershell", bom16("# \U0001F600\r\n$a = 1")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, target, req := profileSite(t, c.shell, c.original)
			line := req.Hooks[0].Line
			before := testenv.Snapshot(t, s.home.Dir)

			plan := mustApply(t, req)
			want := appended(c.original, line)
			if got := read(t, target); got != string(want) {
				t.Fatalf("after apply:\n got %q\nwant %q\nplan:\n%s", got, want, describe(plan))
			}

			applied := testenv.Snapshot(t, s.home.Dir)
			again := mustApply(t, req)
			if again.Changes() != 0 {
				t.Errorf("the second apply changes something:\n%s", describe(again))
			}
			sameTree(t, applied, s.home.Dir, "second apply")

			removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
			if got := read(t, target); got != string(c.original) {
				t.Errorf("after uninstall:\n got %q\nwant %q\nplan:\n%s", got, c.original, describe(removal))
			}
			if removal.ExitCode() != ExitOK {
				t.Errorf("uninstall: exit %d", removal.ExitCode())
			}
			sameTree(t, before, s.home.Dir, "after uninstall")
		})
	}
}

// TestHookLineAlreadyThere: an identical line that was in the profile before
// sereno is neither added a second time nor removed by uninstall.
func TestHookLineAlreadyThere(t *testing.T) {
	for name, original := range map[string]string{
		"LF":                  "a\n" + bashLine + "\nb\n",
		"CRLF":                "a\r\n" + bashLine + "\r\nb\r\n",
		"last line, no final": "a\n" + bashLine,
		"twice":               bashLine + "\n" + bashLine + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			s, target, req := profileSite(t, "bash", []byte(original))
			before := testenv.Snapshot(t, s.home.Dir)
			plan := mustApply(t, req)
			if got := read(t, target); got != original {
				t.Errorf("an identical line was not a no-op: %q\n%s", got, describe(plan))
			}
			removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
			if got := read(t, target); got != original {
				t.Errorf("uninstall changed a line that was there before sereno: %q\n%s", got, describe(removal))
			}
			sameTree(t, before, s.home.Dir, "after uninstall")
		})
	}
}

// userLinesWithTheMarker are lines of the user's own that mention the marker
// without ending in it. None of them is sereno's line (R21, R22).
var userLinesWithTheMarker = map[string]string{
	"marker in the middle of a command":   `alias findhook='grep -n "# sereno:hook" ~/.bashrc'`,
	"marker in a comment with text after": `# note to self: the line ending in # sereno:hook belongs to sereno, do not edit`,
	"marker in an echo":                   `echo "add # sereno:hook lines by hand"; export IMPORTANT=1`,
	"marker without a space in front":     `export NOTE=x# sereno:hook`,
	"marker glued to a longer word":       `# see docs# sereno:hooks`,
}

// TestMarkerInsideOtherCode: apply must append and leave such a line alone,
// and uninstall must give the file back byte for byte.
func TestMarkerInsideOtherCode(t *testing.T) {
	for name, userLine := range userLinesWithTheMarker {
		t.Run(name, func(t *testing.T) {
			original := "export A=1\n" + userLine + "\nexport B=2\n"
			s, target, req := profileSite(t, "bash", []byte(original))
			before := testenv.Snapshot(t, s.home.Dir)
			plan := mustApply(t, req)
			if got := read(t, target); got != original+bashLine+"\n" {
				t.Errorf("apply did not simply append:\n before: %q\n after:  %q\n%s", original, got, describe(plan))
			}
			for _, op := range plan.Ops {
				if op.Path == target && len(op.Removed) != 0 {
					t.Errorf("the plan announces the removal of %q", op.Removed)
				}
			}
			removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
			if got := read(t, target); got != original {
				t.Errorf("after uninstall: %q\n%s", got, describe(removal))
			}
			sameTree(t, before, s.home.Dir, "after uninstall")
		})
	}
}

// TestMarkerInsideOtherCodeThenUserEdits: the same, but the user changes the
// file before uninstalling, and in a way that takes sereno's recorded bytes
// away, so that uninstall falls back to looking for lines with the marker.
func TestMarkerInsideOtherCodeThenUserEdits(t *testing.T) {
	for name, userLine := range userLinesWithTheMarker {
		t.Run(name, func(t *testing.T) {
			original := "export A=1\n" + userLine + "\nexport B=2\n"
			s, target, req := profileSite(t, "bash", []byte(original))
			mustApply(t, req)

			// More lines at the end: the recorded bytes are still there.
			put(t, target, read(t, target)+"export C=3\n")
			// And the file converted to CRLF: now they are not.
			put(t, target, strings.ReplaceAll(read(t, target), "\n", "\r\n"))

			removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
			want := strings.ReplaceAll(original+"export C=3\n", "\n", "\r\n")
			if got := read(t, target); got != want {
				t.Errorf("a line of the user's was lost:\n got %q\nwant %q\n%s", got, want, describe(removal))
			}
		})
	}
}

// TestUninstallAfterUserChanges covers what users do to a hook line once it
// is in their profile. The recorded bytes are taken out only where they stand
// as whole lines; otherwise every line that ends with the marker goes, as a
// whole line, and nothing else is touched.
func TestUninstallAfterUserChanges(t *testing.T) {
	const base, rest = "export A=1\n", "export B=2\n"
	cases := []struct {
		name   string
		edit   func(applied string) string
		want   string // the profile after uninstall
		backup bool   // whether the backup must stay
	}{
		{"untouched", func(s string) string { return s }, base, false},
		{"lines added after the hook", func(s string) string { return s + rest }, base + rest, true},
		{"lines added before the hook", func(s string) string { return "export Z=0\n" + s }, "export Z=0\n" + base, true},
		{"hook commented out with '# '", func(s string) string {
			return strings.Replace(s, bashLine, "# "+bashLine, 1) + rest
		}, base + rest, true},
		{"hook commented out with '#'", func(s string) string {
			return strings.Replace(s, bashLine, "#"+bashLine, 1) + rest
		}, base + rest, true},
		{"hook indented inside an if block", func(s string) string {
			return strings.Replace(s, bashLine+"\n", "if true; then\n  "+bashLine+"\nfi\n", 1)
		}, base + "if true; then\nfi\n", true},
		{"a command put in front on the same line", func(s string) string {
			return strings.Replace(s, bashLine, "true; "+bashLine, 1) + rest
		}, base + rest, true},
		{"file converted to CRLF", func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }, "export A=1\r\n", true},
		{"hook line moved to the top", func(s string) string { return bashLine + "\n" + base }, base, false},
		// R39 takes out exactly the bytes sereno recorded: the copy the user
		// made is the user's line and stays.
		{"hook line duplicated by the user", func(s string) string { return s + bashLine + "\n" }, base + bashLine + "\n", true},
		{"a commented copy above the real line", func(s string) string {
			return strings.Replace(s, bashLine, "# "+bashLine+"\n"+bashLine, 1)
		}, base + "# " + bashLine + "\n", true},
		{"hook line deleted by the user", func(s string) string { return base }, base, false},
		{"everything deleted but the hook", func(s string) string { return bashLine + "\n" }, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, target, req := profileSite(t, "bash", []byte(base))
			mustApply(t, req)
			applied := read(t, target)
			if applied != base+bashLine+"\n" {
				t.Fatalf("after apply %q", applied)
			}
			if edited := c.edit(applied); edited != applied {
				put(t, target, edited)
			}
			removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
			if got := read(t, target); got != c.want {
				t.Errorf("after uninstall:\n got %q\nwant %q\nplan:\n%s", got, c.want, describe(removal))
			}
			backups, _ := filepath.Glob(filepath.Join(s.root, BackupsDir, "*"))
			if (len(backups) > 0) != c.backup {
				t.Errorf("backup kept = %v, want %v\nplan:\n%s", len(backups) > 0, c.backup, describe(removal))
			}
			if len(backups) > 0 && read(t, backups[0]) != base {
				t.Errorf("the backup is not the profile from before the hook: %q", read(t, backups[0]))
			}
		})
	}
}

// TestUninstallKeepsTheLineBeforeAnUnterminatedHook is the case of a profile
// that had no final newline: sereno added one in front of its line. Once the
// user has written more below, taking the recorded bytes out as they are
// would glue the user's two lines together.
func TestUninstallKeepsTheLineBeforeAnUnterminatedHook(t *testing.T) {
	for name, eol := range map[string]string{"LF": "\n", "CRLF": "\r\n"} {
		t.Run(name, func(t *testing.T) {
			original := "export A=1" + eol + "export B=2"
			s, target, req := profileSite(t, "bash", []byte(original))
			mustApply(t, req)
			put(t, target, read(t, target)+"export C=3"+eol)
			removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
			want := original + eol + "export C=3" + eol
			if got := read(t, target); got != want {
				t.Errorf("after uninstall:\n got %q\nwant %q\nplan:\n%s", got, want, describe(removal))
			}
		})
	}
}

// TestApplyAfterTheUserCommentedTheHookOut: the commented line is a line with
// the marker that differs from the wanted one, so it is replaced in place
// (R22), and the plan shows the line that goes as well as the one that comes.
func TestApplyAfterTheUserCommentedTheHookOut(t *testing.T) {
	const base, rest = "export A=1\n", "export B=2\n"
	s, target, req := profileSite(t, "bash", []byte(base))
	mustApply(t, req)
	put(t, target, base+"# "+bashLine+"\n"+rest)

	plan := mustPlan(t, req)
	var op *Op
	for i := range plan.Ops {
		if plan.Ops[i].Path == target {
			op = &plan.Ops[i]
		}
	}
	if op == nil || op.Verb != VerbAppend || op.Note.Key != NoteHookReplaces ||
		len(op.Removed) != 1 || op.Removed[0] != "# "+bashLine || len(op.Added) != 1 || op.Added[0] != bashLine {
		t.Fatalf("the plan does not show the replacement of the commented line:\n%s", describe(plan))
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, target); got != base+bashLine+"\n"+rest {
		t.Errorf("after the second apply: %q", got)
	}
	mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
	if got := read(t, target); got != base+rest {
		t.Errorf("after uninstall: %q", got)
	}
}

// TestOlderHookLineComesBack: a different sereno line from an earlier
// installation is replaced in place and returns, byte for byte, on uninstall.
func TestOlderHookLineComesBack(t *testing.T) {
	cases := map[string][]byte{
		"middle LF":         []byte("a\n" + oldLine + "\nb\n"),
		"middle CRLF":       []byte("a\r\n" + oldLine + "\r\nb\r\n"),
		"last, no final":    []byte("a\n" + oldLine),
		"with trailing tab": []byte("a\n" + oldLine + "\t\nb\n"),
		"UTF-16LE":          bom16("a\r\n" + oldLine + "\r\nb\r\n"),
	}
	for name, original := range cases {
		t.Run(name, func(t *testing.T) {
			shell := "bash"
			encode := func(text string) []byte { return []byte(text) }
			if name == "UTF-16LE" {
				shell, encode = "powershell", utf16le
			}
			s, target, req := profileSite(t, shell, original)
			line := req.Hooks[0].Line
			before := testenv.Snapshot(t, s.home.Dir)

			plan := mustApply(t, req)
			got := []byte(read(t, target))
			if bytes.Contains(got, encode(oldLine)) || !bytes.Contains(got, encode(line)) {
				t.Errorf("not replaced in place: %q\n%s", got, describe(plan))
			}
			for _, op := range plan.Ops {
				if op.Path == target && (len(op.Removed) != 1 || !strings.HasPrefix(op.Removed[0], oldLine)) {
					t.Errorf("the plan does not show the line that is replaced: %q", op.Removed)
				}
			}
			removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
			if got := read(t, target); got != string(original) {
				t.Errorf("after uninstall %q, want %q\n%s", got, original, describe(removal))
			}
			sameTree(t, before, s.home.Dir, "after uninstall")
		})
	}
}
