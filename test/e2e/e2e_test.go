package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/joaooliveira10/serena-terminal/internal/cli"
	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/mode"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
	"github.com/joaooliveira10/serena-terminal/internal/target/winterminal"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// TestMain gives every test of this package the hermetic home of rule T1.
func TestMain(m *testing.M) { testenv.Main(m) }

// machine is one kind of host the tree tests run for.
type machine struct {
	name string
	os   string
	// customRoot moves sereno's folder with SERENO_HOME, to a path with a
	// space in it; hook lines and the fragment then carry the absolute path.
	customRoot bool
	// defaultRoot leaves XDG_DATA_HOME unset on macOS and Linux, so that ROOT
	// is ~/.local/share/sereno and the hook line is the portable one.
	defaultRoot bool
	// hooks is the value of --hook for the shells of that system.
	hooks string
}

var machines = []machine{
	{name: "windows", os: platform.Windows, hooks: "powershell,bash"},
	{name: "windows with SERENO_HOME", os: platform.Windows, customRoot: true, hooks: "powershell,bash"},
	{name: "linux with XDG_DATA_HOME", os: platform.Linux, hooks: "bash,zsh"},
	{name: "macos", os: platform.Darwin, defaultRoot: true, hooks: "bash,zsh"},
}

// site is one machine with a home: the place the commands run against.
type site struct {
	t        *testing.T
	machine  machine
	home     *testenv.Home
	vars     map[string]string
	lang     string // the value of --lang, "" for none
	terminal bool
}

// noProcess fails the test when a command would have started a PowerShell:
// no test of the module starts one.
type noProcess struct{ t *testing.T }

func (n noProcess) Probe(program string) (shells.ProbeResult, error) {
	n.t.Errorf("the prober was asked to run %s", program)
	return shells.ProbeResult{}, errors.New("no process in a test")
}

// newSite copies a fixture home ("" for an empty one) into a temporary
// directory and builds the machine around it.
func newSite(t *testing.T, m machine, fixture string) *site {
	t.Helper()
	var home *testenv.Home
	if fixture == "" {
		home = testenv.New(t)
	} else {
		home = testenv.NewFrom(t, fixture)
	}
	s := &site{t: t, machine: m, home: home, vars: home.Env()}
	if m.os == platform.Windows {
		// Every Windows has Windows PowerShell on PATH. The stand-in is
		// never started: the profile path comes from the environment (R20).
		bin := filepath.Join(home.Dir, "bin")
		s.write(filepath.Join(bin, "powershell.exe"), "")
		s.vars["PATH"] = bin
	}
	if m.customRoot {
		s.vars[platform.EnvHome] = filepath.Join(home.Dir, "tools", "sereno home")
	}
	if m.defaultRoot {
		s.vars[platform.EnvDataHome] = ""
	}
	return s
}

func (s *site) host() platform.Host {
	return platform.Host{OS: s.machine.os, Getenv: testenv.Getenv(s.vars)}
}

// result is what one command line produced.
type result struct {
	code     int
	out, err string
}

// run carries out one command line through the entry point of the command
// line package, in this process.
func (s *site) run(args ...string) result {
	s.t.Helper()
	var out, errs bytes.Buffer
	var signals mode.Reader = mode.Unknown{}
	if s.machine.os == platform.Windows {
		signals = mode.Fixed{}
	}
	env := cli.Env{
		Version:    "0.1.0-test",
		Stdout:     &out,
		Stderr:     &errs,
		Host:       s.host(),
		Signals:    signals,
		Prober:     noProcess{s.t},
		Executable: filepath.Join(s.home.Dir, "bin", cli.Program),
	}
	if s.terminal {
		env.Terminal = func() (func(), bool) { return func() {}, true }
	}
	if s.lang != "" {
		args = append(args, "--lang", s.lang)
	}
	code := cli.Run(env, args)
	return result{code: code, out: out.String(), err: errs.String()}
}

