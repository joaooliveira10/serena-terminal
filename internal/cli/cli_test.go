package cli

import (
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/i18n"
	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
)

// missingKey matches what a catalogue returns for a key it does not have.
var missingKey = regexp.MustCompile(`![a-z0-9]+(?:[.-][a-z0-9]+)+!`)

// usageOfR3 is the command list of R3, word for word.
var usageOfR3 = []string{
	"apply [--dry-run] [--hook LIST] [--shell auto|powershell|pwsh] [--no-profile] [--keys profile|always|off] [--prompt on|off]",
	"uninstall [--dry-run] [--hook LIST]",
	"theme [auto|dark|light|hc|hc-light] [--flavor soft|vivid] [--inband profile|always|off] [--show] [--paint VARIANT] [--reset]",
	"status [--json]",
	"check [--json|--markdown]",
	"export [--target all|ghostty|kitty|wezterm|alacritty] [--to DIR]",
	"version",
	"help [COMMAND]",
}

// TestCommandsAreThoseOfR3 pins the commands, their usage lines and the
// flags the parser accepts to the text of the requirement.
func TestCommandsAreThoseOfR3(t *testing.T) {
	if len(commands) != len(usageOfR3) {
		t.Fatalf("%d commands, want %d", len(commands), len(usageOfR3))
	}
	flagInUsage := regexp.MustCompile(`--([a-z-]+)`)
	for i, cmd := range commands {
		if cmd.usage != usageOfR3[i] {
			t.Errorf("usage of %s:\n got %s\nwant %s", cmd.name, cmd.usage, usageOfR3[i])
		}
		if !strings.HasPrefix(cmd.usage, cmd.name) {
			t.Errorf("the usage of %s does not start with its name", cmd.name)
		}
		var inUsage, accepted []string
		for _, match := range flagInUsage.FindAllStringSubmatch(cmd.usage, -1) {
			inUsage = append(inUsage, match[1])
		}
		for _, flag := range cmd.flags {
			accepted = append(accepted, flag.name)
			// Every value the usage line offers is accepted, and no other.
			if flag.values != nil && !flag.list && flag.arg != "VARIANT" && flag.arg != strings.Join(flag.values, "|") {
				t.Errorf("%s --%s: the help shows %q, the parser accepts %v", cmd.name, flag.name, flag.arg, flag.values)
			}
		}
		if !reflect.DeepEqual(inUsage, accepted) {
			t.Errorf("%s: the usage line names the flags %v, the parser accepts %v", cmd.name, inUsage, accepted)
		}
	}
	if got := variantValues(); !reflect.DeepEqual(got, []string{"dark", "dark-vivid", "light", "light-vivid", "hc", "hc-light"}) {
		t.Errorf("VARIANT is one of %v", got)
	}
	if got := hookValues(); !reflect.DeepEqual(got, []string{"powershell", "pwsh", "bash", "zsh"}) {
		t.Errorf("LIST is made of %v", got)
	}
	if !reflect.DeepEqual(Commands(), []string{"apply", "uninstall", "theme", "status", "check", "export", "version", "help"}) {
		t.Errorf("Commands() = %v", Commands())
	}
}

