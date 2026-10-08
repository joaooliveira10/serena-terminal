package engine

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// This file is about two commands that work on one folder at the same time.
// Each of them plans first and writes afterwards, so without care the second
// writes from a picture of the folder that the first has made untrue, and
// then "corrects" the manifest to its own picture: files the first one wrote
// are no longer listed, the next apply takes them for foreign files and
// uninstall leaves them behind.

// everyFileIsListed fails the test when sereno's folder holds a file the
// manifest does not record: the state in which apply refuses and uninstall
// leaves things behind.
func everyFileIsListed(t *testing.T, s site) {
	t.Helper()
	m, found, err := LoadManifest(s.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{s.init, s.mode, s.outside} {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if _, listed := m.Owned[path]; !found || !listed {
			t.Errorf("%s is on disk and the manifest does not list it", path)
		}
	}
}

// TestRepairKeepsWhatIsOnDisk is the mechanism behind the damage, step by
// step and without a second process: two runs plan from the same empty home;
// the first carries its plan out; the second has written the same manifest
// (the bytes are equal) and then finds the first owned file already there.
// When it corrects the manifest it must not take out the files that are on
// disk with exactly the content it wanted to give them.
func TestRepairKeepsWhatIsOnDisk(t *testing.T) {
	s := newSite(t, "")
	first := mustPlan(t, s.request("init v1\n"))
	second := mustPlan(t, s.request("init v1\n"))
	if err := first.Apply(); err != nil {
		t.Fatal(err)
	}
	manifest := read(t, filepath.Join(s.root, ManifestName))

	failed := second.ledger.index + 1 // the first owned file
	if op := second.Ops[failed]; op.Path != s.init || op.Verb != VerbCreate {
		t.Fatalf("operation %d is %s %s, want the creation of the init file", failed, op.Verb, op.Path)
	}
	// The file is there with the bytes the plan wanted: that is not a change
	// to stop for.
	if err := second.Ops[failed].act.run(s.init); err != nil {
		t.Errorf("a file that already has the wanted content stops the run: %v", err)
	}
	if err := second.ledger.repair(second.Ops, failed); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(s.root, ManifestName)); got != manifest {
		t.Errorf("the manifest was rewritten:\n%s\nbefore:\n%s", got, manifest)
	}
	everyFileIsListed(t, s)
	mustBeTrue(t, s)

	// With other content on disk the file is not the plan's, and both the
	// operation and the correction say so.
	put(t, s.init, "somebody else's\n")
	if err := second.Ops[failed].act.run(s.init); !errors.Is(err, ErrChanged) {
		t.Errorf("run over a file with other content = %v, want ErrChanged", err)
	}
	if read(t, s.init) != "somebody else's\n" {
		t.Error("a file that changed after planning was overwritten")
	}
}

