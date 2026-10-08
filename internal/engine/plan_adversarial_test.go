package engine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// More cases added by an independent review of the engine: profiles at the
// size limit, read-only profiles, the permission bits of backups, ROOT spelled
// in another letter case, and the bytes of a manifest that holds everything.

// filler returns exactly n bytes of comment lines that end with a newline.
func filler(n int) []byte {
	const line = "# 0123456789abcdef0123456789abc\n"
	out := bytes.Repeat([]byte(line), n/len(line)+1)[:n]
	out[n-1] = '\n'
	return out
}

// TestHookLineMustFitTheSizeLimit: sereno edits profiles of up to 1 MiB (R22)
// and must never produce one it would refuse to read at the next run.
func TestHookLineMustFitTheSizeLimit(t *testing.T) {
	added := len(bashLine) + 1

	t.Run("a profile the line would take over the limit is refused", func(t *testing.T) {
		for _, size := range []int{MaxProfileSize, MaxProfileSize - added + 1} {
			s, target, req := profileSite(t, "bash", filler(size))
			before := testenv.Snapshot(t, s.home.Dir)
			plan := mustPlan(t, req)
			if plan.ExitCode() != ExitRefused || len(plan.Refusals) != 1 {
				t.Fatalf("%d bytes: exit %d\n%s", size, plan.ExitCode(), describe(plan))
			}
			if r := plan.Refusals[0]; r.Path != target || r.Reason.Key != ReasonNoRoom || r.Advice.Key != AdviceAddByHand || r.Text != bashLine {
				t.Errorf("%d bytes: refusal = %+v", size, r)
			}
			sameTree(t, before, s.home.Dir, "refused hook")
		}
	})

	t.Run("a profile that just fits goes through every step", func(t *testing.T) {
		original := filler(MaxProfileSize - added)
		s, target, req := profileSite(t, "bash", original)
		before := testenv.Snapshot(t, s.home.Dir)
		mustApply(t, req)
		if size := len(read(t, target)); size != MaxProfileSize {
			t.Fatalf("the profile is %d bytes after apply, want exactly %d", size, MaxProfileSize)
		}
		again := mustApply(t, req)
		if again.Refused() || again.Changes() != 0 {
			t.Errorf("the second apply:\n%s", describe(again))
		}
		removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
		if removal.ExitCode() != ExitOK {
			t.Errorf("uninstall: exit %d\n%s", removal.ExitCode(), describe(removal))
		}
		sameTree(t, before, s.home.Dir, "after uninstall")
	})

	t.Run("a full profile that already has the line is kept", func(t *testing.T) {
		original := append(filler(MaxProfileSize-added), bashLine+"\n"...)
		_, target, req := profileSite(t, "bash", original)
		plan := mustApply(t, req)
		if plan.Refused() || read(t, target) != string(original) {
			t.Errorf("the plan:\n%s", describe(plan))
		}
	})

	t.Run("uninstall takes the line out however large the profile has grown", func(t *testing.T) {
		s, target, req := profileSite(t, "bash", []byte("export A=1\n"))
		mustApply(t, req)
		grown := read(t, target) + string(filler(MaxProfileSize+4096))
		put(t, target, grown)

		// Apply does not edit a profile of that size any more...
		if plan := mustPlan(t, req); len(plan.Refusals) != 1 || plan.Refusals[0].Reason.Key != ReasonTooLarge {
			t.Errorf("apply on the grown profile:\n%s", describe(plan))
		}
		// ...but its own line still comes out.
		removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
		if removal.ExitCode() != ExitOK {
			t.Errorf("uninstall: exit %d\n%s", removal.ExitCode(), describe(removal))
		}
		if got := read(t, target); got != strings.Replace(grown, bashLine+"\n", "", 1) {
			t.Errorf("after uninstall the profile is %d bytes and holds the marker: %v", len(got), strings.Contains(got, HookMarker))
		}
	})
}

