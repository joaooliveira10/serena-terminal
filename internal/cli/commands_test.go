package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/i18n"
	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
	"github.com/joaooliveira10/serena-terminal/internal/target/winterminal"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// ---------------------------------------------------------------------------
// check (R9)

func TestCheck(t *testing.T) {
	w := newWorld(t, platform.Linux, "")
	before := w.tree()

	text := w.must(0, "check")
	lines := strings.Split(strings.TrimSuffix(text.out, "\n"), "\n")
	// The minimums R9 expects of today's palette.
	want := []struct{ id, minimum string }{
		{"dark", "4.52"}, {"dark-vivid", "4.52"}, {"light", "4.51"}, {"light-vivid", "4.51"}, {"hc", "7.82"}, {"hc-light", "7.28"},
	}
	if len(lines) != len(want)+1 {
		t.Fatalf("check printed %d lines, want one per variant and a verdict:\n%s", len(lines), text.out)
	}
	for i, variant := range want {
		if !strings.HasPrefix(lines[i], variant.id+" ") || !strings.Contains(lines[i], "minimum "+variant.minimum+":1 ") || !strings.HasSuffix(lines[i], ": pass") {
			t.Errorf("line %d = %q, want %s with minimum %s", i+1, lines[i], variant.id, variant.minimum)
		}
	}
	if lines[len(want)] != "Every value is at or above its floor." {
		t.Errorf("verdict = %q", lines[len(want)])
	}
	if portuguese := w.must(0, "check", "--lang", "pt-BR"); !strings.Contains(portuguese.out, "mínimo 4.52:1 no slot 8, piso 4.5: passa") {
		t.Errorf("check in pt-BR:\n%s", portuguese.out)
	}

	// --markdown prints the committed document, byte for byte (T10).
	committed, err := os.ReadFile(filepath.Join(testenv.RepoRoot(t), "docs", "contrast.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := w.must(0, "check", "--markdown"); got.out != string(committed) {
		t.Error("`sereno check --markdown` differs from docs/contrast.md")
	}
	// The document is a file content and is not translated (R5).
	if got := w.must(0, "check", "--markdown", "--lang", "pt-BR"); got.out != string(committed) {
		t.Error("`sereno check --markdown` depends on the language")
	}

	var report palette.Report
	if err := json.Unmarshal([]byte(w.must(0, "check", "--json").out), &report); err != nil {
		t.Fatalf("check --json: %v", err)
	}
	if !report.OK || len(report.Variants) != 6 || len(report.Roles) != len(palette.RoleList()) {
		t.Errorf("check --json: ok %v, %d variants, %d roles", report.OK, len(report.Variants), len(report.Roles))
	}
	for i, variant := range want {
		if got := ratio(report.Variants[i].Minimum); got != variant.minimum {
			t.Errorf("check --json: minimum of %s = %s, want %s", variant.id, got, variant.minimum)
		}
	}
	sameTree(t, "check", before, w.tree())
}

// TestCheckFails is the exit code rule of R9: any value under its floor, or
// a role outside the allowed parameters, ends the command with 1 and is
// named.
func TestCheckFails(t *testing.T) {
	pal, err := palette.Load()
	if err != nil {
		t.Fatal(err)
	}
	pal.Variants[0].ANSI[3] = pal.Variants[0].Background
	good, err := palette.LoadRoles()
	if err != nil {
		t.Fatal(err)
	}
	table := map[string]string{}
	for _, role := range palette.RoleList() {
		table[string(role)] = good.SGR(role)
	}
	table["error"] = "41"
	data, _ := json.Marshal(table)
	roles, err := palette.ParseRoles(data)
	if err != nil {
		t.Fatal(err)
	}
	report := palette.Check(pal, roles)

	for _, form := range []string{"", "json", "markdown"} {
		var out bytes.Buffer
		r := &run{env: Env{Stdout: &out, Stderr: &out}, catalog: i18n.For(i18n.English)}
		p := parsed{flags: map[string][]string{}}
		if form != "" {
			p.flags[form] = []string{"true"}
		}
		if code := r.checkReport(report, p); code != engine.ExitFailure {
			t.Errorf("check %s: exit code %d for a failing palette, want 1", form, code)
		}
		if form != "" {
			continue
		}
		for _, wanted := range []string{
			": FAIL\n",
			"  dark: ansi[3] on background is 1.00:1, under the floor of 4.5\n",
			"  role error uses SGR 41: SGR parameter 41 is not allowed\n",
			"Under the floor or not allowed: 2.\n",
		} {
			if !strings.Contains(out.String(), wanted) {
				t.Errorf("check does not print %q:\n%s", wanted, out.String())
			}
		}
	}
}

// ---------------------------------------------------------------------------
// theme --paint and --reset (R36)

func TestPaint(t *testing.T) {
	for _, id := range variantValues() {
		w := newWorld(t, platform.Linux, "")
		w.terminal = true
		before := w.tree()
		got := w.must(0, "theme", "--paint", id)
		sequence := golden(t, "palette/"+id+".bin")
		if !strings.HasPrefix(got.out, sequence) {
			t.Errorf("--paint %s does not start with PALETTE of %s", id, id)
		}
		if rest := strings.TrimPrefix(got.out, sequence); strings.Contains(rest, "\x1b") || !strings.Contains(rest, "sereno theme --reset") {
			t.Errorf("--paint %s: after the palette comes %q", id, rest)
		}
		// A trial with no footprint: no file is touched and no apply is needed.
		sameTree(t, "--paint "+id, before, w.tree())
	}

	w := newWorld(t, platform.Windows, "")
	w.terminal = true
	before := w.tree()
	if got := w.must(0, "theme", "--reset"); !strings.HasPrefix(got.out, golden(t, "palette/reset.bin")) || strings.Count(got.out, "\x1b") != 10 {
		t.Errorf("--reset wrote %q", got.out)
	}
	sameTree(t, "--reset", before, w.tree())
}

// TestPaintRefused: painting is refused with exit code 3 when standard
// output is not a terminal, in VS Code's terminal, and inside tmux or screen
// (R36). Nothing is written to standard output then: the bytes of a palette
// in a file or a pipe would be of no use to anybody.
func TestPaintRefused(t *testing.T) {
	tests := []struct {
		name     string
		terminal bool
		vars     map[string]string
		refused  bool
	}{
		{"a terminal", true, nil, false},
		{"Windows Terminal", true, map[string]string{"WT_SESSION": "1"}, false},
		{"an xterm", true, map[string]string{"TERM": "xterm-256color"}, false},
		{"not a terminal", false, nil, true},
		{"VS Code", true, map[string]string{"TERM_PROGRAM": "vscode"}, true},
		{"VS Code inside Windows Terminal variables", true, map[string]string{"TERM_PROGRAM": "vscode", "WT_SESSION": "1"}, true},
		{"tmux", true, map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0"}, true},
		{"TERM of screen", true, map[string]string{"TERM": "screen-256color"}, true},
		{"TERM of tmux", true, map[string]string{"TERM": "tmux-256color"}, true},
	}
	bothLanguages(t, func(t *testing.T, lang string) {
		for _, test := range tests {
			for _, args := range [][]string{{"theme", "--paint", "light"}, {"theme", "--reset"}} {
				w := newWorld(t, platform.Linux, "")
				w.terminal = test.terminal
				for key, value := range test.vars {
					w.vars[key] = value
				}
				before := w.tree()
				got := w.run(append(args, "--lang", lang)...)
				switch {
				case !test.refused && (got.code != 0 || !strings.Contains(got.out, "\x1b]")):
					t.Errorf("%s, %v: exit code %d, want the sequences", test.name, args, got.code)
				case test.refused && (got.code != engine.ExitRefused || got.out != ""):
					t.Errorf("%s, %v: exit code %d and %q on standard output, want a refusal", test.name, args, got.code, got.out)
				case test.refused && strings.Count(got.err, "\n") != 2:
					t.Errorf("%s, %v: the refusal should be the reason and the way out:\n%s", test.name, args, got.err)
				}
				noMissingKey(t, got.out, got.err)
				sameTree(t, test.name, before, w.tree())
			}
		}
	})
}

// ---------------------------------------------------------------------------
// theme settings (R35)

func TestThemeBeforeAnApply(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		w := newWorld(t, platform.Windows, "wt-present")
		before := w.tree()
		for _, args := range [][]string{{"theme"}, {"theme", "dark"}, {"theme", "--flavor", "vivid"}, {"theme", "--inband", "off"}, {"theme", "light", "--show"}} {
			got := w.must(engine.ExitRefused, append(args, "--lang", lang)...)
			if got.out != "" || !strings.Contains(got.err, "`sereno apply`") || !strings.Contains(got.err, "sereno theme --paint dark") {
				t.Errorf("sereno %v before an apply: %q %q", args, got.out, got.err)
			}
			noMissingKey(t, got.err)
		}
		// --show reads no file of sereno's and shows the defaults.
		if got := w.must(0, "theme", "--show", "--lang", lang); !strings.Contains(got.out, "Serena Dark") {
			t.Errorf("theme --show before an apply:\n%s", got.out)
		}
		sameTree(t, "theme before an apply", before, w.tree())
	})
}

const (
	defaultMode    = "mode=auto\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\n"
	lightVividMode = "mode=light\nflavor=vivid\ninband=profile\nkeys=profile\nprompt=on\n"
)