// must runs a command line and fails the test unless it ends with the exit
// code that is wanted.
func (s *site) must(code int, args ...string) result {
	s.t.Helper()
	got := s.run(args...)
	if got.code != code {
		s.t.Fatalf("sereno %s: exit code %d, want %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), got.code, code, got.out, got.err)
	}
	for _, text := range []string{got.out, got.err} {
		if missing.MatchString(text) || strings.Contains(text, "%!") {
			s.t.Errorf("sereno %s shows a missing message or a formatting mistake:\n%s", strings.Join(args, " "), text)
		}
	}
	return got
}

// missing matches what a catalogue returns for a key it does not have.
var missing = regexp.MustCompile(`![a-z0-9]+(?:[.-][a-z0-9]+)+!`)

// root is sereno's folder on the machine (R10).
func (s *site) root() string {
	root, err := s.host().Root()
	if err != nil {
		s.t.Fatal(err)
	}
	return root.Path
}

// fragment is the one file sereno writes for Windows Terminal (R16).
func (s *site) fragment() string { return winterminal.FragmentPath(s.home.LocalAppData) }

// rc is the start-up file of a shell on the machine (R20).
func (s *site) rc(shell string) string {
	switch shell {
	case "powershell":
		return s.home.PSProfile
	case "pwsh":
		return s.home.PwshProfile
	case "zsh":
		return filepath.Join(s.home.Dir, ".zshrc")
	}
	return filepath.Join(s.home.Dir, ".bashrc")
}

// hookLine is the line of R21 for a shell on the machine. For a folder in
// its default place the requirement gives the text letter for letter; a
// moved folder is named by its path, as package shells spells it.
func (s *site) hookLine(shell string) string {
	s.t.Helper()
	switch {
	case s.machine.customRoot || (s.machine.os != platform.Windows && !s.machine.defaultRoot):
		root, err := s.host().Root()
		if err != nil {
			s.t.Fatal(err)
		}
		line, err := shells.HookLine(s.host(), shells.Shell(shell), root)
		if err != nil {
			s.t.Fatal(err)
		}
		if !strings.Contains(line, "'") || !strings.HasSuffix(line, " # sereno:hook") {
			s.t.Fatalf("the hook line of a moved folder does not quote its path: %s", line)
		}
		return line
	case shell == "powershell" || shell == "pwsh":
		return `if ($ExecutionContext.SessionState.LanguageMode -eq 'FullLanguage' -and [System.IO.File]::Exists("$env:LOCALAPPDATA\sereno\shell\init.ps1")) { . "$env:LOCALAPPDATA\sereno\shell\init.ps1" } # sereno:hook`
	case s.machine.os == platform.Windows:
		return `[ -r "$LOCALAPPDATA/sereno/shell/init.` + shell + `" ] && . "$LOCALAPPDATA/sereno/shell/init.` + shell + `" # sereno:hook`
	}
	return `[ -r "$HOME/.local/share/sereno/shell/init.` + shell + `" ] && . "$HOME/.local/share/sereno/shell/init.` + shell + `" # sereno:hook`
}

func (s *site) tree() map[string]string { return testenv.Snapshot(s.t, s.home.Dir) }

func (s *site) read(path string) string {
	s.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		s.t.Fatal(err)
	}
	return string(data)
}

