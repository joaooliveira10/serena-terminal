package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/joaooliveira10/serena-terminal/internal/mode"
	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

func loadPalette(t *testing.T) *palette.Palette {
	t.Helper()
	p, err := palette.Load()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// defaultOptions is the fragment of a first apply on a machine without
// PowerShell 7: profile, Windows PowerShell, auto, soft, ROOT in its place.
func defaultOptions() FragmentOptions {
	return FragmentOptions{Profile: true, Shell: shells.PowerShell, Mode: mode.Auto, Flavor: mode.Soft, Root: platform.Root{Default: true}}
}

// node is a JSON value that remembers the order of the keys of an object.
type node struct {
	keys   []string
	fields map[string]*node
	items  []*node
	value  any // string, bool, json.Number or nil for a scalar
}

// parseOrdered decodes a JSON document keeping the key order, which is part
// of what R16 to R18 prescribe and which a map would lose.
func parseOrdered(t *testing.T, data []byte) *node {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	n, err := readNode(dec)
	if err != nil {
		t.Fatalf("the fragment is not JSON: %v", err)
	}
	if dec.More() {
		t.Fatal("data after the JSON document")
	}
	return n
}

func readNode(dec *json.Decoder) (*node, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		n := &node{fields: map[string]*node{}}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, err
			}
			name := key.(string)
			if _, twice := n.fields[name]; twice {
				return nil, fmt.Errorf("key %q appears twice", name)
			}
			child, err := readNode(dec)
			if err != nil {
				return nil, err
			}
			n.keys = append(n.keys, name)
			n.fields[name] = child
		}
		_, err = dec.Token()
		return n, err
	case json.Delim('['):
		n := &node{items: []*node{}}
		for dec.More() {
			child, err := readNode(dec)
			if err != nil {
				return nil, err
			}
			n.items = append(n.items, child)
		}
		_, err = dec.Token()
		return n, err
	}
	return &node{value: token}, nil
}

// The key orders of R17 and R18.
var (
	schemeKeys = []string{
		"name", "background", "foreground", "cursorColor", "selectionBackground",
		"black", "red", "green", "yellow", "blue", "purple", "cyan", "white",
		"brightBlack", "brightRed", "brightGreen", "brightYellow", "brightBlue", "brightPurple", "brightCyan", "brightWhite",
	}
	profileKeys = []string{"guid", "name", "commandline", "startingDirectory", "colorScheme", "font", "environment", "autoMarkPrompts", "showMarksOnScrollbar"}
)

// fragmentVariations are the option sets whose output the shape tests look at.
func fragmentVariations() map[string]FragmentOptions {
	out := map[string]FragmentOptions{}
	for _, m := range []mode.Mode{mode.Auto, mode.Dark, mode.Light, mode.HC, mode.HCLight} {
		for _, f := range []mode.Flavor{mode.Soft, mode.Vivid} {
			for _, shell := range []shells.Shell{shells.PowerShell, shells.Pwsh} {
				o := defaultOptions()
				o.Mode, o.Flavor, o.Shell = m, f, shell
				out[fmt.Sprintf("%s/%s/%s", m, f, shell)] = o
			}
		}
	}
	none := defaultOptions()
	none.Profile = false
	out["no profile"] = none
	moved := defaultOptions()
	moved.Root = platform.Root{Path: `D:\Tools\sereno home`}
	out["custom root"] = moved
	return out
}