func TestTheme(t *testing.T) {
	w := newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply")
	if w.read(w.modeFile()) != defaultMode || w.read(w.fragment()) != golden(t, "fragment/auto-soft.json") {
		t.Fatal("apply did not write the first-apply mode file and fragment")
	}

	// Without arguments: the five settings, the system, the variant, the route.
	before := w.tree()
	shown := w.must(0, "theme")
	for _, line := range []string{
		`^mode +auto$`, `^flavor +soft$`, `^inband +profile$`, `^keys +profile$`, `^prompt +on$`,
		`^system theme +dark, no contrast theme$`, `^variant +dark \(Serena Dark\)$`, `^palette route +emulator: `,
	} {
		if !regexp.MustCompile(`(?m)` + line).MatchString(shown.out) {
			t.Errorf("`sereno theme` has no line %s:\n%s", line, shown.out)
		}
	}
	if strings.Contains(shown.out, "\x1b") {
		t.Error("`sereno theme` wrote escape sequences to something that is not a terminal")
	}
	sameTree(t, "theme without arguments", before, w.tree())

	// A mode and a flavour: the mode file and the profile's colorScheme (R18).
	changed := w.must(0, "theme", "light", "--flavor", "vivid")
	if w.read(w.modeFile()) != lightVividMode {
		t.Errorf("mode file after `theme light --flavor vivid`: %q", w.read(w.modeFile()))
	}
	if w.read(w.fragment()) != golden(t, "fragment/light-vivid.json") {
		t.Error("the fragment is not the one of a fixed light vivid mode")
	}
	for _, line := range []string{"  replace " + w.modeFile() + "\n", "  replace " + w.fragment() + "\n", "light-vivid (Serena Light Vivid)"} {
		if !strings.Contains(changed.out, line) {
			t.Errorf("`theme light --flavor vivid` does not print %q:\n%s", line, changed.out)
		}
	}
	if strings.Contains(changed.out, engine.ManifestName) {
		t.Errorf("the manifest is sereno's bookkeeping and is not worth a line:\n%s", changed.out)
	}

	// The manifest hash was updated (R35): nothing counts as edited, and an
	// apply neither undoes the theme nor finds anything to do.
	after := w.tree()
	again := w.must(0, "apply")
	if strings.Contains(again.out, "replace") || strings.Contains(again.out, "skip") || strings.Contains(again.out, "create") {
		t.Errorf("apply after theme found something to do:\n%s", again.out)
	}
	sameTree(t, "apply after theme", after, w.tree())

	// The same setting again writes nothing.
	if same := w.must(0, "theme", "light"); strings.Contains(same.out, "replace") {
		t.Errorf("`theme light` twice wrote again:\n%s", same.out)
	}
	sameTree(t, "theme light again", after, w.tree())

	// --inband changes the mode file only.
	w.must(0, "theme", "--inband", "always")
	if got := w.read(w.modeFile()); got != strings.Replace(lightVividMode, "inband=profile", "inband=always", 1) {
		t.Errorf("mode file after --inband always: %q", got)
	}
	if w.read(w.fragment()) != golden(t, "fragment/light-vivid.json") {
		t.Error("--inband changed the fragment")
	}

	// Every mode gives the fragment of the golden files.
	for _, step := range []struct {
		args   []string
		golden string
	}{
		{[]string{"hc"}, "hc"}, {[]string{"hc-light"}, "hc-light"}, {[]string{"dark"}, "dark-vivid"},
		{[]string{"dark", "--flavor", "soft"}, "dark-soft"}, {[]string{"light"}, "light-soft"},
		{[]string{"auto", "--flavor=vivid"}, "auto-vivid"}, {[]string{"--flavor", "soft"}, "auto-soft"},
	} {
		w.must(0, append([]string{"theme"}, step.args...)...)
		if w.read(w.fragment()) != golden(t, "fragment/"+step.golden+".json") {
			t.Errorf("after `theme %s` the fragment is not %s.json", strings.Join(step.args, " "), step.golden)
		}
	}

	// uninstall still removes everything: the hashes in the manifest are true.
	w.must(0, "uninstall")
	if _, err := os.Stat(w.root()); !os.IsNotExist(err) {
		t.Errorf("uninstall after theme left %s (%v)", w.root(), err)
	}
}

// TestThemeRepaints is the last sentence of R35: after showing or changing
// the settings, sereno writes PALETTE of the resolved variant when standard
// output is a terminal, painting is allowed, and the session is one that
// follows the system (R30). Other sessions keep the colours they have.
func TestThemeRepaints(t *testing.T) {
	guid := winterminal.ProfileGUID()
	tests := []struct {
		name     string
		terminal bool
		vars     map[string]string
		inband   string
		args     []string
		paints   string // the variant whose PALETTE ends the output; "" for none
	}{
		{"Serena profile session", true, map[string]string{"WT_SESSION": "1", "SERENO_SESSION": "wt-profile"}, "", nil, "dark"},
		{"system in light mode", true, map[string]string{"WT_SESSION": "1", "SERENO_SESSION": "wt-profile", "SERENO_OS_MODE": "light"}, "", nil, "light"},
		{"contrast theme", true, map[string]string{"WT_SESSION": "1", "SERENO_SESSION": "wt-profile", "SERENO_OS_MODE": "hc-light"}, "", nil, "hc-light"},
		{"recognised by the profile GUID", true, map[string]string{"WT_SESSION": "1", "WT_PROFILE_ID": strings.ToUpper(guid)}, "", nil, "dark"},
		{"a change is painted at once", true, map[string]string{"WT_SESSION": "1", "SERENO_SESSION": "wt-profile"}, "", []string{"light", "--flavor", "vivid"}, "light-vivid"},
		{"another profile, inband=profile", true, map[string]string{"WT_SESSION": "1"}, "", nil, ""},
		{"another profile, inband=always", true, map[string]string{"WT_SESSION": "1"}, "always", nil, "dark"},
		{"Serena profile, inband=off", true, map[string]string{"WT_SESSION": "1", "SERENO_SESSION": "wt-profile"}, "off", nil, ""},
		{"turning inband off does not paint", true, map[string]string{"WT_SESSION": "1", "SERENO_SESSION": "wt-profile"}, "", []string{"--inband", "off"}, ""},
		{"not Windows Terminal", true, map[string]string{"SERENO_SESSION": "wt-profile"}, "always", nil, ""},
		{"VS Code", true, map[string]string{"WT_SESSION": "1", "SERENO_SESSION": "wt-profile", "TERM_PROGRAM": "vscode"}, "always", nil, ""},
		{"tmux", true, map[string]string{"WT_SESSION": "1", "SERENO_SESSION": "wt-profile", "TMUX": "x"}, "always", nil, ""},
		{"output redirected", false, map[string]string{"WT_SESSION": "1", "SERENO_SESSION": "wt-profile"}, "always", nil, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := newWorld(t, platform.Windows, "wt-present")
			w.must(0, "apply")
			if test.inband != "" {
				w.must(0, "theme", "--inband", test.inband)
			}
			w.terminal = test.terminal
			for key, value := range test.vars {
				w.vars[key] = value
			}
			got := w.must(0, append([]string{"theme", "--no-color"}, test.args...)...)
			if test.paints == "" {
				if strings.Contains(got.out, "\x1b") {
					t.Errorf("the session was painted: %q", got.out)
				}
				return
			}
			sequence := golden(t, "palette/"+test.paints+".bin")
			if !strings.HasSuffix(got.out, sequence) || strings.Count(got.out, "\x1b") != strings.Count(sequence, "\x1b") {
				t.Errorf("the output does not end with PALETTE of %s and nothing else of its kind:\n%q", test.paints, got.out)
			}
		})
	}
}

func TestThemeOnUnix(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		w := newWorld(t, platform.Linux, "lf")
		w.must(0, "apply")
		w.must(0, "theme", "dark")
		auto := w.must(0, "theme", "auto", "--lang", lang)
		if w.read(w.modeFile()) != defaultMode {
			t.Errorf("`theme auto` did not store the setting: %q", w.read(w.modeFile()))
		}
		// R35: it says that detection is not part of 0.1 and names the route
		// that is supported.
		if !strings.Contains(auto.out, "0.1") || !strings.Contains(auto.out, "`sereno export`") || !strings.Contains(auto.out, "dark (Serena Dark)") {
			t.Errorf("`theme auto` on Linux:\n%s", auto.out)
		}
		if fixed := w.must(0, "theme", "light", "--lang", lang); strings.Contains(fixed.out, "`sereno export`") {
			t.Errorf("a fixed mode needs no word about detection:\n%s", fixed.out)
		}
		noMissingKey(t, auto.out)
	})
}

// TestThemeAndAnEditedModeFile is R15 for the mode file: one the user edited
// is left as it is, the command ends with 4, and what it reports is what the
// file says, because that is what the shells go by.
func TestThemeAndAnEditedModeFile(t *testing.T) {
	w := newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply")
	edited := "mode=dark\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\n"
	w.write(w.modeFile(), edited)

	got := w.must(engine.ExitIncomplete, "theme", "light")
	if w.read(w.modeFile()) != edited {
		t.Error("an edited mode file was overwritten")
	}
	// The fragment follows the file, not the setting that was not stored.
	if w.read(w.fragment()) != golden(t, "fragment/dark-soft.json") {
		t.Error("the fragment says something else than the mode file")
	}
	if !strings.Contains(got.out, "  skip    "+w.modeFile()+" (") || !regexp.MustCompile(`(?m)^mode +dark$`).MatchString(got.out) {
		t.Errorf("the edited mode file is not reported as left alone, or the report does not show what it says:\n%s", got.out)
	}
	if !strings.Contains(got.out, "run the command again") {
		t.Errorf("no way out is named:\n%s", got.out)
	}

	// The way out: delete the file; sereno then writes it again.
	if err := os.Remove(w.modeFile()); err != nil {
		t.Fatal(err)
	}
	created := w.must(0, "theme", "light")
	if w.read(w.modeFile()) != strings.Replace(defaultMode, "mode=auto", "mode=light", 1) || !strings.Contains(created.out, "  create  "+w.modeFile()) {
		t.Errorf("a deleted mode file was not written again:\n%s", created.out)
	}

	// A malformed file means the defaults, here as in the shells (R11).
	w.write(w.modeFile(), "\xef\xbb\xbf"+lightVividMode)
	if got := w.must(0, "theme"); !regexp.MustCompile(`(?m)^mode +auto$`).MatchString(got.out) {
		t.Errorf("a mode file with a byte order mark was read:\n%s", got.out)
	}
}

// TestThemeKeepsTheShellOfTheProfile: with `--shell auto` recorded, a
// PowerShell 7 that appears after the apply does not slip into the Serena
// profile through `sereno theme`; the next apply is what takes it.
func TestThemeKeepsTheShellOfTheProfile(t *testing.T) {
	w := newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply")
	w.program("pwsh.exe")
	w.must(0, "theme", "dark")
	if w.read(w.fragment()) != golden(t, "fragment/dark-soft.json") {
		t.Error("`sereno theme` changed more than the colour scheme of the profile")
	}
	w.must(0, "theme", "auto")
	w.must(0, "apply")
	if w.read(w.fragment()) != golden(t, "fragment/pwsh.json") {
		t.Error("apply with --shell auto did not take the PowerShell 7 that is on PATH now")
	}
	// And from then on theme keeps PowerShell 7.
	w.must(0, "theme", "--flavor", "vivid")
	if !strings.Contains(w.read(w.fragment()), `"commandline": "pwsh.exe `) {
		t.Error("`sereno theme` put Windows PowerShell back into a profile that starts PowerShell 7")
	}
}

// ---------------------------------------------------------------------------
// status (R37)

// keysOfR37 is the JSON of R37: the top-level keys in order, and the keys of
// each object.
var (
	keysOfR37 = []string{
		"version", "os", "root", "applied", "lang", "mode", "signals", "variant", "session",
		"windowsTerminal", "hooks", "owned", "pwsh", "warnings",
	}
	nestedKeysOfR37 = map[string][]string{
		"mode":            {"mode", "flavor", "inband", "keys", "prompt"},
		"signals":         {"appsLight", "highContrast", "hcLight"},
		"session":         {"emulator", "serenaProfile", "paletteRoute"},
		"windowsTerminal": {"detected", "fragment", "profile", "settingsPath", "theme", "defaultsOverride", "stubPresent", "guidKnown"},
		"hooks":           {"shell", "path", "present"},
		"owned":           {"total", "modified", "missing"},
		"pwsh":            {"onPath"},
		"warnings":        {"id", "text"},
	}
)

// statusOf runs `sereno status --json` and decodes it.
func statusOf(t *testing.T, w *world, args ...string) (statusReport, string) {
	t.Helper()
	got := w.must(0, append([]string{"status", "--json"}, args...)...)
	if got.err != "" {
		t.Errorf("status wrote to standard error: %s", got.err)
	}
	var report statusReport
	decoder := json.NewDecoder(strings.NewReader(got.out))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		t.Fatalf("status --json: %v\n%s", err, got.out)
	}
	return report, got.out
}