// TestUsageErrors is the last sentence of R3: an unknown command, flag or
// value prints usage on standard error and exits with 2. Each complaint is
// three lines: what is wrong, the usage, and where the details are. Nothing
// is printed on standard output and nothing is written.
func TestUsageErrors(t *testing.T) {
	tests := []struct {
		args    []string
		command string // the command whose usage is shown; "" for the general one
		names   string // what the complaint must name
	}{
		{[]string{"frob"}, "", "frob"},
		{[]string{"--frob"}, "", "--frob"},
		{[]string{"--frob", "apply"}, "", "--frob"},
		{[]string{"apply", "--frob"}, "apply", "--frob"},
		{[]string{"apply", "-dry-run"}, "apply", "--dry-run"},
		{[]string{"apply", "---dry-run"}, "apply", "--dry-run"},
		{[]string{"apply", "--dry-run=yes"}, "apply", "--dry-run"},
		{[]string{"apply", "--shell", "fish"}, "apply", "auto, powershell, pwsh"},
		{[]string{"apply", "--shell=fish"}, "apply", "fish"},
		{[]string{"apply", "--shell"}, "apply", "auto|powershell|pwsh"},
		{[]string{"apply", "--shell", "--dry-run"}, "apply", "--shell"},
		{[]string{"apply", "--hook", "bash,fish"}, "apply", "fish"},
		{[]string{"apply", "--hook", "bash,"}, "apply", "--hook"},
		{[]string{"apply", "--hook", ""}, "apply", "--hook"},
		{[]string{"apply", "--keys", "sometimes"}, "apply", "profile, always, off"},
		{[]string{"apply", "--prompt", "yes"}, "apply", "on, off"},
		{[]string{"apply", "now"}, "apply", "now"},
		{[]string{"apply", "--show"}, "apply", "--show"},
		{[]string{"uninstall", "--hook"}, "uninstall", "LIST"},
		{[]string{"uninstall", "--no-profile"}, "uninstall", "--no-profile"},
		{[]string{"theme", "purple"}, "theme", "auto, dark, light, hc, hc-light"},
		{[]string{"theme", "dark", "light"}, "theme", "light"},
		{[]string{"theme", "--flavor", "loud"}, "theme", "soft, vivid"},
		{[]string{"theme", "--inband", "on"}, "theme", "profile, always, off"},
		{[]string{"theme", "--paint", "plaid"}, "theme", "dark-vivid"},
		{[]string{"theme", "--paint"}, "theme", "VARIANT"},
		{[]string{"theme", "--paint", "dark", "--reset"}, "theme", "--reset"},
		{[]string{"theme", "--paint", "dark", "light"}, "theme", "--paint"},
		{[]string{"theme", "--paint", "dark", "--show"}, "theme", "--paint"},
		{[]string{"theme", "--reset", "--flavor", "vivid"}, "theme", "--reset"},
		{[]string{"status", "--yaml"}, "status", "--yaml"},
		{[]string{"status", "all"}, "status", "all"},
		{[]string{"check", "--json", "--markdown"}, "check", "--markdown"},
		{[]string{"export", "--target", "iterm"}, "export", "ghostty, kitty, wezterm, alacritty"},
		{[]string{"export", "--to"}, "export", "DIR"},
		{[]string{"export", "--to", ""}, "export", "--to"},
		{[]string{"version", "now"}, "version", "now"},
		{[]string{"help", "frob"}, "", "frob"},
		{[]string{"help", "apply", "theme"}, "help", "theme"},
		{[]string{"--lang", "fr", "version"}, "", "en, pt-BR"},
		{[]string{"--lang"}, "", "--lang"},
		{[]string{"version", "--lang=klingon"}, "", "klingon"},
	}
	bothLanguages(t, func(t *testing.T, lang string) {
		w := newWorld(t, platform.Windows, "")
		w.vars["SERENO_LANG"] = lang
		before := w.tree()
		label := map[string]string{"en": "Usage: ", "pt-BR": "Uso: "}[lang]
		for _, test := range tests {
			got := w.run(test.args...)
			name := strings.Join(test.args, " ")
			if got.code != engine.ExitUsage {
				t.Errorf("sereno %s: exit code %d, want 2\n%s%s", name, got.code, got.out, got.err)
				continue
			}
			if got.out != "" {
				t.Errorf("sereno %s: a usage error printed on standard output: %q", name, got.out)
			}
			lines := strings.Split(strings.TrimSuffix(got.err, "\n"), "\n")
			if len(lines) != 3 {
				t.Errorf("sereno %s: %d lines on standard error, want the complaint, the usage and one hint:\n%s", name, len(lines), got.err)
				continue
			}
			if !strings.HasPrefix(lines[0], "sereno: ") || !strings.Contains(lines[0], test.names) {
				t.Errorf("sereno %s: the complaint does not name %q: %s", name, test.names, lines[0])
			}
			usage, hint := label+"sereno "+generalUsage, "`sereno help`"
			if test.command != "" {
				usage, hint = label+"sereno "+findCommand(test.command).usage, "`sereno help "+test.command+"`"
			}
			if lines[1] != usage {
				t.Errorf("sereno %s: second line\n got %s\nwant %s", name, lines[1], usage)
			}
			if !strings.Contains(lines[2], hint) {
				t.Errorf("sereno %s: the hint does not name %s: %s", name, hint, lines[2])
			}
			noMissingKey(t, got.err)
		}
		sameTree(t, "usage errors", before, w.tree())
	})
}

