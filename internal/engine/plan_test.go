package engine

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// site is one test installation: a temporary home, ROOT inside it and the two
// owned files the tests ask for.
type site struct {
	home    *testenv.Home
	root    string
	init    string // ROOT/shell/init.bash
	mode    string // ROOT/mode
	outside string // an owned file outside ROOT, like the fragment
}

func newSite(t *testing.T, fixture string) site {
	t.Helper()
	var home *testenv.Home
	if fixture == "" {
		home = testenv.New(t)
	} else {
		home = testenv.NewFrom(t, fixture)
	}
	root := filepath.Join(home.LocalAppData, "sereno")
	return site{
		home:    home,
		root:    root,
		init:    filepath.Join(root, "shell", "init.bash"),
		mode:    filepath.Join(root, ModeName),
		outside: filepath.Join(home.LocalAppData, "Microsoft", "Windows Terminal", "Fragments", "SerenaTerminal", "sereno.json"),
	}
}

// request returns the request most tests start from.
func (s site) request(initContent string) Request {
	return Request{
		Root:    s.root,
		Version: "0.1.0",
		Files: []File{
			{Path: s.init, Data: []byte(initContent)},
			{Path: s.mode, Data: []byte("mode=auto\n")},
			{Path: s.outside, Data: []byte("{}\n")},
		},
	}
}

