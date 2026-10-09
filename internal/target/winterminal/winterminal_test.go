package winterminal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

const (
	serenaGUID = "{6b2d8b49-31d3-5dea-9087-93c26e1a033e}"
	gitGUID    = "{2ece5bfe-50ed-5f3a-ab87-5cd4baafed2b}"
)

// TestFragmentGUID holds the two vectors of R18: the Serena profile, and the
// Git Bash value from Windows Terminal's documentation.
func TestFragmentGUID(t *testing.T) {
	if got := FragmentGUID("SerenaTerminal", "Serena"); got != serenaGUID {
		t.Errorf("FragmentGUID(SerenaTerminal, Serena) = %s, want %s", got, serenaGUID)
	}
	if got := FragmentGUID("Git", "Git Bash"); got != gitGUID {
		t.Errorf("FragmentGUID(Git, Git Bash) = %s, want %s", got, gitGUID)
	}
	if got := ProfileGUID(); got != serenaGUID {
		t.Errorf("ProfileGUID = %s, want %s", got, serenaGUID)
	}
	// The version and variant fields of a UUIDv5.
	guid := FragmentGUID("any", "profile")
	if len(guid) != 38 || guid[15] != '5' || !strings.ContainsRune("89ab", rune(guid[20])) {
		t.Errorf("%s is not a braced version-5 UUID", guid)
	}
	if FragmentGUID("Git", "Git Bash") == FragmentGUID("git", "Git Bash") {
		t.Error("the application name must be case-sensitive")
	}
}

func TestFragmentPath(t *testing.T) {
	local := filepath.Join(testenv.TempDir(t), "AppData", "Local")
	want := filepath.Join(local, "Microsoft", "Windows Terminal", "Fragments", "SerenaTerminal", "sereno.json")
	if got := FragmentPath(local); got != want {
		t.Errorf("FragmentPath = %s, want %s", got, want)
	}
	if FragmentDir(local) != filepath.Dir(want) {
		t.Errorf("FragmentDir = %s", FragmentDir(local))
	}
}