// TestFragmentShape checks the letter of R16 on every variation: UTF-8
// without a byte order mark, LF, four-space indent, a final newline, and
// `profiles` before `schemes`.
func TestFragmentShape(t *testing.T) {
	p := loadPalette(t)
	for name, options := range fragmentVariations() {
		data, err := Fragment(p, options)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !utf8.Valid(data) || bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
			t.Errorf("%s: not UTF-8 without a byte order mark", name)
		}
		if bytes.Contains(data, []byte("\r")) || bytes.Contains(data, []byte("\t")) {
			t.Errorf("%s: contains a carriage return or a tab", name)
		}
		if !bytes.HasSuffix(data, []byte("}\n")) || bytes.HasSuffix(data, []byte("\n\n")) {
			t.Errorf("%s: does not end with exactly one newline", name)
		}

		// The layout is exactly what an indenter with four spaces makes
		// of the same document.
		var compact, indented bytes.Buffer
		if err := json.Compact(&compact, data); err != nil {
			t.Fatalf("%s: not JSON: %v", name, err)
		}
		if err := json.Indent(&indented, compact.Bytes(), "", "    "); err != nil {
			t.Fatal(err)
		}
		indented.WriteByte('\n')
		if !bytes.Equal(indented.Bytes(), data) {
			t.Errorf("%s: the layout is not a four-space indent:\n%s", name, data)
		}

		root := parseOrdered(t, data)
		if !reflect.DeepEqual(root.keys, []string{"profiles", "schemes"}) {
			t.Errorf("%s: top-level keys are %v, want profiles then schemes", name, root.keys)
		}
	}
}

// TestFragmentSchemes is R17: six schemes in the order of R7, each with
// exactly the 21 keys in their order and the colours of the palette in lower
// case. The schemes do not depend on any option.
func TestFragmentSchemes(t *testing.T) {
	p := loadPalette(t)
	for name, options := range fragmentVariations() {
		data, err := Fragment(p, options)
		if err != nil {
			t.Fatal(err)
		}
		schemes := parseOrdered(t, data).fields["schemes"].items
		if len(schemes) != len(p.Variants) {
			t.Fatalf("%s: %d schemes, want %d", name, len(schemes), len(p.Variants))
		}
		for i, v := range p.Variants {
			scheme := schemes[i]
			if !reflect.DeepEqual(scheme.keys, schemeKeys) {
				t.Errorf("%s: scheme %d has keys %v, want %v", name, i, scheme.keys, schemeKeys)
				continue
			}
			want := append([]palette.Color{v.Background, v.Foreground, v.Cursor, v.SelectionBackground}, v.ANSI[:]...)
			if got := scheme.fields["name"].value; got != v.Name {
				t.Errorf("%s: scheme %d is called %v, want %q", name, i, got, v.Name)
			}
			for k, key := range schemeKeys[1:] {
				got, _ := scheme.fields[key].value.(string)
				if got != string(want[k]) || got != strings.ToLower(got) {
					t.Errorf("%s: %s.%s = %q, want %q", name, v.ID, key, got, want[k])
				}
			}
		}
	}
	// ansi[5] is "purple" and ansi[13] "brightPurple": the slot names of
	// Windows Terminal, not those of the palette.
	dark, _ := p.Variant("dark")
	data, _ := Fragment(p, defaultOptions())
	first := parseOrdered(t, data).fields["schemes"].items[0]
	if first.fields["purple"].value != string(dark.ANSI[5]) || first.fields["brightPurple"].value != string(dark.ANSI[13]) {
		t.Error("purple and brightPurple are not slots 5 and 13")
	}
}