func mustPlan(t *testing.T, req Request) *Plan {
	t.Helper()
	plan, err := PlanApply(req)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func mustApply(t *testing.T, req Request) *Plan {
	t.Helper()
	plan := mustPlan(t, req)
	if plan.Refused() {
		t.Fatalf("unexpected refusals: %+v", plan.Refusals)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	return plan
}

func mustUninstall(t *testing.T, req UninstallRequest) *Plan {
	t.Helper()
	plan, err := PlanUninstall(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	return plan
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// verbs lists "verb path" for every operation, with paths relative to the
// home, for compact comparisons.
func verbs(s site, plan *Plan) []string {
	var out []string
	for _, op := range plan.Ops {
		rel, err := filepath.Rel(s.home.Dir, op.Path)
		if err != nil {
			rel = op.Path
		}
		out = append(out, string(op.Verb)+" "+filepath.ToSlash(rel))
	}
	return out
}

func sameTree(t *testing.T, want map[string]string, dir, what string) {
	t.Helper()
	got := testenv.Snapshot(t, dir)
	if !reflect.DeepEqual(got, want) {
		for key, value := range got {
			if want[key] != value {
				t.Errorf("%s: %s is new or changed", what, key)
			}
		}
		for key := range want {
			if _, ok := got[key]; !ok {
				t.Errorf("%s: %s is gone", what, key)
			}
		}
	}
}

// TestFirstApply checks the order of phase 2 (R13), the manifest (R12) and
// the plan lines (R14) of an apply into an empty home.
func TestFirstApply(t *testing.T) {
	s := newSite(t, "")
	req := s.request("init v1\n")
	plan := mustPlan(t, req)

	want := []string{
		"mkdir AppData",
		"mkdir AppData/Local",
		"mkdir AppData/Local/sereno",
		"mkdir AppData/Local/sereno/shell",
		"mkdir AppData/Local/Microsoft",
		"mkdir AppData/Local/Microsoft/Windows Terminal",
		"mkdir AppData/Local/Microsoft/Windows Terminal/Fragments",
		"mkdir AppData/Local/Microsoft/Windows Terminal/Fragments/SerenaTerminal",
		"create AppData/Local/sereno/manifest.json",
		"create AppData/Local/sereno/shell/init.bash",
		"create AppData/Local/sereno/mode",
		"create AppData/Local/Microsoft/Windows Terminal/Fragments/SerenaTerminal/sereno.json",
	}
	if got := verbs(s, plan); !reflect.DeepEqual(got, want) {
		t.Errorf("operations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if plan.ExitCode() != ExitOK || plan.Changes() != len(want) || plan.Count(VerbMkdir) != 8 {
		t.Errorf("exit %d, %d changes, %d mkdir", plan.ExitCode(), plan.Changes(), plan.Count(VerbMkdir))
	}

	// Phase 1 wrote nothing.
	sameTree(t, map[string]string{}, s.home.Dir, "after planning")

	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	if read(t, s.init) != "init v1\n" || read(t, s.mode) != "mode=auto\n" || read(t, s.outside) != "{}\n" {
		t.Error("an owned file does not have the requested content")
	}
	m, found, err := LoadManifest(s.root)
	if err != nil || !found {
		t.Fatalf("manifest: found %v, err %v", found, err)
	}
	if m.Version != "0.1.0" || m.Schema != Schema || !m.Options.Profile || m.Options.Shell != "auto" {
		t.Errorf("manifest header: %+v", m)
	}
	wantOwned := map[string]string{
		s.init:    SHA256([]byte("init v1\n")),
		s.mode:    SHA256([]byte("mode=auto\n")),
		s.outside: SHA256([]byte("{}\n")),
	}
	if !reflect.DeepEqual(m.Owned, wantOwned) {
		t.Errorf("owned = %v, want %v", m.Owned, wantOwned)
	}
	if len(m.Dirs) != 8 || m.Dirs[0] != filepath.Join(s.home.Dir, "AppData") || m.Dirs[3] != filepath.Join(s.root, "shell") {
		t.Errorf("dirs are not the created directories in creation order: %v", m.Dirs)
	}
}

// TestSecondApplyDoesNothing is the last sentence of R15.
func TestSecondApplyDoesNothing(t *testing.T) {
	s := newSite(t, "")
	mustApply(t, s.request("init v1\n"))
	before := testenv.Snapshot(t, s.home.Dir)

	plan := mustApply(t, s.request("init v1\n"))
	if plan.Changes() != 0 || plan.ExitCode() != ExitOK {
		t.Errorf("second apply: %d changes, exit %d: %v", plan.Changes(), plan.ExitCode(), verbs(s, plan))
	}
	if plan.Count(VerbKeep) != 4 {
		t.Errorf("second apply keeps %d paths, want the manifest and three files: %v", plan.Count(VerbKeep), verbs(s, plan))
	}
	for _, op := range plan.Ops {
		if op.Note.Key != NoteKept {
			t.Errorf("%s %s has note %q", op.Verb, op.Path, op.Note.Key)
		}
	}
	sameTree(t, before, s.home.Dir, "second apply")
}

// TestOwnershipRules walks the four cases of R15 for a recorded file.
func TestOwnershipRules(t *testing.T) {
	t.Run("recorded and unchanged: replaced when the content differs", func(t *testing.T) {
		s := newSite(t, "")
		mustApply(t, s.request("init v1\n"))
		plan := mustApply(t, s.request("init v2\n"))
		want := []string{
			"replace AppData/Local/sereno/manifest.json",
			"replace AppData/Local/sereno/shell/init.bash",
			"keep AppData/Local/sereno/mode",
			"keep AppData/Local/Microsoft/Windows Terminal/Fragments/SerenaTerminal/sereno.json",
		}
		if got := verbs(s, plan); !reflect.DeepEqual(got, want) {
			t.Errorf("operations: %v", got)
		}
		if read(t, s.init) != "init v2\n" {
			t.Error("the file was not replaced")
		}
		m, _, _ := LoadManifest(s.root)
		if m.Owned[s.init] != SHA256([]byte("init v2\n")) {
			t.Error("the manifest does not record the new hash")
		}
	})

	t.Run("recorded and edited: skipped, exit 4", func(t *testing.T) {
		s := newSite(t, "")
		mustApply(t, s.request("init v1\n"))
		put(t, s.init, "my own init\n")
		plan := mustApply(t, s.request("init v2\n"))
		if plan.ExitCode() != ExitIncomplete || plan.Count(VerbSkip) != 1 || plan.Changes() != 0 {
			t.Errorf("exit %d, operations %v", plan.ExitCode(), verbs(s, plan))
		}
		if read(t, s.init) != "my own init\n" {
			t.Error("the edited file was overwritten")
		}
		m, _, _ := LoadManifest(s.root)
		if m.Owned[s.init] != SHA256([]byte("init v1\n")) {
			t.Error("the manifest must keep the hash of what sereno last wrote")
		}
		// Even content that equals the new version counts as the user's.
		put(t, s.init, "init v2\n")
		if again := mustPlan(t, s.request("init v2\n")); again.Count(VerbSkip) != 1 {
			t.Errorf("an edited file that happens to match was not skipped: %v", verbs(s, again))
		}
	})

	t.Run("recorded and gone: created again", func(t *testing.T) {
		s := newSite(t, "")
		mustApply(t, s.request("init v1\n"))
		if err := os.Remove(s.init); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Dir(s.init)); err != nil {
			t.Fatal(err)
		}
		plan := mustApply(t, s.request("init v1\n"))
		want := []string{
			"mkdir AppData/Local/sereno/shell",
			"keep AppData/Local/sereno/manifest.json",
			"create AppData/Local/sereno/shell/init.bash",
			"keep AppData/Local/sereno/mode",
			"keep AppData/Local/Microsoft/Windows Terminal/Fragments/SerenaTerminal/sereno.json",
		}
		if got := verbs(s, plan); !reflect.DeepEqual(got, want) {
			t.Errorf("operations: %v", got)
		}
		if read(t, s.init) != "init v1\n" {
			t.Error("the file was not created again")
		}
	})

	t.Run("exists and not recorded: refusal, exit 3, nothing written", func(t *testing.T) {
		s := newSite(t, "")
		put(t, s.outside, "someone else's fragment\n")
		before := testenv.Snapshot(t, s.home.Dir)
		plan := mustPlan(t, s.request("init v1\n"))
		if plan.ExitCode() != ExitRefused || len(plan.Refusals) != 1 {
			t.Fatalf("exit %d, refusals %+v", plan.ExitCode(), plan.Refusals)
		}
		if r := plan.Refusals[0]; r.Path != s.outside || r.Reason.Key != ReasonForeign {
			t.Errorf("refusal = %+v", r)
		}
		if err := plan.Apply(); !errors.Is(err, ErrRefused) {
			t.Errorf("Apply on a refused plan = %v, want ErrRefused", err)
		}
		sameTree(t, before, s.home.Dir, "refused apply")
	})

	t.Run("an identical foreign file is still foreign", func(t *testing.T) {
		s := newSite(t, "")
		put(t, s.init, "init v1\n")
		if plan := mustPlan(t, s.request("init v1\n")); !plan.Refused() {
			t.Error("a file sereno did not write was adopted")
		}
	})
}

func TestApplyRefusals(t *testing.T) {
	t.Run("every refusal is reported", func(t *testing.T) {
		s := newSite(t, "")
		put(t, s.outside, "foreign\n")
		put(t, s.mode, "foreign\n")
		req := s.request("init v1\n")
		req.Refusals = []Refusal{{Path: "pwsh", Reason: NewNote("refusal.shell-absent", "pwsh")}}
		plan := mustPlan(t, req)
		if len(plan.Refusals) != 3 || plan.Refusals[0].Path != "pwsh" {
			t.Errorf("refusals = %+v", plan.Refusals)
		}
	})

	t.Run("a directory where a file should be", func(t *testing.T) {
		s := newSite(t, "")
		if err := os.MkdirAll(s.init, 0o755); err != nil {
			t.Fatal(err)
		}
		plan := mustPlan(t, s.request("init v1\n"))
		if len(plan.Refusals) != 1 || plan.Refusals[0].Reason.Key != ReasonNotRegular {
			t.Errorf("refusals = %+v", plan.Refusals)
		}
	})

	t.Run("a file where a directory should be", func(t *testing.T) {
		s := newSite(t, "")
		put(t, filepath.Dir(s.init), "a file named shell\n")
		plan := mustPlan(t, s.request("init v1\n"))
		if len(plan.Refusals) != 1 || plan.Refusals[0].Reason.Key != ReasonParent || plan.Refusals[0].Path != s.init {
			t.Errorf("refusals = %+v", plan.Refusals)
		}
	})

	t.Run("a link where a file should be", func(t *testing.T) {
		s := newSite(t, "")
		target := filepath.Join(s.home.Dir, "target")
		put(t, target, "target\n")
		if err := os.MkdirAll(filepath.Dir(s.mode), 0o755); err != nil {
			t.Fatal(err)
		}
		testenv.Symlink(t, target, s.mode)
		plan := mustPlan(t, s.request("init v1\n"))
		if len(plan.Refusals) != 1 || plan.Refusals[0].Reason.Key != ReasonNotRegular {
			t.Errorf("refusals = %+v", plan.Refusals)
		}
	})
}

func TestRequestValidation(t *testing.T) {
	s := newSite(t, "")
	valid := s.request("x")
	valid.Hooks = []Hook{{Shell: "bash", Path: filepath.Join(s.home.Dir, ".bashrc"), Line: bashLine}}
	if _, err := PlanApply(valid); err != nil {
		t.Fatalf("the valid request is rejected: %v", err)
	}
	tests := map[string]func(*Request){
		"relative root":         func(r *Request) { r.Root = "sereno" },
		"relative file":         func(r *Request) { r.Files[0].Path = "init.bash" },
		"file listed twice":     func(r *Request) { r.Files = append(r.Files, r.Files[0]) },
		"file is the manifest":  func(r *Request) { r.Files[0].Path = filepath.Join(r.Root, ManifestName) },
		"relative hook path":    func(r *Request) { r.Hooks[0].Path = ".bashrc" },
		"hook on an owned file": func(r *Request) { r.Hooks[0].Path = r.Files[0].Path },
		"shell listed twice":    func(r *Request) { r.Hooks = append(r.Hooks, Hook{Shell: "bash", Path: s.mode + "x", Line: bashLine}) },
		"two hooks on one file": func(r *Request) { r.Hooks = append(r.Hooks, Hook{Shell: "zsh", Path: r.Hooks[0].Path, Line: bashLine}) },
		"odd shell name":        func(r *Request) { r.Hooks[0].Shell = "../bash" },
		"line without marker":   func(r *Request) { r.Hooks[0].Line = "echo hello" },
		"line with a newline":   func(r *Request) { r.Hooks[0].Line = "a\nb # sereno:hook" },
	}
	for name, breakIt := range tests {
		t.Run(name, func(t *testing.T) {
			req := s.request("x")
			req.Hooks = []Hook{{Shell: "bash", Path: filepath.Join(s.home.Dir, ".bashrc"), Line: bashLine}}
			breakIt(&req)
			if _, err := PlanApply(req); err == nil {
				t.Error("PlanApply accepted the request")
			}
		})
	}
	if _, err := PlanUninstall(UninstallRequest{Root: "sereno"}); err == nil {
		t.Error("PlanUninstall accepted a relative root")
	}
}

// TestOptionsAreKeptUnlessGiven is the options sentence of R13.
func TestOptionsAreKeptUnlessGiven(t *testing.T) {
	s := newSite(t, "")
	req := s.request("x")
	req.Options = &Options{Shell: "pwsh", Profile: false}
	mustApply(t, req)

	mustApply(t, s.request("y")) // no options given
	m, _, _ := LoadManifest(s.root)
	if m.Options.Shell != "pwsh" || m.Options.Profile {
		t.Errorf("options were not kept: %+v", m.Options)
	}

	req = s.request("y")
	req.Options = &Options{Shell: "auto", Profile: true}
	req.Version = "0.1.1"
	plan := mustApply(t, req)
	m, _, _ = LoadManifest(s.root)
	if m.Options.Shell != "auto" || !m.Options.Profile || m.Version != "0.1.1" {
		t.Errorf("options were not replaced: %+v", m)
	}
	if plan.Changes() != 1 || plan.Ops[0].Verb != VerbReplace {
		t.Errorf("only the manifest should change: %v", verbs(s, plan))
	}
}

// TestSubsetApplyKeepsTheRest covers `theme` and `export`, which write some
// owned files and must leave the other records alone.
func TestSubsetApplyKeepsTheRest(t *testing.T) {
	s := newSite(t, "")
	mustApply(t, s.request("init v1\n"))
	theme := filepath.Join(s.root, "themes", "ghostty", "Serena Dark")
	mustApply(t, Request{Root: s.root, Version: "0.1.0", Files: []File{
		{Path: s.mode, Data: []byte("mode=light\n")},
		{Path: theme, Data: []byte("palette = 0=x\n")},
	}})
	m, _, _ := LoadManifest(s.root)
	if len(m.Owned) != 4 || m.Owned[s.init] != SHA256([]byte("init v1\n")) || m.Owned[s.mode] != SHA256([]byte("mode=light\n")) {
		t.Errorf("owned after a subset apply: %v", m.Owned)
	}
	// A full apply afterwards does not touch the exported file.
	plan := mustApply(t, s.request("init v1\n"))
	if read(t, theme) != "palette = 0=x\n" {
		t.Error("the exported file changed")
	}
	if read(t, s.mode) != "mode=auto\n" || plan.Count(VerbReplace) != 2 {
		t.Errorf("the full apply should replace mode and the manifest: %v", verbs(s, plan))
	}
}

// TestUninstallRestoresTheTree is R39 for owned files: after apply, apply and
// uninstall, the home is byte-identical to what it was.
func TestUninstallRestoresTheTree(t *testing.T) {
	for _, fixture := range []string{"", "wt-present"} {
		t.Run("fixture "+fixture, func(t *testing.T) {
			s := newSite(t, fixture)
			before := testenv.Snapshot(t, s.home.Dir)
			mustApply(t, s.request("init v1\n"))
			mustApply(t, s.request("init v2\n"))

			dry, err := PlanUninstall(UninstallRequest{Root: s.root, Version: "0.1.0"})
			if err != nil {
				t.Fatal(err)
			}
			afterApply := testenv.Snapshot(t, s.home.Dir)
			if dry.ExitCode() != ExitOK || dry.Count(VerbRemove) != 4 {
				t.Errorf("uninstall plan: exit %d, %v", dry.ExitCode(), verbs(s, dry))
			}
			sameTree(t, afterApply, s.home.Dir, "planning the uninstall")

			if err := dry.Apply(); err != nil {
				t.Fatal(err)
			}
			sameTree(t, before, s.home.Dir, "after uninstall")

			// Nothing left to do: an empty plan, exit 0.
			again := mustUninstall(t, UninstallRequest{Root: s.root, Version: "0.1.0"})
			if len(again.Ops) != 0 || again.ExitCode() != ExitOK {
				t.Errorf("uninstall without a manifest: %v", verbs(s, again))
			}
		})
	}
}

func TestUninstallOrder(t *testing.T) {
	s := newSite(t, "")
	mustApply(t, s.request("init v1\n"))
	plan, err := PlanUninstall(UninstallRequest{Root: s.root})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"remove AppData/Local/Microsoft/Windows Terminal/Fragments/SerenaTerminal/sereno.json",
		"remove AppData/Local/sereno/shell/init.bash",
		"remove AppData/Local/sereno/mode",
		"remove AppData/Local/sereno/manifest.json",
		"rmdir AppData/Local/Microsoft/Windows Terminal/Fragments/SerenaTerminal",
		"rmdir AppData/Local/Microsoft/Windows Terminal/Fragments",
		"rmdir AppData/Local/Microsoft/Windows Terminal",
		"rmdir AppData/Local/Microsoft",
		"rmdir AppData/Local/sereno/shell",
		"rmdir AppData/Local/sereno",
		"rmdir AppData/Local",
		"rmdir AppData",
	}
	if got := verbs(s, plan); !reflect.DeepEqual(got, want) {
		t.Errorf("operations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestUninstallLeavesWhatIsNotItsOwn covers an edited owned file (exit 4), the
// install script's ROOT/bin and files other programs put next to sereno's.
func TestUninstallLeavesWhatIsNotItsOwn(t *testing.T) {
	s := newSite(t, "")
	binary := filepath.Join(s.root, "bin", "sereno.exe")
	put(t, binary, "the program\n")
	mustApply(t, s.request("init v1\n"))
	put(t, s.init, "edited by the user\n")
	put(t, s.mode, "mode=dark\n") // settings: removed whatever they say
	neighbour := filepath.Join(filepath.Dir(filepath.Dir(s.outside)), "Git", "git-bash.json")
	put(t, neighbour, "{}\n")

	plan := mustUninstall(t, UninstallRequest{Root: s.root})
	if plan.ExitCode() != ExitIncomplete || plan.Count(VerbSkip) != 1 {
		t.Errorf("exit %d, operations %v", plan.ExitCode(), verbs(s, plan))
	}
	for _, op := range plan.Ops {
		if op.Verb == VerbSkip && (op.Path != s.init || op.Note.Key != NoteEdited) {
			t.Errorf("skip = %+v", op)
		}
	}
	want := map[string]string{
		"AppData":                                                "dir",
		"AppData/Local":                                          "dir",
		"AppData/Local/sereno":                                   "dir",
		"AppData/Local/sereno/bin":                               "dir",
		"AppData/Local/sereno/shell":                             "dir",
		"AppData/Local/Microsoft":                                "dir",
		"AppData/Local/Microsoft/Windows Terminal":               "dir",
		"AppData/Local/Microsoft/Windows Terminal/Fragments":     "dir",
		"AppData/Local/Microsoft/Windows Terminal/Fragments/Git": "dir",
	}
	got := testenv.Snapshot(t, s.home.Dir)
	for key, value := range got {
		if value == "dir" && want[key] != "dir" {
			t.Errorf("directory %s was left behind", key)
		}
	}
	for key := range want {
		if got[key] != "dir" {
			t.Errorf("directory %s was removed although it is not empty", key)
		}
	}
	if read(t, s.init) != "edited by the user\n" || read(t, binary) != "the program\n" || read(t, neighbour) != "{}\n" {
		t.Error("a file sereno does not own changed")
	}
	for _, gone := range []string{s.mode, s.outside, filepath.Join(s.root, ManifestName)} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s is still there", gone)
		}
	}
}

// TestUninstallPlanGlancesPastShortLivedFiles covers what was seen on the
// development machine: a background program drops NAME.EXT.tmp next to a
// script sereno has just read and deletes it a few milliseconds later. On
// Windows the plan waits a moment for such a file; a file that stays, and any
// file on other systems, keeps the directory.
func TestUninstallPlanGlancesPastShortLivedFiles(t *testing.T) {
	s := newSite(t, "")
	mustApply(t, s.request("init v1\n"))
	shell := filepath.Dir(s.init)
	visitor := filepath.Join(shell, "visitor.tmp")
	planned := func() bool {
		plan, err := PlanUninstall(UninstallRequest{Root: s.root})
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range plan.Ops {
			if op.Verb == VerbRmdir && op.Path == shell {
				return true
			}
		}
		return false
	}

	put(t, visitor, "copy")
	gone := make(chan error, 1)
	go func() {
		time.Sleep(30 * time.Millisecond)
		gone <- os.Remove(visitor)
	}()
	short := planned()
	if err := <-gone; err != nil {
		t.Fatal(err)
	}
	if short != (runtime.GOOS == "windows") {
		t.Errorf("rmdir planned past a short-lived file = %v on %s", short, runtime.GOOS)
	}

	put(t, visitor, "a file that stays")
	if planned() {
		t.Error("rmdir planned for a directory that holds a foreign file")
	}
}

// TestUninstallLeavesADirectoryThatFilledUp puts a file into a directory
// between planning and applying. The directory and those above it stay; that
// is "if empty" in R39, not an error, and the wait for it happens only once.
func TestUninstallLeavesADirectoryThatFilledUp(t *testing.T) {
	s := newSite(t, "")
	mustApply(t, s.request("init v1\n"))
	plan, err := PlanUninstall(UninstallRequest{Root: s.root})
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(filepath.Dir(s.init), "notes.txt")
	put(t, foreign, "written by someone else\n")

	start := time.Now()
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*patience {
		t.Errorf("Apply took %s: every directory above the one that stayed waited", elapsed)
	}
	if read(t, foreign) != "written by someone else\n" {
		t.Error("the foreign file changed")
	}
	left := testenv.Snapshot(t, s.home.Dir)
	want := map[string]string{
		"AppData": "dir", "AppData/Local": "dir", "AppData/Local/sereno": "dir", "AppData/Local/sereno/shell": "dir",
		"AppData/Local/sereno/shell/notes.txt": left["AppData/Local/sereno/shell/notes.txt"],
	}
	if !reflect.DeepEqual(left, want) {
		t.Errorf("left after uninstall: %v", left)
	}
}

// TestReadOnlyOwnedFileIsSkipped: an owned file that would be replaced and
// that the user has marked read-only is left as it is, like a profile in that
// state. The plan says so before anything is written, the exit code is 4,
// and the manifest keeps the hash of the content that is there.
func TestReadOnlyOwnedFileIsSkipped(t *testing.T) {
	s := newSite(t, "")
	mustApply(t, s.request("init v1\n"))
	if err := os.Chmod(s.mode, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(s.mode, 0o644) })

	start := time.Now()
	plan := mustApply(t, s.upgrade())
	if elapsed := time.Since(start); elapsed > patience/2 {
		t.Errorf("the apply took %s", elapsed)
	}
	var skipped []Op
	for _, op := range plan.Ops {
		if op.Verb == VerbSkip {
			skipped = append(skipped, op)
		}
	}
	if len(skipped) != 1 || skipped[0].Path != s.mode || skipped[0].Note.Key != NoteReadOnly || !skipped[0].Incomplete || plan.ExitCode() != ExitIncomplete {
		t.Errorf("exit %d:\n%s", plan.ExitCode(), describe(plan))
	}
	if read(t, s.mode) != "mode=auto\n" || read(t, s.init) != "init v2\n" {
		t.Errorf("mode %q, init %q", read(t, s.mode), read(t, s.init))
	}
	if m := mustBeTrue(t, s); m.Owned[s.mode] != SHA256([]byte("mode=auto\n")) {
		t.Error("the manifest records a hash for the mode file that it does not have")
	}
	// A read-only file that is already as wanted is kept, without a word.
	if same := mustApply(t, s.request("init v2\n")); same.ExitCode() != ExitOK || same.Count(VerbSkip) != 0 {
		t.Errorf("exit %d:\n%s", same.ExitCode(), describe(same))
	}
	// The mark comes off: the file is replaced. Uninstall removes it even
	// with the mark on.
	if err := os.Chmod(s.mode, 0o644); err != nil {
		t.Fatal(err)
	}
	if again := mustApply(t, s.upgrade()); again.ExitCode() != ExitOK || read(t, s.mode) != "mode=dark\n" {
		t.Errorf("exit %d:\n%s", again.ExitCode(), describe(again))
	}
	if err := os.Chmod(s.init, 0o444); err != nil {
		t.Fatal(err)
	}
	mustUninstall(t, UninstallRequest{Root: s.root})
	sameTree(t, map[string]string{}, s.home.Dir, "after uninstall")
}

// TestApplyVerifiesWhatItPlannedFor changes a file between the two phases:
// the write must not happen.
func TestApplyVerifiesWhatItPlannedFor(t *testing.T) {
	s := newSite(t, "")
	mustApply(t, s.request("init v1\n"))

	plan := mustPlan(t, s.request("init v2\n"))
	put(t, s.init, "changed in between\n")
	if err := plan.Apply(); !errors.Is(err, ErrChanged) {
		t.Errorf("Apply = %v, want ErrChanged", err)
	}
	if read(t, s.init) != "changed in between\n" {
		t.Error("the file was overwritten")
	}

	s = newSite(t, "")
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		t.Fatal(err)
	}
	plan = mustPlan(t, s.request("init v1\n"))
	put(t, s.mode, "appeared in between\n")
	if err := plan.Apply(); !errors.Is(err, ErrChanged) {
		t.Errorf("Apply = %v, want ErrChanged", err)
	}
	if read(t, s.mode) != "appeared in between\n" {
		t.Error("the file that appeared was overwritten")
	}
}

func TestPlanUntracked(t *testing.T) {
	s := newSite(t, "")
	dir := filepath.Join(s.home.Dir, "exported")
	files := []File{
		{Path: filepath.Join(dir, "ghostty", "Serena Dark"), Data: []byte("dark\n")},
		{Path: filepath.Join(dir, "kitty", "Serena Dark.conf"), Data: []byte("dark\n")},
	}
	plan, err := PlanUntracked(files)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"mkdir exported", "mkdir exported/ghostty", "mkdir exported/kitty",
		"create exported/ghostty/Serena Dark", "create exported/kitty/Serena Dark.conf",
	}
	if got := verbs(s, plan); !reflect.DeepEqual(got, want) {
		t.Errorf("operations: %v", got)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.root, ManifestName)); !os.IsNotExist(err) {
		t.Error("an untracked plan wrote a manifest")
	}

	again, err := PlanUntracked(files)
	if err != nil {
		t.Fatal(err)
	}
	if again.Changes() != 0 || again.Count(VerbKeep) != 2 {
		t.Errorf("identical files should be kept: %v", verbs(s, again))
	}

	put(t, files[0].Path, "edited\n")
	refused, err := PlanUntracked(files)
	if err != nil {
		t.Fatal(err)
	}
	if refused.ExitCode() != ExitRefused || len(refused.Refusals) != 1 || refused.Refusals[0].Path != files[0].Path || refused.Refusals[0].Reason.Key != ReasonDifferent {
		t.Errorf("an existing different file must be a refusal: %+v", refused.Refusals)
	}
	if _, err := PlanUntracked([]File{{Path: "relative"}}); err == nil {
		t.Error("PlanUntracked accepted a relative path")
	}
}