func warningIDs(report statusReport) []string {
	ids := []string{}
	for _, warning := range report.Warnings {
		ids = append(ids, warning.ID)
	}
	return ids
}

func TestStatusJSONShape(t *testing.T) {
	w := newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply", "--hook", "powershell")
	_, raw := statusOf(t, w)

	// Top level: exactly the keys of R37, in that order.
	position := -1
	for _, key := range keysOfR37 {
		at := strings.Index(raw, "\n  \""+key+"\": ")
		if at < 0 {
			t.Errorf("status --json has no key %q", key)
		} else if at < position {
			t.Errorf("key %q is out of the order of R37", key)
		}
		position = at
	}
	var generic map[string]any
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		t.Fatal(err)
	}
	if len(generic) != len(keysOfR37) {
		t.Errorf("status --json has %d keys, want %d", len(generic), len(keysOfR37))
	}
	for name, want := range nestedKeysOfR37 {
		object, ok := generic[name].(map[string]any)
		if list, isList := generic[name].([]any); isList && len(list) > 0 {
			object, ok = list[0].(map[string]any)
		}
		if !ok {
			t.Errorf("%s is not an object or a list with an object: %v", name, generic[name])
			continue
		}
		var got []string
		for key := range object {
			got = append(got, key)
		}
		if len(got) != len(want) {
			t.Errorf("%s has the keys %v, want %v", name, got, want)
		}
		for _, key := range want {
			if _, ok := object[key]; !ok {
				t.Errorf("%s has no key %q", name, key)
			}
		}
	}
	if !strings.HasSuffix(raw, "}\n") || strings.Contains(raw, "\t") || strings.Contains(raw, "\r") {
		t.Error("status --json is not indented JSON with a final newline")
	}
	// JSON keys and warning ids are not translated (R5).
	_, portuguese := statusOf(t, w, "--lang", "pt-BR")
	keys := regexp.MustCompile(`(?m)^\s*"([A-Za-z]+)": `)
	if !reflect.DeepEqual(keys.FindAllStringSubmatch(raw, -1), keys.FindAllStringSubmatch(portuguese, -1)) {
		t.Error("the keys of status --json depend on the language")
	}
}

func TestStatus(t *testing.T) {
	w := newWorld(t, platform.Windows, "wt-present")

	// Before any apply.
	before := w.tree()
	report, _ := statusOf(t, w)
	want := statusReport{
		Version: testVersion, OS: "windows", Root: w.root(), Applied: false, Lang: "en",
		Mode:    statusMode{"auto", "soft", "profile", "profile", "on"},
		Signals: &statusSignals{},
		Variant: "dark",
		Session: statusSession{Emulator: "other", PaletteRoute: "emulator"},
		WindowsTerminal: statusTerminal{
			Detected: true, SettingsPath: filepath.Join(filepath.Dir(w.fragment()), "..", "..", "..", "..", "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"),
			DefaultsOverride: []string{}, GUIDKnown: false,
		},
		Hooks:    []statusHook{},
		Warnings: []statusWarning{},
	}
	want.WindowsTerminal.SettingsPath = filepath.Clean(want.WindowsTerminal.SettingsPath)
	report.Warnings = []statusWarning{}
	if !reflect.DeepEqual(report, want) {
		t.Errorf("status before an apply:\n got %+v\nwant %+v", report, want)
	}
	text := w.must(0, "status")
	if !strings.Contains(text.out, "`sereno apply --dry-run`") {
		t.Errorf("status before an apply does not say how to start:\n%s", text.out)
	}

	// After an apply with a hook, in a Serena profile session of a light system.
	w.must(0, "apply", "--hook", "powershell")
	afterApply := w.tree()
	w.vars["WT_SESSION"], w.vars["SERENO_SESSION"], w.vars["SERENO_OS_MODE"] = "1", "wt-profile", "light"
	report, _ = statusOf(t, w)
	if !report.Applied || report.Variant != "light" || !report.Signals.AppsLight ||
		report.Session != (statusSession{"windows-terminal", true, "in-band"}) ||
		!report.WindowsTerminal.Fragment || !report.WindowsTerminal.Profile ||
		report.Owned != (statusOwned{Total: 5}) || report.Pwsh.OnPath ||
		!reflect.DeepEqual(report.Hooks, []statusHook{{"powershell", w.home.PSProfile, true}}) {
		t.Errorf("status after an apply: %+v", report)
	}
	// T12 (7): wt.theme-not-system, because the fixture's Windows Terminal
	// has no theme setting, and the note about PowerShell 7.
	if got := warningIDs(report); !reflect.DeepEqual(got, []string{WarnThemeNotSystem, WarnPwshAbsent}) {
		t.Errorf("warnings after an apply = %v", got)
	}
	for _, warning := range report.Warnings {
		if warning.ID == WarnPwshAbsent && !strings.Contains(warning.Text, "winget install -e --id Microsoft.PowerShell --source winget") {
			t.Errorf("pwsh.absent does not show the exact command: %s", warning.Text)
		}
	}

	// The text mirrors the JSON, in both languages.
	bothLanguages(t, func(t *testing.T, lang string) {
		text := w.must(0, "status", "--lang", lang)
		for _, fact := range []string{
			testVersion, w.root(), "light (Serena Light)", "in-band", "Windows Terminal", "powershell  " + w.home.PSProfile,
			WarnThemeNotSystem, WarnPwshAbsent, PwshInstallCommand, lang,
		} {
			if !strings.Contains(text.out, fact) {
				t.Errorf("the status text does not show %q:\n%s", fact, text.out)
			}
		}
		noMissingKey(t, text.out)
		if text.err != "" {
			t.Errorf("status wrote to standard error: %s", text.err)
		}
	})

	// status writes nothing (R37).
	sameTree(t, "status", afterApply, w.tree())
	_ = before
}

// TestStatusWarnings produces every warning id of R37 at least once, and the
// situations in which each must not appear.
func TestStatusWarnings(t *testing.T) {
	guid := winterminal.ProfileGUID()
	localState := func(w *world) string {
		return filepath.Join(w.home.LocalAppData, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState")
	}
	tests := []struct {
		name    string
		os      string
		fixture string
		apply   []string // nil: no apply
		then    func(w *world)
		want    []string
		text    string // must appear in the text of a warning
	}{
		{name: "Windows without Windows Terminal", os: platform.Windows, apply: []string{"apply"},
			want: []string{WarnTerminalAbsent, WarnPwshAbsent}, text: "sereno apply --hook powershell"},
		{name: "Windows Terminal with theme system", os: platform.Windows, fixture: "wt-present", apply: []string{"apply"},
			then: func(w *world) { w.write(filepath.Join(localState(w), "settings.json"), `{"theme": "system"}`) },
			want: []string{WarnPwshAbsent}},
		{name: "a light and dark theme pair", os: platform.Windows, fixture: "wt-present", apply: []string{"apply"},
			then: func(w *world) {
				w.write(filepath.Join(localState(w), "settings.json"), `{"theme": {"light": "light", "dark": "dark"}}`)
			},
			want: []string{WarnPwshAbsent}},
		{name: "theme dark by name", os: platform.Windows, fixture: "wt-present", apply: []string{"apply"},
			then: func(w *world) { w.write(filepath.Join(localState(w), "settings.json"), `{"theme": "dark"}`) },
			want: []string{WarnThemeNotSystem, WarnPwshAbsent}, text: "(it is dark)"},
		{name: "a fixed mode does not need the pair", os: platform.Windows, fixture: "wt-present", apply: []string{"apply"},
			then: func(w *world) { w.must(0, "theme", "dark") },
			want: []string{WarnPwshAbsent}},
		{name: "schemes only", os: platform.Windows, fixture: "wt-present", apply: []string{"apply", "--no-profile"},
			want: []string{WarnPwshAbsent}},
		{name: "profiles.defaults", os: platform.Windows, fixture: "wt-present", apply: []string{"apply"},
			then: func(w *world) {
				w.write(filepath.Join(localState(w), "settings.json"),
					`{"theme": "system", "profiles": {"defaults": {"font": {"face": "Consolas"}, "colorScheme": "Campbell"}, "list": []}}`)
			},
			want: []string{WarnDefaultsOverride, WarnPwshAbsent}, text: "colorScheme, font"},
		{name: "profile deleted in Windows Terminal", os: platform.Windows, fixture: "wt-present", apply: []string{"apply"},
			then: func(w *world) {
				w.write(filepath.Join(localState(w), "settings.json"), `{"theme": "system", "profiles": {"list": []}}`)
				w.write(filepath.Join(localState(w), "state.json"), `{"generatedProfiles": ["`+strings.ToUpper(guid)+`"]}`)
			},
			want: []string{WarnProfileSuppressed, WarnPwshAbsent},
			text: `{ "guid": "` + guid + `", "hidden": false, "name": "Serena", "source": "SerenaTerminal" }`},
		{name: "profile seen and still listed", os: platform.Windows, fixture: "wt-present", apply: []string{"apply"},
			then: func(w *world) {
				w.write(filepath.Join(localState(w), "settings.json"), `{"theme": "system", "profiles": {"list": [{"guid": "`+guid+`", "hidden": false}]}}`)
				w.write(filepath.Join(localState(w), "state.json"), `{"generatedProfiles": ["`+guid+`"]}`)
			},
			want: []string{WarnPwshAbsent}},
		{name: "settings.json is not JSON", os: platform.Windows, fixture: "wt-present", apply: []string{"apply"},
			then: func(w *world) { w.write(filepath.Join(localState(w), "settings.json"), `{"theme": `) },
			want: []string{WarnSettingsUnreadable, WarnPwshAbsent}, text: "settings.json"},
		{name: "state.json is not JSON", os: platform.Windows, fixture: "wt-present", apply: []string{"apply"},
			then: func(w *world) { w.write(filepath.Join(localState(w), "state.json"), `[]`) },
			want: []string{WarnThemeNotSystem, WarnSettingsUnreadable, WarnPwshAbsent}},
		{name: "an owned file edited", os: platform.Windows, fixture: "wt-present", apply: []string{"apply"},
			then: func(w *world) { w.write(filepath.Join(w.root(), "shell", "init.ps1"), "# mine\n") },
			want: []string{WarnThemeNotSystem, WarnOwnedModified, WarnPwshAbsent}, text: filepath.Join("shell", "init.ps1")},
		{name: "an owned file deleted", os: platform.Linux, fixture: "lf", apply: []string{"apply", "--hook", "bash"},
			then: func(w *world) {
				if err := os.Remove(filepath.Join(w.root(), "shell", "init.zsh")); err != nil {
					w.t.Fatal(err)
				}
				w.must(0, "theme", "dark")
			},
			want: []string{WarnOwnedMissing}, text: "`sereno apply`"},
		{name: "the hook line deleted by hand", os: platform.Linux, fixture: "lf", apply: []string{"apply", "--hook", "bash"},
			then: func(w *world) {
				w.write(filepath.Join(w.home.Dir, ".bashrc"), "# nothing of sereno's here\n")
				w.must(0, "theme", "dark")
			},
			want: []string{WarnHookMissing}, text: "sereno uninstall --hook bash"},
		{name: "the hook line commented out still counts", os: platform.Linux, fixture: "lf", apply: []string{"apply", "--hook", "bash"},
			then: func(w *world) {
				bashrc := filepath.Join(w.home.Dir, ".bashrc")
				w.write(bashrc, strings.Replace(w.read(bashrc), "[ -r", "# [ -r", 1))
				w.must(0, "theme", "dark")
			},
			want: []string{}},
		{name: "VS Code's terminal", os: platform.Windows, fixture: "wt-present", apply: []string{"apply", "--hook", "powershell"},
			then: func(w *world) {
				w.write(filepath.Join(localState(w), "settings.json"), `{"theme": "system"}`)
				w.vars["TERM_PROGRAM"] = "vscode"
				w.program("pwsh.exe")
			},
			want: []string{WarnVSCode}, text: "VS Code"},
		{name: "mode auto on Linux", os: platform.Linux, fixture: "lf", apply: []string{"apply"},
			want: []string{WarnUnixAuto}, text: "`sereno export`"},
		{name: "mode auto on macOS before an apply", os: platform.Darwin,
			want: []string{WarnUnixAuto}},
		{name: "Linux with SERENO_OS_MODE", os: platform.Linux, fixture: "lf", apply: []string{"apply"},
			then: func(w *world) { w.vars["SERENO_OS_MODE"] = "light"; w.must(0, "theme", "hc") },
			want: []string{}},
	}
	produced := map[string]bool{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := newWorld(t, test.os, test.fixture)
			if test.apply != nil {
				w.must(0, test.apply...)
			}
			if test.then != nil {
				test.then(w)
			}
			tree := w.tree()
			report, _ := statusOf(t, w)
			if got := warningIDs(report); !reflect.DeepEqual(got, test.want) {
				t.Errorf("warnings = %v, want %v", got, test.want)
			}
			all := ""
			for _, warning := range report.Warnings {
				produced[warning.ID] = true
				all += warning.Text + "\n"
				noMissingKey(t, warning.Text)
			}
			if !strings.Contains(all, test.text) {
				t.Errorf("no warning text holds %q:\n%s", test.text, all)
			}
			// The same warnings, by their ids, in the other language and in
			// the text for people.
			portuguese, _ := statusOf(t, w, "--lang", "pt-BR")
			if !reflect.DeepEqual(warningIDs(portuguese), test.want) {
				t.Errorf("warnings in pt-BR = %v", warningIDs(portuguese))
			}
			for i, warning := range portuguese.Warnings {
				if warning.Text == report.Warnings[i].Text {
					t.Errorf("%s has the same text in both languages", warning.ID)
				}
				noMissingKey(t, warning.Text)
			}
			text := w.must(0, "status")
			for _, id := range test.want {
				if !strings.Contains(text.out, "  "+id+"\n") {
					t.Errorf("the status text does not list %s", id)
				}
			}
			sameTree(t, "status", tree, w.tree())
		})
	}
	for _, id := range WarningIDs() {
		if !produced[id] {
			t.Errorf("no case produced the warning %s", id)
		}
	}
	if len(WarningIDs()) != 11 {
		t.Errorf("%d warning ids, R37 lists 11", len(WarningIDs()))
	}
}

