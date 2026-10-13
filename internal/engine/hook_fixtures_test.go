package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// This file is test T5: the hook editor against the fixture trees of
// testdata/homes. Every case asserts the exact bytes after apply and byte
// equality with the original after uninstall, and compares the whole home
// tree so that nothing else is left behind.

const (
	bashrc    = ".bashrc"
	psProfile = "Documents/WindowsPowerShell/Microsoft.PowerShell_profile.ps1"
)

// hookCase is one row of T5.
type hookCase struct {
	name    string
	fixture string
	target  string // relative to the home, with slashes
	shell   string
	line    string
	crlf    bool // line ending for a file sereno has to create
	// after returns the expected content of the target from the original.
	after func(original []byte) []byte
	// ops are the expected operations on the hook target and its backup,
	// as "verb note-key".
	ops []string
}

func appendText(text string) func([]byte) []byte {
	return func(original []byte) []byte { return append(append([]byte{}, original...), text...) }
}

var hookCases = []hookCase{
	{
		name: "absent file and absent directory", fixture: "empty", target: psProfile, shell: "powershell", line: psLine, crlf: true,
		after: func([]byte) []byte { return []byte(psLine + "\r\n") },
		ops:   []string{"mkdir ", "mkdir ", "create "},
	},
	{
		name: "absent file in an existing directory", fixture: "empty", target: bashrc, shell: "bash", line: bashLine,
		after: func([]byte) []byte { return []byte(bashLine + "\n") },
		ops:   []string{"create "},
	},
	{
		name: "LF file", fixture: "lf", target: bashrc, shell: "bash", line: bashLine,
		after: appendText(bashLine + "\n"),
		ops:   []string{"create " + NoteBackupOf, "append "},
	},
	{
		name: "CRLF file", fixture: "crlf", target: psProfile, shell: "powershell", line: psLine, crlf: true,
		after: appendText(psLine + "\r\n"),
		ops:   []string{"create " + NoteBackupOf, "append "},
	},
	{
		name: "file without final newline", fixture: "no-final-newline", target: bashrc, shell: "bash", line: bashLine,
		after: appendText("\n" + bashLine + "\n"),
		ops:   []string{"create " + NoteBackupOf, "append "},
	},
	{
		name: "UTF-8 BOM", fixture: "utf8-bom", target: psProfile, shell: "powershell", line: psLine, crlf: true,
		after: appendText(psLine + "\r\n"),
		ops:   []string{"create " + NoteBackupOf, "append "},
	},
	{
		name: "UTF-16LE", fixture: "utf16le", target: psProfile, shell: "powershell", line: psLine, crlf: true,
		after: appendText(string(utf16le(psLine + "\r\n"))),
		ops:   []string{"create " + NoteBackupOf, "append "},
	},
	{
		name: "existing identical hook", fixture: "hook-identical", target: bashrc, shell: "bash", line: bashLine,
		after: func(original []byte) []byte { return original },
		ops:   []string{"keep " + NoteHookPresent},
	},
	{
		name: "existing different hook", fixture: "hook-different", target: bashrc, shell: "bash", line: bashLine,
		after: func(original []byte) []byte { return bytes.Replace(original, []byte(oldLine), []byte(bashLine), 1) },
		ops:   []string{"create " + NoteBackupOf, "append " + NoteHookReplaces},
	},
}

// hookSite prepares a fixture home and the request that asks for the hook.
func hookSite(t *testing.T, c hookCase) (site, string, Request) {
	t.Helper()
	s := newSite(t, c.fixture)
	target := filepath.Join(s.home.Dir, filepath.FromSlash(c.target))
	req := s.request("init v1\n")
	req.Hooks = []Hook{{Shell: c.shell, Path: target, Line: c.line, NewFileCRLF: c.crlf}}
	return s, target, req
}

// record returns the manifest record of a shell's hook.
func record(t *testing.T, s site, shell string) HookRecord {
	t.Helper()
	m, _, err := LoadManifest(s.root)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := m.Hook(shell)
	if !ok {
		t.Fatalf("no hook record for %s", shell)
	}
	return r
}