// TestReadOnlyProfile: a profile the user marked read-only is not edited. On
// Windows the write would fail half way through phase 2; on macOS and Linux
// it would silently succeed, because a rename does not ask the file.
func TestReadOnlyProfile(t *testing.T) {
	const original = "export A=1\n"
	lock := func(t *testing.T, path string) {
		t.Helper()
		if err := os.Chmod(path, 0o444); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(path, 0o644) })
	}

	t.Run("apply refuses before anything is written", func(t *testing.T) {
		s, target, req := profileSite(t, "bash", []byte(original))
		lock(t, target)
		before := testenv.Snapshot(t, s.home.Dir)
		start := time.Now()
		plan := mustPlan(t, req)
		if plan.ExitCode() != ExitRefused || len(plan.Refusals) != 1 {
			t.Fatalf("exit %d\n%s", plan.ExitCode(), describe(plan))
		}
		if r := plan.Refusals[0]; r.Path != target || r.Reason.Key != ReasonReadOnly || r.Advice.Key != AdviceAddByHand || r.Text != bashLine {
			t.Errorf("refusal = %+v", r)
		}
		if err := plan.Apply(); err != ErrRefused {
			t.Errorf("Apply = %v", err)
		}
		if elapsed := time.Since(start); elapsed > patience/2 {
			t.Errorf("the refusal took %s: something waited for the file", elapsed)
		}
		sameTree(t, before, s.home.Dir, "refused hook")
	})

	t.Run("a read-only profile that has the line is fine", func(t *testing.T) {
		s, target, req := profileSite(t, "bash", []byte(original+bashLine+"\n"))
		lock(t, target)
		plan := mustApply(t, req)
		if plan.ExitCode() != ExitOK || read(t, target) != original+bashLine+"\n" {
			t.Errorf("exit %d\n%s", plan.ExitCode(), describe(plan))
		}
		removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
		if removal.ExitCode() != ExitOK || read(t, target) != original+bashLine+"\n" {
			t.Errorf("uninstall: exit %d\n%s", removal.ExitCode(), describe(removal))
		}
	})

	t.Run("uninstall leaves a profile that became read-only and says which line", func(t *testing.T) {
		s, target, req := profileSite(t, "bash", []byte(original))
		mustApply(t, req)
		lock(t, target)
		removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
		if removal.ExitCode() != ExitIncomplete {
			t.Errorf("exit %d, want %d\n%s", removal.ExitCode(), ExitIncomplete, describe(removal))
		}
		var reported bool
		for _, op := range removal.Ops {
			if op.Path == target && op.Verb == VerbSkip && op.Incomplete && op.Note.Key == ReasonReadOnly && reflect.DeepEqual(op.Removed, []string{bashLine}) {
				reported = true
			}
		}
		if !reported {
			t.Errorf("the plan does not name the line to remove by hand:\n%s", describe(removal))
		}
		if read(t, target) != original+bashLine+"\n" {
			t.Errorf("the read-only profile was edited: %q", read(t, target))
		}
		if info, err := os.Stat(target); err != nil || info.Mode().Perm()&0o200 != 0 {
			t.Errorf("the profile is no longer read-only: %v, %v", info.Mode(), err)
		}
		if backups, _ := filepath.Glob(filepath.Join(s.root, BackupsDir, "*")); len(backups) != 1 {
			t.Errorf("the backup must stay with a profile that was not restored: %v", backups)
		}
	})

	t.Run("a hook-only uninstall waits until the profile can be written", func(t *testing.T) {
		s, target, req := profileSite(t, "bash", []byte(original))
		before := testenv.Snapshot(t, s.home.Dir)
		mustApply(t, req)
		lock(t, target)
		first := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0", Shells: []string{"bash"}})
		if first.ExitCode() != ExitIncomplete || first.Changes() != 0 {
			t.Errorf("exit %d\n%s", first.ExitCode(), describe(first))
		}
		if m, _, _ := LoadManifest(s.root); len(m.Hooks) != 1 {
			t.Errorf("the record was dropped although the line is still there: %+v", m.Hooks)
		}
		if err := os.Chmod(target, 0o644); err != nil {
			t.Fatal(err)
		}
		second := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0", Shells: []string{"bash"}})
		if second.ExitCode() != ExitOK || read(t, target) != original {
			t.Errorf("exit %d, profile %q\n%s", second.ExitCode(), read(t, target), describe(second))
		}
		mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
		sameTree(t, before, s.home.Dir, "after the full uninstall")
	})
}