func TestStatusOnUnix(t *testing.T) {
	w := newWorld(t, platform.Linux, "lf")
	w.program("pwsh")
	report, raw := statusOf(t, w)
	if report.Signals != nil || !strings.Contains(raw, "\n  \"signals\": null,\n") {
		t.Errorf("signals on Linux = %v, want null", report.Signals)
	}
	if report.OS != "linux" || report.Root != w.root() || report.WindowsTerminal.Detected || report.WindowsTerminal.SettingsPath != "" {
		t.Errorf("status on Linux: %+v", report)
	}
	text := w.must(0, "status")
	if strings.Contains(text.out, "\nWindows Terminal ") || strings.Contains(text.out, "settings.json") {
		t.Errorf("the status text talks about Windows Terminal on Linux:\n%s", text.out)
	}
	w.vars["SERENO_OS_MODE"] = "hc"
	if report, _ := statusOf(t, w); report.Signals == nil || !report.Signals.HighContrast || report.Variant != "hc" {
		t.Errorf("SERENO_OS_MODE=hc on Linux: %+v", report)
	}
}

// ---------------------------------------------------------------------------
// export (R38)

// exported lists the 24 theme files below a folder, keyed by their name with
// forward slashes.
func exported(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for path, state := range testenv.Snapshot(t, dir) {
		if strings.HasPrefix(state, "file:") {
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
			if err != nil {
				t.Fatal(err)
			}
			files[path] = string(data)
		}
	}
	return files
}

func TestExportToAFolder(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		w := newWorld(t, platform.Linux, "")
		destination := filepath.Join(w.home.Dir, "my themes")
		got := w.must(0, "export", "--to", destination, "--lang", lang)

		files := exported(t, destination)
		if len(files) != 24 {
			t.Fatalf("export wrote %d files, want 24", len(files))
		}
		for name, content := range files {
			if content != golden(t, "export/"+name) {
				t.Errorf("%s differs from the golden file", name)
			}
		}
		// Not tracked: no manifest, no folder of sereno's (R38).
		if _, err := os.Stat(w.root()); !os.IsNotExist(err) {
			t.Errorf("export --to created %s", w.root())
		}
		// Where the files go and the line that switches them on, per target.
		for _, fact := range []string{
			"theme = dark:Serena Dark,light:Serena Light",
			"dark-theme.auto.conf", "light-theme.auto.conf",
			filepath.Join(destination, "kitty", "Serena Dark.conf"), filepath.Join(destination, "kitty", "Serena Light.conf"),
			"config.color_scheme = 'Serena Dark'",
			`import = ["` + filepath.ToSlash(filepath.Join(destination, "alacritty", "serena-dark.toml")) + `"]`,
			filepath.Join(destination, "ghostty"), filepath.Join(destination, "wezterm"),
		} {
			if !strings.Contains(got.out, fact) {
				t.Errorf("export does not print %q:\n%s", fact, got.out)
			}
		}
		if n := strings.Count(got.out, "  create  "); n != 24 {
			t.Errorf("the plan has %d create lines, want 24", n)
		}
		noMissingKey(t, got.out)

		// Again: everything is kept.
		tree := w.tree()
		again := w.must(0, "export", "--to", destination, "--lang", lang)
		if strings.Contains(again.out, "  create  ") || strings.Count(again.out, "  keep    ") != 24 {
			t.Errorf("a second export did not keep the 24 files:\n%s", again.out)
		}
		sameTree(t, "second export", tree, w.tree())

		// A different file in the way is a refusal: nothing is written.
		other := filepath.Join(destination, "kitty", "Serena Dark.conf")
		w.write(other, "# my own\n")
		if err := os.Remove(filepath.Join(destination, "ghostty", "Serena Dark")); err != nil {
			t.Fatal(err)
		}
		tree = w.tree()
		refused := w.must(engine.ExitRefused, "export", "--to", destination, "--lang", lang)
		if refused.out != "" || !strings.Contains(refused.err, other+": ") || !strings.Contains(refused.err, "--to") {
			t.Errorf("the refusal does not name the file and the way out: %q %q", refused.out, refused.err)
		}
		sameTree(t, "refused export", tree, w.tree())
	})
}

func TestExportOneTarget(t *testing.T) {
	for _, target := range []string{"ghostty", "kitty", "wezterm", "alacritty"} {
		w := newWorld(t, platform.Darwin, "")
		destination := filepath.Join(w.home.Dir, "out")
		got := w.must(0, "export", "--target", target, "--to", destination)
		files := exported(t, destination)
		if len(files) != 6 {
			t.Errorf("--target %s wrote %d files, want 6", target, len(files))
		}
		for name := range files {
			if !strings.HasPrefix(name, target+"/") {
				t.Errorf("--target %s wrote %s", target, name)
			}
		}
		for _, other := range []string{"Ghostty: ", "kitty: ", "WezTerm: ", "Alacritty: "} {
			if strings.Contains(got.out, other) != strings.EqualFold(strings.TrimSuffix(other, ": "), target) {
				t.Errorf("--target %s: the instructions for %q are wrong:\n%s", target, other, got.out)
			}
		}
	}
	w := newWorld(t, platform.Darwin, "")
	w.must(0, "export", "--target=all", "--to", filepath.Join(w.home.Dir, "out"))
	if len(exported(t, filepath.Join(w.home.Dir, "out"))) != 24 {
		t.Error("--target all did not write 24 files")
	}
}

// TestExportIntoRoot: without --to the files go to ROOT/themes, which needs
// a previous apply (R4, exit code 3), and are recorded, so that uninstall
// removes them.
func TestExportIntoRoot(t *testing.T) {
	w := newWorld(t, platform.Linux, "lf")
	start := w.tree()
	refused := w.must(engine.ExitRefused, "export")
	if refused.out != "" || !strings.Contains(refused.err, "`sereno apply`") || !strings.Contains(refused.err, "--to DIR") {
		t.Errorf("export before an apply: %q %q", refused.out, refused.err)
	}
	sameTree(t, "export before an apply", start, w.tree())

	w.must(0, "apply")
	got := w.must(0, "export")
	themes := filepath.Join(w.root(), "themes")
	if len(exported(t, themes)) != 24 || !strings.Contains(got.out, "`sereno uninstall`") {
		t.Errorf("export into ROOT:\n%s", got.out)
	}
	if report, _ := statusOf(t, w); report.Owned != (statusOwned{Total: 4 + 24}) {
		t.Errorf("the theme files are not recorded: %+v", report.Owned)
	}
	// apply leaves them alone, export keeps them, uninstall removes them.
	tree := w.tree()
	w.must(0, "apply")
	w.must(0, "export", "--target", "kitty")
	sameTree(t, "apply and export again", tree, w.tree())
	w.must(0, "uninstall")
	sameTree(t, "uninstall after export", start, w.tree())
}

// ---------------------------------------------------------------------------
// apply: options, notices and refusals

