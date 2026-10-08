package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// This file is about an apply that does not get to its end. R13 writes the
// manifest before the files it describes; when a later operation fails or
// the run is interrupted, the manifest must be corrected, or files sereno
// wrote itself count as edited by the user from then on: apply skips them,
// uninstall leaves them behind and the next apply finds them foreign.

// countdown is a context that reports cancellation from a chosen moment on.
// ApplyContext asks once before each operation, so stopBefore(n) lets
// exactly n operations happen, like an interrupt that arrives at that point.
type countdown struct {
	context.Context
	left int
}

func (c *countdown) Err() error {
	if c.left <= 0 {
		return context.Canceled
	}
	c.left--
	return nil
}

func stopBefore(n int) context.Context {
	return &countdown{Context: context.Background(), left: n}
}

// A file can change under a plan at two moments, and they end differently.
// Before the run has the lock, the plan is made again and found to be another
// one: the run stops before its first write (see TestStalePlans and
// TestChangeBeforeTheFirstWrite). Once the run has the lock, no other sereno
// can change anything, but the user's editor, a scanner or a script can; that
// is found when the operation's turn comes, in the middle of the run, and
// then the manifest has to be corrected. The tests of this file are about
// that second moment.

// applyChanging carries a plan out as Apply does and calls change at the
// moment the run holds the lock and has made its plan again. change stands
// for a program that is not sereno and writes a file all the same.
func applyChanging(t *testing.T, plan *Plan, change func()) error {
	t.Helper()
	fresh, release, err := plan.begin(context.Background())
	if err != nil {
		t.Fatalf("the run could not begin: %v", err)
	}
	change()
	return fresh.carry(context.Background(), release, true)
}

// mustBeTrue fails the test when the manifest of the site says something the
// disk does not bear out: an owned file with another hash than recorded, a
// recorded directory or backup that does not exist, inserted bytes that are
// not in their profile.
func mustBeTrue(t *testing.T, s site) *Manifest {
	t.Helper()
	m, found, err := LoadManifest(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		return m
	}
	for path, recorded := range m.Owned {
		current, err := HashFile(path)
		if err == nil && current != recorded {
			t.Errorf("the manifest records another hash for %s than the file has", path)
		}
		if err != nil && !os.IsNotExist(err) {
			t.Errorf("owned file %s: %v", path, err)
		}
	}
	for _, dir := range m.Dirs {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("the manifest records the directory %s, which is not there", dir)
		}
	}
	for _, record := range m.Hooks {
		for _, dir := range record.DirsCreated {
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				t.Errorf("the %s record lists the directory %s, which is not there", record.Shell, dir)
			}
		}
		if record.Backup != "" {
			if sum, err := HashFile(record.Backup); err != nil || sum != record.BeforeSHA256 {
				t.Errorf("the %s record names a backup that is missing or not the file from before: %v", record.Shell, err)
			}
		}
		if len(record.Inserted) > 0 {
			if data, err := os.ReadFile(record.Path); err != nil || !bytes.Contains(data, record.Inserted) {
				t.Errorf("the %s record says the line is in %s, and it is not", record.Shell, record.Path)
			}
		}
	}
	return m
}

// upgrade returns the request that changes all three owned files of a site.
func (s site) upgrade() Request {
	req := s.request("init v2\n")
	req.Files[1].Data = []byte("mode=dark\n")
	req.Files[2].Data = []byte("{\"v\": 2}\n")
	return req
}