// hookOps lists the operations that concern the target, its directories and
// its backup, as "verb note-key".
func hookOps(s site, target string, plan *Plan) []string {
	backups := filepath.Join(s.root, BackupsDir)
	var out []string
	for _, op := range plan.Ops {
		inBackups := strings.HasPrefix(op.Path, backups+string(filepath.Separator))
		aboveTarget := op.Verb == VerbMkdir && strings.HasPrefix(target, op.Path+string(filepath.Separator)) && !strings.HasPrefix(s.root, op.Path)
		if op.Path == target || inBackups || aboveTarget {
			out = append(out, string(op.Verb)+" "+op.Note.Key)
		}
	}
	return out
}

func TestHookFixtures(t *testing.T) {
	for _, c := range hookCases {
		t.Run(c.name, func(t *testing.T) {
			s, target, req := hookSite(t, c)
			before := testenv.Snapshot(t, s.home.Dir)
			original, err := os.ReadFile(target)
			existed := err == nil

			// Apply: the exact bytes.
			plan := mustApply(t, req)
			if plan.ExitCode() != ExitOK {
				t.Fatalf("apply: exit %d", plan.ExitCode())
			}
			if got := hookOps(s, target, plan); !reflect.DeepEqual(got, c.ops) {
				t.Errorf("operations on the target = %q, want %q", got, c.ops)
			}
			want := c.after(original)
			if got := read(t, target); got != string(want) {
				t.Fatalf("after apply:\n got %q\nwant %q", got, want)
			}
			for _, op := range plan.Ops {
				if op.Path == target && op.Changes() && !reflect.DeepEqual(op.Added, []string{c.line}) {
					t.Errorf("the plan line does not show the exact text: %q", op.Added)
				}
			}

			// The manifest records what R12 lists.
			m, _, err := LoadManifest(s.root)
			if err != nil {
				t.Fatal(err)
			}
			record, ok := m.Hook(c.shell)
			if !ok || record.Path != target || record.FileCreated == existed {
				t.Fatalf("hook record = %+v (file existed: %v)", record, existed)
			}
			if !reflect.DeepEqual(m.Options.Hooks, []string{c.shell}) {
				t.Errorf("option hooks = %v", m.Options.Hooks)
			}
			if existed && record.BeforeSHA256 != SHA256(original) {
				t.Error("beforeSha256 is not the hash of the original file")
			}
			if !existed && (record.BeforeSHA256 != "" || record.Backup != "") {
				t.Errorf("a created file has before-facts: %+v", record)
			}
			if record.Backup != "" {
				if filepath.Dir(record.Backup) != filepath.Join(s.root, BackupsDir) {
					t.Errorf("the backup is not in ROOT/backups: %s", record.Backup)
				}
				if got := read(t, record.Backup); got != string(original) {
					t.Error("the backup is not a copy of the original file")
				}
			}
			if len(record.Inserted) > 0 && !bytes.Contains(want, record.Inserted) {
				t.Errorf("the recorded bytes %q are not in the file", record.Inserted)
			}

			// A second apply performs nothing.
			afterApply := testenv.Snapshot(t, s.home.Dir)
			again := mustApply(t, req)
			if again.Changes() != 0 {
				t.Errorf("second apply changes something: %v", verbs(s, again))
			}
			sameTree(t, afterApply, s.home.Dir, "second apply")

			// Uninstall: byte equality with the original, for the whole home.
			removal := mustUninstall(t, UninstallRequest{Root: s.root})
			if removal.ExitCode() != ExitOK {
				t.Errorf("uninstall: exit %d, %v", removal.ExitCode(), verbs(s, removal))
			}
			sameTree(t, before, s.home.Dir, "after uninstall")
			if existed {
				if got := read(t, target); got != string(original) {
					t.Errorf("after uninstall the file differs from the original:\n got %q\nwant %q", got, original)
				}
			}
		})
	}
}