// TestApplyKeepsOptions is R13: options that are not on the command line
// keep the values of the previous apply.
func TestApplyKeepsOptions(t *testing.T) {
	w := newWorld(t, platform.Windows, "wt-present")
	w.program("pwsh.exe")
	w.must(0, "apply", "--shell", "powershell", "--no-profile", "--keys", "always", "--prompt", "off")
	wantMode := "mode=auto\nflavor=soft\ninband=profile\nkeys=always\nprompt=off\n"
	if w.read(w.modeFile()) != wantMode {
		t.Errorf("mode file = %q", w.read(w.modeFile()))
	}
	if w.read(w.fragment()) != golden(t, "fragment/no-profile.json") {
		t.Error("--no-profile did not give the fragment without a profile")
	}
	manifest := w.read(w.manifest())

	// A plain apply changes nothing, although PowerShell 7 is on PATH and
	// the defaults would give a profile.
	tree := w.tree()
	again := w.must(0, "apply")
	if strings.Contains(again.out, "replace") {
		t.Errorf("a plain apply did not keep the recorded options:\n%s", again.out)
	}
	sameTree(t, "plain apply", tree, w.tree())
	if w.read(w.manifest()) != manifest {
		t.Error("the manifest changed")
	}

	// One option changes, the others stay.
	w.must(0, "apply", "--prompt", "on")
	if got := w.read(w.modeFile()); got != strings.Replace(wantMode, "prompt=off", "prompt=on", 1) {
		t.Errorf("mode file after --prompt on = %q", got)
	}
	if report, _ := statusOf(t, w); report.WindowsTerminal.Profile || !report.WindowsTerminal.Fragment {
		t.Errorf("--no-profile was forgotten: %+v", report.WindowsTerminal)
	}
	// The theme stays as well: apply never resets it.
	w.must(0, "theme", "hc")
	w.must(0, "apply", "--keys", "off")
	if got := w.read(w.modeFile()); got != "mode=hc\nflavor=soft\ninband=profile\nkeys=off\nprompt=on\n" {
		t.Errorf("mode file after theme and apply = %q", got)
	}
}

// TestApplyShell is `--shell` of R18.
func TestApplyShell(t *testing.T) {
	// auto without PowerShell 7: Windows PowerShell.
	w := newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply")
	if w.read(w.fragment()) != golden(t, "fragment/auto-soft.json") {
		t.Error("auto without pwsh.exe is not Windows PowerShell")
	}
	// pwsh without PowerShell 7: refused, nothing changed, the way out named.
	tree := w.tree()
	refused := w.must(engine.ExitRefused, "apply", "--shell", "pwsh")
	if refused.out != "" || !strings.Contains(refused.err, "pwsh.exe") || !strings.Contains(refused.err, "--shell auto") {
		t.Errorf("--shell pwsh without PowerShell 7: %q %q", refused.out, refused.err)
	}
	sameTree(t, "refused --shell pwsh", tree, w.tree())
	// Without a profile the shell does not matter.
	w.must(0, "apply", "--shell", "pwsh", "--no-profile")

	// auto with PowerShell 7, and powershell named in spite of it.
	w = newWorld(t, platform.Windows, "wt-present")
	w.program("pwsh.exe")
	w.must(0, "apply")
	if w.read(w.fragment()) != golden(t, "fragment/pwsh.json") {
		t.Error("auto with pwsh.exe on PATH is not PowerShell 7")
	}
	w.must(0, "apply", "--shell", "powershell")
	if w.read(w.fragment()) != golden(t, "fragment/auto-soft.json") {
		t.Error("--shell powershell did not give Windows PowerShell")
	}
	w.must(0, "apply", "--shell", "pwsh")
	if w.read(w.fragment()) != golden(t, "fragment/pwsh.json") {
		t.Error("--shell pwsh did not give PowerShell 7")
	}
}

// TestApplyNotices is R19 and what apply says besides: the two sentences
// after a fragment with a profile was written, and the stub sentence already
// in the dry run, before anything exists.
func TestApplyNotices(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		catalog := i18n.For(i18n.Lang(lang))
		stub, open := catalog.T("wt.notice.stub"), catalog.T("wt.notice.open")

		w := newWorld(t, platform.Windows, "wt-present")
		dry := w.must(0, "apply", "--dry-run", "--lang", lang)
		if !strings.Contains(dry.out, stub+"\n") || strings.Contains(dry.out, open) {
			t.Errorf("the dry run must say what Windows Terminal will write, and nothing about opening it:\n%s", dry.out)
		}
		if !strings.HasSuffix(dry.out, catalog.T("plan.nothing-changed")+"\n") {
			t.Errorf("the dry run does not end with the sentence of R13:\n%s", dry.out)
		}
		first := w.must(0, "apply", "--lang", lang)
		if !strings.Contains(first.out, stub+"\n"+open+"\n") || !strings.Contains(first.out, "`sereno uninstall`") {
			t.Errorf("the first apply does not print the notices of R19 and the way back:\n%s", first.out)
		}
		second := w.must(0, "apply", "--lang", lang)
		if strings.Contains(second.out, stub) || strings.Contains(second.out, open) || strings.Contains(second.out, "`sereno uninstall`") {
			t.Errorf("an apply that wrote nothing repeats the notices:\n%s", second.out)
		}
		// A fragment that is written again says it again. It is replaced
		// then, not created: a Windows Terminal that is running lists Serena
		// already, as the profile was before, which is why the second
		// sentence speaks of what the apply changed and not of a profile
		// that is not listed.
		w.program("pwsh.exe")
		replaced := w.must(0, "apply", "--shell", "pwsh", "--lang", lang)
		if !strings.Contains(replaced.out, "  replace "+w.fragment()) || !strings.Contains(replaced.out, stub+"\n"+open+"\n") {
			t.Errorf("an apply that replaces the fragment does not print the notices of R19 again:\n%s", replaced.out)
		}
		noMissingKey(t, replaced.out)
		changed := w.must(0, "theme", "dark", "--lang", lang)
		_ = changed
		w.must(0, "apply", "--no-profile", "--lang", lang)
		back := newWorld(t, platform.Windows, "wt-present")
		schemes := back.must(0, "apply", "--no-profile", "--lang", lang)
		if strings.Contains(schemes.out, stub) || strings.Contains(schemes.out, open) || !strings.Contains(schemes.out, catalog.T("apply.note.schemes-only")) {
			t.Errorf("--no-profile: the notices are about a profile that is not there:\n%s", schemes.out)
		}
		noMissingKey(t, dry.out, first.out, second.out, schemes.out)
	})
}

// TestApplyWithoutWindowsTerminal is R16: the fragment is skipped with a
// note, the exit code stays 0, and the user is told how the files that were
// written come into use.
func TestApplyWithoutWindowsTerminal(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		w := newWorld(t, platform.Windows, "")
		got := w.must(0, "apply", "--lang", lang)
		if !strings.Contains(got.out, "  skip    "+w.fragment()+" (") {
			t.Errorf("no skip line for the fragment:\n%s", got.out)
		}
		if _, err := os.Stat(filepath.Dir(filepath.Dir(w.fragment()))); !os.IsNotExist(err) {
			t.Error("a Windows Terminal folder was created on a machine without Windows Terminal")
		}
		if !strings.Contains(got.out, "sereno apply --hook powershell") {
			t.Errorf("apply does not say how to use the shell files:\n%s", got.out)
		}
		if strings.Contains(got.out, i18n.For(i18n.Lang(lang)).T("wt.notice.open")) {
			t.Error("apply talks about a Windows Terminal window on a machine without Windows Terminal")
		}
		noMissingKey(t, got.out)
		// Nothing of it is recorded: uninstall has no fragment to look for.
		if report, _ := statusOf(t, w); report.Owned.Total != 4 || report.WindowsTerminal.Fragment {
			t.Errorf("status: %+v", report)
		}
	})

	// macOS and Linux: no line about a fragment at all.
	w := newWorld(t, platform.Linux, "")
	got := w.must(0, "apply")
	if strings.Contains(got.out, "skip") || strings.Contains(got.out, "Windows Terminal") {
		t.Errorf("apply on Linux talks about Windows Terminal:\n%s", got.out)
	}
	if !strings.Contains(got.out, "sereno apply --hook bash") || !strings.Contains(got.out, "`sereno export`") {
		t.Errorf("apply on Linux does not say what to do next:\n%s", got.out)
	}
}

// TestApplyRefusals goes through what R4 lists under exit code 3 as far as
// the command decides it. Each refusal names what it is about and the way
// out, writes nothing, and prints nothing on standard output.
func TestApplyRefusals(t *testing.T) {
	restricted := &fakeProber{result: shells.ProbeResult{ProfilePath: `C:\Users\u\Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1`, Policy: "Restricted"}}
	if !filepath.IsAbs(restricted.result.ProfilePath) {
		restricted.result.ProfilePath = "/home/u/Documents/WindowsPowerShell/Microsoft.PowerShell_profile.ps1"
	}
	tests := []struct {
		name    string
		os      string
		fixture string
		prepare func(w *world)
		args    []string
		names   []string // what standard error must name
	}{
		{name: "a foreign file at the fragment path", os: platform.Windows, fixture: "wt-present",
			prepare: func(w *world) { w.write(w.fragment(), "{}\n") },
			args:    []string{"apply"}, names: []string{"sereno.json: ", "Move it away or delete it"}},
		{name: "a folder where the mode file belongs", os: platform.Linux,
			prepare: func(w *world) {
				if err := os.MkdirAll(w.modeFile(), 0o755); err != nil {
					w.t.Fatal(err)
				}
			},
			args: []string{"apply"}, names: []string{"mode: ", "Move it away or delete it"}},
		{name: "Windows PowerShell outside Windows", os: platform.Linux,
			args: []string{"apply", "--hook", "powershell"}, names: []string{"powershell: ", "pwsh"}},
		{name: "PowerShell 7 that is not installed", os: platform.Windows,
			args: []string{"apply", "--hook", "pwsh"}, names: []string{"pwsh: pwsh.exe", "--hook"}},
		{name: "pwsh that is not installed, on Linux", os: platform.Linux,
			args: []string{"apply", "--hook", "bash,pwsh"}, names: []string{"pwsh: pwsh ", "--hook"}},
		{name: "an execution policy that blocks profiles", os: platform.Windows,
			prepare: func(w *world) { w.vars["SERENO_PS_PROFILE_PATH"] = ""; w.prober = restricted },
			args:    []string{"apply", "--hook", "powershell"}, names: []string{"Restricted", shells.PolicyCommand}},
		{name: "a folder the profile cannot name", os: platform.Windows, fixture: "wt-present",
			prepare: func(w *world) { w.vars["SERENO_HOME"] = filepath.Join(w.home.Dir, "%NAME%", "sereno") },
			args:    []string{"apply"}, names: []string{"%NAME%", "--no-profile", "SERENO_HOME"}},
		{name: "a folder a hook line cannot carry", os: platform.Linux,
			prepare: func(w *world) { w.vars["SERENO_HOME"] = filepath.Join(w.home.Dir, "ação", "sereno") },
			args:    []string{"apply", "--hook", "bash"}, names: []string{"ação", "SERENO_HOME"}},
		{name: "two refusals are both printed", os: platform.Windows, fixture: "wt-present",
			prepare: func(w *world) { w.write(w.fragment(), "{}\n") },
			args:    []string{"apply", "--hook", "pwsh"}, names: []string{"sereno.json: ", "pwsh: pwsh.exe"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bothLanguages(t, func(t *testing.T, lang string) {
				w := newWorld(t, test.os, test.fixture)
				if test.prepare != nil {
					test.prepare(w)
				}
				tree := w.tree()
				for _, dry := range []bool{true, false} {
					args := append(append([]string{}, test.args...), "--lang", lang)
					if dry {
						args = append(args, "--dry-run")
					}
					got := w.must(engine.ExitRefused, args...)
					if got.out != "" {
						t.Errorf("a refused apply printed on standard output: %q", got.out)
					}
					for _, name := range test.names {
						if lang != "en" && strings.Contains(name, " it ") {
							continue // an English sentence
						}
						if !strings.Contains(got.err, name) {
							t.Errorf("the refusal does not name %q:\n%s", name, got.err)
						}
					}
					if !strings.HasPrefix(got.err, "sereno: ") || strings.Count(got.err, "\n") < 2 {
						t.Errorf("a refusal is a header and, for each reason, what it is about and the way out:\n%s", got.err)
					}
					noMissingKey(t, got.err)
					sameTree(t, "a refused apply", tree, w.tree())
				}
			})
		})
	}
	if restricted.asked == 0 {
		t.Error("the prober was never consulted")
	}
}

