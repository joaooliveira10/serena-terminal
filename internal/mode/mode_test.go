package mode

import (
	"os"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// tablePath is the shared truth table of R28 at the root of the repository.
const tablePath = "../../testdata/modes.tsv"

type tableRow struct {
	mode    string
	flavor  string
	signals Signals
	variant string
}

// readTable parses testdata/modes.tsv: a header line, then tab-separated rows
// with 0 and 1 for the three signals.
func readTable(t *testing.T) []tableRow {
	t.Helper()
	data, err := os.ReadFile(tablePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\r") {
		t.Fatal("modes.tsv contains carriage returns; the shell tests read it with LF")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if lines[0] != "mode\tflavor\thc_on\thc_light\tapps_light\tvariant" {
		t.Fatalf("unexpected header %q", lines[0])
	}
	flag := func(s string) bool {
		if s != "0" && s != "1" {
			t.Fatalf("signal value %q is not 0 or 1", s)
		}
		return s == "1"
	}
	var rows []tableRow
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			t.Fatalf("row %q has %d fields, want 6", line, len(fields))
		}
		rows = append(rows, tableRow{
			mode: fields[0], flavor: fields[1],
			signals: Signals{HighContrast: flag(fields[2]), HCLight: flag(fields[3]), AppsLight: flag(fields[4])},
			variant: fields[5],
		})
	}
	return rows
}

// TestResolveMatchesTheSharedTable drives the Go implementation of R28 with
// testdata/modes.tsv, the file the PowerShell and bash/zsh implementations
// are tested against as well.
func TestResolveMatchesTheSharedTable(t *testing.T) {
	for _, row := range readTable(t) {
		mode, err := ParseMode(row.mode)
		if err != nil {
			t.Fatal(err)
		}
		flavor, err := ParseFlavor(row.flavor)
		if err != nil {
			t.Fatal(err)
		}
		if got := Resolve(mode, flavor, row.signals); got != row.variant {
			t.Errorf("Resolve(%s, %s, %+v) = %s, want %s", mode, flavor, row.signals, got, row.variant)
		}
	}
}

// TestTableIsComplete makes sure the table lists every combination exactly
// once and names only variants that exist in the palette.
func TestTableIsComplete(t *testing.T) {
	rows := readTable(t)
	if len(rows) != 5*2*8 {
		t.Errorf("%d rows, want %d (5 modes, 2 flavours, 8 signal combinations)", len(rows), 5*2*8)
	}
	p, err := palette.Load()
	if err != nil {
		t.Fatal(err)
	}
	type key struct {
		mode, flavor string
		signals      Signals
	}
	seen := map[key]bool{}
	for _, row := range rows {
		k := key{row.mode, row.flavor, row.signals}
		if seen[k] {
			t.Errorf("combination %+v is listed twice", k)
		}
		seen[k] = true
		if _, ok := p.Variant(row.variant); !ok {
			t.Errorf("variant %q is not in the palette", row.variant)
		}
	}
}