// TestOpLines pins the layout of R14. The translation here is a table: it
// fails the test when it is asked for a key that is not a note.
func TestOpLines(t *testing.T) {
	texts := map[string]string{
		NoteKept:         "already as wanted",
		NoteHookRemoved:  "hook line removed",
		NoteHookReplaces: "replaces the sereno line that is there",
		NoteBackupOf:     "copy of %s from before the hook line",
	}
	translate := func(key string, args ...any) string {
		format, ok := texts[key]
		if !ok {
			t.Errorf("the translation was asked for %q", key)
		}
		return fmt.Sprintf(format, args...)
	}
	tests := []struct {
		op   Op
		want []string
	}{
		{Op{Verb: VerbMkdir, Path: "/r/shell"}, []string{"  mkdir   /r/shell"}},
		{Op{Verb: VerbReplace, Path: "/r/mode"}, []string{"  replace /r/mode"}},
		{Op{Verb: VerbKeep, Path: "/r/mode", Note: NewNote(NoteKept)}, []string{"  keep    /r/mode (already as wanted)"}},
		{Op{Verb: VerbCreate, Path: "/r/backups/b.bak", Note: NewNote(NoteBackupOf, "/h/.bashrc")},
			[]string{"  create  /r/backups/b.bak (copy of /h/.bashrc from before the hook line)"}},
		{Op{Verb: VerbAppend, Path: "/h/.bashrc", Added: []string{"L # sereno:hook"}},
			[]string{"  append  /h/.bashrc", "          + L # sereno:hook"}},
		{Op{Verb: VerbAppend, Path: "/h/.bashrc", Note: NewNote(NoteHookReplaces), Added: []string{"new # sereno:hook"}, Removed: []string{"old # sereno:hook"}},
			[]string{"  append  /h/.bashrc (replaces the sereno line that is there)", "          - old # sereno:hook", "          + new # sereno:hook"}},
		{Op{Verb: VerbReplace, Path: "/h/.bashrc", Note: NewNote(NoteHookRemoved), Removed: []string{"L # sereno:hook"}},
			[]string{"  replace /h/.bashrc (hook line removed)", "          - L # sereno:hook"}},
		{Op{Verb: VerbRmdir, Path: "/r"}, []string{"  rmdir   /r"}},
	}
	for _, test := range tests {
		if got := test.op.Lines(translate); !reflect.DeepEqual(got, test.want) {
			t.Errorf("Lines = %q, want %q", got, test.want)
		}
	}
	for _, verb := range []Verb{VerbMkdir, VerbCreate, VerbReplace, VerbKeep, VerbSkip, VerbAppend, VerbRemove, VerbRmdir} {
		if len(verb) > 7 {
			t.Errorf("verb %q does not fit 8 columns with a space after it", verb)
		}
		line := Op{Verb: verb, Path: "/p"}.Lines(translate)[0]
		if strings.Index(line, "/p") != 10 || line != strings.TrimRight(line, " ") {
			t.Errorf("%q: the path does not start in column 11, or the line has a tail", line)
		}
	}

	// Without a translation the key stands in, and no note is still no note.
	if got := (Op{Verb: VerbKeep, Path: "/p", Note: NewNote(NoteKept)}).Lines(nil)[0]; got != "  keep    /p ("+NoteKept+")" {
		t.Errorf("Lines(nil) = %q", got)
	}
	if got := (Op{Verb: VerbKeep, Path: "/p"}).Lines(nil)[0]; got != "  keep    /p" {
		t.Errorf("Lines(nil) without a note = %q", got)
	}
	if (Note{}).Text(translate) != "" || NewNote(NoteKept).Text(nil) != NoteKept || NewNote(NoteBackupOf, "X").Text(translate) != "copy of X from before the hook line" {
		t.Error("Note.Text is wrong")
	}
}