// TestHookOnlyUninstall is `sereno uninstall --hook LIST`: step 1 of R39 for
// the named shells, the manifest updated, everything else untouched.
func TestHookOnlyUninstall(t *testing.T) {
	for _, c := range hookCases {
		t.Run(c.name, func(t *testing.T) {
			s, target, req := hookSite(t, c)
			original, err := os.ReadFile(target)
			existed := err == nil
			mustApply(t, req)
			applied := record(t, s, c.shell)

			plan := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0", Shells: []string{c.shell, "zsh"}})
			if plan.ExitCode() != ExitOK {
				t.Errorf("exit %d", plan.ExitCode())
			}
			if existed {
				if got := read(t, target); got != string(original) {
					t.Errorf("the file differs from the original:\n got %q\nwant %q", got, original)
				}
			} else if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Error("the file sereno created is still there")
			}
			m, found, err := LoadManifest(s.root)
			if err != nil || !found {
				t.Fatalf("the manifest must stay: found %v, err %v", found, err)
			}
			if len(m.Hooks) != 0 || len(m.Options.Hooks) != 0 {
				t.Errorf("the hook is still recorded: %+v, options %v", m.Hooks, m.Options.Hooks)
			}
			if len(m.Owned) != 3 || read(t, s.init) != "init v1\n" {
				t.Error("owned files were touched")
			}
			if backups := filepath.Join(s.root, BackupsDir); applied.Backup != "" {
				if left := testenv.Snapshot(t, backups); len(left) != 0 {
					t.Errorf("a backup was left although the file is back to its original: %v", left)
				}
			}

			// The next apply without --hook must not bring the line back.
			again := mustApply(t, s.request("init v1\n"))
			if again.Changes() != 0 {
				t.Errorf("apply after uninstall --hook changes something: %v", verbs(s, again))
			}
		})
	}
}

// TestHookSymlinkIsRefused is the symlink row of T5: exit 3, nothing written,
// and the line is offered for adding by hand.
func TestHookSymlinkIsRefused(t *testing.T) {
	s := newSite(t, "symlink-rc")
	target := filepath.Join(s.home.Dir, bashrc)
	testenv.Symlink(t, filepath.Join("dotfiles", "bashrc"), target)
	before := testenv.Snapshot(t, s.home.Dir)

	req := s.request("init v1\n")
	req.Hooks = []Hook{{Shell: "bash", Path: target, Line: bashLine}}
	plan := mustPlan(t, req)
	if plan.ExitCode() != ExitRefused || len(plan.Refusals) != 1 {
		t.Fatalf("exit %d, refusals %+v", plan.ExitCode(), plan.Refusals)
	}
	refusal := plan.Refusals[0]
	if refusal.Path != target || refusal.Reason.Key != ReasonSymlink || refusal.Advice.Key != AdviceAddByHand || refusal.Text != bashLine {
		t.Errorf("refusal = %+v", refusal)
	}
	if err := plan.Apply(); err == nil {
		t.Error("a refused plan was applied")
	}
	sameTree(t, before, s.home.Dir, "refused hook")
}

