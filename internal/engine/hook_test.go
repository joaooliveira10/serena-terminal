package engine

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// The two hook lines the tests use. The engine takes the line as given; which
// line belongs to which system is tested in internal/target/shells.
const (
	bashLine = `[ -r "$HOME/.local/share/sereno/shell/init.bash" ] && . "$HOME/.local/share/sereno/shell/init.bash" # sereno:hook`
	psLine   = `if ($ExecutionContext.SessionState.LanguageMode -eq 'FullLanguage' -and [System.IO.File]::Exists("$env:LOCALAPPDATA\sereno\shell\init.ps1")) { . "$env:LOCALAPPDATA\sereno\shell\init.ps1" } # sereno:hook`
	oldLine  = `[ -r '/opt/old place/sereno/shell/init.bash' ] && . '/opt/old place/sereno/shell/init.bash' # sereno:hook`
)

// utf16le encodes text as UTF-16 little endian without a byte order mark. It
// is written independently of the code under test.
func utf16le(text string) []byte {
	var out []byte
	for _, unit := range utf16.Encode([]rune(text)) {
		out = binary.LittleEndian.AppendUint16(out, unit)
	}
	return out
}

func existing(data string) profile {
	kind, reason := classify([]byte(data))
	if reason != "" {
		panic("test profile is not editable: " + reason)
	}
	return profile{exists: true, data: []byte(data), perm: FilePerm, kind: kind}
}

