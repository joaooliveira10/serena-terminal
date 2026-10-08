// Package render turns the palette and a handful of options into the bytes of
// every file sereno generates: the Windows Terminal fragment (R16-R18), the
// three init files (R23-R34) and the emulator themes of `sereno export` (R38).
//
// It returns bytes and writes nothing; commands hand the bytes to the engine.
// The output depends only on the arguments: no path, user name or machine fact
// is read here, which is what makes the files golden-testable and the init
// files identical for every user of the same version (R23).
//
// Colours enter through package palette only. The templates hold no hex value
// (a test in internal/policy scans them); the init files address the sixteen
// slots through the role table, and the hex values they do contain are the
// PALETTE byte strings of R29, taken from palette.Variant.Sequence.
package render

import (
	"bytes"
	"embed"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/joaooliveira10/serena-terminal/internal/mode"
	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
	"github.com/joaooliveira10/serena-terminal/internal/target/winterminal"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// File is one rendered file.
type File struct {
	// Name is the path of the file below the folder it belongs to, with
	// forward slashes: below ROOT for the init files ("shell/init.ps1"),
	// below the export destination for the themes ("kitty/Serena Dark.conf").
	Name string
	Data []byte
}

// Template delimiters. The usual braces would collide with PowerShell and
// shell code on every other line.
const (
	leftDelim  = "<%"
	rightDelim = "%>"
)

// Control bytes of R1 and the spellings the init files use for ESC. No init
// file contains a raw control byte other than the line feed.
const (
	esc     = "\x1b"
	escPS   = "!"    // replaced by [char]27 when the string is written (R23: never the backtick-e escape)
	escSh   = `\033` // octal escape of printf and of $'...' in bash 3.2+ and zsh
	bslash  = `\`
	bslash2 = `\\`
)

// parse loads the named templates with the functions they share.
func parse(roles *palette.Roles, names ...string) (*template.Template, error) {
	funcs := template.FuncMap{
		// role gives the SGR parameters of a role: "34" for function.
		"role": func(name string) (string, error) {
			params := roles.SGR(palette.Role(name))
			if params == "" {
				return "", fmt.Errorf("unknown role %q", name)
			}
			return params, nil
		},
		// slot gives the palette slot a role's colour stands for, which is
		// how zsh prompts name colours: 4 for "34", 11 for "93".
		"slot": func(name string) (int, error) { return roleSlot(roles, name) },
		// json quotes a string for a JSON document.
		"json": jsonString,
		// toml quotes a string as a TOML basic string.
		"toml": tomlString,
	}
	files := make([]string, len(names))
	for i, name := range names {
		files[i] = "templates/" + name
	}
	return template.New(names[0]).Delims(leftDelim, rightDelim).Option("missingkey=error").Funcs(funcs).ParseFS(templateFS, files...)
}

// execute renders the first of the named templates.
func execute(data any, names ...string) ([]byte, error) {
	roles, err := palette.LoadRoles()
	if err != nil {
		return nil, err
	}
	tmpl, err := parse(roles, names...)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, names[0], data); err != nil {
		return nil, err
	}
	if bytes.Contains(buf.Bytes(), []byte("\r")) {
		return nil, fmt.Errorf("%s: the rendered file contains a carriage return", names[0])
	}
	return buf.Bytes(), nil
}

// roleSlot returns the palette slot of a role whose SGR parameters are one
// foreground colour: 30-37 are slots 0-7 and 90-97 slots 8-15.
func roleSlot(roles *palette.Roles, name string) (int, error) {
	params := roles.SGR(palette.Role(name))
	n, err := strconv.Atoi(params)
	switch {
	case err != nil:
		return 0, fmt.Errorf("role %q (%q) is not a single slot colour", name, params)
	case n >= 30 && n <= 37:
		return n - 30, nil
	case n >= 90 && n <= 97:
		return n - 90 + 8, nil
	}
	return 0, fmt.Errorf("role %q (%q) is not a single slot colour", name, params)
}

// jsonString quotes text as a JSON string. It is written out here, not left to
// encoding/json, so that the fragment is the same byte for byte on every Go
// version: only the quote, the backslash and control characters are escaped,
// and everything else, including non-ASCII letters of a path, stays as UTF-8.
func jsonString(text string) (string, error) {
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("%q is not valid UTF-8", text)
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range text {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}

// tomlString quotes text as a TOML basic string. Scheme names and colours are
// plain ASCII, so anything that would need more than the two simple escapes
// is an error instead of a guess.
func tomlString(text string) (string, error) {
	for _, r := range text {
		if r < 0x20 || r > 0x7e {
			return "", fmt.Errorf("%q cannot be written as a TOML string here", text)
		}
	}
	text = strings.ReplaceAll(text, bslash, bslash2)
	return `"` + strings.ReplaceAll(text, `"`, `\"`) + `"`, nil
}

// ---------------------------------------------------------------------------
// Windows Terminal fragment (R16-R18)

// FragmentOptions are the settings that shape the fragment.
type FragmentOptions struct {
	// Profile is false for `--no-profile`: only the six schemes are written.
	Profile bool
	// Shell is the PowerShell the Serena profile starts: shells.PowerShell
	// (Windows PowerShell 5.1) or shells.Pwsh. `--shell auto` is resolved by
	// the caller. It is not looked at without a profile.
	Shell shells.Shell
	// Mode and Flavor choose the profile's colorScheme: a dark/light pair
	// for auto, one scheme for a fixed mode.
	Mode   mode.Mode
	Flavor mode.Flavor
	// Root is sereno's folder. When it is not the default one, the profile
	// names init.ps1 by its absolute path.
	Root platform.Root
}

// Parts of the profile's command line (R18).
const (
	windowsPowerShell = `%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe`
	pwshProgram       = "pwsh.exe"
	profileArguments  = " -NoLogo -NoExit -ExecutionPolicy RemoteSigned -File "
	defaultInitPath   = `%LOCALAPPDATA%\sereno\shell\init.ps1`
	initBelowRoot     = `\shell\init.ps1`
)

type fragmentScheme struct {
	Name                string
	Background          palette.Color
	Foreground          palette.Color
	Cursor              palette.Color
	SelectionBackground palette.Color
	ANSI                palette.Slots
}

type fragmentProfile struct {
	GUID        string
	Name        string
	CommandLine string
	// Scheme is the single scheme of a fixed mode; SchemeDark and
	// SchemeLight are the pair of mode auto.
	Scheme      string
	SchemeDark  string
	SchemeLight string
}

type fragmentData struct {
	Profile *fragmentProfile
	Schemes []fragmentScheme
}

// CommandLine returns the commandline value of the Serena profile (R18),
// unescaped: the PowerShell, its fixed arguments and init.ps1 in quotes.
func CommandLine(shell shells.Shell, root platform.Root) (string, error) {
	var program string
	switch shell {
	case shells.PowerShell:
		program = windowsPowerShell
	case shells.Pwsh:
		program = pwshProgram
	default:
		return "", fmt.Errorf("the Serena profile starts powershell or pwsh, not %q", shell)
	}
	init := defaultInitPath
	if !root.Default {
		// The fragment exists on Windows only, so the path is joined with
		// backslashes whatever system renders it.
		init = strings.TrimRight(root.Path, `\/`) + initBelowRoot
		if root.Path == "" || strings.ContainsAny(init, "\"\r\n") {
			return "", fmt.Errorf("the folder %q cannot be written into a command line", root.Path)
		}
	}
	return program + profileArguments + `"` + init + `"`, nil
}

// SchemeNames returns what the profile's colorScheme says for a mode and a
// flavour (R18): for auto the names of the dark and the light scheme of that
// flavour and an empty single; for a fixed mode the one scheme (dark and light
// honour the flavour, hc and hc-light have none).
func SchemeNames(p *palette.Palette, m mode.Mode, flavor mode.Flavor) (single, dark, light string, err error) {
	name := func(id string) string {
		v, ok := p.Variant(id)
		if !ok && err == nil {
			err = fmt.Errorf("the palette has no variant %q", id)
		}
		return v.Name
	}
	if _, bad := mode.ParseMode(string(m)); bad != nil {
		return "", "", "", bad
	}
	if _, bad := mode.ParseFlavor(string(flavor)); bad != nil {
		return "", "", "", bad
	}
	if m == mode.Auto {
		dark = name(mode.Resolve(mode.Dark, flavor, mode.Signals{}))
		light = name(mode.Resolve(mode.Light, flavor, mode.Signals{}))
		return "", dark, light, err
	}
	single = name(mode.Resolve(m, flavor, mode.Signals{}))
	return single, "", "", err
}

// Fragment renders the one file sereno writes for Windows Terminal: UTF-8
// without a byte order mark, LF, four-space indent, `profiles` then `schemes`,
// every key in the order of R17 and R18, and a final newline.
func Fragment(p *palette.Palette, o FragmentOptions) ([]byte, error) {
	data := fragmentData{}
	for _, v := range p.Variants {
		data.Schemes = append(data.Schemes, fragmentScheme{
			Name: v.Name, Background: v.Background, Foreground: v.Foreground, Cursor: v.Cursor,
			SelectionBackground: v.SelectionBackground, ANSI: v.ANSI,
		})
	}
	if o.Profile {
		command, err := CommandLine(o.Shell, o.Root)
		if err != nil {
			return nil, err
		}
		single, dark, light, err := SchemeNames(p, o.Mode, o.Flavor)
		if err != nil {
			return nil, err
		}
		data.Profile = &fragmentProfile{
			GUID: winterminal.ProfileGUID(), Name: winterminal.ProfileName, CommandLine: command,
			Scheme: single, SchemeDark: dark, SchemeLight: light,
		}
	}
	return execute(data, "windows-terminal.fragment.json.tmpl")
}

// ---------------------------------------------------------------------------
// Init files (R23-R34)

// initVariant is one variant as the init files need it: its id and PALETTE
// (R29) spelled for a PowerShell string and for a printf format.
type initVariant struct {
	ID string
	PS string
	Sh string
}

type initData struct {
	// GUID is the GUID of the Serena profile (R18), which Windows Terminal
	// puts into WT_PROFILE_ID. GUIDRegex is it as a regular expression for
	// PowerShell; GUIDPattern matches it in a shell `case` whatever the
	// letter case (bash 3.2 cannot lower a string).
	GUID        string
	GUIDRegex   string
	GUIDPattern string
	Variants    []initVariant
	// RESET (R29) in the same two spellings.
	ResetPS string
	ResetSh string
}

// powerShellString spells a byte string for the inside of a PowerShell
// single-quoted string. The escape character is written as an exclamation
// mark, which init.ps1 replaces by [char]27 at the moment it writes the
// string: a literal costs nothing at start, and a session that never paints
// never builds the real string.
func powerShellString(sequence string) (string, error) {
	if strings.ContainsAny(sequence, escPS+"'\n\r") {
		return "", fmt.Errorf("%q cannot be embedded in a PowerShell string", sequence)
	}
	return strings.ReplaceAll(sequence, esc, escPS), checkPrintable(strings.ReplaceAll(sequence, esc, ""))
}

// shellFormat spells a byte string as a printf format between single quotes,
// for bash and zsh.
func shellFormat(sequence string) (string, error) {
	if strings.ContainsAny(sequence, "%'\n\r") {
		return "", fmt.Errorf("%q cannot be embedded in a printf format", sequence)
	}
	out := strings.ReplaceAll(sequence, bslash, bslash2)
	return strings.ReplaceAll(out, esc, escSh), checkPrintable(strings.ReplaceAll(sequence, esc, ""))
}

func checkPrintable(text string) error {
	for i := 0; i < len(text); i++ {
		if text[i] < 0x20 || text[i] > 0x7e {
			return fmt.Errorf("%q holds a byte that is not printable ASCII", text)
		}
	}
	return nil
}

// guidPattern turns "{6b2d...}" into a glob that matches it in either letter
// case, with the braces quoted: '{'6[bB]2[dD]...'}'.
func guidPattern(guid string) string {
	var b strings.Builder
	for _, r := range guid {
		switch {
		case r == '{' || r == '}':
			b.WriteString("'" + string(r) + "'")
		case r >= 'a' && r <= 'f':
			b.WriteString("[" + string(r) + strings.ToUpper(string(r)) + "]")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func newInitData(p *palette.Palette) (initData, error) {
	guid := winterminal.ProfileGUID()
	data := initData{GUID: guid, GUIDRegex: regexp.QuoteMeta(guid), GUIDPattern: guidPattern(guid)}
	var err error
	if data.ResetPS, err = powerShellString(palette.ResetSequence()); err != nil {
		return initData{}, err
	}
	if data.ResetSh, err = shellFormat(palette.ResetSequence()); err != nil {
		return initData{}, err
	}
	for _, v := range p.Variants {
		iv := initVariant{ID: v.ID}
		if iv.PS, err = powerShellString(v.Sequence()); err != nil {
			return initData{}, err
		}
		if iv.Sh, err = shellFormat(v.Sequence()); err != nil {
			return initData{}, err
		}
		data.Variants = append(data.Variants, iv)
	}
	return data, nil
}

// sharedShell is the part of init.bash and init.zsh that is the same text in
// both: the mode file, the signals, the resolution table, the follow check,
// the branch and the ls colours, written in what bash 3.2 and zsh have in
// common.
const sharedShell = "init.shared.sh.tmpl"

// Init renders the init file of a shell (R23): init.ps1 for both PowerShells,
// init.bash or init.zsh. The result is printable ASCII with LF line endings
// and the same for every user.
func Init(p *palette.Palette, shell shells.Shell) ([]byte, error) {
	data, err := newInitData(p)
	if err != nil {
		return nil, err
	}
	var out []byte
	switch shell {
	case shells.PowerShell, shells.Pwsh:
		out, err = execute(data, "init.ps1.tmpl")
	case shells.Bash:
		out, err = execute(data, "init.bash.tmpl", sharedShell)
	case shells.Zsh:
		out, err = execute(data, "init.zsh.tmpl", sharedShell)
	default:
		return nil, fmt.Errorf("no init file for shell %q", shell)
	}
	if err != nil {
		return nil, err
	}
	for i, b := range out {
		if b != '\n' && (b < 0x20 || b > 0x7e) {
			return nil, fmt.Errorf("%s: byte %#x at offset %d is not printable ASCII (R23)", shell.InitFile(), b, i)
		}
	}
	return out, nil
}

// InitFiles renders the three init files sereno owns below ROOT (R10), in the
// order shell/init.ps1, shell/init.bash, shell/init.zsh.
func InitFiles(p *palette.Palette) ([]File, error) {
	var files []File
	for _, shell := range []shells.Shell{shells.PowerShell, shells.Bash, shells.Zsh} {
		data, err := Init(p, shell)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Name: shell.InitFile(), Data: data})
	}
	return files, nil
}

// ---------------------------------------------------------------------------
// Emulator themes of `sereno export` (R38)

// Target is an emulator `sereno export` writes themes for.
type Target string

// The four targets of R38, in the order of R3.
const (
	Ghostty   Target = "ghostty"
	Kitty     Target = "kitty"
	WezTerm   Target = "wezterm"
	Alacritty Target = "alacritty"
)

// Targets returns the four export targets in the order of R3.
func Targets() []Target { return []Target{Ghostty, Kitty, WezTerm, Alacritty} }

// ParseTarget recognises one target name.
func ParseTarget(name string) (Target, bool) {
	for _, target := range Targets() {
		if string(target) == name {
			return target, true
		}
	}
	return "", false
}

// exportData is what a theme template sees: one variant and where the colours
// came from.
type exportData struct {
	palette.Variant
	Tag string
}

// Normal and Bright split the sixteen slots the way WezTerm and Alacritty do.
func (d exportData) Normal() []palette.Color { return d.ANSI[:8] }
func (d exportData) Bright() []palette.Color { return d.ANSI[8:] }

// ExportName returns the name of a variant's theme file for a target, with the
// target's folder in front (R38): the scheme name for Ghostty (no extension),
// kitty (.conf) and WezTerm (.toml), and serena-<id>.toml for Alacritty.
func ExportName(target Target, v palette.Variant) (string, error) {
	switch target {
	case Ghostty:
		return "ghostty/" + v.Name, nil
	case Kitty:
		return "kitty/" + v.Name + ".conf", nil
	case WezTerm:
		return "wezterm/" + v.Name + ".toml", nil
	case Alacritty:
		return "alacritty/serena-" + v.ID + ".toml", nil
	}
	return "", fmt.Errorf("unknown export target %q", target)
}

// Export renders the six theme files of one target, in the order of R7.
func Export(p *palette.Palette, target Target) ([]File, error) {
	templates := map[Target]string{
		Ghostty: "ghostty.tmpl", Kitty: "kitty.conf.tmpl", WezTerm: "wezterm.toml.tmpl", Alacritty: "alacritty.toml.tmpl",
	}
	name, ok := templates[target]
	if !ok {
		return nil, fmt.Errorf("unknown export target %q", target)
	}
	var files []File
	for _, v := range p.Variants {
		path, err := ExportName(target, v)
		if err != nil {
			return nil, err
		}
		data, err := execute(exportData{Variant: v, Tag: p.Source.Tag}, name)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Name: path, Data: data})
	}
	return files, nil
}