// TestHookRefusals covers the other targets R22 refuses. Each must leave the
// home untouched and carry the line.
func TestHookRefusals(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		isDir   bool
		reason  string
	}{
		{"directory", nil, true, ReasonNotRegular},
		{"over 1 MiB", bytes.Repeat([]byte("# filler\n"), MaxProfileSize/9+1), false, ReasonTooLarge},
		{"UTF-16BE", []byte("\xfe\xff\x00#\x00\n"), false, ReasonUTF16BE},
		{"NUL bytes without a mark", []byte("#\x00\n\x00"), false, ReasonBinary},
		{"odd UTF-16LE", []byte("\xff\xfe#\x00\n"), false, ReasonMalformed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newSite(t, "empty")
			target := filepath.Join(s.home.Dir, bashrc)
			if test.isDir {
				if err := os.Mkdir(target, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(target, test.content, 0o644); err != nil {
				t.Fatal(err)
			}
			before := testenv.Snapshot(t, s.home.Dir)
			req := s.request("init v1\n")
			req.Hooks = []Hook{{Shell: "bash", Path: target, Line: bashLine}}
			plan := mustPlan(t, req)
			if plan.ExitCode() != ExitRefused || len(plan.Refusals) != 1 {
				t.Fatalf("exit %d, refusals %+v", plan.ExitCode(), plan.Refusals)
			}
			if r := plan.Refusals[0]; r.Reason.Key != test.reason || r.Text != bashLine || r.Path != target {
				t.Errorf("refusal = %+v", r)
			}
			sameTree(t, before, s.home.Dir, "refused hook")
		})
	}

	t.Run("a file where the profile directory should be", func(t *testing.T) {
		s := newSite(t, "empty")
		put(t, filepath.Join(s.home.Dir, "Documents"), "not a folder\n")
		target := filepath.Join(s.home.Dir, filepath.FromSlash(psProfile))
		req := s.request("init v1\n")
		req.Hooks = []Hook{{Shell: "powershell", Path: target, Line: psLine, NewFileCRLF: true}}
		plan := mustPlan(t, req)
		if len(plan.Refusals) != 1 || plan.Refusals[0].Reason.Key != ReasonParent {
			t.Errorf("refusals = %+v", plan.Refusals)
		}
	})
}

// TestUserEditsAfterTheHook is the last row of T5: uninstall removes only the
// line and keeps the backup, because the file is no longer what it was.
func TestUserEditsAfterTheHook(t *testing.T) {
	c := hookCases[2] // LF file
	s, target, req := hookSite(t, c)
	original := read(t, target)
	mustApply(t, req)
	m, _, _ := LoadManifest(s.root)
	record, _ := m.Hook("bash")

	edited := "# added at the top by the user\n" + read(t, target) + "export PAGER=less\n"
	put(t, target, edited)

	plan := mustUninstall(t, UninstallRequest{Root: s.root})
	if plan.ExitCode() != ExitOK {
		t.Errorf("a kept backup is not an error, got exit %d", plan.ExitCode())
	}
	want := "# added at the top by the user\n" + original + "export PAGER=less\n"
	if got := read(t, target); got != want {
		t.Errorf("after uninstall:\n got %q\nwant %q", got, want)
	}
	if got := read(t, record.Backup); got != original {
		t.Error("the backup was removed or changed")
	}
	var kept, removed bool
	for _, op := range plan.Ops {
		if op.Path == record.Backup && op.Verb == VerbKeep && op.Note.Key == NoteBackupKept && reflect.DeepEqual(op.Note.Args, []any{target}) {
			kept = true
		}
		if op.Path == target && op.Verb == VerbReplace && op.Note.Key == NoteHookRemoved && reflect.DeepEqual(op.Removed, []string{bashLine}) {
			removed = true
		}
	}
	if !kept || !removed {
		t.Errorf("the plan does not report the removed line and the kept backup: %v", verbs(s, plan))
	}
	// ROOT stays because the backup is in it; everything else of sereno is gone.
	for _, gone := range []string{s.init, s.mode, s.outside, filepath.Join(s.root, ManifestName)} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s is still there", gone)
		}
	}
}