// TestEditHook covers the rules of R22 on plain byte strings.
func TestEditHook(t *testing.T) {
	const line = "L # sereno:hook"
	wide := "\xff\xfe" + string(utf16le("a\r\n"))
	tests := []struct {
		name     string
		before   string
		recorded string
		action   hookAction
		after    string
		inserted string
	}{
		{"LF file", "a\nb\n", "", hookAppend, "a\nb\n" + line + "\n", line + "\n"},
		{"CRLF file", "a\r\nb\r\n", "", hookAppend, "a\r\nb\r\n" + line + "\r\n", line + "\r\n"},
		{"one CRLF among LF makes it CRLF", "a\nb\r\nc\n", "", hookAppend, "a\nb\r\nc\n" + line + "\r\n", line + "\r\n"},
		{"no final newline, LF", "a\nb", "", hookAppend, "a\nb\n" + line + "\n", "\n" + line + "\n"},
		{"no final newline, CRLF", "a\r\nb", "", hookAppend, "a\r\nb\r\n" + line + "\r\n", "\r\n" + line + "\r\n"},
		{"single line without newline", "a", "", hookAppend, "a\n" + line + "\n", "\n" + line + "\n"},
		{"empty file", "", "", hookAppend, line + "\n", line + "\n"},
		{"only a UTF-8 mark", "\xef\xbb\xbf", "", hookAppend, "\xef\xbb\xbf" + line + "\n", line + "\n"},
		{"UTF-8 mark is left alone", "\xef\xbb\xbfa\r\n", "", hookAppend, "\xef\xbb\xbfa\r\n" + line + "\r\n", line + "\r\n"},
		{"UTF-16LE", wide, "", hookAppend, wide + string(utf16le(line+"\r\n")), string(utf16le(line + "\r\n"))},
		{"UTF-16LE without final newline", "\xff\xfe" + string(utf16le("a")), "", hookAppend,
			"\xff\xfe" + string(utf16le("a\n"+line+"\n")), string(utf16le("\n" + line + "\n"))},
		{"only a UTF-16LE mark", "\xff\xfe", "", hookAppend, "\xff\xfe" + string(utf16le(line+"\n")), string(utf16le(line + "\n"))},

		{"identical line, not recorded", "a\n" + line + "\nb\n", "", hookKeep, "", ""},
		{"identical line as last line without newline", "a\n" + line, "", hookKeep, "", ""},
		{"identical line in UTF-16LE", "\xff\xfe" + string(utf16le("a\r\n"+line+"\r\n")), "", hookKeep, "", ""},
		{"identical line, recorded", "a\n" + line + "\n", line + "\n", hookKeep, "", line + "\n"},
		{"identical line, recorded bytes gone", "a\n  \n" + line + "\nb\n", "\n" + line + "\n\n", hookKeep, "", "\n" + line + "\n\n"},

		{"different line replaced in place", "a\nold # sereno:hook\nb\n", "", hookReplace, "a\n" + line + "\nb\n", line + "\n"},
		{"different line keeps its CRLF", "a\r\nold # sereno:hook\r\nb\r\n", "", hookReplace, "a\r\n" + line + "\r\nb\r\n", line + "\r\n"},
		{"different last line without newline", "a\nold # sereno:hook", "", hookReplace, "a\n" + line, line},
		{"indented line differs", "a\n  " + line + "\n", "", hookReplace, "a\n" + line + "\n", line + "\n"},
		{"only the first of two different lines", "x # sereno:hook\ny # sereno:hook\n", "", hookReplace, line + "\ny # sereno:hook\n", line + "\n"},
		{"an identical line later wins over a different one", "x # sereno:hook\n" + line + "\n", "", hookKeep, "", ""},
		{"different line in UTF-16LE", "\xff\xfe" + string(utf16le("a\r\nold # sereno:hook\r\n")), "", hookReplace,
			"\xff\xfe" + string(utf16le("a\r\n"+line+"\r\n")), string(utf16le(line + "\r\n"))},

		// A line sereno recorded earlier is replaced together with the line
		// ending it had to add in front, so removal stays exact.
		{"recorded line changes", "a\nold # sereno:hook\n", "\nold # sereno:hook\n", hookReplace, "a\n" + line + "\n", "\n" + line + "\n"},
		{"recorded line changes, user text after it", "a\nold # sereno:hook\nuser\n", "old # sereno:hook\n", hookReplace, "a\n" + line + "\nuser\n", line + "\n"},
		{"recorded CRLF line changes", "a\r\nold # sereno:hook\r\n", "old # sereno:hook\r\n", hookReplace, "a\r\n" + line + "\r\n", line + "\r\n"},

		// The recorded bytes count only where they stand as whole lines.
		// Inside a line they are part of something the user wrote, and that
		// line, which still ends with the marker, is replaced as a whole.
		{"recorded line commented out", "a\n# " + line + "\nb\n", line + "\n", hookReplace, "a\n" + line + "\nb\n", line + "\n"},
		{"recorded line behind a command", "a\ntrue; " + line + "\nb\n", line + "\n", hookReplace, "a\n" + line + "\nb\n", line + "\n"},
		{"recorded line commented out in UTF-16LE", "\xff\xfe" + string(utf16le("a\r\n# "+line+"\r\nb\r\n")), string(utf16le(line + "\r\n")), hookReplace,
			"\xff\xfe" + string(utf16le("a\r\n"+line+"\r\nb\r\n")), string(utf16le(line + "\r\n"))},
		{"recorded bytes found later on a line of their own", "# " + line + "\n" + line + "\n", line + "\n", hookKeep, "", line + "\n"},
		{"leading line ending recorded, text below: only the line changes", "a\nold # sereno:hook\nuser\n", "\nold # sereno:hook\n", hookReplace,
			"a\n" + line + "\nuser\n", line + "\n"},
		{"leading line ending recorded, text below, same line", "a\n" + line + "\nuser\n", "\n" + line + "\n", hookKeep, "", "\n" + line + "\n"},

		// Only a line that ends with the marker is sereno's.
		{"marker inside a command", "alias h='grep \"# sereno:hook\" f'\n", "", hookAppend, "alias h='grep \"# sereno:hook\" f'\n" + line + "\n", line + "\n"},
		{"marker followed by more text", "# the # sereno:hook line is sereno's\n", "", hookAppend, "# the # sereno:hook line is sereno's\n" + line + "\n", line + "\n"},
		{"marker without the space in front", "x# sereno:hook\n", "", hookAppend, "x# sereno:hook\n" + line + "\n", line + "\n"},
		{"the marker alone", "# sereno:hook\n", "", hookAppend, "# sereno:hook\n" + line + "\n", line + "\n"},
		{"blanks after the marker", "a\nold # sereno:hook \t\nb\n", "", hookReplace, "a\n" + line + "\nb\n", line + "\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			edit := editHook(existing(test.before), []byte(test.recorded), line, false)
			if edit.action != test.action {
				t.Fatalf("action = %d, want %d", edit.action, test.action)
			}
			if test.action != hookKeep && string(edit.after) != test.after {
				t.Errorf("after = %q\n want %q", edit.after, test.after)
			}
			if string(edit.inserted) != test.inserted {
				t.Errorf("inserted = %q\n    want %q", edit.inserted, test.inserted)
			}
		})
	}
}