// TestBackupIsNoMoreOpenThanTheProfile: the backup of a profile holds the
// same text as the profile, so it gets the same permission bits, and never
// more than an ordinary file. The first part reads the planned operation and
// runs everywhere; the second looks at real files where modes exist.
func TestBackupIsNoMoreOpenThanTheProfile(t *testing.T) {
	for profilePerm, want := range map[os.FileMode]os.FileMode{0o600: 0o600, 0o640: 0o640, 0o644: 0o644, 0o666: 0o644, 0o700: 0o600, 0o755: 0o644} {
		s := newSite(t, "")
		b := &builder{root: s.root, old: NewManifest(), next: NewManifest(), planned: map[string]bool{}}
		record := HookRecord{Shell: "bash", Path: filepath.Join(s.home.Dir, bashrc)}
		if !b.backup(&record, profile{exists: true, data: []byte("export TOKEN=1\n"), perm: profilePerm}, "") || len(b.backups) != 1 {
			t.Fatalf("no backup planned: %+v", b.refusals)
		}
		if got := b.backups[0].act.perm; got != want {
			t.Errorf("a %v profile gets a %v backup, want %v", profilePerm, got, want)
		}
	}

	if runtime.GOOS == "windows" {
		return // permission bits other than read-only do not exist here
	}
	s, target, req := profileSite(t, "bash", []byte("export TOKEN=1\n"))
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatal(err)
	}
	mustApply(t, req)
	mode := func(path string) os.FileMode {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode().Perm()
	}
	if mode(target) != 0o600 {
		t.Errorf("the profile is %v after apply, want 0600", mode(target))
	}
	backups, _ := filepath.Glob(filepath.Join(s.root, BackupsDir, "*"))
	if len(backups) != 1 {
		t.Fatalf("backups: %v", backups)
	}
	if mode(backups[0]) != 0o600 {
		t.Errorf("the backup of a 0600 profile is %v", mode(backups[0]))
	}
	mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
	if mode(target) != 0o600 {
		t.Errorf("the profile is %v after uninstall, want 0600", mode(target))
	}
}

// TestRootInAnotherLetterCase: on Windows the same folder may be named in
// another case from one run to the next (SERENO_HOME typed by hand, for
// one). It is the same installation: nothing is foreign, nothing is written
// twice, and uninstall finds everything.
func TestRootInAnotherLetterCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("file names are case-sensitive here")
	}
	s := newSite(t, "lf")
	before := testenv.Snapshot(t, s.home.Dir)
	rc := filepath.Join(s.home.Dir, bashrc)
	first := s.request("init v1\n")
	first.Hooks = []Hook{{Shell: "bash", Path: rc, Line: bashLine}}
	mustApply(t, first)
	manifest := read(t, filepath.Join(s.root, ManifestName))
	applied := testenv.Snapshot(t, s.home.Dir)

	upper := func(path string) string {
		rel, err := filepath.Rel(s.home.Dir, path)
		if err != nil {
			t.Fatal(err)
		}
		return filepath.Join(s.home.Dir, strings.ToUpper(rel))
	}
	shouted := Request{Root: upper(s.root), Version: "0.1.0",
		Files: []File{
			{Path: upper(s.init), Data: []byte("init v1\n")},
			{Path: upper(s.mode), Data: []byte("mode=auto\n")},
			{Path: upper(s.outside), Data: []byte("{}\n")},
		},
		Hooks: []Hook{{Shell: "bash", Path: upper(rc), Line: bashLine}},
	}

	same := mustApply(t, shouted)
	if same.Changes() != 0 || same.ExitCode() != ExitOK {
		t.Errorf("the same request in upper case: exit %d\n%s", same.ExitCode(), describe(same))
	}
	if read(t, filepath.Join(s.root, ManifestName)) != manifest {
		t.Error("the manifest changed")
	}
	sameTree(t, applied, s.home.Dir, "apply in upper case")

	// New content and one more file, still through the upper-case spelling.
	shouted.Files[0].Data = []byte("init v2\n")
	shouted.Files = append(shouted.Files, File{Path: filepath.Join(upper(s.root), "THEMES", "A.CONF"), Data: []byte("a\n")})
	changed := mustApply(t, shouted)
	if changed.ExitCode() != ExitOK || read(t, s.init) != "init v2\n" {
		t.Errorf("exit %d\n%s", changed.ExitCode(), describe(changed))
	}
	m := mustBeTrue(t, s)
	keys := map[string]bool{}
	for path := range m.Owned {
		keys[strings.ToLower(path)] = true
	}
	if len(m.Owned) != 4 || len(keys) != 4 {
		t.Errorf("owned holds the same file under two spellings: %v", m.Owned)
	}

	// A lower-case run after that is still the same installation.
	lower := s.request("init v2\n")
	lower.Hooks = first.Hooks
	if plan := mustApply(t, lower); plan.Changes() != 0 {
		t.Errorf("back in the first spelling:\n%s", describe(plan))
	}

	removal := mustUninstall(t, UninstallRequest{Root: upper(s.root), Version: "0.1.0"})
	if removal.ExitCode() != ExitOK {
		t.Errorf("uninstall: exit %d\n%s", removal.ExitCode(), describe(removal))
	}
	sameTree(t, before, s.home.Dir, "after uninstall")
}