// TestFailedApplyCanBeFinishedOrUndone: phase 2 fails on the first owned
// file, because something changed it after the manifest was written. The
// files that were never written must not count as edited afterwards.
func TestFailedApplyCanBeFinishedOrUndone(t *testing.T) {
	prepare := func(t *testing.T) site {
		s := newSite(t, "")
		mustApply(t, s.request("init v1\n"))
		plan := mustPlan(t, s.upgrade())
		err := applyChanging(t, plan, func() { put(t, s.init, "touched\n") })
		if !errors.Is(err, ErrChanged) {
			t.Fatalf("Apply = %v, want ErrChanged", err)
		}
		if strings.Contains(err.Error(), "manifest") {
			t.Errorf("the error blames the manifest although it could be corrected: %v", err)
		}
		// The user did nothing: the file is again what sereno wrote.
		put(t, s.init, "init v1\n")
		if read(t, s.mode) != "mode=auto\n" || read(t, s.outside) != "{}\n" {
			t.Fatal("a file was written after the failure")
		}
		m := mustBeTrue(t, s)
		if m.Owned[s.init] != SHA256([]byte("init v1\n")) || m.Owned[s.mode] != SHA256([]byte("mode=auto\n")) || m.Owned[s.outside] != SHA256([]byte("{}\n")) {
			t.Errorf("the manifest does not record the hashes of the files as they are: %v", m.Owned)
		}
		return s
	}

	t.Run("the next apply finishes the job", func(t *testing.T) {
		s := prepare(t)
		again := mustApply(t, s.upgrade())
		// The manifest and the three files.
		if again.ExitCode() != ExitOK || again.Count(VerbSkip) != 0 || again.Count(VerbReplace) != 4 {
			t.Errorf("exit %d:\n%s", again.ExitCode(), describe(again))
		}
		if read(t, s.init) != "init v2\n" || read(t, s.mode) != "mode=dark\n" || read(t, s.outside) != "{\"v\": 2}\n" {
			t.Error("the files do not have the new content")
		}
		mustBeTrue(t, s)
	})

	t.Run("uninstall removes everything", func(t *testing.T) {
		s := prepare(t)
		removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
		if removal.ExitCode() != ExitOK {
			t.Errorf("exit %d:\n%s", removal.ExitCode(), describe(removal))
		}
		sameTree(t, map[string]string{}, s.home.Dir, "after uninstall")
		if fresh := mustPlan(t, s.request("init v1\n")); fresh.Refused() {
			t.Errorf("a fresh apply is refused:\n%s", describe(fresh))
		}
	})
}

// TestFailureAfterSomeFilesWereWritten: the files written before the failure
// are recorded with their new hashes, the others with their old ones.
func TestFailureAfterSomeFilesWereWritten(t *testing.T) {
	s := newSite(t, "")
	mustApply(t, s.request("init v1\n"))
	plan := mustPlan(t, s.upgrade())
	// The second of the three files.
	if err := applyChanging(t, plan, func() { put(t, s.mode, "touched\n") }); !errors.Is(err, ErrChanged) {
		t.Fatalf("Apply = %v, want ErrChanged", err)
	}
	if read(t, s.init) != "init v2\n" || read(t, s.outside) != "{}\n" {
		t.Fatalf("init %q, fragment %q", read(t, s.init), read(t, s.outside))
	}
	m, _, _ := LoadManifest(s.root)
	want := map[string]string{
		s.init:    SHA256([]byte("init v2\n")),
		s.mode:    SHA256([]byte("mode=auto\n")),
		s.outside: SHA256([]byte("{}\n")),
	}
	if !reflect.DeepEqual(m.Owned, want) {
		t.Errorf("owned = %v\n want %v", m.Owned, want)
	}

	// The mode file really is edited now: it is skipped, the rest goes on.
	again := mustApply(t, s.upgrade())
	if again.ExitCode() != ExitIncomplete || again.Count(VerbSkip) != 1 || read(t, s.outside) != "{\"v\": 2}\n" {
		t.Errorf("exit %d:\n%s", again.ExitCode(), describe(again))
	}
	put(t, s.mode, "mode=auto\n")
	if last := mustApply(t, s.upgrade()); last.ExitCode() != ExitOK || read(t, s.mode) != "mode=dark\n" {
		t.Errorf("exit %d:\n%s", last.ExitCode(), describe(last))
	}
	mustBeTrue(t, s)
}

// TestFailedFirstApply: there was no manifest before. What the failed run
// wrote is recorded, what it did not write is not.
func TestFailedFirstApply(t *testing.T) {
	s := newSite(t, "")
	if err := os.MkdirAll(filepath.Dir(s.outside), 0o755); err != nil {
		t.Fatal(err)
	}
	before := testenv.Snapshot(t, s.home.Dir)
	plan := mustPlan(t, s.request("init v1\n"))
	// Where the last of the three files goes.
	if err := applyChanging(t, plan, func() { put(t, s.outside, "appeared in between\n") }); !errors.Is(err, ErrChanged) {
		t.Fatalf("Apply = %v, want ErrChanged", err)
	}
	m := mustBeTrue(t, s)
	want := map[string]string{s.init: SHA256([]byte("init v1\n")), s.mode: SHA256([]byte("mode=auto\n"))}
	if !reflect.DeepEqual(m.Owned, want) {
		t.Errorf("owned = %v, want the two files that were written", m.Owned)
	}
	if !reflect.DeepEqual(m.Dirs, []string{s.root, filepath.Join(s.root, "shell")}) {
		t.Errorf("dirs = %v, want the two directories the run created", m.Dirs)
	}

	// The file that appeared is not sereno's, and sereno says so.
	if again := mustPlan(t, s.request("init v1\n")); len(again.Refusals) != 1 || again.Refusals[0].Path != s.outside || again.Refusals[0].Reason.Key != ReasonForeign {
		t.Errorf("the next plan:\n%s", describe(again))
	}
	// Uninstall takes away what sereno wrote and nothing else.
	removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
	if removal.ExitCode() != ExitOK {
		t.Errorf("exit %d:\n%s", removal.ExitCode(), describe(removal))
	}
	if read(t, s.outside) != "appeared in between\n" {
		t.Error("the foreign file was touched")
	}
	if err := os.Remove(s.outside); err != nil {
		t.Fatal(err)
	}
	sameTree(t, before, s.home.Dir, "after uninstall, the foreign file aside")
}