// TestEditHookNamesTheLineItReplaces: the plan must be able to show the line
// that goes, whichever of the two ways it was found.
func TestEditHookNamesTheLineItReplaces(t *testing.T) {
	const line = "L # sereno:hook"
	tests := []struct {
		name, before, recorded, replaced string
	}{
		{"by its recorded bytes", "a\nold # sereno:hook\n", "\nold # sereno:hook\n", "old # sereno:hook"},
		{"by the marker", "a\n  old # sereno:hook \nb\n", "", "  old # sereno:hook "},
		{"commented out", "a\n# " + line + "\n", line + "\n", "# " + line},
		{"in UTF-16LE", "\xff\xfe" + string(utf16le("old # sereno:hook\r\n")), "", "old # sereno:hook"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			edit := editHook(existing(test.before), []byte(test.recorded), line, false)
			if edit.action != hookReplace || edit.replaced != test.replaced {
				t.Errorf("action %d, replaced %q; want a replacement of %q", edit.action, edit.replaced, test.replaced)
			}
		})
	}
	if edit := editHook(existing("a\n"), nil, line, false); edit.replaced != "" {
		t.Errorf("an append replaces %q", edit.replaced)
	}
}

func TestEditHookCreatesAbsentFiles(t *testing.T) {
	for _, crlf := range []bool{false, true} {
		eol := "\n"
		if crlf {
			eol = "\r\n"
		}
		edit := editHook(profile{}, nil, psLine, crlf)
		if edit.action != hookCreate || string(edit.after) != psLine+eol || string(edit.inserted) != psLine+eol {
			t.Errorf("crlf=%v: action %d, after %q, inserted %q", crlf, edit.action, edit.after, edit.inserted)
		}
	}
}

// TestEditThenRemoveIsExact is the heart of R39: for every way the line gets
// in, taking it out gives back the bytes from before.
func TestEditThenRemoveIsExact(t *testing.T) {
	const line = "L # sereno:hook"
	originals := map[string]string{
		"LF":                  "a\nb\n",
		"CRLF":                "a\r\nb\r\n",
		"no final newline":    "a\nb",
		"CRLF no final":       "a\r\nb",
		"empty":               "",
		"UTF-8 mark":          "\xef\xbb\xbfa\r\n",
		"UTF-16LE":            "\xff\xfe" + string(utf16le("a\r\nb\r\n")),
		"UTF-16LE no final":   "\xff\xfe" + string(utf16le("a\r\nb")),
		"older line":          "a\nold # sereno:hook\nb\n",
		"older line at end":   "a\nold # sereno:hook",
		"older line UTF-16LE": "\xff\xfe" + string(utf16le("a\r\nold # sereno:hook\r\nb\r\n")),
		"marker in a comment": "# see # sereno:hook below\nb\n",
	}
	for name, original := range originals {
		t.Run(name, func(t *testing.T) {
			before := existing(original)
			edit := editHook(before, nil, line, false)
			if edit.action == hookKeep {
				t.Fatal("the line was reported as present")
			}
			edited := existing(string(edit.after))

			// A second apply finds its own bytes and changes nothing.
			if again := editHook(edited, edit.inserted, line, false); again.action != hookKeep {
				t.Errorf("second edit has action %d, want keep", again.action)
			}
			removal := removeHook(edited, edit.inserted, []byte(original), true)
			if !bytes.Equal(removal.after, []byte(original)) {
				t.Errorf("after removal = %q\n         want %q", removal.after, original)
			}
			if len(removal.removed) != 1 || removal.removed[0] != line {
				t.Errorf("removed = %q, want the line", removal.removed)
			}
		})
	}
}