// TestApplyAsksPowerShellOnlyWhenItMust: the one child process of R20 is
// started for a PowerShell hook whose profile path the environment does not
// give, and never otherwise.
func TestApplyAsksPowerShellOnlyWhenItMust(t *testing.T) {
	w := newWorld(t, platform.Windows, "wt-present")
	profile := w.home.PSProfile
	prober := &fakeProber{result: shells.ProbeResult{ProfilePath: profile, Policy: "RemoteSigned"}}
	w.prober = prober
	for _, args := range [][]string{{"apply"}, {"apply", "--hook", "bash"}, {"status"}, {"theme"}, {"apply", "--hook", "powershell"}, {"uninstall"}} {
		w.must(0, args...)
	}
	if prober.asked != 0 {
		t.Errorf("PowerShell was asked %d times although SERENO_PS_PROFILE_PATH names the profile", prober.asked)
	}
	w.vars["SERENO_PS_PROFILE_PATH"] = ""
	w.must(0, "apply", "--hook", "powershell")
	if prober.asked != 1 {
		t.Errorf("PowerShell was asked %d times for one hook, want once", prober.asked)
	}
	if !strings.Contains(w.read(profile), engine.HookMarker) {
		t.Error("the hook line is not in the profile the prober named")
	}
	// A recorded hook is an option of the next apply (R13), which looks after
	// it in the file the manifest records and starts nothing (R20): not as a
	// dry run either, and not when it is another shell that is asked for.
	w.must(0, "apply")
	if prober.asked != 1 {
		t.Errorf("asked %d times after a second apply, which names no shell; want 1", prober.asked)
	}
	w.must(0, "apply", "--dry-run")
	if prober.asked != 1 {
		t.Errorf("asked %d times after a dry run that names no shell, want 1", prober.asked)
	}
	w.must(0, "apply", "--hook", "bash", "--dry-run")
	if prober.asked != 1 {
		t.Errorf("asked %d times after a dry run for bash, want 1", prober.asked)
	}
	// Named again, PowerShell is asked again, by a dry run too.
	w.must(0, "apply", "--hook", "powershell", "--dry-run")
	if prober.asked != 2 {
		t.Errorf("asked %d times after a dry run that names powershell, want 2", prober.asked)
	}
	w.must(0, "apply", "--hook", "powershell")
	if prober.asked != 3 {
		t.Errorf("asked %d times after an apply that names powershell again, want 3", prober.asked)
	}
	// status, theme, export and uninstall never ask (R37, R39).
	for _, args := range [][]string{{"status"}, {"theme", "dark"}, {"export"}, {"uninstall", "--dry-run"}, {"uninstall"}} {
		w.must(0, args...)
	}
	if prober.asked != 3 {
		t.Errorf("a command other than apply asked PowerShell (%d calls)", prober.asked)
	}
}

// recordedHook is a Windows machine on which `sereno apply --hook powershell`
// has asked PowerShell once and put the hook line into the profile it named,
// which is returned as first. Each case of the two tests below starts from a
// machine of its own, so that a step that fails ends that case and no other.
func recordedHook(t *testing.T) (w *world, prober *fakeProber, first string) {
	t.Helper()
	return recordedHookOn(t, newWorld(t, platform.Windows, "wt-present"))
}

// recordedHookOn is recordedHook on a machine the caller has prepared.
func recordedHookOn(t *testing.T, w *world) (_ *world, prober *fakeProber, first string) {
	t.Helper()
	first = w.home.PSProfile
	prober = &fakeProber{result: shells.ProbeResult{ProfilePath: first, Policy: "RemoteSigned"}}
	w.prober = prober
	w.vars["SERENO_PS_PROFILE_PATH"] = ""
	w.must(0, "apply", "--hook", "powershell")
	if prober.asked != 1 || !strings.Contains(w.read(first), engine.HookMarker) {
		t.Fatalf("asked %d times, and the hook line is not in %s", prober.asked, first)
	}
	return w, prober, first
}

// TestRecordedHookWhenPowerShellAnswersDifferently is what the amended R20
// gives up and what it keeps, while the file the manifest records is there.
// An apply that does not name the shell looks after the line in that file and
// asks nobody, so it does not notice that PowerShell has come to name another
// profile or that the execution policy has changed. An apply that names the
// shell still asks, and still refuses.
func TestRecordedHookWhenPowerShellAnswersDifferently(t *testing.T) {
	answers := []struct {
		name   string
		result func(w *world, first string) shells.ProbeResult
		naming string // what the refusal of an apply that names the shell says
	}{
		{"another profile", func(w *world, first string) shells.ProbeResult {
			return shells.ProbeResult{ProfilePath: filepath.Join(w.home.Dir, "OneDrive", "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"), Policy: "RemoteSigned"}
		}, "sereno uninstall --hook powershell"},
		{"a policy that loads no profile", func(w *world, first string) shells.ProbeResult {
			return shells.ProbeResult{ProfilePath: first, Policy: "Restricted"}
		}, shells.PolicyCommand},
	}
	for _, answer := range answers {
		t.Run(answer.name, func(t *testing.T) {
			w, prober, first := recordedHook(t)
			applied := w.tree()
			prober.result = answer.result(w, first)
			asked := prober.asked

			// Given up: the apply that names no shell keeps the line where it is.
			kept := w.must(0, "apply")
			if prober.asked != asked {
				t.Errorf("an apply that names no shell asked PowerShell (%d calls)", prober.asked-asked)
			}
			if !strings.Contains(kept.out, "keep") || !strings.Contains(kept.out, first) {
				t.Errorf("the plan does not keep the hook line in %s:\n%s", first, kept.out)
			}
			sameTree(t, "an apply that names no shell", applied, w.tree())

			// Kept: the apply that names the shell asks, and refuses.
			refused := w.must(engine.ExitRefused, "apply", "--hook", "powershell")
			if prober.asked != asked+1 {
				t.Errorf("an apply that names powershell asked %d times, want once", prober.asked-asked)
			}
			if !strings.Contains(refused.err, answer.naming) {
				t.Errorf("the refusal does not name %q:\n%s", answer.naming, refused.err)
			}
			noMissingKey(t, refused.err)
			sameTree(t, "a refused apply", applied, w.tree())
		})
	}
}

// TestRecordedHookWhoseFileIsGone is the limit of the same rule: the file
// the manifest records is looked after without a question only while it is
// there. When it is gone, an apply that names no shell must not make it
// again unasked. After a Documents folder has moved into OneDrive the
// profile is in the new place, with sereno's line in it, and a new file in
// the old place would be one PowerShell does not read, announced as a hook
// that takes effect. So PowerShell is asked, as if the shell had been named:
// a profile that moved is refused with the way out, and one the user deleted
// is made again only where PowerShell still names it and still loads it.
//
// The last two cases are the two ends of "there". Anything that stands at
// the recorded path counts, also what is no profile: nobody is asked, and
// the engine, which looks at that path itself, says what is wrong with it
// (R22). A link that leads nowhere does not count: PowerShell is asked.
func TestRecordedHookWhoseFileIsGone(t *testing.T) {
	t.Run("the profile moved and PowerShell names the new place", func(t *testing.T) {
		w, prober, first := recordedHook(t)
		moved := filepath.Join(w.home.Dir, "OneDrive", "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
		w.write(moved, w.read(first))
		if err := os.Remove(first); err != nil {
			t.Fatal(err)
		}
		prober.result = shells.ProbeResult{ProfilePath: moved, Policy: "RemoteSigned"}
		tree := w.tree()
		for _, args := range [][]string{{"apply"}, {"apply", "--dry-run"}, {"apply", "--hook", "bash"}} {
			asked := prober.asked
			refused := w.must(engine.ExitRefused, args...)
			if prober.asked != asked+1 {
				t.Errorf("sereno %s asked PowerShell %d times, want once: the recorded file is gone", strings.Join(args, " "), prober.asked-asked)
			}
			if !strings.Contains(refused.err, "sereno uninstall --hook powershell") || !strings.Contains(refused.err, first) {
				t.Errorf("the refusal of sereno %s does not name the recorded file and the way out:\n%s", strings.Join(args, " "), refused.err)
			}
			noMissingKey(t, refused.err)
			sameTree(t, "sereno "+strings.Join(args, " ")+" after the profile moved", tree, w.tree())
		}

		// The way out the refusal names, and the hook asked for again: the
		// line is looked after where PowerShell reads it, once, and nothing
		// is made in the old place. What that ends in at the uninstall is
		// TestUninstallAfterTheProfileMoved.
		w.must(0, "uninstall", "--hook", "powershell")
		again := w.must(0, "apply", "--hook", "powershell")
		if !strings.Contains(again.out, moved) {
			t.Errorf("the plan does not name the profile PowerShell reads now:\n%s", again.out)
		}
		if got := strings.Count(w.read(moved), engine.HookMarker); got != 1 {
			t.Errorf("the profile PowerShell reads now holds %d hook lines, want 1", got)
		}
		if _, err := os.Lstat(first); !os.IsNotExist(err) {
			t.Errorf("a file was made again in the old place %s (%v)", first, err)
		}
	})

	t.Run("the profile was deleted and PowerShell names the same place", func(t *testing.T) {
		w, prober, first := recordedHook(t)
		if err := os.Remove(first); err != nil {
			t.Fatal(err)
		}
		tree := w.tree()
		dry := w.must(0, "apply", "--dry-run")
		if prober.asked != 2 {
			t.Errorf("a dry run asked PowerShell %d times in all, want 2: the recorded file is gone", prober.asked)
		}
		if !strings.Contains(dry.out, "create") || !strings.Contains(dry.out, first) {
			t.Errorf("the plan of the dry run does not create %s:\n%s", first, dry.out)
		}
		sameTree(t, "a dry run after the profile was deleted", tree, w.tree())

		made := w.must(0, "apply", "--lang", "en")
		if prober.asked != 3 {
			t.Errorf("the apply asked PowerShell %d times in all, want 3", prober.asked)
		}
		if got := strings.Count(w.read(first), engine.HookMarker); got != 1 {
			t.Errorf("the profile that was made again holds %d hook lines, want 1", got)
		}
		if !strings.Contains(made.out, i18n.For(i18n.English).T("apply.note.hooked", first)) {
			t.Errorf("the apply does not say that the hook line was added to %s:\n%s", first, made.out)
		}
		// The file is there again: the next apply asks nobody.
		w.must(0, "apply")
		if prober.asked != 3 {
			t.Errorf("an apply that names no shell asked PowerShell although the recorded file is there (%d calls in all)", prober.asked)
		}
	})

	t.Run("the profile was deleted and the policy loads none now", func(t *testing.T) {
		w, prober, first := recordedHook(t)
		if err := os.Remove(first); err != nil {
			t.Fatal(err)
		}
		prober.result = shells.ProbeResult{ProfilePath: first, Policy: "Restricted"}
		tree := w.tree()
		refused := w.must(engine.ExitRefused, "apply")
		if prober.asked != 2 {
			t.Errorf("the apply asked PowerShell %d times in all, want 2", prober.asked)
		}
		if !strings.Contains(refused.err, shells.PolicyCommand) {
			t.Errorf("the refusal does not name %q:\n%s", shells.PolicyCommand, refused.err)
		}
		noMissingKey(t, refused.err)
		sameTree(t, "a refused apply", tree, w.tree())
	})

	t.Run("a folder stands where the profile was", func(t *testing.T) {
		w, prober, first := recordedHook(t)
		if err := os.Remove(first); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(first, 0o755); err != nil {
			t.Fatal(err)
		}
		tree := w.tree()
		refused := w.must(engine.ExitRefused, "apply", "--lang", "en")
		if prober.asked != 1 {
			t.Errorf("the apply asked PowerShell %d times in all, want 1: something stands at the recorded path, and the engine is the one to judge it", prober.asked)
		}
		if want := first + ": " + i18n.For(i18n.English).T(engine.ReasonNotRegular); !strings.Contains(refused.err, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, refused.err)
		}
		noMissingKey(t, refused.err)
		sameTree(t, "a refused apply", tree, w.tree())
	})

	t.Run("a link that leads nowhere stands where the profile was", func(t *testing.T) {
		w, prober, first := recordedHook(t)
		if err := os.Remove(first); err != nil {
			t.Fatal(err)
		}
		testenv.Symlink(t, filepath.Join(filepath.Dir(first), "nowhere.ps1"), first)
		tree := w.tree()
		refused := w.must(engine.ExitRefused, "apply", "--lang", "en")
		if prober.asked != 2 {
			t.Errorf("the apply asked PowerShell %d times in all, want 2: a link that leads nowhere is no file that is there", prober.asked)
		}
		if want := first + ": " + i18n.For(i18n.English).T(engine.ReasonSymlink); !strings.Contains(refused.err, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, refused.err)
		}
		noMissingKey(t, refused.err)
		sameTree(t, "a refused apply", tree, w.tree())
	})
}