// TestFragmentProfile is R18: the keys of the Serena profile in their order,
// and every fixed value.
func TestFragmentProfile(t *testing.T) {
	p := loadPalette(t)
	data, err := Fragment(p, defaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	profiles := parseOrdered(t, data).fields["profiles"].items
	if len(profiles) != 1 {
		t.Fatalf("%d profiles, want 1", len(profiles))
	}
	profile := profiles[0]
	if !reflect.DeepEqual(profile.keys, profileKeys) {
		t.Fatalf("profile keys are %v, want %v (no other key is written)", profile.keys, profileKeys)
	}
	text := func(n *node) string { s, _ := n.value.(string); return s }
	if got := text(profile.fields["guid"]); got != "{6b2d8b49-31d3-5dea-9087-93c26e1a033e}" {
		t.Errorf("guid = %q", got)
	}
	if got := text(profile.fields["name"]); got != "Serena" {
		t.Errorf("name = %q", got)
	}
	const command = `%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoLogo -NoExit -ExecutionPolicy RemoteSigned -File "%LOCALAPPDATA%\sereno\shell\init.ps1"`
	if got := text(profile.fields["commandline"]); got != command {
		t.Errorf("commandline = %q\n        want %q", got, command)
	}
	// Windows Terminal has no default for the starting folder: a profile
	// without the key starts where the window was started from, which for
	// one opened from the Start menu is the System32 folder of Windows. The
	// home folder is named through the environment, as its own profiles do,
	// so the file stays the same for every user.
	if got := text(profile.fields["startingDirectory"]); got != "%USERPROFILE%" {
		t.Errorf("startingDirectory = %q, want %q", got, "%USERPROFILE%")
	}
	scheme := profile.fields["colorScheme"]
	if !reflect.DeepEqual(scheme.keys, []string{"dark", "light"}) || text(scheme.fields["dark"]) != "Serena Dark" || text(scheme.fields["light"]) != "Serena Light" {
		t.Errorf("colorScheme of auto/soft is not the pair Serena Dark / Serena Light")
	}
	font := profile.fields["font"]
	if !reflect.DeepEqual(font.keys, []string{"face"}) || text(font.fields["face"]) != "Cascadia Code" {
		t.Errorf("font is not {face: Cascadia Code}")
	}
	environment := profile.fields["environment"]
	wantKeys := []string{"SERENO_SESSION", "POWERSHELL_TELEMETRY_OPTOUT", "POWERSHELL_UPDATECHECK"}
	wantValues := []string{"wt-profile", "1", "Off"}
	if !reflect.DeepEqual(environment.keys, wantKeys) {
		t.Errorf("environment keys are %v, want %v", environment.keys, wantKeys)
	} else {
		for i, key := range wantKeys {
			if got := text(environment.fields[key]); got != wantValues[i] {
				t.Errorf("environment.%s = %q, want %q", key, got, wantValues[i])
			}
		}
	}
	for _, key := range []string{"autoMarkPrompts", "showMarksOnScrollbar"} {
		if profile.fields[key].value != true {
			t.Errorf("%s is not true", key)
		}
	}
	if bytes.Contains(data, []byte("adjustIndistinguishableColors")) {
		t.Error("adjustIndistinguishableColors is written")
	}
}

// TestFragmentWithoutProfile is `--no-profile`: an empty array, the schemes
// untouched.
func TestFragmentWithoutProfile(t *testing.T) {
	p := loadPalette(t)
	options := defaultOptions()
	options.Profile = false
	// A shell that could not start a profile must not matter here.
	options.Shell = shells.Bash
	data, err := Fragment(p, options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("{\n    \"profiles\": [],\n    \"schemes\": [\n")) {
		t.Errorf("the fragment does not start with an empty profiles array:\n%.80s", data)
	}
	root := parseOrdered(t, data)
	if len(root.fields["profiles"].items) != 0 || len(root.fields["schemes"].items) != 6 {
		t.Error("want no profile and six schemes")
	}
	with, _ := Fragment(p, defaultOptions())
	if !bytes.Equal(data[bytes.Index(data, []byte(`"schemes"`)):], with[bytes.Index(with, []byte(`"schemes"`)):]) {
		t.Error("the schemes differ between a fragment with and without the profile")
	}
}

// TestCommandLine covers the three forms of R18.
func TestCommandLine(t *testing.T) {
	const arguments = ` -NoLogo -NoExit -ExecutionPolicy RemoteSigned -File `
	const windowsPowerShell = `%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe`
	tests := []struct {
		name  string
		shell shells.Shell
		root  platform.Root
		want  string
	}{
		{"powershell", shells.PowerShell, platform.Root{Default: true}, windowsPowerShell + arguments + `"%LOCALAPPDATA%\sereno\shell\init.ps1"`},
		{"pwsh", shells.Pwsh, platform.Root{Default: true}, `pwsh.exe` + arguments + `"%LOCALAPPDATA%\sereno\shell\init.ps1"`},
		{"default root ignores the path", shells.PowerShell, platform.Root{Path: `C:\elsewhere`, Default: true}, windowsPowerShell + arguments + `"%LOCALAPPDATA%\sereno\shell\init.ps1"`},
		{"moved root", shells.PowerShell, platform.Root{Path: `D:\Tools\sereno home`}, windowsPowerShell + arguments + `"D:\Tools\sereno home\shell\init.ps1"`},
		{"moved root, pwsh", shells.Pwsh, platform.Root{Path: `E:\s`}, `pwsh.exe` + arguments + `"E:\s\shell\init.ps1"`},
		{"trailing separator", shells.PowerShell, platform.Root{Path: `D:\s\`}, windowsPowerShell + arguments + `"D:\s\shell\init.ps1"`},
		{"letters outside ASCII stay", shells.PowerShell, platform.Root{Path: `D:\Ação\sereno`}, windowsPowerShell + arguments + `"D:\Ação\sereno\shell\init.ps1"`},
		// One percent sign cannot be the name of a variable, and stays.
		{"one percent sign", shells.PowerShell, platform.Root{Path: `D:\100% mine\sereno`}, windowsPowerShell + arguments + `"D:\100% mine\sereno\shell\init.ps1"`},
		{"a percent sign at the end", shells.Pwsh, platform.Root{Path: `D:\s%`}, `pwsh.exe` + arguments + `"D:\s%\shell\init.ps1"`},
		{"characters a shell would stumble on", shells.PowerShell, platform.Root{Path: `D:\it's & (a) [b] $c ` + "`d" + ` {e}, @f #g ^h !i =j +k ~l; -File x`}, windowsPowerShell + arguments + `"D:\it's & (a) [b] $c ` + "`d" + ` {e}, @f #g ^h !i =j +k ~l; -File x\shell\init.ps1"`},
	}
	for _, tt := range tests {
		got, err := CommandLine(tt.shell, tt.root)
		if err != nil || got != tt.want {
			t.Errorf("%s: CommandLine = %q, %v\n   want %q", tt.name, got, err, tt.want)
		}
	}
	for _, shell := range []shells.Shell{shells.Bash, shells.Zsh, "fish", ""} {
		if _, err := CommandLine(shell, platform.Root{Default: true}); err == nil {
			t.Errorf("CommandLine accepted the shell %q", shell)
		}
	}
	// Folders that cannot stand in a command line. Windows Terminal expands
	// %NAME% in it and has no way of writing a percent sign that stays one:
	// a path with two of them may name a variable that happens to exist, and
	// the profile would then start PowerShell on a file that is not there.
	refused := []string{
		"", `D:\a"b`, "D:\\a\nb", "D:\\a\rb",
		`D:\%TEMP%`, `D:\a%TEMP%b\sereno`, `D:\%a%`, `D:\50%\75%`, `%USERPROFILE%\sereno`, `D:\%%`,
	}
	for _, path := range refused {
		_, err := CommandLine(shells.PowerShell, platform.Root{Path: path})
		if err == nil {
			t.Errorf("CommandLine accepted the folder %q", path)
			continue
		}
		if !errors.Is(err, ErrRootInCommandLine) {
			t.Errorf("CommandLine(%q) failed with %v, which is not ErrRootInCommandLine", path, err)
		}
		options := defaultOptions()
		options.Root = platform.Root{Path: path}
		if _, err := Fragment(loadPalette(t), options); !errors.Is(err, ErrRootInCommandLine) {
			t.Errorf("Fragment with the folder %q: %v, want ErrRootInCommandLine", path, err)
		}
	}
	// The default ROOT is written with a variable on purpose, and without a
	// profile there is no command line to refuse.
	if _, err := CommandLine(shells.PowerShell, platform.Root{Path: `D:\%TEMP%`, Default: true}); err != nil {
		t.Errorf("the default ROOT was refused: %v", err)
	}
	without := defaultOptions()
	without.Profile = false
	without.Root = platform.Root{Path: `D:\%TEMP%`}
	if _, err := Fragment(loadPalette(t), without); err != nil {
		t.Errorf("a fragment without a profile was refused for its ROOT: %v", err)
	}
}

// TestFragmentEscapesThePath makes sure a moved ROOT reaches the file as valid
// JSON and comes back unchanged, whatever characters the path has.
func TestFragmentEscapesThePath(t *testing.T) {
	p := loadPalette(t)
	for _, path := range []string{`D:\Tools\sereno home`, `D:\Ação\sereno`, `\\server\share\sereno`, `D:\100% & <ok>\s`} {
		options := defaultOptions()
		options.Root = platform.Root{Path: path}
		data, err := Fragment(p, options)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		var decoded struct {
			Profiles []struct {
				CommandLine string `json:"commandline"`
			} `json:"profiles"`
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		want, _ := CommandLine(shells.PowerShell, options.Root)
		if len(decoded.Profiles) != 1 || decoded.Profiles[0].CommandLine != want {
			t.Errorf("%s: commandline read back as %q, want %q", path, decoded.Profiles, want)
		}
		if !strings.HasSuffix(want, `"`+path+`\shell\init.ps1"`) {
			t.Errorf("%s: the path is not in the command line %q", path, want)
		}
	}
}

// TestSchemeNames is the colorScheme rule of R18 for every mode and flavour.
func TestSchemeNames(t *testing.T) {
	p := loadPalette(t)
	tests := []struct {
		mode                mode.Mode
		flavor              mode.Flavor
		single, dark, light string
	}{
		{mode.Auto, mode.Soft, "", "Serena Dark", "Serena Light"},
		{mode.Auto, mode.Vivid, "", "Serena Dark Vivid", "Serena Light Vivid"},
		{mode.Dark, mode.Soft, "Serena Dark", "", ""},
		{mode.Dark, mode.Vivid, "Serena Dark Vivid", "", ""},
		{mode.Light, mode.Soft, "Serena Light", "", ""},
		{mode.Light, mode.Vivid, "Serena Light Vivid", "", ""},
		{mode.HC, mode.Soft, "Serena High Contrast", "", ""},
		{mode.HC, mode.Vivid, "Serena High Contrast", "", ""},
		{mode.HCLight, mode.Soft, "Serena High Contrast Light", "", ""},
		{mode.HCLight, mode.Vivid, "Serena High Contrast Light", "", ""},
	}
	for _, tt := range tests {
		single, dark, light, err := SchemeNames(p, tt.mode, tt.flavor)
		if err != nil || single != tt.single || dark != tt.dark || light != tt.light {
			t.Errorf("SchemeNames(%s, %s) = %q, %q, %q, %v; want %q, %q, %q", tt.mode, tt.flavor, single, dark, light, err, tt.single, tt.dark, tt.light)
		}

		// And the same through the fragment.
		options := defaultOptions()
		options.Mode, options.Flavor = tt.mode, tt.flavor
		data, err := Fragment(p, options)
		if err != nil {
			t.Fatal(err)
		}
		scheme := parseOrdered(t, data).fields["profiles"].items[0].fields["colorScheme"]
		if tt.single != "" {
			if scheme.value != tt.single {
				t.Errorf("%s/%s: colorScheme = %v, want the single name %q", tt.mode, tt.flavor, scheme.value, tt.single)
			}
		} else if scheme.fields["dark"].value != tt.dark || scheme.fields["light"].value != tt.light {
			t.Errorf("%s/%s: colorScheme is not the pair %q / %q", tt.mode, tt.flavor, tt.dark, tt.light)
		}
	}
	if _, _, _, err := SchemeNames(p, "sepia", mode.Soft); err == nil {
		t.Error("SchemeNames accepted an unknown mode")
	}
	if _, _, _, err := SchemeNames(p, mode.Auto, "loud"); err == nil {
		t.Error("SchemeNames accepted an unknown flavour")
	}
	if _, err := Fragment(p, FragmentOptions{Profile: true, Shell: shells.PowerShell, Mode: "sepia", Flavor: mode.Soft, Root: platform.Root{Default: true}}); err == nil {
		t.Error("Fragment accepted an unknown mode")
	}
}

// schemaPath is the copy of Windows Terminal's settings schema (release-1.24)
// kept for the next test; testdata/wt/README.txt says where it is from.
const schemaPath = "testdata/wt/profiles.schema.json"

// TestFragmentKeysExistInTheSchema is the drift check of T4: every key of the
// profile, of its font and of a scheme exists in the schema of Windows
// Terminal 1.24, the oldest version sereno supports.
func TestFragmentKeysExistInTheSchema(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(testenv.RepoRoot(t), filepath.FromSlash(schemaPath)))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Items      struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"items"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	profile := schema.Defs["Profile"].Properties
	font := schema.Defs["FontConfig"].Properties
	scheme := schema.Defs["SchemeList"].Items.Properties
	if len(profile) < 40 || len(font) < 3 || len(scheme) < 20 {
		t.Fatalf("the schema was not read as expected: %d profile, %d font and %d scheme properties", len(profile), len(font), len(scheme))
	}

	p := loadPalette(t)
	for name, options := range fragmentVariations() {
		data, err := Fragment(p, options)
		if err != nil {
			t.Fatal(err)
		}
		root := parseOrdered(t, data)
		for _, entry := range root.fields["profiles"].items {
			for _, key := range entry.keys {
				if _, ok := profile[key]; !ok {
					t.Errorf("%s: profile key %q is not in the schema", name, key)
				}
			}
			for _, key := range entry.fields["font"].keys {
				if _, ok := font[key]; !ok {
					t.Errorf("%s: font key %q is not in the schema", name, key)
				}
			}
		}
		for _, entry := range root.fields["schemes"].items {
			for _, key := range entry.keys {
				if _, ok := scheme[key]; !ok {
					t.Errorf("%s: scheme key %q is not in the schema", name, key)
				}
			}
		}
	}
	// The pair form of colorScheme and the environment map are what the
	// fragment relies on beyond plain keys.
	for _, want := range []string{`"SchemePair"`, `"environment"`} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("the schema does not mention %s", want)
		}
	}
}

// TestJSONString pins the escaping the fragment is written with.
func TestJSONString(t *testing.T) {
	tests := map[string]string{
		"plain":       `"plain"`,
		`a\b`:         `"a\\b"`,
		`say "hi"`:    `"say \"hi\""`,
		"tab\there":   `"tab\there"`,
		"line\nbreak": `"line\nbreak"`,
		"cr\rhere":    `"cr\rhere"`,
		"bell\x07":    `"bell\u0007"`,
		"Ação <&>":    `"Ação <&>"`,
		"":            `""`,
	}
	for in, want := range tests {
		got, err := jsonString(in)
		if err != nil || got != want {
			t.Errorf("jsonString(%q) = %s, %v; want %s", in, got, err, want)
		}
		var back string
		if err := json.Unmarshal([]byte(got), &back); err != nil || back != in {
			t.Errorf("jsonString(%q) does not decode back: %q, %v", in, back, err)
		}
	}
	if _, err := jsonString("bad\xff"); err == nil {
		t.Error("jsonString accepted invalid UTF-8")
	}
}