// TestHookLifecycles walks the less common paths through the hook editor.
func TestHookLifecycles(t *testing.T) {
	t.Run("the user reformats the line: any line with the marker goes", func(t *testing.T) {
		s, target, req := hookSite(t, hookCases[2])
		original := read(t, target)
		mustApply(t, req)
		put(t, target, original+"    "+bashLine+"   \n")
		mustUninstall(t, UninstallRequest{Root: s.root})
		if got := read(t, target); got != original {
			t.Errorf("after uninstall: %q, want %q", got, original)
		}
	})

	t.Run("the user deletes the line: nothing to remove, backup goes", func(t *testing.T) {
		s, target, req := hookSite(t, hookCases[2])
		before := testenv.Snapshot(t, s.home.Dir)
		original := read(t, target)
		mustApply(t, req)
		put(t, target, original)
		plan := mustUninstall(t, UninstallRequest{Root: s.root})
		for _, op := range plan.Ops {
			if op.Path == target {
				t.Errorf("the file is already clean but the plan has %s for it", op.Verb)
			}
		}
		if len(plan.GoneHooks) != 0 {
			t.Errorf("the file is there, and the plan lists it as gone: %+v", plan.GoneHooks)
		}
		sameTree(t, before, s.home.Dir, "after uninstall")
	})

	t.Run("the user deletes the file sereno edited: backup stays", func(t *testing.T) {
		s, target, req := hookSite(t, hookCases[2])
		mustApply(t, req)
		m, _, _ := LoadManifest(s.root)
		record, _ := m.Hook("bash")
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		// Deleted or moved, planning cannot tell which: the plan notes the
		// hook whose file is gone, for an uninstall of hook lines only and
		// for a whole one, so that the command can say what it met.
		for _, req := range []UninstallRequest{{Root: s.root, Version: "0.1.0", Shells: []string{"bash"}}, {Root: s.root}} {
			planned, err := PlanUninstall(req)
			if err != nil {
				t.Fatal(err)
			}
			if len(planned.GoneHooks) != 1 || planned.GoneHooks[0].Shell != "bash" || planned.GoneHooks[0].Path != target {
				t.Errorf("uninstall of %v: the plan does not note the hook whose file is gone: %+v", req.Shells, planned.GoneHooks)
			}
		}
		plan := mustUninstall(t, UninstallRequest{Root: s.root})
		if plan.ExitCode() != ExitOK || plan.Count(VerbKeep) != 1 {
			t.Errorf("exit %d, %v", plan.ExitCode(), verbs(s, plan))
		}
		// The line of the backup says that the file is no longer there. That
		// it changed after the hook line was added is the note of a file
		// planning has read, and cannot be said of one that is gone.
		for _, op := range plan.Ops {
			if op.Verb == VerbKeep && (op.Path != record.Backup || op.Note.Key != NoteBackupGone || !reflect.DeepEqual(op.Note.Args, []any{target})) {
				t.Errorf("the plan keeps %s with the note %s %v, want %s with %s for %s", op.Path, op.Note.Key, op.Note.Args, record.Backup, NoteBackupGone, target)
			}
		}
		if _, err := os.Stat(record.Backup); err != nil {
			t.Error("the only copy of the user's file was deleted")
		}
	})

	t.Run("the user adds to the file sereno created: file stays without the line", func(t *testing.T) {
		s, target, req := hookSite(t, hookCases[0])
		mustApply(t, req)
		put(t, target, read(t, target)+"Set-Alias ll Get-ChildItem\r\n")
		mustUninstall(t, UninstallRequest{Root: s.root})
		if got := read(t, target); got != "Set-Alias ll Get-ChildItem\r\n" {
			t.Errorf("after uninstall: %q", got)
		}
	})

	t.Run("the profile becomes a link: left alone, exit 4", func(t *testing.T) {
		s, target, req := hookSite(t, hookCases[2])
		mustApply(t, req)
		moved := filepath.Join(s.home.Dir, "moved-bashrc")
		if err := os.Rename(target, moved); err != nil {
			t.Fatal(err)
		}
		testenv.Symlink(t, "moved-bashrc", target)
		content := read(t, moved)
		plan := mustUninstall(t, UninstallRequest{Root: s.root})
		if plan.ExitCode() != ExitIncomplete {
			t.Errorf("exit %d, want %d", plan.ExitCode(), ExitIncomplete)
		}
		if read(t, moved) != content {
			t.Error("the file behind the link was edited")
		}
		var reported bool
		for _, op := range plan.Ops {
			if op.Path == target && op.Verb == VerbSkip && op.Note.Key == ReasonSymlink && reflect.DeepEqual(op.Removed, []string{bashLine}) {
				reported = true
			}
		}
		if !reported {
			t.Errorf("the plan does not say which line to remove by hand: %v", verbs(s, plan))
		}
	})

	t.Run("a hook-only uninstall that cannot edit keeps the record", func(t *testing.T) {
		s, target, req := hookSite(t, hookCases[2])
		mustApply(t, req)
		moved := filepath.Join(s.home.Dir, "moved-bashrc")
		if err := os.Rename(target, moved); err != nil {
			t.Fatal(err)
		}
		testenv.Symlink(t, "moved-bashrc", target)
		plan := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0", Shells: []string{"bash"}})
		if plan.ExitCode() != ExitIncomplete || plan.Changes() != 0 {
			t.Errorf("exit %d, %v", plan.ExitCode(), verbs(s, plan))
		}
		// Once the profile is a file again, the same command finishes the job.
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(moved, target); err != nil {
			t.Fatal(err)
		}
		again := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0", Shells: []string{"bash"}})
		if again.ExitCode() != ExitOK || strings.Contains(read(t, target), HookMarker) {
			t.Errorf("exit %d, file %q", again.ExitCode(), read(t, target))
		}
		m, _, _ := LoadManifest(s.root)
		if len(m.Hooks) != 0 || len(m.Options.Hooks) != 0 {
			t.Errorf("the hook is still recorded: %+v", m)
		}
	})

	t.Run("the line text changes between two applies", func(t *testing.T) {
		s, target, req := hookSite(t, hookCases[4]) // no final newline
		before := testenv.Snapshot(t, s.home.Dir)
		original := read(t, target)
		mustApply(t, req)
		newLine := strings.Replace(bashLine, ".local/share", ".data", 2)
		req.Hooks[0].Line = newLine
		plan := mustApply(t, req)
		if got := read(t, target); got != original+"\n"+newLine+"\n" {
			t.Errorf("after the second apply: %q", got)
		}
		if got := hookOps(s, target, plan); !reflect.DeepEqual(got, []string{"append " + NoteHookReplaces}) {
			t.Errorf("operations = %q (no second backup expected)", got)
		}
		mustUninstall(t, UninstallRequest{Root: s.root})
		sameTree(t, before, s.home.Dir, "after uninstall")
	})

	t.Run("the note for a new file is shown only when the file is created", func(t *testing.T) {
		note := NewNote("plan.note.bash-profile")
		created := func(fixture string) Note {
			s := newSite(t, fixture)
			target := filepath.Join(s.home.Dir, bashrc)
			req := s.request("init v1\n")
			req.Hooks = []Hook{{Shell: "bash", Path: target, Line: bashLine, CreateNote: note}}
			for _, op := range mustPlan(t, req).Ops {
				if op.Path == target {
					return op.Note
				}
			}
			t.Fatal("no operation for the hook target")
			return Note{}
		}
		if got := created("empty"); !reflect.DeepEqual(got, note) {
			t.Errorf("note on a created file = %+v", got)
		}
		if got := created("lf"); !got.IsZero() {
			t.Errorf("note on an existing file = %+v", got)
		}
	})

	t.Run("the hook target moves: refusal", func(t *testing.T) {
		s, _, req := hookSite(t, hookCases[2])
		mustApply(t, req)
		req.Hooks[0].Path = filepath.Join(s.home.Dir, "elsewhere", bashrc)
		plan := mustPlan(t, req)
		if len(plan.Refusals) != 1 || plan.Refusals[0].Reason.Key != ReasonHookMoved {
			t.Errorf("refusals = %+v", plan.Refusals)
		}
	})

	t.Run("recorded hooks stay recorded when an apply does not list them", func(t *testing.T) {
		s, target, req := hookSite(t, hookCases[2])
		mustApply(t, req)
		withHook := read(t, target)
		options := DefaultOptions() // the caller forgot the hook in the options
		plan := mustApply(t, Request{Root: s.root, Version: "0.1.0", Options: &options, Files: s.request("init v1\n").Files})
		m, _, _ := LoadManifest(s.root)
		if _, ok := m.Hook("bash"); !ok || !reflect.DeepEqual(m.Options.Hooks, []string{"bash"}) {
			t.Errorf("the hook record or option was lost: %+v", m)
		}
		if plan.Changes() != 0 || read(t, target) != withHook {
			t.Errorf("the apply changed something: %v", verbs(s, plan))
		}
	})

	t.Run("two shells, one uninstalled", func(t *testing.T) {
		s := newSite(t, "wt-present")
		bash := filepath.Join(s.home.Dir, bashrc)
		ps := filepath.Join(s.home.Dir, filepath.FromSlash(psProfile))
		before := testenv.Snapshot(t, s.home.Dir)
		originalBash, originalPS := read(t, bash), read(t, ps)
		req := s.request("init v1\n")
		req.Hooks = []Hook{
			{Shell: "powershell", Path: ps, Line: psLine, NewFileCRLF: true},
			{Shell: "bash", Path: bash, Line: bashLine},
		}
		mustApply(t, req)
		if read(t, bash) != originalBash+bashLine+"\n" || read(t, ps) != originalPS+psLine+"\r\n" {
			t.Fatal("the two profiles do not carry their lines")
		}
		mustUninstall(t, UninstallRequest{Root: s.root, Shells: []string{"bash"}})
		if read(t, bash) != originalBash || read(t, ps) != originalPS+psLine+"\r\n" {
			t.Error("uninstall --hook bash touched the wrong profile")
		}
		m, _, _ := LoadManifest(s.root)
		if !reflect.DeepEqual(m.Options.Hooks, []string{"powershell"}) || len(m.Hooks) != 1 {
			t.Errorf("manifest after uninstall --hook bash: %+v", m)
		}
		mustUninstall(t, UninstallRequest{Root: s.root})
		sameTree(t, before, s.home.Dir, "after the full uninstall")
	})
}