// TestUninstallAfterTheProfileMoved is where a profile that has moved ends,
// at the uninstall. The rule that uninstall takes back exactly what apply
// wrote does not hold there, and the documents say so (docs/uninstall.md,
// docs/support-matrix.md, both READMEs): this test is what holds them to it.
//
// A Documents folder that moves takes the profile along, with sereno's line
// in it. Uninstall looks for the line in the file the manifest records, and
// nowhere else, so an uninstall right after the move leaves the line in the
// moved file. The way out an apply names (`sereno uninstall --hook
// powershell`, then `sereno apply --hook powershell`) ends in the same: the
// apply finds the line in the file PowerShell names now, keeps it, and what
// an apply finds in a file was there before sereno (R22), so the uninstall
// leaves it, and the file, also one sereno itself had created, and its plan
// names neither. Only when the line is taken out of the moved file between
// the two commands does sereno add it itself, and take it back.
func TestUninstallAfterTheProfileMoved(t *testing.T) {
	english := i18n.For(i18n.English)
	// moved is a machine with a recorded hook whose Documents folder has
	// gone into OneDrive, and a PowerShell that names the profile there.
	// before is the profile as it was before sereno, "" when there was none.
	moved := func(t *testing.T, created bool) (w *world, prober *fakeProber, first, now, before string) {
		t.Helper()
		w = newWorld(t, platform.Windows, "wt-present")
		documents := filepath.Join(w.home.Dir, "Documents")
		if created {
			if err := os.RemoveAll(documents); err != nil {
				t.Fatal(err)
			}
		} else {
			before = w.read(w.home.PSProfile)
		}
		w, prober, first = recordedHookOn(t, w)
		oneDrive := filepath.Join(w.home.Dir, "OneDrive")
		if err := os.Mkdir(oneDrive, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(documents, filepath.Join(oneDrive, "Documents")); err != nil {
			t.Fatal(err)
		}
		now = filepath.Join(oneDrive, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
		prober.result = shells.ProbeResult{ProfilePath: now, Policy: "RemoteSigned"}
		return w, prober, first, now, before
	}
	// left checks what an uninstall left behind: the moved profile as it
	// was, sereno's line in it, without a word about it in the plan; and of
	// sereno's folder the copy of a profile that was there before sereno,
	// or nothing at all.
	left := func(t *testing.T, w *world, uninstall result, now, with, before string) {
		t.Helper()
		if got := w.read(now); got != with || strings.Count(got, engine.HookMarker) != 1 {
			t.Errorf("the moved profile is %q after the uninstall, want it as it was, with sereno's line: %q", got, with)
		}
		if strings.Contains(uninstall.out+uninstall.err, now) {
			t.Errorf("the uninstall names the moved profile, which the documents say it does not look for:\n%s", uninstall.out)
		}
		noMissingKey(t, uninstall.out, uninstall.err)
		backups, err := os.ReadDir(filepath.Join(w.root(), engine.BackupsDir))
		if before == "" {
			if _, err := os.Lstat(w.root()); !os.IsNotExist(err) {
				t.Errorf("sereno's folder is still there after the uninstall (%v)", err)
			}
			return
		}
		if err != nil || len(backups) != 1 {
			t.Fatalf("backups holds %d files (%v), want the one copy of the profile", len(backups), err)
		}
		if got := w.read(filepath.Join(w.root(), engine.BackupsDir, backups[0].Name())); got != before {
			t.Errorf("the copy in backups is %q, want the profile as it was before sereno: %q", got, before)
		}
	}
	for _, home := range []struct {
		name    string
		created bool
	}{
		{"a profile that was there before sereno", false},
		{"a profile sereno created", true},
	} {
		t.Run(home.name+", uninstalled right after the move", func(t *testing.T) {
			w, prober, _, now, before := moved(t, home.created)
			with := w.read(now)
			uninstall := w.must(0, "uninstall", "--lang", "en")
			left(t, w, uninstall, now, with, before)
			if prober.asked != 1 {
				t.Errorf("the uninstall asked PowerShell (%d calls in all)", prober.asked)
			}
		})

		t.Run(home.name+", uninstalled after the way out", func(t *testing.T) {
			w, _, first, now, before := moved(t, home.created)
			with := w.read(now)
			w.must(engine.ExitRefused, "apply")
			w.must(0, "uninstall", "--hook", "powershell")
			again := w.must(0, "apply", "--hook", "powershell", "--lang", "en")
			if want := "keep    " + now + " (" + english.T(engine.NoteHookPresent) + ")"; !strings.Contains(again.out, want) {
				t.Errorf("the plan of the second apply does not keep the line it found in the moved profile (%q):\n%s", want, again.out)
			}
			if _, err := os.Lstat(first); !os.IsNotExist(err) {
				t.Errorf("a file was made again in the old place %s (%v)", first, err)
			}
			uninstall := w.must(0, "uninstall", "--lang", "en")
			left(t, w, uninstall, now, with, before)
		})
	}

	t.Run("the line is deleted from the moved profile on the way out", func(t *testing.T) {
		w, _, _, now, before := moved(t, false)
		w.must(0, "uninstall", "--hook", "powershell")
		w.write(now, before)
		again := w.must(0, "apply", "--hook", "powershell", "--lang", "en")
		if !strings.Contains(again.out, "append  "+now) || strings.Count(w.read(now), engine.HookMarker) != 1 {
			t.Errorf("the second apply did not add the line to the moved profile itself:\n%s", again.out)
		}
		w.must(0, "uninstall")
		if got := w.read(now); got != before {
			t.Errorf("the moved profile is %q after the uninstall, want it as it was before sereno: %q", got, before)
		}
		if _, err := os.Lstat(w.root()); !os.IsNotExist(err) {
			t.Errorf("sereno's folder is still there after the uninstall (%v)", err)
		}
	})
}

// TestRecordedHookOfAShellThatIsGone: a hook is an option of every later
// apply (R13). When its shell has left the machine, the refusal says how to
// withdraw the hook.
func TestRecordedHookOfAShellThatIsGone(t *testing.T) {
	w := newWorld(t, platform.Windows, "wt-present")
	w.program("pwsh.exe")
	w.must(0, "apply", "--hook", "pwsh")
	if err := os.Remove(filepath.Join(w.home.Dir, "bin", "pwsh.exe")); err != nil {
		t.Fatal(err)
	}
	refused := w.must(engine.ExitRefused, "apply", "--shell", "powershell")
	if !strings.Contains(refused.err, "sereno uninstall --hook pwsh") {
		t.Errorf("the refusal does not say how to withdraw the hook:\n%s", refused.err)
	}
	w.must(0, "uninstall", "--hook", "pwsh")
	w.must(0, "apply", "--shell", "powershell")
}

// ---------------------------------------------------------------------------
// Failures (exit code 1)

// TestInterruptedApply is the interrupt of R13 at the level of the command:
// the run stops between two operations with exit code 1, says so, and
// leaves a manifest that the next apply and the next uninstall can work
// from.
func TestInterruptedApply(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		w := newWorld(t, platform.Windows, "wt-present")
		start := w.tree()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		w.ctx = ctx
		got := w.must(engine.ExitFailure, "apply", "--hook", "powershell", "--lang", lang)
		if !strings.Contains(got.err, "sereno: ") || !strings.Contains(got.err, "`sereno uninstall`") || strings.Count(got.err, "\n") != 2 {
			t.Errorf("an interrupted apply should say that it stopped and how to go on:\n%s", got.err)
		}
		noMissingKey(t, got.err)
		sameTree(t, "interrupted before the first operation", start, w.tree())

		// Not interrupted any more: the same command finishes the job.
		w.ctx = nil
		w.must(0, "apply", "--hook", "powershell", "--lang", lang)
		w.must(0, "uninstall", "--lang", lang)
		sameTree(t, "apply and uninstall after an interrupt", start, w.tree())
	})
}