func TestRemoveHook(t *testing.T) {
	const line = "L # sereno:hook"
	tests := []struct {
		name     string
		current  string
		recorded string
		backup   string // "-" means no usable backup
		after    string
		removed  []string
		restored bool
	}{
		{"exact bytes", "a\n" + line + "\nb\n", line + "\n", "-", "a\nb\n", []string{line}, false},
		{"exact bytes with the leading line ending", "a\n" + line + "\n", "\n" + line + "\n", "-", "a", []string{line}, false},
		{"user added text after the hook", "a\n" + line + "\nuser\n", line + "\n", "a\n", "a\nuser\n", []string{line}, false},
		{"first of two identical occurrences only", line + "\n" + line + "\n", line + "\n", "-", line + "\n", []string{line}, false},
		{"bytes gone: any line with the marker", "a\n  " + line + "\nb\nx # sereno:hook", "\n" + line + "\n", "-", "a\nb\n", []string{"  " + line, "x # sereno:hook"}, false},
		{"bytes gone and no marker: nothing to do", "a\nb\n", line + "\n", "-", "a\nb\n", nil, false},
		{"nothing was inserted", "a\n" + line + "\n", "", "-", "a\n" + line + "\n", nil, false},
		{"older line comes back from the backup", "a\n" + line + "\nb\n", line + "\n", "a\nold # sereno:hook\nb\n", "a\nold # sereno:hook\nb\n", []string{line}, true},
		{"backup differs elsewhere: only the bytes go", "a\n" + line + "\nUSER\n", line + "\n", "a\nold # sereno:hook\nb\n", "a\nUSER\n", []string{line}, false},
		{"backup holds more than one line there", "a\n" + line + "\nb\n", line + "\n", "a\nx\nold # sereno:hook\nb\n", "a\nb\n", []string{line}, false},
		{"backup holds a line without the marker there", "a\n" + line + "\nb\n", line + "\n", "a\nplain\nb\n", "a\nb\n", []string{line}, false},

		// The recorded bytes inside a line are not cut out of it: the line,
		// which ends with the marker, goes as a whole.
		{"commented out", "a\n# " + line + "\nb\n", line + "\n", "-", "a\nb\n", []string{"# " + line}, false},
		{"glued to a comment sign", "a\n#" + line + "\nb\n", line + "\n", "-", "a\nb\n", []string{"#" + line}, false},
		{"behind a command", "a\ntrue; " + line + "\nb\n", line + "\n", "-", "a\nb\n", []string{"true; " + line}, false},
		{"indented in a block", "if x; then\n  " + line + "\nfi\n", line + "\n", "-", "if x; then\nfi\n", []string{"  " + line}, false},
		{"commented out in UTF-16LE", "\xff\xfe" + string(utf16le("a\r\n# "+line+"\r\nb\r\n")), string(utf16le(line + "\r\n")), "-",
			"\xff\xfe" + string(utf16le("a\r\nb\r\n")), []string{"# " + line}, false},
		{"a later occurrence on a line of its own", "# " + line + "\n" + line + "\nb\n", line + "\n", "-", "# " + line + "\nb\n", []string{line}, false},
		{"commented out, backup at hand: no restore", "a\n# " + line + "\nb\n", line + "\n", "a\nold # sereno:hook\nb\n", "a\nb\n", []string{"# " + line}, false},

		// A line ending sereno added in front stays with the line above once
		// there is text below; an unterminated insertion takes its line along.
		{"leading line ending, text below", "a\n" + line + "\nuser\n", "\n" + line + "\n", "-", "a\nuser\n", []string{line}, false},
		{"leading CRLF, text below", "a\r\n" + line + "\r\nuser\r\n", "\r\n" + line + "\r\n", "-", "a\r\nuser\r\n", []string{line}, false},
		{"leading line ending after a lone CR", "a\nb\r\n" + line + "\n", "\n" + line + "\n", "-", "a\nb\r", []string{line}, false},
		{"unterminated insertion at the end", "a\n" + line, line, "-", "a\n", []string{line}, false},
		{"unterminated insertion, text below", "a\n" + line + "\nuser\n", line, "-", "a\nuser\n", []string{line}, false},

		// Lines of the user's own that mention the marker stay.
		{"marker inside a command", "alias h='grep \"# sereno:hook\" f'\nb\n", line + "\n", "-", "alias h='grep \"# sereno:hook\" f'\nb\n", nil, false},
		{"marker followed by more text", "# the # sereno:hook line\n  " + line + "\n", line + "\n", "-", "# the # sereno:hook line\n", []string{"  " + line}, false},
		{"recorded bytes that are not an insertion", "a\nplain text\nb\n", "plain text\n", "-", "a\nplain text\nb\n", nil, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var backup []byte
			have := test.backup != "-"
			if have {
				backup = []byte(test.backup)
			}
			removal := removeHook(existing(test.current), []byte(test.recorded), backup, have)
			if string(removal.after) != test.after {
				t.Errorf("after = %q\n want %q", removal.after, test.after)
			}
			if strings.Join(removal.removed, "|") != strings.Join(test.removed, "|") {
				t.Errorf("removed = %q, want %q", removal.removed, test.removed)
			}
			if removal.restored != test.restored {
				t.Errorf("restored = %v, want %v", removal.restored, test.restored)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name   string
		data   string
		kind   textKind
		reason string
	}{
		{"empty", "", textBytes, ""},
		{"ASCII", "a\n", textBytes, ""},
		{"UTF-8 with mark", "\xef\xbb\xbfol\xc3\xa1\n", textBytes, ""},
		{"ANSI bytes", "ol\xe1\n", textBytes, ""},
		{"UTF-16LE", "\xff\xfea\x00\n\x00", textUTF16LE, ""},
		{"UTF-16LE mark only", "\xff\xfe", textUTF16LE, ""},
		{"UTF-16BE", "\xfe\xff\x00a\x00\n", 0, ReasonUTF16BE},
		{"UTF-16LE with an odd length", "\xff\xfea\x00\n", 0, ReasonMalformed},
		{"UTF-32LE", "\xff\xfe\x00\x00a\x00\x00\x00", 0, ReasonMalformed},
		{"NUL without a mark", "a\x00b\n", 0, ReasonBinary},
		{"UTF-16LE without a mark", "a\x00\n\x00", 0, ReasonBinary},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kind, reason := classify([]byte(test.data))
			if reason != test.reason || (reason == "" && kind != test.kind) {
				t.Errorf("classify = %d, %q; want %d, %q", kind, reason, test.kind, test.reason)
			}
		})
	}
}