// TestFlagForms: a value may follow its flag or be joined to it with "=",
// the global flags may stand anywhere, and a flag given twice counts once
// (the last value; for --hook, all of them).
func TestFlagForms(t *testing.T) {
	theme := findCommand("theme")
	r := &run{catalog: i18n.For(i18n.English)}
	tests := []struct {
		args  []string
		flag  string
		value string
	}{
		{[]string{"--flavor", "vivid"}, "flavor", "vivid"},
		{[]string{"--flavor=vivid"}, "flavor", "vivid"},
		{[]string{"--flavor", "soft", "--flavor", "vivid"}, "flavor", "vivid"},
		{[]string{"dark", "--inband=off"}, "inband", "off"},
		{[]string{"--show"}, "show", "true"},
	}
	for _, test := range tests {
		parsed, problem := r.parse(theme, test.args)
		if problem != "" {
			t.Errorf("%v: %s", test.args, problem)
			continue
		}
		if value, _ := parsed.value(test.flag); value != test.value || !parsed.has(test.flag) {
			t.Errorf("%v: --%s = %q, want %q", test.args, test.flag, value, test.value)
		}
	}
	parsed, problem := r.parse(findCommand("apply"), []string{"--hook", "bash", "--hook=zsh,pwsh"})
	if problem != "" || !reflect.DeepEqual(parsed.flags["hook"], []string{"bash", "zsh,pwsh"}) {
		t.Errorf("--hook twice = %v (%s)", parsed.flags["hook"], problem)
	}

	global, rest := splitGlobal([]string{"--no-color", "status", "--lang", "pt-BR", "--json", "-h"})
	if !global.noColor || !global.help || global.lang != "pt-BR" || !reflect.DeepEqual(rest, []string{"status", "--json"}) {
		t.Errorf("splitGlobal = %+v, %v", global, rest)
	}
}

// TestHelp: the help is on standard output with exit code 0, is the same by
// every way of asking for it, names every flag of the command, and exists in
// both languages (R3, R5).
func TestHelp(t *testing.T) {
	texts := map[string]map[string]string{}
	bothLanguages(t, func(t *testing.T, lang string) {
		w := newWorld(t, platform.Linux, "")
		texts[lang] = map[string]string{}
		general := w.must(0, "help", "--lang", lang)
		for _, args := range [][]string{{}, {"--help"}, {"-h"}} {
			if got := w.must(0, append(args, "--lang", lang)...); got.out != general.out || got.err != "" {
				t.Errorf("sereno %v prints something else than sereno help:\n%s%s", args, got.out, got.err)
			}
		}
		texts[lang][""] = general.out
		for _, cmd := range commands {
			if !regexp.MustCompile(`(?m)^  ` + cmd.name + ` +\S`).MatchString(general.out) {
				t.Errorf("the general help does not list %s", cmd.name)
			}
			help := w.must(0, "help", cmd.name, "--lang", lang)
			for _, args := range [][]string{{cmd.name, "--help"}, {cmd.name, "-h"}, {"-h", cmd.name}, {cmd.name, "--frob", "--help"}} {
				if got := w.must(0, append(args, "--lang", lang)...); got.out != help.out || got.err != "" {
					t.Errorf("sereno %v prints something else than sereno help %s", args, cmd.name)
				}
			}
			lines := strings.Split(help.out, "\n")
			if !strings.HasSuffix(lines[0], " sereno "+cmd.usage) {
				t.Errorf("help %s does not start with the usage line: %s", cmd.name, lines[0])
			}
			for _, flag := range cmd.flags {
				if !regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(flag.syntax()) + `( {2,}\S|$)`).MatchString(help.out) {
					t.Errorf("help %s does not explain %s:\n%s", cmd.name, flag.syntax(), help.out)
				}
			}
			texts[lang][cmd.name] = help.out
			noMissingKey(t, help.out)
		}
		for _, global := range []string{"--lang en|pt-BR", "--no-color", "-h, --help"} {
			if !strings.Contains(general.out, "  "+global+" ") {
				t.Errorf("the general help does not explain %s", global)
			}
		}
		if !strings.Contains(general.out, "sereno apply --dry-run") {
			t.Error("the general help does not say where to start")
		}
		noMissingKey(t, general.out)
	})
	for name, english := range texts["en"] {
		portuguese := texts["pt-BR"][name]
		if english == portuguese {
			t.Errorf("help %q is the same text in both languages", name)
		}
		// Short: the general help fits a screen, and no command needs more
		// than a dozen lines.
		limit := 14
		if name == "" {
			limit = 24
		}
		for lang, text := range map[string]string{"en": english, "pt-BR": portuguese} {
			if n := strings.Count(text, "\n"); n > limit {
				t.Errorf("help %q has %d lines in %s; the limit is %d", name, n, lang, limit)
			}
		}
	}
}