// TestSandbox is the last sentence of R10 at the level of the command: with
// SERENO_SANDBOX set, a write outside it ends the command with exit code 1
// before it happens.
func TestSandbox(t *testing.T) {
	w := newWorld(t, platform.Linux, "lf")
	outside := testenv.TempDir(t)
	for _, args := range [][]string{
		{"export", "--to", filepath.Join(outside, "themes")},
		{"export", "--to", outside},
	} {
		got := w.must(engine.ExitFailure, args...)
		if !strings.Contains(got.err, engine.SandboxEnv) || !strings.Contains(got.err, outside) {
			t.Errorf("sereno %v: the message does not name the sandbox:\n%s", args, got.err)
		}
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Errorf("something was written outside the sandbox: %v %v", entries, err)
	}

	// An apply whose folder is outside the sandbox.
	w.vars["SERENO_HOME"] = filepath.Join(outside, "sereno")
	got := w.must(engine.ExitFailure, "apply")
	if !strings.Contains(got.err, engine.SandboxEnv) {
		t.Errorf("apply outside the sandbox:\n%s", got.err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("apply wrote outside the sandbox: %v", entries)
	}
}

// TestBrokenInstallation: a manifest that cannot be read ends every command
// that needs it with exit code 1 and a way out, and nothing is changed; an
// environment without a place for sereno's folder does the same.
func TestBrokenInstallation(t *testing.T) {
	w := newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply")
	w.write(w.manifest(), "{ not json")
	tree := w.tree()
	for _, args := range [][]string{{"apply"}, {"apply", "--dry-run"}, {"uninstall"}, {"status"}, {"status", "--json"}, {"theme"}, {"theme", "dark"}, {"export"}} {
		bothLanguages(t, func(t *testing.T, lang string) {
			got := w.must(engine.ExitFailure, append(args, "--lang", lang)...)
			if got.out != "" || !strings.Contains(got.err, w.manifest()) || !strings.Contains(got.err, engine.HookMarker) {
				t.Errorf("sereno %v with a broken manifest: %q %q", args, got.out, got.err)
			}
			noMissingKey(t, got.err)
		})
	}
	sameTree(t, "a broken manifest", tree, w.tree())

	// Commands that need no folder still work.
	for _, args := range [][]string{{"version"}, {"help"}, {"check"}} {
		w.must(0, args...)
	}

	for _, test := range []struct {
		os   string
		vars map[string]string
	}{
		{platform.Windows, map[string]string{"LOCALAPPDATA": ""}},
		{platform.Linux, map[string]string{"HOME": "", "XDG_DATA_HOME": ""}},
		{platform.Linux, map[string]string{"SERENO_HOME": "relative/sereno"}},
	} {
		w := newWorld(t, test.os, "")
		for key, value := range test.vars {
			w.vars[key] = value
		}
		for _, args := range [][]string{{"apply"}, {"status"}, {"uninstall"}, {"theme"}} {
			if got := w.must(engine.ExitFailure, args...); got.out != "" || !strings.HasPrefix(got.err, "sereno: ") {
				t.Errorf("sereno %v on %s with %v: %q %q", args, test.os, test.vars, got.out, got.err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// What apply writes, and what uninstall says it leaves

// TestApplyWritesTheRenderedFiles: the init files and the mode file on disk
// are, byte for byte, the golden files the shell tests run against. That is
// what lets those tests speak for an installed sereno.
func TestApplyWritesTheRenderedFiles(t *testing.T) {
	for _, goos := range []string{platform.Windows, platform.Linux, platform.Darwin} {
		w := newWorld(t, goos, "wt-present")
		w.must(0, "apply")
		for _, name := range []string{"init.ps1", "init.bash", "init.zsh"} {
			if w.read(filepath.Join(w.root(), "shell", name)) != golden(t, "init/"+name) {
				t.Errorf("%s: shell/%s is not the golden init file", goos, name)
			}
		}
		if w.read(w.modeFile()) != defaultMode {
			t.Errorf("%s: mode file = %q", goos, w.read(w.modeFile()))
		}
		// The manifest is deterministic: no time, no machine fact besides
		// the paths (R12).
		manifest := w.read(w.manifest())
		if strings.Contains(manifest, "\r") || strings.Contains(manifest, "\t") || !strings.HasSuffix(manifest, "}\n") || !strings.Contains(manifest, `"version": "`+testVersion+`"`) {
			t.Errorf("%s: the manifest is not in its canonical form:\n%s", goos, manifest)
		}
	}
}

// TestUninstallSaysWhatRemains is the last sentence of R39: what uninstall
// does not take back is said, and only where it applies. The entry Windows
// Terminal wrote for the profile is mentioned when Windows Terminal has seen
// the profile, or when that cannot be found out. What sereno's own fragment
// holds at the moment does not matter: the entry outlives a profile that an
// apply with --no-profile took out again (see TestUninstallAfterNoProfile).
func TestUninstallSaysWhatRemains(t *testing.T) {
	guid := winterminal.ProfileGUID()
	localState := func(w *world) string {
		return filepath.Join(w.home.LocalAppData, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState")
	}
	tests := []struct {
		name     string
		os       string
		fixture  string
		apply    []string
		then     func(w *world)
		terminal bool // the sentence about Windows Terminal is expected
	}{
		{"Windows Terminal never saw the profile", platform.Windows, "wt-present", []string{"apply"}, nil, false},
		{"the entry is in settings.json", platform.Windows, "wt-present", []string{"apply"}, func(w *world) {
			w.write(filepath.Join(localState(w), "settings.json"), `{"profiles": {"list": [{"guid": "`+guid+`", "hidden": false, "name": "Serena", "source": "SerenaTerminal"}]}}`)
		}, true},
		{"only state.json knows the profile", platform.Windows, "wt-present", []string{"apply"}, func(w *world) {
			w.write(filepath.Join(localState(w), "state.json"), `{"generatedProfiles": ["`+guid+`"]}`)
		}, true},
		{"settings.json cannot be read", platform.Windows, "wt-present", []string{"apply"}, func(w *world) {
			w.write(filepath.Join(localState(w), "settings.json"), `{{`)
		}, true},
		{"schemes only, and Windows Terminal never saw a profile", platform.Windows, "wt-present", []string{"apply", "--no-profile"}, nil, false},
		{"schemes only, and Windows Terminal knows the profile from before", platform.Windows, "wt-present", []string{"apply", "--no-profile"}, func(w *world) {
			w.write(filepath.Join(localState(w), "state.json"), `{"generatedProfiles": ["`+guid+`"]}`)
		}, true},
		{"no Windows Terminal", platform.Windows, "", []string{"apply"}, nil, false},
		{"Linux", platform.Linux, "wt-present", []string{"apply"}, nil, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bothLanguages(t, func(t *testing.T, lang string) {
				catalog := i18n.For(i18n.Lang(lang))
				w := newWorld(t, test.os, test.fixture)
				w.must(0, test.apply...)
				if test.then != nil {
					test.then(w)
				}
				program := filepath.Join(w.home.Dir, "bin", Program)
				for _, args := range [][]string{{"uninstall", "--dry-run"}, {"uninstall"}} {
					got := w.must(0, append(args, "--lang", lang)...)
					if strings.Contains(got.out, catalog.T("uninstall.remains.terminal")) != test.terminal {
						t.Errorf("sereno %v: the sentence about Windows Terminal: want %v\n%s", args, test.terminal, got.out)
					}
					if !strings.Contains(got.out, program) {
						t.Errorf("sereno %v does not print the path of the program (R39):\n%s", args, got.out)
					}
					noMissingKey(t, got.out)
				}
				// A second uninstall has nothing to do and says so.
				if got := w.must(0, "uninstall", "--lang", lang); !strings.Contains(got.out, w.root()) || strings.Contains(got.out, "plan") {
					t.Errorf("uninstall with nothing applied:\n%s", got.out)
				}
			})
		})
	}

	// ROOT/bin belongs to the install script: it stays, and uninstall says so
	// (R10).
	w := newWorld(t, platform.Linux, "lf")
	bin := filepath.Join(w.root(), "bin")
	w.write(filepath.Join(bin, Program), "the program\n")
	before := w.tree()
	w.must(0, "apply")
	got := w.must(0, "uninstall")
	if !strings.Contains(got.out, bin+" ") {
		t.Errorf("uninstall does not say that it left %s alone:\n%s", bin, got.out)
	}
	sameTree(t, "uninstall with a bin folder", before, w.tree())
}

// TestThemeSaysWhenItTakesEffect: after a change on Windows, one sentence
// says when sessions notice; a command that wrote nothing does not say it.
func TestThemeSaysWhenItTakesEffect(t *testing.T) {
	note := i18n.For(i18n.English).T("theme.changed")
	w := newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply")
	if got := w.must(0, "theme", "light"); !strings.Contains(got.out, note) {
		t.Errorf("a change does not say when it takes effect:\n%s", got.out)
	}
	for _, args := range [][]string{{"theme", "light"}, {"theme"}, {"theme", "--show"}} {
		if got := w.must(0, args...); strings.Contains(got.out, note) {
			t.Errorf("sereno %v wrote nothing and still talks about a new setting:\n%s", args, got.out)
		}
	}
}

// TestReportLabelsFit keeps the label column of `sereno theme` and `sereno
// status` straight in both languages.
func TestReportLabelsFit(t *testing.T) {
	labels := []string{"mode", "flavor", "inband", "keys", "prompt", "Windows Terminal", "PowerShell 7"}
	for _, lang := range []i18n.Lang{i18n.English, i18n.Portuguese} {
		catalog := i18n.For(lang)
		for _, key := range i18n.Keys() {
			if strings.HasPrefix(key, "label.") {
				labels = append(labels, catalog.T(key))
			}
		}
	}
	if len(labels) < 25 {
		t.Fatalf("only %d labels found", len(labels))
	}
	for _, label := range labels {
		if n := len([]rune(label)); n > labelWidth {
			t.Errorf("the label %q has %d characters; the column has %d", label, n, labelWidth)
		}
	}
	w := newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply", "--hook", "powershell")
	for _, lang := range []string{"en", "pt-BR"} {
		for _, line := range strings.Split(w.must(0, "status", "--lang", lang).out, "\n") {
			if line == "" {
				break // the warnings follow, in a layout of their own
			}
			// A label or, on a continuation line, blanks; then two blanks;
			// then the value.
			runes := []rune(line)
			if len(runes) < labelWidth+3 || string(runes[labelWidth:labelWidth+2]) != "  " || runes[labelWidth+2] == ' ' {
				t.Errorf("status in %s: the value does not start in column %d: %q", lang, labelWidth+3, line)
			}
		}
	}
}

// TestApplyOrder is the order of phase 2 in R13: directories, backups of the
// files about to be edited, the manifest, the owned files, the hook lines.
// Among the owned files the fragment comes last, so that Windows Terminal
// never finds a profile whose init file is not written yet.
func TestApplyOrder(t *testing.T) {
	w := newWorld(t, platform.Windows, "wt-present")
	got := w.must(0, "apply", "--hook", "powershell")
	var order []string
	for _, line := range strings.Split(got.out, "\n") {
		if len(line) < 11 || !strings.HasPrefix(line, "  ") || line[2] == ' ' {
			continue
		}
		path := line[10:]
		if i := strings.Index(path, " ("); i >= 0 {
			path = path[:i]
		}
		name := filepath.Base(path)
		if strings.HasSuffix(name, ".bak") {
			name = "backup"
		}
		order = append(order, strings.TrimSpace(line[2:10])+" "+name)
	}
	want := []string{
		"mkdir sereno", "mkdir shell", "mkdir SerenaTerminal", "mkdir backups",
		"create backup",
		"create manifest.json",
		"create init.ps1", "create init.bash", "create init.zsh", "create mode", "create sereno.json",
		"append Microsoft.PowerShell_profile.ps1",
	}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("the plan of a first apply:\n got %v\nwant %v", order, want)
	}
}