// TestResolveRules states R28 directly, independent of the table file.
func TestResolveRules(t *testing.T) {
	light := Signals{AppsLight: true}
	contrast := Signals{HighContrast: true, AppsLight: true}
	contrastLight := Signals{HighContrast: true, HCLight: true}
	tests := []struct {
		mode    Mode
		flavor  Flavor
		signals Signals
		want    string
	}{
		{Auto, Soft, Signals{}, "dark"},
		{Auto, Vivid, Signals{}, "dark-vivid"},
		{Auto, Soft, light, "light"},
		{Auto, Vivid, light, "light-vivid"},
		{Auto, Vivid, contrast, "hc"},                // contrast wins over the app theme; no flavour
		{Auto, Soft, contrastLight, "hc-light"},      //
		{Auto, Soft, Signals{HCLight: true}, "dark"}, // the window colour means nothing without a contrast theme
		{Dark, Vivid, contrastLight, "dark-vivid"},   // a fixed mode ignores the signals
		{Light, Soft, Signals{}, "light"},
		{HC, Vivid, light, "hc"},
		{HCLight, Vivid, Signals{}, "hc-light"},
		{"sepia", "neon", light, "light"}, // unknown values fall back to auto and soft
	}
	for _, test := range tests {
		if got := Resolve(test.mode, test.flavor, test.signals); got != test.want {
			t.Errorf("Resolve(%s, %s, %+v) = %s, want %s", test.mode, test.flavor, test.signals, got, test.want)
		}
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	defaults := Defaults()
	want := "mode=auto\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\n"
	if got := string(defaults.Bytes()); got != want {
		t.Errorf("first-apply mode file = %q, want %q", got, want)
	}
	all := []Settings{
		defaults,
		{Mode: HCLight, Flavor: Vivid, InBand: ScopeAlways, Keys: ScopeOff, Prompt: false},
		{Mode: Light, Flavor: Vivid, InBand: ScopeOff, Keys: ScopeAlways, Prompt: true},
	}
	for _, s := range all {
		if err := s.Validate(); err != nil {
			t.Errorf("Validate(%+v): %v", s, err)
		}
		data := s.Bytes()
		for _, b := range data {
			if b > 126 || (b < 32 && b != '\n') {
				t.Fatalf("mode file contains byte %#x", b)
			}
		}
		if strings.Count(string(data), "\n") != 5 {
			t.Errorf("mode file has %d lines, want 5", strings.Count(string(data), "\n"))
		}
		got, err := Parse(data)
		if err != nil || got != s {
			t.Errorf("Parse(Bytes(%+v)) = %+v, %v", s, got, err)
		}
	}
	if got := all[1].Variant(Signals{AppsLight: true}); got != "hc-light" {
		t.Errorf("Variant = %s", got)
	}
}

func TestParseRejectsMalformedModeFiles(t *testing.T) {
	valid := "mode=auto\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\n"
	tests := map[string]string{
		"empty":               "",
		"four lines":          "mode=auto\nflavor=soft\ninband=profile\nkeys=profile\n",
		"six lines":           valid + "extra=1\n",
		"no final newline":    strings.TrimSuffix(valid, "\n"),
		"CRLF":                strings.ReplaceAll(valid, "\n", "\r\n"),
		"wrong order":         "flavor=soft\nmode=auto\ninband=profile\nkeys=profile\nprompt=on\n",
		"unknown mode":        strings.Replace(valid, "mode=auto", "mode=sepia", 1),
		"unknown flavor":      strings.Replace(valid, "flavor=soft", "flavor=neon", 1),
		"unknown inband":      strings.Replace(valid, "inband=profile", "inband=sometimes", 1),
		"unknown keys":        strings.Replace(valid, "keys=profile", "keys=on", 1),
		"unknown prompt":      strings.Replace(valid, "prompt=on", "prompt=yes", 1),
		"space around equals": strings.Replace(valid, "mode=auto", "mode = auto", 1),
		"trailing space":      strings.Replace(valid, "mode=auto", "mode=auto ", 1),
		"upper case":          strings.Replace(valid, "mode=auto", "MODE=auto", 1),
		"blank line":          strings.Replace(valid, "flavor=soft\n", "\nflavor=soft\n", 1),
	}
	if _, err := Parse([]byte(valid)); err != nil {
		t.Fatalf("the valid file is rejected: %v", err)
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(input)); err == nil {
				t.Errorf("Parse accepted %q", input)
			}
		})
	}
	for _, bad := range []Settings{
		{Mode: "sepia", Flavor: Soft, InBand: ScopeOff, Keys: ScopeOff},
		{Mode: Auto, Flavor: "neon", InBand: ScopeOff, Keys: ScopeOff},
		{Mode: Auto, Flavor: Soft, InBand: "x", Keys: ScopeOff},
		{Mode: Auto, Flavor: Soft, InBand: ScopeOff, Keys: "x"},
	} {
		if bad.Validate() == nil {
			t.Errorf("Validate accepted %+v", bad)
		}
	}
}

// fakeRegistry is a Registry backed by two maps keyed by `key\name`.
type fakeRegistry struct {
	text    map[string]string
	numbers map[string]uint32
}