// TestFailureBeforeTheManifest: a directory cannot be created. The
// directories made before it must not be forgotten, or they would stay
// behind for ever.
func TestFailureBeforeTheManifest(t *testing.T) {
	t.Run("some directories were created", func(t *testing.T) {
		s := newSite(t, "")
		if err := os.MkdirAll(s.home.LocalAppData, 0o755); err != nil {
			t.Fatal(err)
		}
		before := testenv.Snapshot(t, s.home.Dir)
		plan := mustPlan(t, s.request("init v1\n"))
		intruder := filepath.Join(s.home.LocalAppData, "Microsoft")

		err := applyChanging(t, plan, func() { put(t, intruder, "a file where a folder should be\n") })
		var sandboxErr *SandboxError
		if err == nil || errors.As(err, &sandboxErr) || errors.Is(err, ErrChanged) {
			t.Fatalf("Apply = %v, want the failure of mkdir", err)
		}
		m := mustBeTrue(t, s)
		if !reflect.DeepEqual(m.Dirs, []string{s.root, filepath.Join(s.root, "shell")}) || len(m.Owned) != 0 || len(m.Hooks) != 0 {
			t.Errorf("the manifest after the failure: dirs %v, owned %v", m.Dirs, m.Owned)
		}
		mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
		if err := os.Remove(intruder); err != nil {
			t.Fatal(err)
		}
		sameTree(t, before, s.home.Dir, "after uninstall, the intruder aside")
	})

	t.Run("nothing was created", func(t *testing.T) {
		s := newSite(t, "")
		if err := os.MkdirAll(s.home.LocalAppData, 0o755); err != nil {
			t.Fatal(err)
		}
		plan := mustPlan(t, s.request("init v1\n"))
		put(t, s.root, "a file where ROOT should be\n")
		before := testenv.Snapshot(t, s.home.Dir)
		err := plan.Apply()
		if err == nil || strings.Contains(err.Error(), "manifest") {
			t.Fatalf("Apply = %v, want the failure of mkdir and no word about the manifest", err)
		}
		sameTree(t, before, s.home.Dir, "after the failed apply")
	})
}

// TestFailedHookKeepsWhatWasCreated: the profile changes between the two
// phases, so the line is not written. The backup that was taken stays
// recorded, the line does not.
func TestFailedHookKeepsWhatWasCreated(t *testing.T) {
	const original = "export A=1\n"
	prepare := func(t *testing.T) (site, string, Request, map[string]string) {
		s, target, req := profileSite(t, "bash", []byte(original))
		before := testenv.Snapshot(t, s.home.Dir)
		plan := mustPlan(t, req)
		if err := applyChanging(t, plan, func() { put(t, target, "changed in between\n") }); !errors.Is(err, ErrChanged) {
			t.Fatalf("Apply = %v, want ErrChanged", err)
		}
		if read(t, target) != "changed in between\n" {
			t.Fatal("the profile was written although it had changed")
		}
		put(t, target, original)
		m := mustBeTrue(t, s)
		record, ok := m.Hook("bash")
		if !ok || record.Backup == "" || record.BeforeSHA256 != SHA256([]byte(original)) || len(record.Inserted) != 0 || record.FileCreated {
			t.Fatalf("the record after the failure = %+v (found %v)", record, ok)
		}
		if read(t, record.Backup) != original {
			t.Error("the backup is not the profile from before")
		}
		return s, target, req, before
	}

	t.Run("uninstall removes the backup", func(t *testing.T) {
		s, _, _, before := prepare(t)
		removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
		if removal.ExitCode() != ExitOK {
			t.Errorf("exit %d:\n%s", removal.ExitCode(), describe(removal))
		}
		sameTree(t, before, s.home.Dir, "after uninstall")
	})

	t.Run("the next apply adds the line and takes no second backup", func(t *testing.T) {
		s, target, req, before := prepare(t)
		again := mustApply(t, req)
		if got := hookOps(s, target, again); !reflect.DeepEqual(got, []string{"append "}) {
			t.Errorf("operations on the profile = %q", got)
		}
		if read(t, target) != original+bashLine+"\n" {
			t.Errorf("the profile after the second apply: %q", read(t, target))
		}
		mustBeTrue(t, s)
		mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
		sameTree(t, before, s.home.Dir, "after uninstall")
	})
}