// TestHookPathInAnotherLetterCase: the same for the profile of a recorded
// hook, which must not look as if it had moved.
func TestHookPathInAnotherLetterCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("file names are case-sensitive here")
	}
	s, target, req := profileSite(t, "bash", []byte("export A=1\n"))
	mustApply(t, req)
	req.Hooks[0].Path = filepath.Join(strings.ToUpper(filepath.Dir(target)), strings.ToUpper(filepath.Base(target)))
	plan := mustApply(t, req)
	if plan.Refused() || plan.Changes() != 0 {
		t.Errorf("the hook in upper case:\n%s", describe(plan))
	}
	if record, _ := mustBeTrue(t, s).Hook("bash"); record.Path != target {
		t.Errorf("the record now names %s", record.Path)
	}
}

// TestCorruptManifest: a manifest that cannot be parsed stops apply and
// uninstall with an error. Guessing would mean deleting or overwriting files
// on the word of a damaged list.
func TestCorruptManifest(t *testing.T) {
	s := newSite(t, "")
	mustApply(t, s.request("init v1\n"))
	put(t, filepath.Join(s.root, ManifestName), "{ not json")
	before := testenv.Snapshot(t, s.home.Dir)
	if _, err := PlanApply(s.request("init v1\n")); err == nil {
		t.Error("apply accepted a corrupt manifest")
	}
	if _, err := PlanUninstall(UninstallRequest{Root: s.root, Version: "0.1.0"}); err == nil {
		t.Error("uninstall accepted a corrupt manifest")
	}
	if _, err := Inspect(s.root); err == nil {
		t.Error("Inspect accepted a corrupt manifest")
	}
	sameTree(t, before, s.home.Dir, "after the refused runs")
}

// TestManifestOfAFullInstallation checks the rules of R12 on the manifest a
// real apply writes with everything in it: two hooks, the mode file, a file
// outside ROOT.
func TestManifestOfAFullInstallation(t *testing.T) {
	s, target, req := profileSite(t, "bash", []byte("a\r\nb"))
	req.Hooks = append(req.Hooks, Hook{Shell: "powershell", Path: s.home.PSProfile, Line: psLine, NewFileCRLF: true})
	mustApply(t, req)
	data := []byte(read(t, filepath.Join(s.root, ManifestName)))

	if bytes.Contains(data, []byte("\r")) || !bytes.HasSuffix(data, []byte("}\n")) || bytes.HasPrefix(data, bomUTF8) {
		t.Error("the manifest is not LF with one final newline and no byte order mark")
	}
	for _, line := range strings.Split(string(data), "\n") {
		if indent := len(line) - len(strings.TrimLeft(line, " ")); indent%2 != 0 || strings.Contains(line, "\t") {
			t.Errorf("indent of %q", line)
		}
	}
	if regexp.MustCompile(`20\d\d-\d\d-\d\d|\d{2}:\d{2}:\d{2}|"(time|date|timestamp|created|updated|modified)[A-Za-z]*"`).Match(data) {
		t.Error("the manifest holds something that looks like a timestamp")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for key := range top {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"dirs", "hooks", "options", "owned", "schema", "version"}) {
		t.Errorf("top-level keys = %v, want exactly those of R12", keys)
	}
	assertSortedKeys(t, data)

	m, err := ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Schema != 1 || m.Version != "0.1.0" || len(m.Hooks) != 2 || m.Hooks[0].Shell != "bash" || m.Hooks[1].Shell != "powershell" {
		t.Errorf("manifest = %+v", m)
	}
	if string(m.Hooks[0].Inserted) != "\r\n"+bashLine+"\r\n" || m.Hooks[0].Path != target || m.Hooks[0].FileCreated {
		t.Errorf("the bash record = %+v", m.Hooks[0])
	}
	if string(m.Hooks[1].Inserted) != psLine+"\r\n" || !m.Hooks[1].FileCreated || len(m.Hooks[1].DirsCreated) != 2 {
		t.Errorf("the powershell record = %+v", m.Hooks[1])
	}
	if !reflect.DeepEqual(m.Options.Hooks, []string{"bash", "powershell"}) || !m.Options.Profile || m.Options.Shell != "auto" {
		t.Errorf("options = %+v", m.Options)
	}
	for _, owned := range []string{s.init, s.mode, s.outside} {
		if sum, err := HashFile(owned); err != nil || m.Owned[owned] != sum {
			t.Errorf("%s is not recorded with the hash of the file", owned)
		}
	}
	if again, _ := m.Encode(); !bytes.Equal(again, data) {
		t.Error("parsing and encoding the manifest changes its bytes")
	}
}
