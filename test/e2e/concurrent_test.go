package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
)

// This file is about commands that run at the same time on one home. sereno
// is not meant to be used that way, but a second terminal, a script and an
// impatient hand are enough for it to happen, and what must hold then is
// what holds always: the manifest lists every file sereno wrote (R13), and
// one uninstall takes everything back (R39). Each command line either runs
// to its end or makes way: it ends with exit code 1, before its first write,
// and says why. Nothing in between.

// together starts the command lines at the same moment, each through the
// entry point of the command line package, and returns what each produced.
func (s *site) together(commands ...[]string) []result {
	s.t.Helper()
	results := make([]result, len(commands))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for i, args := range commands {
		wait.Add(1)
		go func(i int, args []string) {
			defer wait.Done()
			<-start
			results[i] = s.run(args...)
		}(i, args)
	}
	close(start)
	wait.Wait()
	return results
}

// settled fails the test unless the installation is whole after commands ran
// at the same time: no owned file counts as edited or missing, every file in
// sereno's folder is one the manifest knows, a plain apply has nothing to
// refuse, and one uninstall gives back the tree the test started from.
func (s *site) settled(start map[string]string, what string) {
	s.t.Helper()
	var report struct {
		Applied bool `json:"applied"`
		Owned   struct{ Total, Modified, Missing int }
	}
	status := s.must(0, "status", "--json")
	if err := json.Unmarshal([]byte(status.out), &report); err != nil {
		s.t.Fatalf("%s: status --json: %v", what, err)
	}
	if report.Owned.Modified != 0 || report.Owned.Missing != 0 {
		s.t.Errorf("%s: status counts %d owned files as edited and %d as missing:\n%s", what, report.Owned.Modified, report.Owned.Missing, status.out)
	}
	if got := s.run("apply"); got.code != engine.ExitOK {
		s.t.Errorf("%s: the next apply ends with exit code %d:\n%s%s", what, got.code, got.out, got.err)
	}
	if got := s.run("uninstall"); got.code != engine.ExitOK {
		s.t.Errorf("%s: uninstall ends with exit code %d:\n%s%s", what, got.code, got.out, got.err)
	}
	same(s.t, what+", after uninstall", start, s.tree())
}

// madeWay is how a command that had to make way for another one ends: exit
// code 1 and, on standard error, what happened and what to do. Usually that
// is the sentence about another sereno at work. A command that looked at the
// folder in the very moment the other one finished cannot know who changed
// it; it names the path that is no longer what it planned for.
func madeWay(r result) bool {
	if r.code != engine.ExitFailure || !strings.Contains(r.err, "run the command again") {
		return false
	}
	return strings.Contains(r.err, "another sereno is working on this folder") || strings.Contains(r.err, ": changed while sereno was running")
}

// TestAppliesAtTheSameTime: several first applies of one home, started
// together. Whatever the order, at least one of them carries out its plan,
// the others either find nothing left to do or make way, and the home ends
// up as one apply alone would have left it.
func TestAppliesAtTheSameTime(t *testing.T) {
	const rounds, runs = 8, 6
	for _, m := range machines {
		t.Run(m.name, func(t *testing.T) {
			for round := 0; round < rounds; round++ {
				s := newSite(t, m, "wt-present")
				s.lang = "en"
				start := s.tree()
				commands := make([][]string, runs)
				for i := range commands {
					commands[i] = []string{"apply", "--hook", m.hooks}
				}
				done := 0
				for i, got := range s.together(commands...) {
					switch {
					case got.code == engine.ExitOK:
						done++
					case madeWay(got):
					default:
						t.Errorf("round %d, run %d: exit code %d\nstdout:\n%s\nstderr:\n%s", round, i, got.code, got.out, got.err)
					}
				}
				if done == 0 {
					t.Errorf("round %d: no apply ran to its end", round)
				}
				// The lock file is there only while a command writes (R10).
				if _, err := os.Stat(filepath.Join(s.root(), "lock")); !os.IsNotExist(err) {
					t.Errorf("round %d: the lock file is still there after the commands ended (%v)", round, err)
				}
				for _, shell := range strings.Split(m.hooks, ",") {
					if n := strings.Count(s.read(s.rc(shell)), "# sereno:hook"); n != 1 {
						t.Errorf("round %d: %s holds %d hook lines, want 1", round, s.rc(shell), n)
					}
				}
				s.settled(start, "round "+string(rune('a'+round)))
				if t.Failed() {
					return
				}
			}
		})
	}
}

// TestDifferentCommandsAtTheSameTime: an installation that is there already,
// and commands that each want something else of it, started together. Which
// of them win is open; that the installation stays whole is not.
func TestDifferentCommandsAtTheSameTime(t *testing.T) {
	const rounds = 8
	for _, m := range machines {
		t.Run(m.name, func(t *testing.T) {
			first, second := strings.Split(m.hooks, ",")[0], strings.Split(m.hooks, ",")[1]
			for round := 0; round < rounds; round++ {
				s := newSite(t, m, "wt-present")
				s.lang = "en"
				start := s.tree()
				s.must(0, "apply", "--hook", first)
				results := s.together(
					[]string{"apply", "--keys", "always"},
					[]string{"apply", "--hook", second},
					[]string{"theme", "light", "--flavor", "vivid"},
					[]string{"theme", "hc"},
					[]string{"export"},
					[]string{"uninstall", "--hook", first},
					[]string{"apply", "--prompt", "off"},
				)
				for i, got := range results {
					if got.code != engine.ExitOK && !madeWay(got) {
						t.Errorf("round %d, command %d: exit code %d\nstdout:\n%s\nstderr:\n%s", round, i, got.code, got.out, got.err)
					}
				}
				// The mode file is one of the forms a single command writes.
				if got := s.read(filepath.Join(s.root(), "mode")); strings.Count(got, "\n") != 5 || !strings.HasPrefix(got, "mode=") {
					t.Errorf("round %d: mode file %q", round, got)
				}
				if m.os == platform.Windows {
					if !json.Valid([]byte(s.read(s.fragment()))) {
						t.Errorf("round %d: the fragment is not JSON any more", round)
					}
				}
				s.settled(start, "round "+string(rune('a'+round)))
				if t.Failed() {
					return
				}
			}
		})
	}
}

// TestUninstallWhileAnApplyRuns: an uninstall and applies started together.
// Either the uninstall came last and the home is as it was before sereno, or
// an apply came last and the installation is whole; never something that
// neither command would have left.
func TestUninstallWhileAnApplyRuns(t *testing.T) {
	const rounds = 8
	for _, m := range machines {
		t.Run(m.name, func(t *testing.T) {
			for round := 0; round < rounds; round++ {
				s := newSite(t, m, "wt-present")
				s.lang = "en"
				start := s.tree()
				s.must(0, "apply", "--hook", m.hooks)
				results := s.together(
					[]string{"uninstall"},
					[]string{"apply", "--keys", "off"},
					[]string{"uninstall"},
					[]string{"theme", "dark"},
				)
				for i, got := range results {
					// `theme` after an uninstall is refused: there is nothing to change.
					if got.code != engine.ExitOK && !madeWay(got) && !(i == 3 && got.code == engine.ExitRefused) {
						t.Errorf("round %d, command %d: exit code %d\nstdout:\n%s\nstderr:\n%s", round, i, got.code, got.out, got.err)
					}
				}
				s.settled(start, "round "+string(rune('a'+round)))
				if t.Failed() {
					return
				}
			}
		})
	}
}