// TestInterruptedApplyAtEveryPoint stops an apply before each of its
// operations in turn. Wherever it stops, the manifest must be true, a second
// apply must finish the job, and an uninstall right away must give back the
// home as it was before sereno.
//
// The plan is as full as plans get: it upgrades an installation that already
// has a hook line in ~/.bashrc, adds a line to an existing PowerShell profile
// (which needs a backup) and creates the profile of PowerShell 7 with the
// directory above it.
func TestInterruptedApplyAtEveryPoint(t *testing.T) {
	type world struct {
		s      site
		before map[string]string
		req    Request
		bash   string
	}
	prepare := func(t *testing.T) world {
		s := newSite(t, "wt-present")
		before := testenv.Snapshot(t, s.home.Dir)
		bash := filepath.Join(s.home.Dir, bashrc)
		first := s.request("init v1\n")
		first.Hooks = []Hook{{Shell: "bash", Path: bash, Line: bashLine}}
		mustApply(t, first)

		req := s.upgrade()
		req.Hooks = []Hook{
			{Shell: "bash", Path: bash, Line: bashLine},
			{Shell: "powershell", Path: s.home.PSProfile, Line: psLine, NewFileCRLF: true},
			{Shell: "pwsh", Path: s.home.PwshProfile, Line: psLine, NewFileCRLF: true},
		}
		return world{s: s, before: before, req: req, bash: bash}
	}

	probe := prepare(t)
	steps := len(mustPlan(t, probe.req).Ops)
	if steps < 9 {
		t.Fatalf("the plan has only %d operations:\n%s", steps, describe(mustPlan(t, probe.req)))
	}
	originalPS := read(t, probe.s.home.PSProfile)

	for stop := 0; stop <= steps; stop++ {
		interrupt := func(t *testing.T) world {
			w := prepare(t)
			plan := mustPlan(t, w.req)
			err := plan.ApplyContext(stopBefore(stop))
			if stop < steps && !errors.Is(err, context.Canceled) {
				t.Fatalf("stopped before operation %d: ApplyContext = %v\n%s", stop, err, describe(plan))
			}
			if stop == steps && err != nil {
				t.Fatal(err)
			}
			mustBeTrue(t, w.s)
			return w
		}

		t.Run("apply again after stop "+itoa(stop), func(t *testing.T) {
			w := interrupt(t)
			again := mustApply(t, w.req)
			if again.ExitCode() != ExitOK {
				t.Fatalf("exit %d:\n%s", again.ExitCode(), describe(again))
			}
			if read(t, w.s.init) != "init v2\n" || read(t, w.s.mode) != "mode=dark\n" || read(t, w.s.outside) != "{\"v\": 2}\n" {
				t.Error("the owned files do not have the new content")
			}
			if read(t, w.s.home.PSProfile) != originalPS+psLine+"\r\n" || read(t, w.s.home.PwshProfile) != psLine+"\r\n" {
				t.Errorf("the profiles do not carry their lines: %q, %q", read(t, w.s.home.PSProfile), read(t, w.s.home.PwshProfile))
			}
			mustBeTrue(t, w.s)
			if third := mustApply(t, w.req); third.Changes() != 0 {
				t.Errorf("a third apply still changes something:\n%s", describe(third))
			}
			removal := mustUninstall(t, UninstallRequest{Root: w.s.root, Version: "0.1.0"})
			if removal.ExitCode() != ExitOK {
				t.Errorf("uninstall: exit %d:\n%s", removal.ExitCode(), describe(removal))
			}
			sameTree(t, w.before, w.s.home.Dir, "after uninstall")
		})

		t.Run("uninstall after stop "+itoa(stop), func(t *testing.T) {
			w := interrupt(t)
			removal := mustUninstall(t, UninstallRequest{Root: w.s.root, Version: "0.1.0"})
			if removal.ExitCode() != ExitOK {
				t.Errorf("exit %d:\n%s", removal.ExitCode(), describe(removal))
			}
			sameTree(t, w.before, w.s.home.Dir, "after uninstall")
		})
	}
}