// TestSecondPlanFromTheSameState: two plans from the same state, carried out
// one after the other. The second must notice that the folder is no longer
// what it planned for, write nothing, and leave the manifest of the first
// with every file in it.
func TestSecondPlanFromTheSameState(t *testing.T) {
	for _, dirs := range []bool{false, true} {
		name := "from an empty home"
		if dirs {
			name = "the folders are there already"
		}
		t.Run(name, func(t *testing.T) {
			s := newSite(t, "")
			if dirs {
				for _, dir := range []string{filepath.Dir(s.init), filepath.Dir(s.outside)} {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := testenv.Snapshot(t, s.home.Dir)
			first := mustPlan(t, s.request("init v1\n"))
			second := mustPlan(t, s.request("init v1\n"))
			if err := first.Apply(); err != nil {
				t.Fatal(err)
			}
			after := testenv.Snapshot(t, s.home.Dir)

			if err := second.Apply(); !errors.Is(err, ErrBusy) {
				t.Errorf("the second Apply = %v, want ErrBusy", err)
			}
			sameTree(t, after, s.home.Dir, "after the second plan")
			everyFileIsListed(t, s)
			mustBeTrue(t, s)

			// A plan made now has nothing to do, and uninstall takes all back.
			if third := mustApply(t, s.request("init v1\n")); third.Changes() != 0 {
				t.Errorf("a third apply still changes something:\n%s", describe(third))
			}
			mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
			sameTree(t, before, s.home.Dir, "after uninstall")
		})
	}
}

// TestStalePlans: a plan is only carried out for the folder it was made for.
// Each case makes a plan, lets another command finish first, and expects the
// plan to stop before its first write.
func TestStalePlans(t *testing.T) {
	t.Run("an upgrade after another upgrade", func(t *testing.T) {
		s := newSite(t, "")
		mustApply(t, s.request("init v1\n"))
		stale := mustPlan(t, s.request("init v2\n"))
		mustApply(t, s.request("init v3\n"))
		tree := testenv.Snapshot(t, s.home.Dir)
		if err := stale.Apply(); !errors.Is(err, ErrBusy) {
			t.Errorf("Apply = %v, want ErrBusy", err)
		}
		sameTree(t, tree, s.home.Dir, "after the stale plan")
		mustBeTrue(t, s)
	})

	t.Run("an apply after an uninstall", func(t *testing.T) {
		s := newSite(t, "")
		mustApply(t, s.request("init v1\n"))
		stale := mustPlan(t, s.request("init v2\n"))
		mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
		if err := stale.Apply(); !errors.Is(err, ErrBusy) {
			t.Errorf("Apply = %v, want ErrBusy", err)
		}
		sameTree(t, map[string]string{}, s.home.Dir, "after the stale plan")
	})

	// The case a look at the manifest alone does not catch: the other command
	// wrote a file and left the manifest as it was. An uninstall planned
	// while an owned file was missing would delete the manifest and leave the
	// file that an apply has put back in the meantime.
	t.Run("an uninstall after an apply that put a file back", func(t *testing.T) {
		s := newSite(t, "")
		mustApply(t, s.request("init v1\n"))
		if err := os.Remove(s.init); err != nil {
			t.Fatal(err)
		}
		manifest := read(t, filepath.Join(s.root, ManifestName))
		stale, err := PlanUninstall(UninstallRequest{Root: s.root, Version: "0.1.0"})
		if err != nil {
			t.Fatal(err)
		}
		mustApply(t, s.request("init v1\n"))
		if read(t, filepath.Join(s.root, ManifestName)) != manifest {
			t.Fatal("the apply changed the manifest; the case needs one that does not")
		}
		tree := testenv.Snapshot(t, s.home.Dir)
		if err := stale.Apply(); !errors.Is(err, ErrChanged) || !strings.Contains(err.Error(), s.init) {
			t.Errorf("Apply = %v, want ErrChanged for %s", err, s.init)
		}
		sameTree(t, tree, s.home.Dir, "after the stale uninstall")
		mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
		sameTree(t, map[string]string{}, s.home.Dir, "after the uninstall that was planned afresh")
	})

	t.Run("the caller read another manifest than the one that is there", func(t *testing.T) {
		s := newSite(t, "")
		mustApply(t, s.request("init v1\n"))
		seen, _, err := LoadManifest(s.root)
		if err != nil {
			t.Fatal(err)
		}
		mustApply(t, s.request("init v2\n"))
		req := s.request("init v3\n")
		req.Manifest = seen
		if _, err := PlanApply(req); !errors.Is(err, ErrBusy) {
			t.Errorf("PlanApply from a manifest that was replaced = %v, want ErrBusy", err)
		}
		if _, err := PlanUninstall(UninstallRequest{Root: s.root, Manifest: seen}); !errors.Is(err, ErrBusy) {
			t.Errorf("PlanUninstall from a manifest that was replaced = %v, want ErrBusy", err)
		}
	})
}

// TestLock: the lock of sereno's folder is held by one command at a time, is
// a file only while it is held, and never takes a file of the user's.
func TestLock(t *testing.T) {
	s := newSite(t, "")
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.root, LockName)

	release, err := Lock(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("no lock file while the lock is held: %v", err)
	}
	if !Busy(s.root) {
		t.Error("Busy = false while the lock is held")
	}
	if _, err := Lock(s.root); !errors.Is(err, ErrBusy) {
		t.Errorf("a second Lock = %v, want ErrBusy", err)
	}
	release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the lock file is still there after the release: %v", err)
	}
	if Busy(s.root) {
		t.Error("Busy = true after the release")
	}

	// What a killed command leaves behind: the file without a holder. It
	// does not wedge the next command, which also takes the file away.
	put(t, path, "")
	if Busy(s.root) {
		t.Error("a lock file nobody holds counts as busy")
	}
	release, err = Lock(s.root)
	if err != nil {
		t.Fatalf("Lock over a lock file nobody holds: %v", err)
	}
	release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the stale lock file is still there: %v", err)
	}

	// A file with content, or a folder, is not sereno's lock: it stays, and
	// planning says so before any command gets as far as the lock (R15).
	put(t, path, "notes of mine\n")
	if _, err := Lock(s.root); err == nil || errors.Is(err, ErrBusy) {
		t.Errorf("Lock over a file with content = %v, want an error that is not ErrBusy", err)
	}
	if read(t, path) != "notes of mine\n" {
		t.Error("the file in the place of the lock was changed")
	}
	plan := mustPlan(t, s.request("init v1\n"))
	if len(plan.Refusals) != 1 || plan.Refusals[0].Path != path || plan.Refusals[0].Reason.Key != ReasonForeign {
		t.Errorf("the plan of an apply with a foreign file in the place of the lock:\n%s", describe(plan))
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	mustApply(t, s.request("init v1\n"))
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	tree := testenv.Snapshot(t, s.home.Dir)
	removal, err := PlanUninstall(UninstallRequest{Root: s.root})
	if err != nil {
		t.Fatal(err)
	}
	if len(removal.Refusals) != 1 || removal.Refusals[0].Path != path || !errors.Is(removal.Apply(), ErrRefused) {
		t.Errorf("the plan of an uninstall with a folder in the place of the lock:\n%s", describe(removal))
	}
	sameTree(t, tree, s.home.Dir, "after the refused uninstall")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	mustUninstall(t, UninstallRequest{Root: s.root})

	// Outside the sandbox there is no lock to be had.
	outside := testenv.TempDir(t)
	var sandboxErr *SandboxError
	if _, err := Lock(outside); !errors.As(err, &sandboxErr) {
		t.Errorf("Lock outside the sandbox = %v, want a SandboxError", err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("a lock file was created outside the sandbox: %v", entries)
	}
}

// TestPlansAtTheSameTime runs whole commands of the engine side by side,
// many times: plans of the same first apply, with a ROOT whose parents do
// not exist either. However they interleave, at least one runs to its end,
// the others find nothing left to do or make way before their first write
// (ErrBusy, or ErrChanged for a run that looked at the folder in the moment
// another one finished), the manifest is true, no file is unlisted, and
// uninstall gives the empty home back, with every folder any of them
// created.
func TestPlansAtTheSameTime(t *testing.T) {
	const rounds, runs = 20, 6
	for round := 0; round < rounds; round++ {
		s := newSite(t, "")
		errs := make([]error, runs)
		start := make(chan struct{})
		var wait sync.WaitGroup
		for i := 0; i < runs; i++ {
			wait.Add(1)
			go func(i int) {
				defer wait.Done()
				<-start
				plan, err := PlanApply(s.request("init v1\n"))
				if err == nil && plan.Refused() {
					err = ErrRefused
				}
				if err == nil {
					err = plan.Apply()
				}
				errs[i] = err
			}(i)
		}
		close(start)
		wait.Wait()

		done := 0
		for i, err := range errs {
			switch {
			case err == nil:
				done++
			case errors.Is(err, ErrBusy), errors.Is(err, ErrChanged):
			default:
				t.Errorf("round %d, run %d: %v", round, i, err)
			}
		}
		if done == 0 {
			t.Errorf("round %d: no run got to its end", round)
		}
		everyFileIsListed(t, s)
		m := mustBeTrue(t, s)
		want := map[string]string{
			s.init: SHA256([]byte("init v1\n")), s.mode: SHA256([]byte("mode=auto\n")), s.outside: SHA256([]byte("{}\n")),
		}
		if !reflect.DeepEqual(m.Owned, want) {
			t.Errorf("round %d: owned = %v", round, m.Owned)
		}
		if again := mustApply(t, s.request("init v1\n")); again.Changes() != 0 {
			t.Errorf("round %d: an apply afterwards still changes something:\n%s", round, describe(again))
		}
		if removal := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"}); removal.ExitCode() != ExitOK {
			t.Errorf("round %d: uninstall: exit %d:\n%s", round, removal.ExitCode(), describe(removal))
		}
		sameTree(t, map[string]string{}, s.home.Dir, "after uninstall")
		if t.Failed() {
			return
		}
	}
}