// TestDetect creates each of the three folders of R16 alone and together.
func TestDetect(t *testing.T) {
	stable := filepath.Join("Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState")
	preview := filepath.Join("Packages", "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe", "LocalState")
	unpackaged := filepath.Join("Microsoft", "Windows Terminal")
	tests := []struct {
		name     string
		dirs     []string
		files    []string // settings.json or state.json to create, relative to LOCALAPPDATA
		present  bool
		settings string
		state    string
	}{
		{"nothing", nil, nil, false, "", ""},
		{"only the Packages folder", []string{"Packages"}, nil, false, "", ""},
		{"stable", []string{stable}, []string{filepath.Join(stable, "settings.json"), filepath.Join(stable, "state.json")},
			true, filepath.Join(stable, "settings.json"), filepath.Join(stable, "state.json")},
		{"stable before its first start", []string{stable}, nil, true, "", ""},
		{"preview", []string{preview}, []string{filepath.Join(preview, "settings.json")}, true, filepath.Join(preview, "settings.json"), ""},
		{"unpackaged", []string{unpackaged}, []string{filepath.Join(unpackaged, "settings.json")}, true, filepath.Join(unpackaged, "settings.json"), ""},
		{"only a Fragments folder of another program", []string{filepath.Join(unpackaged, "Fragments", "Git")}, nil, true, "", ""},
		{"stable wins over preview", []string{stable, preview},
			[]string{filepath.Join(stable, "settings.json"), filepath.Join(preview, "settings.json"), filepath.Join(preview, "state.json")},
			true, filepath.Join(stable, "settings.json"), ""},
		{"preview has the only settings", []string{stable, preview}, []string{filepath.Join(preview, "settings.json")},
			true, filepath.Join(preview, "settings.json"), ""},
		{"a file named like the folder", nil, []string{unpackaged}, false, "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := testenv.New(t)
			for _, dir := range test.dirs {
				if err := os.MkdirAll(filepath.Join(home.LocalAppData, dir), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, file := range test.files {
				path := filepath.Join(home.LocalAppData, file)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got := Detect(home.LocalAppData)
			if got.Present != test.present {
				t.Errorf("Present = %v, want %v", got.Present, test.present)
			}
			abs := func(rel string) string {
				if rel == "" {
					return ""
				}
				return filepath.Join(home.LocalAppData, rel)
			}
			if got.SettingsPath != abs(test.settings) || got.StatePath != abs(test.state) {
				t.Errorf("settings %q, state %q; want %q, %q", got.SettingsPath, got.StatePath, abs(test.settings), abs(test.state))
			}
			if got.Present != (len(got.Dirs) > 0) {
				t.Errorf("Dirs = %v with Present = %v", got.Dirs, got.Present)
			}
		})
	}
}

// TestFixtureHome reads the fixture "Windows Terminal present with Git and WSL
// stubs" the way status will, and proves that reading changes nothing.
func TestFixtureHome(t *testing.T) {
	home := testenv.NewFrom(t, "wt-present")
	before := testenv.Snapshot(t, home.Dir)

	found := Detect(home.LocalAppData)
	if !found.Present || len(found.Dirs) != 2 || found.SettingsPath == "" || found.StatePath == "" {
		t.Fatalf("Detect = %+v", found)
	}
	settings, err := ReadSettings(found.SettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Theme != "" || settings.FollowsSystem() || len(settings.DefaultsOverride) != 0 {
		t.Errorf("settings = %+v", settings)
	}
	if !settings.HasProfile(gitGUID) || !settings.HasProfile(strings.ToUpper(gitGUID)) {
		t.Error("the Git stub is not found")
	}
	if settings.HasProfile(serenaGUID) {
		t.Error("the Serena stub is reported although the fixture has none")
	}
	state, err := ReadState(found.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.GeneratedProfiles) != 4 || !state.Knows(gitGUID) || state.Knows(serenaGUID) {
		t.Errorf("state = %+v", state)
	}

	after := testenv.Snapshot(t, home.Dir)
	if !reflect.DeepEqual(before, after) {
		t.Error("reading Windows Terminal's files changed the home")
	}
	if _, err := ReadSettings(filepath.Join(home.Dir, "missing.json")); err == nil {
		t.Error("a missing settings.json was read")
	}
	if _, err := ReadState(filepath.Join(home.Dir, "missing.json")); err == nil {
		t.Error("a missing state.json was read")
	}
}

func TestParseSettings(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		theme    string
		follows  bool
		defaults []string
		stub     bool
	}{
		{"empty object", `{}`, "", false, nil, false},
		{"theme dark", `{"theme": "dark"}`, "dark", false, nil, false},
		{"theme system", `{"theme": "system"}`, "system", true, nil, false},
		{"theme legacySystem", `{"theme": "legacySystem"}`, "legacySystem", true, nil, false},
		{"theme pair", `{"theme": {"dark": "dark", "light": "light"}}`, "", true, nil, false},
		{"custom theme that follows", `{"theme": "mine", "themes": [{"name": "mine", "window": {"applicationTheme": "system"}}]}`, "mine", true, nil, false},
		{"custom theme that is dark", `{"theme": "mine", "themes": [{"name": "mine", "window": {"applicationTheme": "dark"}}, {"name": "other"}]}`, "mine", false, nil, false},
		{"custom theme that is not defined", `{"theme": "mine"}`, "mine", false, nil, false},
		// Windows Terminal 1.24 as its source has it: the default of
		// applicationTheme is the system's theme, and a theme of the user
		// with a built-in name is ignored.
		{"custom theme that names no applicationTheme", `{"theme": "mine", "themes": [{"name": "mine", "window": {"useMica": true}}]}`, "mine", true, nil, false},
		{"custom theme without a window object", `{"theme": "mine", "themes": [{"name": "mine", "tab": {"background": "accent"}}]}`, "mine", true, nil, false},
		{"a theme of the user named like a built-in one", `{"theme": "dark", "themes": [{"name": "dark", "window": {"applicationTheme": "system"}}]}`, "dark", false, nil, false},
		{"defaults override", `{"profiles": {"defaults": {"font": {"face": "Consolas"}, "opacity": 90, "environment": {}, "colorScheme": "Campbell"}, "list": []}}`,
			"", false, []string{"colorScheme", "font", "environment"}, false},
		{"stub present", `{"profiles": {"list": [{"guid": "{6B2D8B49-31D3-5DEA-9087-93C26E1A033E}", "hidden": false, "name": "Serena", "source": "SerenaTerminal"}]}}`,
			"", false, nil, true},
		{"old layout: profiles is the list", `{"profiles": [{"guid": "` + serenaGUID + `"}, {"name": "no guid"}, 7]}`, "", false, nil, true},
		{"comments and trailing commas", `// settings
{
    /* the theme
       spans lines */
    "theme": "system", // follows Windows
    "profiles": {
        "defaults": { "colorScheme": "One // Half /* Dark */", },
        "list": [
            { "guid": "` + serenaGUID + `", }, // the stub
        ],
    },
}
`, "system", true, []string{"colorScheme"}, true},
		{"byte order mark", "\xef\xbb\xbf" + `{"theme": "light"}`, "light", false, nil, false},
		{"unexpected types are ignored", `{"theme": 3, "profiles": "none", "themes": {"name": "x"}}`, "", false, nil, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, err := ParseSettings([]byte(test.json))
			if err != nil {
				t.Fatal(err)
			}
			if s.Theme != test.theme || s.FollowsSystem() != test.follows {
				t.Errorf("theme %q, follows %v; want %q, %v", s.Theme, s.FollowsSystem(), test.theme, test.follows)
			}
			if !reflect.DeepEqual(s.DefaultsOverride, test.defaults) {
				t.Errorf("DefaultsOverride = %v, want %v", s.DefaultsOverride, test.defaults)
			}
			if s.HasProfile(serenaGUID) != test.stub {
				t.Errorf("HasProfile = %v, want %v", s.HasProfile(serenaGUID), test.stub)
			}
		})
	}
	// Each of the six built-in themes by its name: a theme of the user that
	// takes one of those names is ignored by Windows Terminal, so what it
	// says about following Windows does not count either way.
	for name, follows := range map[string]bool{
		"dark": false, "light": false, "legacyDark": false, "legacyLight": false,
		"system": true, "legacySystem": true,
	} {
		says := "system"
		if follows {
			says = "dark"
		}
		s, err := ParseSettings([]byte(`{"theme": "` + name + `", "themes": [{"name": "` + name + `", "window": {"applicationTheme": "` + says + `"}}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if s.Theme != name || s.FollowsSystem() != follows {
			t.Errorf("a theme of the user named %q that says %q: theme %q, follows %v; want %q, %v", name, says, s.Theme, s.FollowsSystem(), name, follows)
		}
	}
	for name, bad := range map[string]string{
		"empty":                "",
		"array":                `[]`,
		"null":                 `null`,
		"truncated":            `{"theme": "dark"`,
		"unterminated comment": `{"theme": "dark" /* }`,
		"comment only":         `// nothing`,
	} {
		if _, err := ParseSettings([]byte(bad)); err == nil {
			t.Errorf("%s: ParseSettings accepted %q", name, bad)
		}
	}
}

func TestParseState(t *testing.T) {
	state, err := ParseState([]byte(`{
    "generatedProfiles" :
    [
        "{61c54bbd-c2c6-5271-96e7-009a87ff44bf}",
        "{6B2D8B49-31D3-5DEA-9087-93C26E1A033E}", // seen once
        42,
    ],
}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.GeneratedProfiles) != 2 || !state.Knows(serenaGUID) || state.Knows(gitGUID) {
		t.Errorf("state = %+v", state)
	}
	if empty, err := ParseState([]byte(`{}`)); err != nil || empty.Knows(serenaGUID) {
		t.Errorf("an empty state: %+v, %v", empty, err)
	}
	if _, err := ParseState([]byte(`not json`)); err == nil {
		t.Error("ParseState accepted text that is not JSON")
	}
}

// TestStripJSONC checks the normaliser on its own: the result must be plain
// JSON with strings untouched and line numbers preserved.
func TestStripJSONC(t *testing.T) {
	tests := []struct{ in, want string }{
		{`{"a": 1}`, `{"a": 1}`},
		{"{\"a\": 1 // c\n}", "{\"a\": 1 \n}"},
		{"{/* x\ny */\"a\": 1}", "{\n\"a\": 1}"},
		{`{"a": [1, 2, ], }`, `{"a": [1, 2  ]  }`},
		{`{"a": "x, // y /* z */ ,"}`, `{"a": "x, // y /* z */ ,"}`},
		{`{"a": "quote \" // still a string, ]"}`, `{"a": "quote \" // still a string, ]"}`},
		{`{"a": "back\\"} // c`, `{"a": "back\\"} `},
		{`{"a": 1,/* c */}`, `{"a": 1 }`},
		{`{"url": "https://aka.ms/terminal"}`, `{"url": "https://aka.ms/terminal"}`},
	}
	for _, test := range tests {
		got := string(stripJSONC([]byte(test.in)))
		if got != test.want {
			t.Errorf("stripJSONC(%q) = %q, want %q", test.in, got, test.want)
		}
		if !json.Valid([]byte(got)) {
			t.Errorf("stripJSONC(%q) is not valid JSON: %q", test.in, got)
		}
		if strings.Count(got, "\n") != strings.Count(test.in, "\n") {
			t.Errorf("stripJSONC(%q) changed the number of lines", test.in)
		}
	}
}