// TestFixturesHaveTheirBytes guards the fixtures themselves: a checkout that
// converts line endings or encodings would make T5 test something else.
func TestFixturesHaveTheirBytes(t *testing.T) {
	file := func(fixture, target string) []byte {
		data, err := os.ReadFile(filepath.Join(testenv.Fixture(t, fixture), filepath.FromSlash(target)))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if data := file("lf", bashrc); bytes.Contains(data, []byte("\r")) || !bytes.HasSuffix(data, []byte("\n")) {
		t.Error("lf: carriage returns or no final newline")
	}
	if data := file("no-final-newline", bashrc); bytes.Contains(data, []byte("\r")) || bytes.HasSuffix(data, []byte("\n")) {
		t.Error("no-final-newline: carriage returns or a final newline")
	}
	if data := file("crlf", psProfile); bytes.Count(data, []byte("\r\n")) != bytes.Count(data, []byte("\n")) || !bytes.HasSuffix(data, []byte("\r\n")) {
		t.Error("crlf: not every line ends in CRLF")
	}
	if data := file("utf8-bom", psProfile); !bytes.HasPrefix(data, bomUTF8) || !bytes.Contains(data, []byte("\r\n")) || !bytes.Contains(data, []byte("usu\xc3\xa1rio")) {
		t.Error("utf8-bom: no mark, no CRLF or no non-ASCII text")
	}
	if data := file("utf16le", psProfile); !bytes.HasPrefix(data, bomUTF16LE) || len(data)%2 != 0 || !bytes.Contains(data, utf16le("usuário\r\n")) {
		t.Error("utf16le: no mark, odd length or no non-ASCII text")
	}
	if data := file("hook-identical", bashrc); bytes.Count(data, []byte(bashLine+"\n")) != 1 {
		t.Error("hook-identical: the line of the tests is not there exactly once")
	}
	if data := file("hook-different", bashrc); bytes.Count(data, []byte(oldLine+"\n")) != 1 || bytes.Contains(data, []byte(bashLine)) {
		t.Error("hook-different: the older line is not there exactly once")
	}
	if entries, err := os.ReadDir(testenv.Fixture(t, "empty")); err != nil || len(entries) != 1 || entries[0].Name() != ".gitkeep" {
		t.Error("empty: must hold only .gitkeep")
	}
}
