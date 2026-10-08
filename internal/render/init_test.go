package render

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
)

// initFile renders one init file.
func initFile(t *testing.T, shell shells.Shell) string {
	t.Helper()
	data, err := Init(loadPalette(t), shell)
	if err != nil {
		t.Fatalf("Init(%s): %v", shell, err)
	}
	return string(data)
}

var initShells = []shells.Shell{shells.PowerShell, shells.Bash, shells.Zsh}

// TestInitFilesAreStatic is the first half of R23: the files are the same on
// every call, hold only printable ASCII and line feeds, and name no path, user
// or machine.
func TestInitFilesAreStatic(t *testing.T) {
	p := loadPalette(t)
	files, err := InitFiles(p)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"shell/init.ps1", "shell/init.bash", "shell/init.zsh"}
	if len(files) != len(wantNames) {
		t.Fatalf("%d init files, want %d", len(files), len(wantNames))
	}
	for i, f := range files {
		if f.Name != wantNames[i] {
			t.Errorf("init file %d is %q, want %q", i, f.Name, wantNames[i])
		}
		for offset, b := range f.Data {
			if b != '\n' && (b < 0x20 || b > 0x7e) {
				t.Fatalf("%s: byte %#x at offset %d is not printable ASCII", f.Name, b, offset)
			}
		}
		if !bytes.HasSuffix(f.Data, []byte("\n")) || bytes.HasSuffix(f.Data, []byte("\n\n")) {
			t.Errorf("%s does not end with exactly one newline", f.Name)
		}
		if bytes.Contains(f.Data, []byte(leftDelim)) || bytes.Contains(f.Data, []byte(rightDelim)) {
			t.Errorf("%s still holds a template delimiter", f.Name)
		}
		again, err := Init(p, initShells[i])
		if err != nil || !bytes.Equal(again, f.Data) {
			t.Errorf("%s is not the same on a second rendering (%v)", f.Name, err)
		}
		// Nothing of a particular machine: no drive, no home folder, no
		// user name could have got in, because the renderer is given none.
		for _, clue := range []string{`C:\`, `D:\`, "/home/", "/Users/", "AppData"} {
			if bytes.Contains(f.Data, []byte(clue)) {
				t.Errorf("%s mentions %q", f.Name, clue)
			}
		}
	}
	// Windows PowerShell and PowerShell 7 share one file.
	if a, b := initFile(t, shells.PowerShell), initFile(t, shells.Pwsh); a != b {
		t.Error("init.ps1 differs between powershell and pwsh")
	}
	if _, err := Init(p, "fish"); err == nil {
		t.Error("Init accepted an unknown shell")
	}
}

// decodeEscapes turns the spellings the init files use for the escape
// character back into the byte: $e and the palette placeholder in init.ps1,
// \033 in init.bash and init.zsh.
func decodeEscapes(shell shells.Shell, text string) string {
	if shell == shells.PowerShell || shell == shells.Pwsh {
		return regexp.MustCompile(`\$e(\[|\]|\\)`).ReplaceAllString(text, esc+"$1")
	}
	return strings.ReplaceAll(text, escSh, esc)
}

var sgrSequence = regexp.MustCompile(esc + `\[([0-9;]*)m`)

// TestInitFilesUseSlotsOnly is the scan of R8: inside every SGR sequence of
// the three files only the allowed parameters appear; none of 38, 48, 40-47,
// 49 and 100-107. The colours that are not written as sequences are checked
// as well: the ls tables and the zsh prompt.
func TestInitFilesUseSlotsOnly(t *testing.T) {
	for _, shell := range initShells {
		text := decodeEscapes(shell, initFile(t, shell))
		found := sgrSequence.FindAllStringSubmatch(text, -1)
		// init.zsh has none: its prompt names slots with %F{n}, checked below.
		if (shell == shells.Zsh) != (len(found) == 0) || (shell != shells.Zsh && len(found) < 8) {
			t.Errorf("%s: %d SGR sequences found; the scan is probably broken", shell.InitFile(), len(found))
		}
		for _, match := range found {
			if match[1] == "" {
				t.Errorf("%s: an SGR sequence without parameters", shell.InitFile())
				continue
			}
			numbers, err := palette.ParseSGR(match[1])
			if err != nil {
				t.Errorf("%s: %q: %v", shell.InitFile(), match[0], err)
				continue
			}
			for _, n := range numbers {
				if palette.ForbiddenSGR(n) || !palette.AllowedSGR(n) {
					t.Errorf("%s: SGR parameter %d in %q is not allowed (R8)", shell.InitFile(), n, match[0])
				}
			}
		}
		if strings.Contains(text, "\x1b[") && !sgrSequence.MatchString(text) {
			t.Errorf("%s: an escape sequence the scan does not understand", shell.InitFile())
		}
	}

	// ls colours: every value of the default table and of the replacement
	// table is a list of allowed parameters.
	lsValue := regexp.MustCompile(`(?:^|[:'=(])([a-z]{2})=([0-9;]+)`)
	replacement := regexp.MustCompile(`__sereno_reply='?([0-9;]+)'? ;;`)
	for _, shell := range []shells.Shell{shells.Bash, shells.Zsh} {
		text := initFile(t, shell)
		line := regexp.MustCompile(`__sereno_set LS_COLORS '([^']+)' && export LS_COLORS\n`).FindStringSubmatch(text)
		if line == nil {
			t.Fatalf("%s: the default LS_COLORS was not found", shell.InitFile())
		}
		const want = "di=1;34:ln=36:so=35:pi=33:ex=1;32:bd=1;33:cd=1;33:su=1;31:sg=1;33:tw=4;34:ow=4;34:st=4;34:or=31:mi=31"
		if line[1] != want {
			t.Errorf("%s: default LS_COLORS = %q, want %q (R33)", shell.InitFile(), line[1], want)
		}
		values := lsValue.FindAllStringSubmatch(line[1], -1)
		for _, m := range replacement.FindAllStringSubmatch(text, -1) {
			values = append(values, []string{m[0], "replacement", m[1]})
		}
		if len(values) < 14+6 {
			t.Errorf("%s: only %d ls colour values found", shell.InitFile(), len(values))
		}
		for _, v := range values {
			if err := palette.CheckSGR(v[2]); err != nil {
				t.Errorf("%s: ls colour %s=%s: %v", shell.InitFile(), v[1], v[2], err)
			}
		}
		if !strings.Contains(text, "__sereno_set LSCOLORS exfxcxdxbxDxDxBxDxExEx && export LSCOLORS\n") {
			t.Errorf("%s: the default LSCOLORS of R33 is missing", shell.InitFile())
		}
		// R33 as amended: a variable that is set but empty counts as unset,
		// because that is how ls takes it.
		for _, test := range []string{`if [ -z "${LS_COLORS-}" ]; then`, `if [ -z "${LSCOLORS-}" ]; then`} {
			if !strings.Contains(text, test) {
				t.Errorf("%s does not contain %s", shell.InitFile(), test)
			}
		}
	}

	// zsh names colours by slot: %F{0} to %F{15}, and never a background.
	zsh := initFile(t, shells.Zsh)
	slots := regexp.MustCompile(`%F\{([^}]*)\}`).FindAllStringSubmatch(zsh, -1)
	if len(slots) != 4 {
		t.Errorf("init.zsh: %d %%F colours, want the 4 of R32", len(slots))
	}
	for _, m := range slots {
		if n, err := strconv.Atoi(m[1]); err != nil || n < 0 || n > 15 {
			t.Errorf("init.zsh: %s is not a palette slot", m[0])
		}
	}
	if strings.Contains(zsh, "%K") || strings.Contains(zsh, "%S") {
		t.Error("init.zsh paints a background in the prompt")
	}
}

// TestInitFilesEmbedThePalette is R29 in the init files: each holds PALETTE of
// the six variants and RESET exactly as package palette produces them, and no
// other colour value anywhere.
func TestInitFilesEmbedThePalette(t *testing.T) {
	p := loadPalette(t)
	colour := regexp.MustCompile(`#[0-9a-fA-F]{6}\b|rgb:[0-9a-fA-F]{2}/[0-9a-fA-F]{2}/[0-9a-fA-F]{2}`)
	for _, shell := range initShells {
		text := initFile(t, shell)
		rest := text
		check := func(id, sequence string) {
			var spelled string
			var err error
			if shell == shells.PowerShell {
				spelled, err = powerShellString(sequence)
				spelled = "'" + id + "' = '" + spelled + "'\n"
			} else {
				spelled, err = shellFormat(sequence)
				spelled = id + ") printf '" + spelled + "' ;;\n"
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(text, spelled) != 1 {
				t.Errorf("%s: the byte string of %q is not there exactly once", shell.InitFile(), id)
				return
			}
			rest = strings.Replace(rest, spelled, "", 1)

			// And the spelling decodes to the bytes of package palette.
			body := spelled[strings.Index(spelled, " '")+2 : strings.LastIndex(spelled, "'")]
			var decoded string
			if shell == shells.PowerShell {
				decoded = strings.ReplaceAll(body, escPS, esc)
			} else {
				decoded = strings.ReplaceAll(strings.ReplaceAll(body, escSh, esc), bslash2, bslash)
			}
			if decoded != sequence {
				t.Errorf("%s: %q decodes to %q, want %q", shell.InitFile(), id, decoded, sequence)
			}
		}
		for _, v := range p.Variants {
			check(v.ID, v.Sequence())
		}
		check("reset", palette.ResetSequence())
		if found := colour.FindString(rest); found != "" {
			t.Errorf("%s holds the colour value %s outside the palette strings", shell.InitFile(), found)
		}
		if strings.Contains(rest, "rgb:") {
			t.Errorf("%s mentions rgb: outside the palette strings", shell.InitFile())
		}
	}
	// What makes the placeholder safe: no byte string contains it.
	for _, v := range p.Variants {
		if strings.Contains(v.Sequence(), escPS) {
			t.Errorf("PALETTE of %s contains the placeholder %q", v.ID, escPS)
		}
	}
	if _, err := powerShellString("a" + escPS); err == nil {
		t.Error("powerShellString accepted the placeholder inside a string")
	}
	if _, err := powerShellString("it's"); err == nil {
		t.Error("powerShellString accepted a quote")
	}
	if _, err := shellFormat("100%"); err == nil {
		t.Error("shellFormat accepted a percent sign")
	}
	if _, err := shellFormat("it's"); err == nil {
		t.Error("shellFormat accepted a quote")
	}
}

// TestPowerShellColourTable is R24: the keys init.ps1 hands to PSReadLine and
// the sequence of each. The fifteen of the base table are set together, the
// three prediction keys each behind the property that tells they exist.
func TestPowerShellColourTable(t *testing.T) {
	text := initFile(t, shells.PowerShell)
	base := map[string]string{
		"Command": "34", "Keyword": "35", "String": "32", "Number": "93", "Type": "33", "Member": "36",
		"Variable": "39", "Parameter": "3;37", "Operator": "37", "Comment": "90", "Error": "91",
		"Emphasis": "96", "Selection": "7", "Default": "39", "ContinuationPrompt": "90",
	}
	guarded := map[string][2]string{
		"InlinePrediction":       {"InlinePredictionColor", "90"},
		"ListPrediction":         {"ListPredictionColor", "33"},
		"ListPredictionSelected": {"ListPredictionSelectedColor", "7"},
	}
	start := strings.Index(text, "$colors = @{")
	end := strings.Index(text, "$arguments['Colors'] = $colors")
	if start < 0 || end < start {
		t.Fatal("the colour table was not found in init.ps1")
	}
	table := text[start:end]

	entries := regexp.MustCompile(`\b([A-Z][A-Za-z]+) = "\$e\[([0-9;]+)m"`).FindAllStringSubmatch(table, -1)
	got := map[string]string{}
	for _, m := range entries {
		got[m[1]] = m[2]
	}
	if len(entries) != len(base) {
		t.Errorf("the base table has %d keys, want %d", len(entries), len(base))
	}
	for key, want := range base {
		if got[key] != want {
			t.Errorf("%s = ESC[%sm, want ESC[%sm", key, got[key], want)
		}
	}
	for key, want := range guarded {
		line := "if ($type.GetProperty('" + want[0] + "')) { $colors['" + key + "'] = \"$e[" + want[1] + "m\" }"
		if !strings.Contains(table, line) {
			t.Errorf("%s is not set behind its property check: want the line %s", key, line)
		}
	}
	if !strings.Contains(text, "if ($type.GetProperty('CommandColor')) {") {
		t.Error("the colour table is not behind the check that -Colors exists")
	}
	// The escape character is [char]27; PowerShell 5.1 has no `e.
	if !strings.Contains(text, "[char]27") || strings.Contains(text, "`e") {
		t.Error("init.ps1 must make the escape character with [char]27 and never with the backtick-e escape")
	}
}

// TestPowerShellStartsCarefully pins three things about init.ps1 that only a
// real PowerShell can prove (test/shell/run.ps1 does) and that must not be
// lost where no PowerShell runs the tests.
func TestPowerShellStartsCarefully(t *testing.T) {
	text := initFile(t, shells.PowerShell)
	code := strings.TrimLeft(regexp.MustCompile(`(?m)^[ \t]*#.*\n`).ReplaceAllString(text, ""), "\n")

	// R23: in a session whose language is constrained the file returns
	// before it calls its first method, which that mode would refuse.
	guards := []string{
		"if (-not ($Host.Name -ceq 'ConsoleHost')) { return }\n",
		"if (-not ([string]$ExecutionContext.SessionState.LanguageMode -ceq 'FullLanguage')) { return }\n",
		"if ($null -ne $ExecutionContext.SessionState.PSVariable.GetValue('global:__sereno')) { return }\n",
	}
	if !strings.HasPrefix(code, strings.Join(guards, "")) {
		t.Errorf("init.ps1 must begin with its three guards, in this order:\n%s", strings.Join(guards, ""))
	}
	before := strings.Join(guards[:2], "")
	if call := regexp.MustCompile(`\.\w+\(|\]::`).FindString(before); call != "" {
		t.Errorf("init.ps1 calls a method (%s) before it has looked at the language mode", call)
	}

	// R11 and R28: the bytes of the mode file decide, as in Go and in bash.
	// ReadAllText would take a byte order mark off and accept the file.
	if !strings.Contains(code, "[System.Text.Encoding]::ASCII.GetString([System.IO.File]::ReadAllBytes($file)) -cmatch '") {
		t.Error("init.ps1 does not read the mode file as bytes taken for ASCII")
	}
	if strings.Contains(code, "ReadAllText($file)") {
		t.Error("init.ps1 reads the mode file with ReadAllText, which accepts a byte order mark")
	}

	// R30: the time of the mode file is kept after the file was read, so
	// that a read that failed is tried again at the next prompt.
	if !strings.Contains(code, "if ($stamp -ne $s['Stamp']) { & $s['Read']; $s['Stamp'] = $stamp }\n") {
		t.Error("init.ps1 does not keep the time of the mode file after reading it")
	}
}

// TestShellFilesSetUserVariablesQuietly is R23 for the variables of the user
// that init.bash and init.zsh set: PS1 or PROMPT, PROMPT_COMMAND, LS_COLORS
// and LSCOLORS may be read-only, and a plain assignment would then print an
// error and abandon the function. In bash they are set through __sereno_set,
// with one exception that is tried through it first.
func TestShellFilesSetUserVariablesQuietly(t *testing.T) {
	comment := regexp.MustCompile(`(?m)^\s*#.*$`)
	bash := comment.ReplaceAllString(initFile(t, shells.Bash), "")
	plain := regexp.MustCompile(`(?m)(^|[\s;])(PS1|PROMPT_COMMAND|LS_COLORS|LSCOLORS)=`).FindAllString(bash, -1)
	if len(plain) != 1 || !strings.Contains(bash, "if __sereno_set PROMPT_COMMAND \"$PROMPT_COMMAND\"; then\n                PROMPT_COMMAND=(__sereno_prompt \"${PROMPT_COMMAND[@]}\")\n") {
		t.Errorf("init.bash assigns to a variable of the user %d times (%q); only the array form of PROMPT_COMMAND may, after __sereno_set has shown that it can", len(plain), plain)
	}
	// printf -v, with set -e put aside for the one command (bash ends a shell
	// that runs with set -e on any failed write to a read-only variable), and
	// a look at the value afterwards (before bash 4.4 printf reports success
	// for a variable it could not set).
	if !strings.Contains(bash, "    case $- in *e*) errexit=1; set +e ;; esac\n    printf -v \"$1\" '%s' \"$2\" 2> /dev/null || failed=1\n    if [ -n \"$errexit\" ]; then set -e; fi\n    if [ \"${!1-}\" != \"$2\" ]; then failed=1; fi\n    return \"$failed\"\n}\n") {
		t.Error("init.bash does not define __sereno_set as it must be: printf -v, without set -e, value checked")
	}

	zsh := comment.ReplaceAllString(initFile(t, shells.Zsh), "")
	for _, want := range []string{
		"[[ ${(Pt)1} == *readonly* ]] && return 1\n    typeset -g \"$1=$2\"\n",
		"if [[ ${(t)PROMPT} == *readonly* || ${(t)psvar} == *readonly* ]]; then\n            __sereno_show=\n            return 0\n",
		"if [[ ${(t)precmd_functions} == *readonly* ]]; then\n            __sereno_show= __sereno_term=\n",
	} {
		if !strings.Contains(zsh, want) {
			t.Errorf("init.zsh does not contain:\n%s", want)
		}
	}
	for _, text := range []string{bash, zsh} {
		if regexp.MustCompile(`(?m)(^|[\s;])(LS_COLORS|LSCOLORS)=`).MatchString(text) {
			t.Error("LS_COLORS or LSCOLORS is assigned directly; a read-only one would make the shell print an error")
		}
	}
}

// TestPromptTemplates pins the prompt strings of R26, R31 and R32 as they are
// written in the files.
func TestPromptTemplates(t *testing.T) {
	// The prompt of bash is put together in a local variable and handed to
	// PS1 in one step that cannot fail loudly (a read-only PS1).
	bash := initFile(t, shells.Bash)
	for _, want := range []string{
		`ps=$'\\[\033[34m\\]\\w\\[\033[0m\\]'`,
		`ps=$ps$' \\[\033[35m\\]${__sereno_branch}\\[\033[0m\\]'`,
		`ps=$ps$'\\n\\[\033[32m\\]>\\[\033[0m\\] '`,
		`ps=$ps$'\\n\\[\033[31m\\]>\\[\033[0m\\] '`,
		`__sereno_set PS1 "$ps" || __sereno_show=`,
	} {
		if !strings.Contains(bash, want) {
			t.Errorf("init.bash does not contain %s", want)
		}
	}
	zsh := initFile(t, shells.Zsh)
	if want := `PROMPT='%F{4}%~%f%(1V. %F{5}%1v%f.)'$'\n''%(?.%F{2}.%F{1})>%f '`; !strings.Contains(zsh, want) {
		t.Errorf("init.zsh does not contain %s", want)
	}
	ps := initFile(t, shells.PowerShell)
	for _, want := range []string{
		`"$e[34m" + $path + "$e[0m"`,
		`" $e[35m" + $branch + "$e[0m"`,
		"\"`n$e[32m\"", "\"`n$e[31m\"",
		`('>' * ($nestedPromptLevel + 1)) + "$e[0m "`,
		`$e + ']133;D;' + $code + $s['Bel']`, `$e + ']133;A' + $s['Bel']`, `$e + ']9;9;"' + $dir + '"' + $s['Bel']`, `$e + ']133;B' + $s['Bel']`,
	} {
		if !strings.Contains(ps, want) {
			t.Errorf("init.ps1 does not contain %s", want)
		}
	}
	// The GUID of the Serena profile is how a session is recognised (R30).
	const guid = "6b2d8b49-31d3-5dea-9087-93c26e1a033e"
	if !strings.Contains(ps, `\{`+guid+`\}`) {
		t.Error("init.ps1 does not compare WT_PROFILE_ID with the GUID of the profile")
	}
	if want := guidPattern("{" + guid + "}"); !strings.Contains(bash, want) || !strings.Contains(zsh, want) {
		t.Errorf("init.bash and init.zsh do not hold the pattern %s", want)
	}
	if got, want := guidPattern("{a1-f}"), "'{'[aA]1-[fF]'}'"; got != want {
		t.Errorf("guidPattern = %s, want %s", got, want)
	}
}

// TestSharedShellText checks that the part of init.bash and init.zsh that is
// meant to be the same text is the same text: what is tested under bash here
// is then what zsh runs in CI.
func TestSharedShellText(t *testing.T) {
	const first = "# What follows is the same text in init.bash and init.zsh"
	const last = "\n# Sets __sereno_reply to 1 when the first byte"
	cut := func(text string) string {
		start, end := strings.Index(text, first), strings.Index(text, last)
		if start < 0 || end < start {
			t.Fatal("the shared part was not found")
		}
		return text[start:end]
	}
	bash, zsh := cut(initFile(t, shells.Bash)), cut(initFile(t, shells.Zsh))
	if bash != zsh {
		t.Error("the shared part differs between init.bash and init.zsh")
	}
	if len(bash) < 6000 {
		t.Errorf("the shared part is only %d bytes long", len(bash))
	}
	// Written for bash 3.2 and for zsh: none of the constructs only one of
	// them, or only newer bash, understands.
	for name, pattern := range map[string]string{
		"an associative array":       `(declare|typeset|local) -A`,
		"lower-casing expansion":     `\$\{[A-Za-z_]+(,,|\^\^)`,
		"mapfile":                    `\b(mapfile|readarray)\b`,
		"a double-bracket test":      `(^|\s)\[\[\s`,
		"== in a test":               ` == `,
		"a command substitution":     "\\$\\([^(]|`",
		"a bash-only variable":       `BASH_|PROMPT_COMMAND|\bshopt\b`,
		"a zsh-only construct":       `\bemulate\b|\bsetopt\b|\$\{\(`,
		"a here-document or -string": `<<`,
	} {
		code := regexp.MustCompile(`(?m)^\s*#.*$`).ReplaceAllString(bash, "")
		if found := regexp.MustCompile(pattern).FindString(code); found != "" {
			t.Errorf("the shared part uses %s: %q", name, found)
		}
	}
}

// TestRoleSlot covers the conversion the zsh prompt needs.
func TestRoleSlot(t *testing.T) {
	roles, err := palette.LoadRoles()
	if err != nil {
		t.Fatal(err)
	}
	for role, want := range map[string]int{"prompt.path": 4, "prompt.branch": 5, "prompt.ok": 2, "prompt.failed": 1, "constant": 11, "comment": 8} {
		if got, err := roleSlot(roles, role); err != nil || got != want {
			t.Errorf("roleSlot(%s) = %d, %v; want %d", role, got, err, want)
		}
	}
	for _, role := range []string{"parameter", "selection", "variable", "nothing"} {
		if _, err := roleSlot(roles, role); err == nil {
			t.Errorf("roleSlot(%s) should fail: it is not one slot colour", role)
		}
	}
}