// TestHelpTextsExist: every command and every flag has its sentences in the
// catalogues, which hold the same keys in both languages.
func TestHelpTextsExist(t *testing.T) {
	seen := 0
	for _, name := range Commands() {
		keys := HelpKeys(name)
		if len(keys) < 2 {
			t.Errorf("%s has no help keys", name)
		}
		for _, key := range keys {
			seen++
			if !i18n.Has(key) {
				t.Errorf("the catalogues have no text for %s", key)
			}
		}
	}
	if seen < 30 {
		t.Errorf("only %d help keys", seen)
	}
	if HelpKeys("frob") != nil {
		t.Error("an unknown command has help keys")
	}
	// And no help text is left behind by a command or flag that is gone.
	wanted := map[string]bool{}
	for _, name := range Commands() {
		for _, key := range HelpKeys(name) {
			wanted[key] = true
		}
	}
	general := map[string]bool{
		"help.tagline": true, "help.commands": true, "help.global": true, "help.global.lang": true,
		"help.global.no-color": true, "help.global.help": true, "help.start": true, "help.more": true,
	}
	for _, key := range i18n.Keys() {
		if strings.HasPrefix(key, "help.") && !wanted[key] && !general[key] {
			t.Errorf("the catalogues hold %s, which no command uses", key)
		}
	}
}

// TestVersion is the last sentence of R1.
func TestVersion(t *testing.T) {
	w := newWorld(t, platform.Linux, "")
	want := "sereno " + testVersion + " (" + runtime.GOOS + "/" + runtime.GOARCH + ")\n"
	if got := w.must(0, "version"); got.out != want || got.err != "" {
		t.Errorf("version printed %q %q, want %q", got.out, got.err, want)
	}
	// A build that was not stamped calls itself dev.
	r := &run{env: Env{Version: DevVersion}}
	if got := r.versionLine(); !strings.HasPrefix(got, "sereno dev (") {
		t.Errorf("an unstamped build prints %q", got)
	}
	if code := Run(Env{}, []string{"version"}); code != 0 {
		t.Errorf("an empty Env: exit code %d", code)
	}
}