// TestChangeBeforeTheFirstWrite is the other moment: a file changes after
// the plan was made and before the run has the lock. The plan is no longer
// the plan of that folder, so nothing of it is carried out, the manifest is
// not touched, and the directories the run made on its way to the lock are
// taken away again.
func TestChangeBeforeTheFirstWrite(t *testing.T) {
	t.Run("an owned file was edited", func(t *testing.T) {
		s := newSite(t, "")
		mustApply(t, s.request("init v1\n"))
		plan := mustPlan(t, s.upgrade())
		put(t, s.mode, "touched\n")
		tree := testenv.Snapshot(t, s.home.Dir)
		err := plan.Apply()
		if !errors.Is(err, ErrChanged) || !strings.Contains(err.Error(), s.mode) {
			t.Errorf("Apply = %v, want ErrChanged for %s", err, s.mode)
		}
		sameTree(t, tree, s.home.Dir, "after the plan that was not carried out")
	})

	t.Run("a file appeared where a first apply writes", func(t *testing.T) {
		s := newSite(t, "")
		plan := mustPlan(t, s.request("init v1\n"))
		put(t, s.outside, "appeared in between\n")
		tree := testenv.Snapshot(t, s.home.Dir)
		err := plan.Apply()
		if !errors.Is(err, ErrChanged) || !strings.Contains(err.Error(), s.outside) {
			t.Errorf("Apply = %v, want ErrChanged for %s", err, s.outside)
		}
		// ROOT and the two directories above it were created for the lock,
		// and are gone again.
		sameTree(t, tree, s.home.Dir, "after the plan that was not carried out")
	})

	t.Run("a profile was edited", func(t *testing.T) {
		s, target, req := profileSite(t, "bash", []byte("export A=1\n"))
		plan := mustPlan(t, req)
		put(t, target, "changed in between\n")
		tree := testenv.Snapshot(t, s.home.Dir)
		err := plan.Apply()
		if !errors.Is(err, ErrChanged) || errors.Is(err, ErrBusy) {
			t.Errorf("Apply = %v, want ErrChanged", err)
		}
		sameTree(t, tree, s.home.Dir, "after the plan that was not carried out")
	})

	t.Run("an interrupt on the way to the lock", func(t *testing.T) {
		s := newSite(t, "")
		plan := mustPlan(t, s.request("init v1\n"))
		// AppData, AppData\Local and ROOT are created, then the interrupt.
		if err := plan.ApplyContext(stopBefore(3)); !errors.Is(err, context.Canceled) {
			t.Fatalf("ApplyContext = %v", err)
		}
		sameTree(t, map[string]string{}, s.home.Dir, "after the interrupt")
	})
}

// itoa avoids importing strconv for one small number.
func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// TestApplyContextBeforeAnything: a context that is already done changes
// nothing at all, and an uninstall can be interrupted and run again.
func TestApplyContextBeforeAnything(t *testing.T) {
	s := newSite(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := mustPlan(t, s.request("init v1\n")).ApplyContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyContext = %v", err)
	}
	sameTree(t, map[string]string{}, s.home.Dir, "after a cancelled apply")

	mustApply(t, s.request("init v1\n"))
	plan, err := PlanUninstall(UninstallRequest{Root: s.root})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.ApplyContext(stopBefore(2)); !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyContext = %v", err)
	}
	if _, found, _ := LoadManifest(s.root); !found {
		t.Fatal("the manifest is gone before the files it lists")
	}
	mustUninstall(t, UninstallRequest{Root: s.root})
	sameTree(t, map[string]string{}, s.home.Dir, "after the second uninstall")
}

// TestCrashBeforeAFileIsFirstCreated: what a killed process leaves behind
// when it was creating files, which no code can tidy up afterwards. The
// manifest lists hashes of files that do not exist; the next apply creates
// them (R15: a recorded path that no longer exists is created again).
func TestCrashBeforeAFileIsFirstCreated(t *testing.T) {
	s, target, req := profileSite(t, "bash", []byte("a\n"))
	before := testenv.Snapshot(t, s.home.Dir)
	plan := mustPlan(t, req)
	for _, op := range plan.Ops {
		if op.Path == s.init || op.Path == target {
			continue // the process died before these two
		}
		if err := op.act.run(op.Path); err != nil {
			t.Fatal(err)
		}
	}
	again := mustApply(t, req)
	if again.ExitCode() != ExitOK || read(t, s.init) != "init v1\n" || read(t, target) != "a\n"+bashLine+"\n" {
		t.Errorf("exit %d, init %q, profile %q\n%s", again.ExitCode(), read(t, s.init), read(t, target), describe(again))
	}
	mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
	sameTree(t, before, s.home.Dir, "after uninstall")
}