func (r fakeRegistry) String(key, name string) (string, bool) {
	value, ok := r.text[key+`\`+name]
	return value, ok
}

func (r fakeRegistry) Integer(key, name string) (uint32, bool) {
	value, ok := r.numbers[key+`\`+name]
	return value, ok
}

func TestFromRegistry(t *testing.T) {
	const (
		flags  = `Control Panel\Accessibility\HighContrast\Flags`
		window = `Control Panel\Colors\Window`
		apps   = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize\AppsUseLightTheme`
	)
	tests := []struct {
		name    string
		text    map[string]string
		numbers map[string]uint32
		want    Signals
	}{
		{"nothing readable", nil, nil, Signals{}},
		{"stock dark Windows", map[string]string{flags: "126", window: "255 255 255"}, map[string]uint32{apps: 0},
			Signals{HCLight: true}},
		{"light apps", map[string]string{flags: "126", window: "255 255 255"}, map[string]uint32{apps: 1},
			Signals{HCLight: true, AppsLight: true}},
		{"contrast theme, dark", map[string]string{flags: "127", window: "0 0 0"}, map[string]uint32{apps: 1},
			Signals{HighContrast: true, AppsLight: true}},
		{"contrast theme, light", map[string]string{flags: "127", window: "255 250 239"}, nil,
			Signals{HighContrast: true, HCLight: true}},
		{"flags as a number", nil, map[string]uint32{flags: 1}, Signals{HighContrast: true}},
		{"flags with bit 0 clear", map[string]string{flags: "4222"}, nil, Signals{}},
		{"flags not a number", map[string]string{flags: "on"}, nil, Signals{}},
		{"flags with spaces", map[string]string{flags: " 1 "}, nil, Signals{HighContrast: true}},
		{"apps value other than 1", nil, map[string]uint32{apps: 2}, Signals{}},
		// 0.2126*128 + 0.7152*127 + 0.0722*127 = 127.2: just above.
		{"window just above the threshold", map[string]string{window: "128 127 127"}, nil, Signals{HCLight: true}},
		{"window exactly 127", map[string]string{window: "127 127 127"}, nil, Signals{}},
		{"green weighs most", map[string]string{window: "0 255 0"}, nil, Signals{HCLight: true}},
		{"blue weighs least", map[string]string{window: "0 0 255"}, nil, Signals{}},
		{"window with two numbers", map[string]string{window: "255 255"}, nil, Signals{}},
		{"window out of range", map[string]string{window: "256 256 256"}, nil, Signals{}},
		{"window not numbers", map[string]string{window: "white"}, nil, Signals{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := FromRegistry(fakeRegistry{text: test.text, numbers: test.numbers})
			if got != test.want {
				t.Errorf("FromRegistry = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestCurrent(t *testing.T) {
	system := Fixed{AppsLight: true}
	env := func(value string) func(string) string {
		return testenv.Getenv(map[string]string{OSModeEnv: value})
	}
	tests := []struct {
		name   string
		osMode string
		reader Reader
		want   Signals
		known  bool
	}{
		{"reader", "", system, Signals{AppsLight: true}, true},
		{"no detection", "", Unknown{}, Signals{}, false},
		{"dark replaces the signals", "dark", system, Signals{}, true},
		{"light", "light", Unknown{}, Signals{AppsLight: true}, true},
		{"hc", "hc", system, Signals{HighContrast: true}, true},
		{"hc-light", "hc-light", Unknown{}, Signals{HighContrast: true, HCLight: true}, true},
		{"an unknown value is ignored", "sepia", system, Signals{AppsLight: true}, true},
		{"variant ids are not values", "dark-vivid", Unknown{}, Signals{}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, known := Current(env(test.osMode), test.reader)
			if got != test.want || known != test.known {
				t.Errorf("Current = %+v, %v; want %+v, %v", got, known, test.want, test.known)
			}
		})
	}
	// Each value of SERENO_OS_MODE resolves, in auto mode, to the variant of
	// the same name.
	for _, osMode := range []string{"dark", "light", "hc", "hc-light"} {
		signals, _ := SignalsFor(osMode)
		if got := Resolve(Auto, Soft, signals); got != osMode {
			t.Errorf("SERENO_OS_MODE=%s resolves to %s", osMode, got)
		}
	}
}

// TestFollowsAndRoute checks the conditions of R30 one by one.
func TestFollowsAndRoute(t *testing.T) {
	wt := platform.Session{Emulator: platform.WindowsTerminal}
	profile := platform.Session{Emulator: platform.WindowsTerminal, SerenaProfile: true}
	code := platform.Session{Emulator: platform.VSCode, SerenaProfile: true}
	other := platform.Session{Emulator: platform.OtherEmulator, SerenaProfile: true}
	tests := []struct {
		name    string
		inband  Scope
		session platform.Session
		follows bool
		route   Route
	}{
		{"profile setting in the Serena profile", ScopeProfile, profile, true, RouteInBand},
		{"profile setting in another profile", ScopeProfile, wt, false, RouteEmulator},
		{"always in another profile", ScopeAlways, wt, true, RouteInBand},
		{"off in the Serena profile", ScopeOff, profile, false, RouteEmulator},
		{"vscode never follows", ScopeAlways, code, false, RouteHost},
		{"outside Windows Terminal", ScopeAlways, other, false, RouteEmulator},
		{"malformed setting", "sometimes", profile, false, RouteEmulator},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := Defaults()
			s.InBand = test.inband
			if got := s.Follows(test.session); got != test.follows {
				t.Errorf("Follows = %v, want %v", got, test.follows)
			}
			if got := s.Route(test.session); got != test.route {
				t.Errorf("Route = %s, want %s", got, test.route)
			}
		})
	}
}

// TestSystemReader runs the real reader. On Windows it reads the registry
// (read only, as T1 allows); elsewhere it must report that nothing is known.
func TestSystemReader(t *testing.T) {
	signals, known := System().Signals()
	if !known && signals != (Signals{}) {
		t.Errorf("unknown signals must all be off, got %+v", signals)
	}
	t.Logf("system signals: %+v (known: %v)", signals, known)
}

// TestFollowRuleTruthTable spells R30 out for every combination of its
// inputs, read from the environment as a session is: Windows Terminal or not,
// VS Code or not, the three in-band settings, and the two ways a Serena
// profile session is recognised (the variable, the profile GUID in any letter
// case) next to a foreign GUID and none.
func TestFollowRuleTruthTable(t *testing.T) {
	const guid = "{6b2d8b49-31d3-5dea-9087-93c26e1a033e}"
	cases := 0
	for _, wt := range []bool{false, true} {
		for _, vscode := range []bool{false, true} {
			for _, inband := range []Scope{ScopeProfile, ScopeAlways, ScopeOff} {
				for _, profile := range []string{"none", "variable", "guid in upper case", "another guid"} {
					env := map[string]string{}
					if wt {
						env["WT_SESSION"] = "x"
					}
					if vscode {
						env["TERM_PROGRAM"] = "vscode"
					}
					serena := false
					switch profile {
					case "variable":
						env["SERENO_SESSION"], serena = "wt-profile", true
					case "guid in upper case":
						env["WT_PROFILE_ID"], serena = strings.ToUpper(guid), true
					case "another guid":
						env["WT_PROFILE_ID"] = "{2ece5bfe-50ed-5f3a-ab87-5cd4baafed2b}"
					}
					session := platform.Host{OS: platform.Windows, Getenv: testenv.Getenv(env)}.Session(guid)
					follows := wt && !vscode && inband != ScopeOff && (inband == ScopeAlways || serena)
					route := RouteEmulator
					switch {
					case vscode:
						route = RouteHost
					case follows:
						route = RouteInBand
					}
					settings := Defaults()
					settings.InBand = inband
					if got := settings.Follows(session); got != follows {
						t.Errorf("wt=%v vscode=%v inband=%s profile=%s: Follows = %v, want %v", wt, vscode, inband, profile, got, follows)
					}
					if got := settings.Route(session); got != route {
						t.Errorf("wt=%v vscode=%v inband=%s profile=%s: Route = %s, want %s", wt, vscode, inband, profile, got, route)
					}
					cases++
				}
			}
		}
	}
	if cases != 48 {
		t.Errorf("%d combinations walked, want 48", cases)
	}
}