// TestLanguage walks the order of R5 at the level of the command: --lang,
// then SERENO_LANG, then the language of the system, otherwise English.
func TestLanguage(t *testing.T) {
	const english, portuguese = "Commands:", "Comandos:"
	tests := []struct {
		name     string
		os       string
		args     []string
		vars     map[string]string
		language uint16
		want     string
	}{
		{"nothing says anything", platform.Windows, nil, nil, 0, english},
		{"Windows in Portuguese of Brazil", platform.Windows, nil, nil, 0x0416, portuguese},
		{"Windows in Portuguese of Portugal", platform.Windows, nil, nil, 0x0816, portuguese},
		{"Windows in English", platform.Windows, nil, nil, 0x0409, english},
		{"SERENO_LANG beats the system", platform.Windows, nil, map[string]string{"SERENO_LANG": "en"}, 0x0416, english},
		{"the flag beats SERENO_LANG", platform.Windows, []string{"--lang", "pt-BR"}, map[string]string{"SERENO_LANG": "en"}, 0x0409, portuguese},
		{"the flag joined with =", platform.Windows, []string{"--lang=pt-br"}, nil, 0, portuguese},
		{"a stray SERENO_LANG is passed over", platform.Windows, nil, map[string]string{"SERENO_LANG": "klingon"}, 0x0416, portuguese},
		{"Linux with LANG", platform.Linux, nil, map[string]string{"LANG": "pt_BR.UTF-8"}, 0, portuguese},
		{"Linux with LC_ALL over LANG", platform.Linux, nil, map[string]string{"LANG": "pt_BR.UTF-8", "LC_ALL": "C"}, 0, english},
		{"macOS with LC_MESSAGES", platform.Darwin, nil, map[string]string{"LC_MESSAGES": "pt_BR.UTF-8"}, 0, portuguese},
		{"the Windows language does not count on Linux", platform.Linux, nil, nil, 0x0416, english},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := newWorld(t, test.os, "")
			for key, value := range test.vars {
				w.vars[key] = value
			}
			w.language = test.language
			got := w.must(0, append([]string{"help"}, test.args...)...)
			if !strings.Contains(got.out, test.want) {
				t.Errorf("want %q in:\n%s", test.want, got.out)
			}
		})
	}
}

// sgr matches an SGR sequence and captures its parameters.
var sgr = regexp.MustCompile("\x1b\\[([0-9;]*)m")

// checkSGR fails the test for every SGR parameter in a text that sereno's
// own output may not use (R5, R8).
func checkSGR(t *testing.T, text string) int {
	t.Helper()
	found := 0
	for _, match := range sgr.FindAllStringSubmatch(text, -1) {
		found++
		if err := palette.CheckSGR(match[1]); err != nil {
			t.Errorf("the output uses the sequence ESC[%sm: %v", match[1], err)
		}
	}
	if strings.Count(text, "\x1b") != found {
		t.Errorf("the output holds an escape sequence that is not SGR")
	}
	return found
}

// TestColour is the last sentence of R5: sereno's own output is coloured
// only with slot SGR codes, only when standard output is a terminal, and
// neither --no-color nor NO_COLOR is set.
func TestColour(t *testing.T) {
	for _, style := range styles {
		if err := palette.CheckSGR(style); err != nil {
			t.Errorf("style %q: %v", style, err)
		}
	}
	commandLines := [][]string{
		{"apply", "--dry-run"}, {"apply", "--hook", "bash"}, {"status"}, {"check"},
		{"theme", "--show"}, {"help"}, {"uninstall", "--dry-run"}, {"uninstall"},
	}
	w := newWorld(t, platform.Windows, "wt-present")
	w.terminal = true
	coloured := 0
	for _, args := range commandLines {
		got := w.run(args...)
		coloured += checkSGR(t, got.out)
		if strings.Contains(got.err, "\x1b") {
			t.Errorf("sereno %v colours standard error", args)
		}
	}
	if coloured < 20 {
		t.Errorf("only %d coloured pieces on a terminal; the plan, the report and the swatches should have more", coloured)
	}

	plain := func(name string, prepare func(w *world) []string) {
		w := newWorld(t, platform.Windows, "wt-present")
		extra := prepare(w)
		for _, args := range commandLines {
			if got := w.run(append(args, extra...)...); strings.Contains(got.out+got.err, "\x1b") {
				t.Errorf("%s: sereno %v prints escape sequences", name, args)
			}
		}
	}
	plain("not a terminal", func(w *world) []string { return nil })
	plain("--no-color", func(w *world) []string { w.terminal = true; return []string{"--no-color"} })
	plain("NO_COLOR", func(w *world) []string { w.terminal = true; w.vars["NO_COLOR"] = "1"; return nil })
}