func TestExitCodes(t *testing.T) {
	if ExitOK != 0 || ExitFailure != 1 || ExitUsage != 2 || ExitRefused != 3 || ExitIncomplete != 4 {
		t.Error("the exit codes are not those of R4")
	}
	tests := []struct {
		plan Plan
		want int
	}{
		{Plan{}, ExitOK},
		{Plan{Ops: []Op{{Verb: VerbKeep}, {Verb: VerbCreate}}}, ExitOK},
		{Plan{Ops: []Op{{Verb: VerbCreate}, {Verb: VerbSkip, Incomplete: true}}}, ExitIncomplete},
		// A skip that only informs (no Windows Terminal, no fragment: R16)
		// is not a failure.
		{Plan{Ops: []Op{{Verb: VerbCreate}, {Verb: VerbSkip}}}, ExitOK},
		{Plan{Ops: []Op{{Verb: VerbSkip, Incomplete: true}}, Refusals: []Refusal{{}}}, ExitRefused},
	}
	for _, test := range tests {
		if got := test.plan.ExitCode(); got != test.want {
			t.Errorf("ExitCode = %d, want %d", got, test.want)
		}
	}
}

// TestSkippedFile is R16 seen from the engine: a file the caller decides not
// to write appears in the plan, in its place, with the caller's note; nothing
// is written or recorded for it and the exit code stays 0.
func TestSkippedFile(t *testing.T) {
	s := newSite(t, "")
	note := NewNote("plan.note.kept") // any key; the engine does not look at it
	req := s.request("init v1\n")
	req.Files[2].Skip = note
	plan := mustApply(t, req)
	want := []string{
		"mkdir AppData",
		"mkdir AppData/Local",
		"mkdir AppData/Local/sereno",
		"mkdir AppData/Local/sereno/shell",
		"create AppData/Local/sereno/manifest.json",
		"create AppData/Local/sereno/shell/init.bash",
		"create AppData/Local/sereno/mode",
		"skip AppData/Local/Microsoft/Windows Terminal/Fragments/SerenaTerminal/sereno.json",
	}
	if got := verbs(s, plan); !reflect.DeepEqual(got, want) {
		t.Errorf("operations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	last := plan.Ops[len(plan.Ops)-1]
	if !reflect.DeepEqual(last.Note, note) || last.Incomplete || last.Changes() {
		t.Errorf("the skip line = %+v", last)
	}
	if plan.ExitCode() != ExitOK || plan.Incomplete() != 0 {
		t.Errorf("exit %d", plan.ExitCode())
	}
	if _, err := os.Lstat(filepath.Dir(filepath.Dir(s.outside))); !os.IsNotExist(err) {
		t.Error("directories were created for a file that is skipped")
	}
	m, _, _ := LoadManifest(s.root)
	if _, recorded := m.Owned[s.outside]; recorded || len(m.Owned) != 2 {
		t.Errorf("owned = %v", m.Owned)
	}

	// A file that was written before and is skipped now stays recorded, and
	// uninstall still removes it.
	mustApply(t, s.request("init v1\n"))
	again := mustApply(t, req)
	if again.Changes() != 0 || read(t, s.outside) != "{}\n" {
		t.Errorf("skipping changed something: %v", verbs(s, again))
	}
	mustUninstall(t, UninstallRequest{Root: s.root})
	sameTree(t, map[string]string{}, s.home.Dir, "after uninstall")
}

// TestSandboxStopsAnApply makes sure a plan cannot write outside the sandbox
// even when asked to: the guard is below the planner.
func TestSandboxStopsAnApply(t *testing.T) {
	s := newSite(t, "")
	other := testenv.TempDir(t)
	req := s.request("init v1\n")
	req.Files = append(req.Files, File{Path: filepath.Join(other, "escaped.txt"), Data: []byte("x")})
	plan := mustPlan(t, req)
	err := plan.Apply()
	var sandboxErr *SandboxError
	if !errors.As(err, &sandboxErr) {
		t.Fatalf("Apply = %v, want a *SandboxError", err)
	}
	if entries, _ := os.ReadDir(other); len(entries) != 0 {
		t.Error("a file was written outside the sandbox")
	}
}

// TestManifestStaysByteIdentical is the determinism sentence of R12.
func TestManifestStaysByteIdentical(t *testing.T) {
	s := newSite(t, "lf")
	req := s.request("init v1\n")
	req.Hooks = []Hook{{Shell: "bash", Path: filepath.Join(s.home.Dir, ".bashrc"), Line: bashLine}}
	mustApply(t, req)
	first := read(t, filepath.Join(s.root, ManifestName))
	mustApply(t, req)
	if second := read(t, filepath.Join(s.root, ManifestName)); second != first {
		t.Errorf("the second apply changed the manifest:\n%s\n---\n%s", first, second)
	}
	if !bytes.Contains([]byte(first), []byte(`"hooks": [`)) || strings.Contains(first, "\r") {
		t.Errorf("unexpected manifest:\n%s", first)
	}
}