func (s *site) write(path, content string) {
	s.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

// rel turns an absolute path below the home into the key of a snapshot.
func (s *site) rel(path string) string {
	s.t.Helper()
	rel, err := filepath.Rel(s.home.Dir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		s.t.Fatalf("%s is not below the home %s", path, s.home.Dir)
	}
	return filepath.ToSlash(rel)
}

// diff describes how a tree changed, with sorted keys.
type diff struct {
	added, changed, removed []string
}

func compare(before, after map[string]string) diff {
	var d diff
	for path, state := range after {
		if old, ok := before[path]; !ok {
			d.added = append(d.added, path)
		} else if old != state {
			d.changed = append(d.changed, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			d.removed = append(d.removed, path)
		}
	}
	sort.Strings(d.added)
	sort.Strings(d.changed)
	sort.Strings(d.removed)
	return d
}

func (d diff) empty() bool { return len(d.added)+len(d.changed)+len(d.removed) == 0 }

// same fails the test when two snapshots differ.
func same(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	if d := compare(before, after); !d.empty() {
		t.Errorf("%s: the tree changed: added %v, changed %v, removed %v", what, d.added, d.changed, d.removed)
	}
}

// planLine is one operation line of a printed plan (R14).
type planLine struct {
	verb string
	rest string // the path, and the note in parentheses when there is one
}

// verbs are the plan verbs of R14.
var verbs = map[string]bool{
	"mkdir": true, "create": true, "replace": true, "keep": true, "skip": true, "append": true, "remove": true, "rmdir": true,
}

// plan reads the operation lines of an output: two spaces, a verb of R14
// padded to eight columns, an absolute path. Other lines are passed over.
func plan(t *testing.T, output string) []planLine {
	t.Helper()
	var lines []planLine
	for _, line := range strings.Split(output, "\n") {
		if len(line) < 11 || !strings.HasPrefix(line, "  ") || line[2] == ' ' {
			continue
		}
		verb := strings.TrimRight(line[2:10], " ")
		if !verbs[verb] || !filepath.IsAbs(planLine{rest: line[10:]}.path()) {
			continue
		}
		lines = append(lines, planLine{verb: verb, rest: line[10:]})
	}
	return lines
}

// path returns the path of a plan line, without its note.
func (l planLine) path() string {
	if i := strings.LastIndex(l.rest, " ("); i >= 0 && strings.HasSuffix(l.rest, ")") {
		return l.rest[:i]
	}
	return l.rest
}

// count returns the number of lines with one of the verbs.
func count(lines []planLine, wanted ...string) int {
	n := 0
	for _, line := range lines {
		for _, verb := range wanted {
			if line.verb == verb {
				n++
			}
		}
	}
	return n
}

// flowOutputs collects what the commands of one walk through T6 printed, to
// compare the two languages.
type flowOutputs struct {
	names []string
	texts map[string]result
}

func (f *flowOutputs) add(name string, r result) result {
	if f.texts == nil {
		f.texts = map[string]result{}
	}
	f.names = append(f.names, name)
	f.texts[name] = r
	return r
}

// walk is test T6: apply --dry-run, apply, apply again, apply --hook, theme,
// export, status, uninstall, with the whole tree compared at every step.
func walk(t *testing.T, m machine, lang string) *flowOutputs {
	t.Helper()
	s := newSite(t, m, "wt-present")
	s.lang = lang
	outputs := &flowOutputs{}
	start := s.tree()
	root := s.rel(s.root())
	windows := m.os == platform.Windows

	// Every command before an apply that writes nothing.
	outputs.add("version", s.must(0, "version"))
	outputs.add("help", s.must(0, "help"))
	outputs.add("help apply", s.must(0, "help", "apply"))
	outputs.add("status before", s.must(0, "status"))
	outputs.add("check", s.must(0, "check"))
	outputs.add("theme refused", s.must(engine.ExitRefused, "theme"))
	outputs.add("export refused", s.must(engine.ExitRefused, "export"))
	outputs.add("uninstall nothing", s.must(0, "uninstall"))
	outputs.add("usage", s.must(engine.ExitUsage, "apply", "--frob"))
	same(t, "commands before an apply", start, s.tree())

	// apply --dry-run: the plan, and nothing changed (R13).
	dry := outputs.add("apply --dry-run", s.must(0, "apply", "--dry-run"))
	same(t, "apply --dry-run", start, s.tree())
	if lines := strings.Split(strings.TrimSuffix(dry.out, "\n"), "\n"); lines[0] != "sereno 0.1.0-test: plan" {
		t.Errorf("the plan does not start with its header (R14): %q", lines[0])
	}

	// apply: exactly the directories and files of the plan, all of them new
	// and all of them below ROOT or, on Windows, at the fragment path.
	first := outputs.add("apply", s.must(0, "apply"))
	afterApply := s.tree()
	created := compare(start, afterApply)
	if len(created.changed)+len(created.removed) != 0 {
		t.Errorf("apply changed or removed something that was there: %v %v", created.changed, created.removed)
	}
	if !reflect.DeepEqual(plan(t, dry.out), plan(t, first.out)) {
		t.Errorf("the dry run and the real run print different plans:\n%s\n%s", dry.out, first.out)
	}
	var planned []string
	for _, line := range plan(t, first.out) {
		if line.verb != "mkdir" && line.verb != "create" {
			t.Errorf("a first apply has a %s line: %s", line.verb, line.rest)
			continue
		}
		planned = append(planned, s.rel(line.path()))
	}
	sort.Strings(planned)
	if !reflect.DeepEqual(planned, created.added) {
		t.Errorf("apply created\n  %v\nand the plan lists\n  %v", created.added, planned)
	}
	fragmentDir := s.rel(filepath.Dir(s.fragment()))
	for _, path := range created.added {
		inRoot := path == root || strings.HasPrefix(path, root+"/")
		atFragment := windows && (path == fragmentDir || strings.HasPrefix(path, fragmentDir+"/"))
		// A moved folder may need parents of its own; they are recorded too.
		above := strings.HasPrefix(root, path+"/")
		if !inRoot && !atFragment && !above {
			t.Errorf("apply created %s, which is neither in sereno's folder nor the fragment", path)
		}
	}
	wantFiles := []string{root + "/manifest.json", root + "/mode", root + "/shell/init.bash", root + "/shell/init.ps1", root + "/shell/init.zsh"}
	if windows {
		wantFiles = append(wantFiles, s.rel(s.fragment()))
	}
	for _, path := range wantFiles {
		if !strings.HasPrefix(afterApply[path], "file:") {
			t.Errorf("apply did not write %s", path)
		}
	}
	if (afterApply[s.rel(s.fragment())] != "") != windows {
		t.Errorf("the fragment exists: %v; on %s it should: %v", !windows, m.os, windows)
	}

	// apply again: zero operations, the manifest byte for byte (R12, R15).
	second := outputs.add("apply again", s.must(0, "apply"))
	same(t, "a second apply", afterApply, s.tree())
	if lines := plan(t, second.out); count(lines, "keep") != len(lines) || len(lines) != len(wantFiles) {
		t.Errorf("a second apply should keep every file and do nothing else:\n%s", second.out)
	}

	// apply --hook for the shells of the system (R20-R22).
	hookDry := s.must(0, "apply", "--hook", m.hooks, "--dry-run")
	same(t, "apply --hook --dry-run", afterApply, s.tree())
	hooked := outputs.add("apply --hook", s.must(0, "apply", "--hook", m.hooks))
	if !reflect.DeepEqual(plan(t, hookDry.out), plan(t, hooked.out)) {
		t.Error("the dry run and the real run of apply --hook print different plans")
	}
	afterHook := s.tree()
	for _, shell := range strings.Split(m.hooks, ",") {
		rc, line := s.rc(shell), s.hookLine(shell)
		if !strings.Contains(hooked.out, "\n          + "+line+"\n") {
			t.Errorf("the plan does not show the exact hook line of %s:\n%s", shell, hooked.out)
		}
		before, existed := start[s.rel(rc)]
		if got := s.read(rc); strings.Count(got, line) != 1 {
			t.Errorf("%s does not hold the hook line exactly once", rc)
		}
		if existed && before == afterHook[s.rel(rc)] {
			t.Errorf("%s did not change", rc)
		}
	}
	s.must(0, "apply")
	same(t, "apply after apply --hook", afterHook, s.tree())

	// theme, export, status.
	outputs.add("theme light --flavor vivid", s.must(0, "theme", "light", "--flavor", "vivid"))
	if got := s.read(filepath.Join(s.root(), "mode")); !strings.HasPrefix(got, "mode=light\nflavor=vivid\n") {
		t.Errorf("mode file after theme: %q", got)
	}
	outputs.add("theme", s.must(0, "theme"))
	outputs.add("theme --show", s.must(0, "theme", "--show"))
	outputs.add("export", s.must(0, "export"))
	outputs.add("export --to", s.must(0, "export", "--to", filepath.Join(s.home.Dir, "out"), "--target", "kitty"))
	outputs.add("status", s.must(0, "status"))
	var report struct {
		Applied bool `json:"applied"`
		Mode    struct {
			Mode   string `json:"mode"`
			Flavor string `json:"flavor"`
		} `json:"mode"`
		Hooks []struct {
			Shell   string `json:"shell"`
			Present bool   `json:"present"`
		} `json:"hooks"`
		Owned struct {
			Total, Modified, Missing int
		} `json:"owned"`
	}
	if err := json.Unmarshal([]byte(s.must(0, "status", "--json").out), &report); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	wantOwned := len(wantFiles) - 1 + 24 // without the manifest, with the themes
	if !report.Applied || report.Mode.Mode != "light" || report.Mode.Flavor != "vivid" || len(report.Hooks) != 2 ||
		!report.Hooks[0].Present || !report.Hooks[1].Present || report.Owned.Total != wantOwned || report.Owned.Modified+report.Owned.Missing != 0 {
		t.Errorf("status --json after the walk: %+v (want %d owned files)", report, wantOwned)
	}

	// uninstall: the tree is what it was, apart from the folder the user
	// asked for with --to, which is not sereno's (R38, R39).
	beforeUninstall := s.tree()
	uninstallDry := outputs.add("uninstall --dry-run", s.must(0, "uninstall", "--dry-run"))
	same(t, "uninstall --dry-run", beforeUninstall, s.tree())
	removed := outputs.add("uninstall", s.must(0, "uninstall"))
	if !reflect.DeepEqual(plan(t, uninstallDry.out), plan(t, removed.out)) {
		t.Error("the dry run and the real run of uninstall print different plans")
	}
	final := s.tree()
	for path := range final {
		if path == "out" || strings.HasPrefix(path, "out/") {
			delete(final, path)
		}
	}
	same(t, "after uninstall", start, final)
	if !strings.Contains(removed.out, filepath.Join(s.home.Dir, "bin", cli.Program)) {
		t.Errorf("uninstall does not say where the program itself is:\n%s", removed.out)
	}
	if windows != strings.Contains(removed.out, "settings.json") {
		// The fixture's Windows Terminal has not seen the profile, so on
		// Windows nothing of it remains either; this branch guards the
		// sentence against appearing on macOS and Linux.
		if !windows {
			t.Errorf("uninstall talks about Windows Terminal on %s:\n%s", m.os, removed.out)
		}
	}
	outputs.add("status after", s.must(0, "status"))
	outputs.add("uninstall again", s.must(0, "uninstall"))
	return outputs
}

// TestTreeWalk runs T6 on every machine and in both languages (R5). The two
// languages must agree on everything that is not a sentence: exit codes (the
// walk checks them), verbs and paths.
func TestTreeWalk(t *testing.T) {
	for _, m := range machines {
		t.Run(m.name, func(t *testing.T) {
			english := walk(t, m, "en")
			portuguese := walk(t, m, "pt-BR")
			if t.Failed() {
				return
			}
			untranslated := map[string]bool{"version": true}
			for _, name := range english.names {
				en, pt := english.texts[name], portuguese.texts[name]
				if (en.out+en.err == pt.out+pt.err) != untranslated[name] {
					t.Errorf("%s: the two languages print the same text: %v, want %v", name, en.out+en.err == pt.out+pt.err, untranslated[name])
				}
				// Paths differ between the two walks (two homes); verbs and
				// the shape of the plan do not.
				enPlan, ptPlan := plan(t, en.out), plan(t, pt.out)
				if len(enPlan) != len(ptPlan) {
					t.Errorf("%s: %d plan lines in English, %d in pt-BR", name, len(enPlan), len(ptPlan))
					continue
				}
				for i := range enPlan {
					if enPlan[i].verb != ptPlan[i].verb || filepath.Base(enPlan[i].path()) != filepath.Base(ptPlan[i].path()) {
						t.Errorf("%s: plan line %d differs: %v and %v", name, i+1, enPlan[i], ptPlan[i])
					}
				}
			}
			// The two sentences R13 fixes.
			if !strings.HasSuffix(english.texts["apply --dry-run"].out, "\nNothing was changed.\n") ||
				!strings.HasSuffix(portuguese.texts["apply --dry-run"].out, "\nNada foi alterado.\n") {
				t.Error("a dry run does not end with the sentence of R13")
			}
			if !strings.HasSuffix(english.texts["uninstall --dry-run"].out, "\nNothing was changed.\n") {
				t.Error("uninstall --dry-run does not end with the sentence of R13")
			}
		})
	}
}

// TestForeignFileInTheWay is the first variant of T6 and R15: a target that
// exists and is not in the manifest is a refusal with exit code 3, and
// nothing is written, not even the files that were not in the way.
func TestForeignFileInTheWay(t *testing.T) {
	for _, m := range machines {
		t.Run(m.name, func(t *testing.T) {
			s := newSite(t, m, "wt-present")
			foreign := filepath.Join(s.root(), "shell", "init.bash")
			if m.os == platform.Windows {
				foreign = s.fragment()
			}
			s.write(foreign, "not sereno's\n")
			start := s.tree()
			for _, args := range [][]string{{"apply", "--dry-run"}, {"apply"}, {"apply", "--hook", m.hooks}} {
				got := s.must(engine.ExitRefused, args...)
				if got.out != "" || !strings.Contains(got.err, "  "+foreign+": ") {
					t.Errorf("sereno %v: the refusal does not name the file: %q %q", args, got.out, got.err)
				}
				same(t, "a refused apply", start, s.tree())
			}
			// status still says: not applied; uninstall has nothing to do.
			s.must(0, "status")
			s.must(0, "uninstall")
			same(t, "status and uninstall", start, s.tree())

			// The way out the refusal names: move the file away.
			if err := os.Remove(foreign); err != nil {
				t.Fatal(err)
			}
			s.must(0, "apply")
		})
	}
}

// TestOwnedFileEdited is the second variant of T6 and R15: a file sereno
// wrote and the user edited is left in place by apply and by uninstall, both
// say so with a skip line, and both end with exit code 4.
func TestOwnedFileEdited(t *testing.T) {
	for _, m := range machines {
		t.Run(m.name, func(t *testing.T) {
			s := newSite(t, m, "wt-present")
			start := s.tree()
			s.must(0, "apply", "--hook", m.hooks)
			edited := filepath.Join(s.root(), "shell", "init.ps1")
			if m.os == platform.Windows {
				edited = s.fragment()
			}
			s.write(edited, "edited by the user\n")
			tree := s.tree()

			for _, args := range [][]string{{"apply", "--dry-run"}, {"apply"}} {
				got := s.must(engine.ExitIncomplete, args...)
				if !strings.Contains(got.out, "  skip    "+edited+" (") || count(plan(t, got.out), "skip") != 1 {
					t.Errorf("sereno %v: the edited file is not the one skip line:\n%s", args, got.out)
				}
				if count(plan(t, got.out), "replace", "create", "mkdir", "append") != 0 {
					t.Errorf("sereno %v wanted to write although nothing else changed:\n%s", args, got.out)
				}
				same(t, "apply with an edited file", tree, s.tree())
			}

			// uninstall after the user edited things: everything else goes,
			// the edited file and the folders that hold it stay.
			dry := s.must(engine.ExitIncomplete, "uninstall", "--dry-run")
			same(t, "uninstall --dry-run", tree, s.tree())
			got := s.must(engine.ExitIncomplete, "uninstall")
			if !reflect.DeepEqual(plan(t, dry.out), plan(t, got.out)) || !strings.Contains(got.out, "  skip    "+edited+" (") {
				t.Errorf("uninstall with an edited file:\n%s", got.out)
			}
			left := compare(start, s.tree())
			if len(left.changed)+len(left.removed) != 0 {
				t.Errorf("uninstall changed or removed something of the user's: %v %v", left.changed, left.removed)
			}
			rel := s.rel(edited)
			for _, path := range left.added {
				if path != rel && !strings.HasPrefix(rel, path+"/") {
					t.Errorf("uninstall left %s behind, which is not the edited file or a folder above it", path)
				}
			}
			if s.read(edited) != "edited by the user\n" {
				t.Error("the edited file was changed")
			}

			// The way out: delete the file. A second uninstall has nothing
			// recorded any more; a new apply starts over where no file is in
			// the way.
			s.must(0, "uninstall")
			if err := os.Remove(edited); err != nil {
				t.Fatal(err)
			}
			s.must(0, "apply")
			s.must(0, "uninstall")
		})
	}
}

// utf16LE encodes text the way Windows PowerShell 5.1 stores a profile it
// wrote with Out-File, without the byte order mark.
func utf16LE(text string) string {
	var out []byte
	for _, unit := range utf16.Encode([]rune(text)) {
		out = append(out, byte(unit), byte(unit>>8))
	}
	return string(out)
}

// TestHookEncodings is T5 at the level of the command: the hook line goes
// into profiles with LF, CRLF, a UTF-8 byte order mark, UTF-16LE and no final
// newline, and into files and folders that do not exist yet. Each case
// asserts the exact bytes after apply and the starting tree after uninstall.
func TestHookEncodings(t *testing.T) {
	windows, custom, linux, macos := machines[0], machines[1], machines[2], machines[3]
	tests := []struct {
		name    string
		machine machine
		fixture string
		shell   string
		// after builds the expected content from the original and the line.
		after func(original, line string) string
		// backup is false when the file did not exist: nothing to back up.
		backup bool
	}{
		{"CRLF profile", windows, "crlf", "powershell", func(o, l string) string { return o + l + "\r\n" }, true},
		{"CRLF profile, moved folder", custom, "crlf", "powershell", func(o, l string) string { return o + l + "\r\n" }, true},
		{"UTF-8 with a byte order mark", windows, "utf8-bom", "powershell", func(o, l string) string { return o + l + "\r\n" }, true},
		{"UTF-16LE", windows, "utf16le", "powershell", func(o, l string) string { return o + utf16LE(l+"\r\n") }, true},
		{"LF rc file", linux, "lf", "bash", func(o, l string) string { return o + l + "\n" }, true},
		{"LF rc file under Git Bash", windows, "lf", "bash", func(o, l string) string { return o + l + "\n" }, true},
		{"no final newline", macos, "no-final-newline", "bash", func(o, l string) string { return o + "\n" + l + "\n" }, true},
		{"no profile and no folder for it", windows, "", "powershell", func(o, l string) string { return l + "\r\n" }, false},
		{"no rc file on macOS", macos, "", "zsh", func(o, l string) string { return l + "\n" }, false},
		{"no rc file under Git Bash", windows, "", "bash", func(o, l string) string { return l + "\n" }, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newSite(t, test.machine, test.fixture)
			rc, line := s.rc(test.shell), s.hookLine(test.shell)
			start := s.tree()
			original := ""
			if test.backup {
				original = s.read(rc)
			}

			got := s.must(0, "apply", "--hook", test.shell)
			want := test.after(original, line)
			if after := s.read(rc); after != want {
				t.Errorf("%s after apply:\n got %q\nwant %q", rc, after, want)
			}
			// A backup of what was there, with the same bytes (R22).
			backups, _ := filepath.Glob(filepath.Join(s.root(), "backups", test.shell+"-*.bak"))
			switch {
			case test.backup && (len(backups) != 1 || s.read(backups[0]) != original):
				t.Errorf("backups = %v, want one copy of the original profile", backups)
			case !test.backup && len(backups) != 0:
				t.Errorf("a backup of a file that did not exist: %v", backups)
			}
			verb := "create"
			if test.backup {
				verb = "append"
			}
			if !strings.Contains(got.out, "\n  "+verb+strings.Repeat(" ", 8-len(verb))+rc) || !strings.Contains(got.out, "\n          + "+line+"\n") {
				t.Errorf("the plan does not show `%s %s` with the line:\n%s", verb, rc, got.out)
			}
			if test.name == "no rc file under Git Bash" && !strings.Contains(got.out, "~/.bash_profile") {
				t.Errorf("the note of R22 about ~/.bash_profile is missing:\n%s", got.out)
			}

			// Again: the line is there, nothing changes. (The one skip line
			// is the fragment of a Windows without Windows Terminal, R16.)
			tree := s.tree()
			again := s.must(0, "apply")
			if count(plan(t, again.out), "keep", "skip") != len(plan(t, again.out)) || !strings.Contains(again.out, "  keep    "+rc+" (") {
				t.Errorf("a second apply found something to do:\n%s", again.out)
			}
			same(t, "a second apply", tree, s.tree())

			// uninstall --hook takes out the line and nothing else; the rest
			// of the installation stays until the full uninstall.
			s.must(0, "uninstall", "--hook", test.shell)
			if test.backup {
				if after := s.read(rc); after != original {
					t.Errorf("%s after uninstall --hook:\n got %q\nwant %q", rc, after, original)
				}
			} else if _, err := os.Stat(rc); !os.IsNotExist(err) {
				t.Errorf("%s was created by sereno and is still there after uninstall --hook", rc)
			}
			if _, err := os.Stat(filepath.Join(s.root(), "manifest.json")); err != nil {
				t.Errorf("uninstall --hook removed more than the hook: %v", err)
			}
			s.must(0, "apply", "--hook", test.shell)
			if after := s.read(rc); after != want {
				t.Errorf("%s after the second apply --hook: %q", rc, after)
			}
			s.must(0, "uninstall")
			same(t, "after uninstall", start, s.tree())
		})
	}
}

// TestHookLineThatIsAlreadyThere covers the two fixtures with a sereno line
// in the rc file: an identical one is kept and never taken for sereno's own
// insertion; a different one is replaced in place, shown in the plan, and
// put back by uninstall (R22, R39).
func TestHookLineThatIsAlreadyThere(t *testing.T) {
	macos := machines[3]

	s := newSite(t, macos, "hook-identical")
	start := s.tree()
	got := s.must(0, "apply", "--hook", "bash")
	if !strings.Contains(got.out, "  keep    "+s.rc("bash")+" (") || s.read(s.rc("bash")) != string(mustRead(t, testenv.Fixture(t, "hook-identical"), ".bashrc")) {
		t.Errorf("an identical hook line was not kept:\n%s", got.out)
	}
	s.must(0, "uninstall")
	same(t, "identical line, after uninstall", start, s.tree())

	s = newSite(t, macos, "hook-different")
	start = s.tree()
	old := `[ -r '/opt/old place/sereno/shell/init.bash' ] && . '/opt/old place/sereno/shell/init.bash' # sereno:hook`
	got = s.must(0, "apply", "--hook", "bash")
	if !strings.Contains(got.out, "\n          - "+old+"\n          + "+s.hookLine("bash")+"\n") {
		t.Errorf("the plan does not show the line that goes above the line that comes:\n%s", got.out)
	}
	want := strings.Replace(string(mustRead(t, testenv.Fixture(t, "hook-different"), ".bashrc")), old, s.hookLine("bash"), 1)
	if s.read(s.rc("bash")) != want {
		t.Errorf("the older line was not replaced in place: %q", s.read(s.rc("bash")))
	}
	s.must(0, "uninstall")
	same(t, "different line, after uninstall", start, s.tree())
}

func mustRead(t *testing.T, dir, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestHookRefusals: a profile sereno does not edit is a refusal with exit
// code 3 that prints the line, so that the user can add it by hand (R22).
func TestHookRefusals(t *testing.T) {
	linux := machines[2]
	tests := []struct {
		name    string
		prepare func(s *site) bool // false: the case cannot be set up here
	}{
		{"a symbolic link", func(s *site) bool {
			link := s.rc("bash")
			if err := os.Symlink(filepath.Join(s.home.Dir, "dotfiles", "bashrc"), link); err != nil {
				return false // Windows without the right to create links
			}
			return true
		}},
		{"UTF-16 big endian", func(s *site) bool { s.write(s.rc("bash"), "\xfe\xff\x00#\x00\n"); return true }},
		{"NUL bytes", func(s *site) bool { s.write(s.rc("bash"), "export A=1\n\x00\x00\n"); return true }},
		{"larger than 1 MiB", func(s *site) bool { s.write(s.rc("bash"), strings.Repeat("# filler\n", 1<<17)); return true }},
		{"a directory", func(s *site) bool { return os.MkdirAll(s.rc("bash"), 0o755) == nil }},
		{"a read-only file", func(s *site) bool {
			s.write(s.rc("bash"), "export EDITOR=vi\n")
			return os.Chmod(s.rc("bash"), 0o444) == nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newSite(t, linux, "symlink-rc")
			if !test.prepare(s) {
				t.Skip("this case cannot be set up on this system")
			}
			start := s.tree()
			got := s.must(engine.ExitRefused, "apply", "--hook", "bash")
			if got.out != "" || !strings.Contains(got.err, "  "+s.rc("bash")+": ") || !strings.Contains(got.err, "      "+s.hookLine("bash")+"\n") {
				t.Errorf("the refusal does not name the file and print the line: %q %q", got.out, got.err)
			}
			same(t, "a refused hook", start, s.tree())
			// Without the hook the apply goes through.
			s.must(0, "apply")
		})
	}
}

// TestUninstallAfterTheUserEditedTheProfile is the last case of T5: lines the
// user added after the hook stay, only sereno's line goes, and the backup is
// kept and named because the file is no longer what it was (R39). That is
// not an error.
func TestUninstallAfterTheUserEditedTheProfile(t *testing.T) {
	for _, m := range machines {
		t.Run(m.name, func(t *testing.T) {
			s := newSite(t, m, "wt-present")
			rc := s.rc("bash")
			original := s.read(rc)
			start := s.tree()
			s.must(0, "apply", "--hook", "bash")
			s.write(rc, s.read(rc)+"alias gs='git status'\n")

			got := s.must(0, "uninstall")
			if after := s.read(rc); after != original+"alias gs='git status'\n" {
				t.Errorf("%s after uninstall: %q", rc, after)
			}
			backups, _ := filepath.Glob(filepath.Join(s.root(), "backups", "bash-*.bak"))
			if len(backups) != 1 || s.read(backups[0]) != original || !strings.Contains(got.out, "  keep    "+backups[0]+" (") {
				t.Errorf("the backup should stay and be named: %v\n%s", backups, got.out)
			}
			// Nothing else of sereno's is left.
			left := compare(start, s.tree())
			backup := s.rel(backups[0])
			for _, path := range left.added {
				if path != backup && !strings.HasPrefix(backup, path+"/") {
					t.Errorf("uninstall left %s behind", path)
				}
			}
			if !reflect.DeepEqual(left.changed, []string{s.rel(rc)}) || len(left.removed) != 0 {
				t.Errorf("changed %v, removed %v", left.changed, left.removed)
			}
		})
	}
}

// TestUninstallOneHook is `sereno uninstall --hook LIST` (R39): only the
// lines of the named shells go, and the manifest forgets them.
func TestUninstallOneHook(t *testing.T) {
	for _, m := range machines {
		t.Run(m.name, func(t *testing.T) {
			s := newSite(t, m, "wt-present")
			shellsOf := strings.Split(m.hooks, ",")
			gone, stays := shellsOf[1], shellsOf[0]
			start := s.tree()
			s.must(0, "apply", "--hook", stays)
			withOne := s.tree()
			s.must(0, "apply", "--hook", gone)

			dry := s.must(0, "uninstall", "--hook", gone, "--dry-run")
			got := s.must(0, "uninstall", "--hook", gone)
			if !reflect.DeepEqual(plan(t, dry.out), plan(t, got.out)) {
				t.Error("the dry run and the real run print different plans")
			}
			same(t, "uninstall --hook", withOne, s.tree())
			// The hook is forgotten: a plain apply does not bring it back.
			s.must(0, "apply")
			same(t, "apply after uninstall --hook", withOne, s.tree())
			// A shell without a hook is said to have none; exit code 0.
			if again := s.must(0, "uninstall", "--hook", gone); !strings.Contains(again.out, gone) || len(plan(t, again.out)) != 0 {
				t.Errorf("uninstall --hook for a shell without a hook:\n%s", again.out)
			}
			same(t, "uninstall --hook again", withOne, s.tree())
			s.must(0, "uninstall")
			same(t, "after uninstall", start, s.tree())
		})
	}
}

// TestFolderTheProfileCannotName is a case the review of the renderer handed
// over: a ROOT with %NAME% in it cannot stand in the command line of the
// Serena profile (R18). apply refuses with exit code 3 and names
// --no-profile, which works.
func TestFolderTheProfileCannotName(t *testing.T) {
	s := newSite(t, machines[0], "wt-present")
	s.vars[platform.EnvHome] = filepath.Join(s.home.Dir, "%USERNAME%", "sereno")
	start := s.tree()
	got := s.must(engine.ExitRefused, "apply")
	if got.out != "" || !strings.Contains(got.err, "%USERNAME%") || !strings.Contains(got.err, "--no-profile") {
		t.Errorf("the refusal does not name the folder and the way round: %q", got.err)
	}
	same(t, "a refused apply", start, s.tree())

	s.must(0, "apply", "--no-profile", "--hook", "powershell")
	if strings.Contains(s.read(s.fragment()), "commandline") {
		t.Error("--no-profile wrote a profile")
	}
	s.must(0, "theme", "light")
	s.must(0, "uninstall")
	same(t, "after uninstall", start, s.tree())
}

// TestReinstallOverALeftover: what an uninstall had to leave behind does not
// wedge the next apply. A backup that stayed is found again and reused when
// the profile is back to that content, and the second uninstall removes it.
//
// What stays in the end are the empty folders above ROOT that the first
// apply created: the first uninstall deleted the manifest that recorded them
// (R39, step 3) while the backup still kept ROOT in place, so the second
// installation found them there and does not own them.
func TestReinstallOverALeftover(t *testing.T) {
	for _, m := range machines {
		t.Run(m.name, func(t *testing.T) {
			s := newSite(t, m, "lf")
			rc := s.rc("bash")
			original := s.read(rc)
			start := s.tree()
			s.must(0, "apply", "--hook", "bash")
			s.write(rc, s.read(rc)+"# added later\n")
			s.must(0, "uninstall")
			// The user undoes the edit; the backup of the first installation
			// is still there.
			s.write(rc, original)
			s.must(0, "apply", "--hook", "bash")
			s.must(0, "uninstall")

			left := compare(start, s.tree())
			if len(left.changed)+len(left.removed) != 0 {
				t.Errorf("changed %v, removed %v", left.changed, left.removed)
			}
			root, after := s.rel(s.root()), s.tree()
			for _, path := range left.added {
				if after[path] != "dir" || !strings.HasPrefix(root, path+"/") {
					t.Errorf("the second uninstall left %s (%s) behind", path, after[path])
				}
			}
		})
	}
}