// TestReadProfileRefusals creates each kind of target R22 refuses.
func TestReadProfileRefusals(t *testing.T) {
	home := testenv.New(t)
	path := func(name string) string { return filepath.Join(home.Dir, name) }
	write := func(name string, data []byte) string {
		if err := os.WriteFile(path(name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		return path(name)
	}

	if p, reason := readProfile(path("absent"), MaxProfileSize); p.exists || !reason.IsZero() {
		t.Errorf("an absent file: exists %v, reason %q", p.exists, reason.Key)
	}
	if p, reason := readProfile(write("ok", []byte("a\n")), MaxProfileSize); !p.exists || !reason.IsZero() || string(p.data) != "a\n" || p.readOnly {
		t.Errorf("a regular file: %+v, reason %q", p, reason.Key)
	}

	dir := path("directory")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	limit := bytes.Repeat([]byte("#"), MaxProfileSize)
	tests := map[string]struct {
		path   string
		reason string
	}{
		"directory":           {dir, ReasonNotRegular},
		"over 1 MiB":          {write("large", append(limit, '\n')), ReasonTooLarge},
		"UTF-16BE":            {write("be", []byte("\xfe\xff\x00a")), ReasonUTF16BE},
		"NUL bytes":           {write("binary", []byte("a\x00b")), ReasonBinary},
		"odd UTF-16LE":        {write("odd", []byte("\xff\xfea")), ReasonMalformed},
		"below a file":        {filepath.Join(path("ok"), "child"), ""},
		"exactly 1 MiB is ok": {write("limit", limit), ""},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, reason := readProfile(test.path, MaxProfileSize)
			if reason.Key != test.reason {
				t.Errorf("reason = %q, want %q", reason.Key, test.reason)
			}
		})
	}

	t.Run("uninstall reads a profile of any size", func(t *testing.T) {
		p, reason := readProfile(path("large"), anySize)
		if !reason.IsZero() || !p.exists || len(p.data) != MaxProfileSize+1 {
			t.Errorf("exists %v, %d bytes, reason %q", p.exists, len(p.data), reason.Key)
		}
	})
	t.Run("a read-only file is read and marked", func(t *testing.T) {
		locked := write("locked", []byte("a\n"))
		if err := os.Chmod(locked, 0o444); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(locked, 0o644)
		p, reason := readProfile(locked, MaxProfileSize)
		if !reason.IsZero() || !p.exists || !p.readOnly || string(p.data) != "a\n" {
			t.Errorf("%+v, reason %q", p, reason.Key)
		}
	})

	t.Run("symbolic link", func(t *testing.T) {
		link := path("link")
		testenv.Symlink(t, path("ok"), link)
		if _, reason := readProfile(link, MaxProfileSize); reason.Key != ReasonSymlink {
			t.Errorf("reason = %q, want %q", reason.Key, ReasonSymlink)
		}
	})
	t.Run("dangling symbolic link", func(t *testing.T) {
		link := path("dangling")
		testenv.Symlink(t, path("nowhere"), link)
		if _, reason := readProfile(link, MaxProfileSize); reason.Key != ReasonSymlink {
			t.Errorf("reason = %q, want %q", reason.Key, ReasonSymlink)
		}
	})
}

func TestCheckHookLine(t *testing.T) {
	for _, ok := range []string{bashLine, psLine, oldLine, "x # sereno:hook"} {
		if err := checkHookLine(ok); err != nil {
			t.Errorf("checkHookLine(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{
		"", "# sereno:hook", "x", "x # sereno:hook ", "x #sereno:hook", "x\ny # sereno:hook",
		"x\t# sereno:hook", "olá # sereno:hook", "x # sereno:hook\n",
	} {
		if err := checkHookLine(bad); err == nil {
			t.Errorf("checkHookLine(%q) succeeded", bad)
		}
	}
}
