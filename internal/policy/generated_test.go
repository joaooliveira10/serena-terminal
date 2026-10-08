package policy

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/render"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// templatesDir holds what the renderer is made of.
const templatesDir = "internal/render/templates"

// TestTemplatesArePlainText keeps the templates printable: ASCII, line feeds,
// no tabs. Whatever a template holds ends up, byte for byte, in a file that a
// shell sources, and R23 asks those files to be ASCII. (That the templates
// hold no colour value is TestOnlyThePaletteFileHoldsHexValues.)
func TestTemplatesArePlainText(t *testing.T) {
	dir := filepath.Join(testenv.RepoRoot(t), filepath.FromSlash(templatesDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".tmpl") {
			t.Errorf("%s/%s is not a template", templatesDir, entry.Name())
			continue
		}
		count++
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for i, b := range data {
			if b != '\n' && (b < 0x20 || b > 0x7e) {
				t.Errorf("%s/%s: byte %#x at offset %d is not printable ASCII", templatesDir, entry.Name(), b, i)
				break
			}
		}
	}
	if count != 9 {
		t.Errorf("%d templates, want the 8 of the repository layout and the part init.bash and init.zsh share", count)
	}
}

// generated renders the three init files, keyed by their name below ROOT.
func generated(t *testing.T) map[string]string {
	t.Helper()
	p, err := palette.Load()
	if err != nil {
		t.Fatal(err)
	}
	files, err := render.InitFiles(p)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range files {
		out[f.Name] = string(f.Data)
	}
	if len(out) != 3 {
		t.Fatalf("%d init files, want 3", len(out))
	}
	return out
}

// withoutComments blanks the lines that are comments, so that a rule about
// what the code does is not tripped by a sentence that explains it.
func withoutComments(text string) string {
	return regexp.MustCompile(`(?m)^[ \t]*#.*$`).ReplaceAllString(text, "")
}

// singleQuoted matches a string between single quotes on one line, in
// PowerShell (where a quote inside is doubled) and in the shells (where the
// ANSI-C form has a dollar sign in front).
var singleQuoted = regexp.MustCompile(`\$?'(?:[^'\n]|'')*'`)

// TestGeneratedShellFilesStartNoProcess is the last sentence of R2: "The shell
// files it generates start no process and contain no network call". The init
// files are rendered and their code is searched for every way a PowerShell,
// bash or zsh script has of starting a program, running a string as code,
// loading other code or reaching the network. The shell tests then prove the
// same thing from the other side, by running the files with PATH emptied.
func TestGeneratedShellFilesStartNoProcess(t *testing.T) {
	files := generated(t)

	powerShell := map[string]string{
		"a cmdlet that starts, loads or downloads something":      `(?i)\b(start-process|start-job|start-threadjob|invoke-expression|iex|invoke-command|invoke-item|invoke-webrequest|invoke-restmethod|new-object|add-type|import-module|install-module|register-\w+|new-pssession|set-executionpolicy|set-itemproperty|new-item|remove-item|set-content|add-content|out-file)\b`,
		"a type that starts a process or opens a connection":      `(?i)\[(system\.)?(diagnostics\.|net\.|io\.pipes|management\.automation\.(powershell|runspaces)|reflection\.|runtime\.interopservices|scriptblock|activator|appdomain)`,
		"the call operator on anything but a variable or a block": `&\s*[^\s${]`,
		"a redirection to a file":                                 `[^-=]>\s*[$'"A-Za-z]`,
		"a program named directly":                                `(?im)(^|[\s;{(|])(cmd|powershell|pwsh|git|reg|wscript|cscript|rundll32|schtasks|curl|wget)(\.exe)?\s`,
		"a write to the registry":                                 `(?i)SetValue|CreateSubKey|DeleteValue|HKEY_LOCAL_MACHINE|HKLM:|HKCU:`,
		"a change of the location":                                `(?i)\b(set-location|push-location|pop-location|cd|chdir|sl)\s`,
		"a use of the backtick-e":                                 "`e",
		"a profile or a stored policy":                            `(?i)\$PROFILE\b|ExecutionPolicy`,
	}
	// Text between single quotes is data: patterns, keys, the palette. It
	// is taken out so that a ">" inside a pattern is not read as a
	// redirection. Double-quoted strings stay: they expand.
	code := singleQuoted.ReplaceAllString(withoutComments(files["shell/init.ps1"]), "''")
	for name, pattern := range powerShell {
		if found := regexp.MustCompile(pattern).FindString(code); found != "" {
			t.Errorf("init.ps1 has %s: %q (R2)", name, found)
		}
	}
	// What it is allowed to call, counted so that the patterns above are
	// known to look at real code: the call operator on variables and blocks.
	if n := len(regexp.MustCompile(`&\s*[${]`).FindAllString(code, -1)); n < 10 {
		t.Errorf("only %d uses of the call operator found in init.ps1; the scan is probably broken", n)
	}
	// The registry is read, and only below HKEY_CURRENT_USER.
	registry := regexp.MustCompile(`\[Microsoft\.Win32\.Registry\]::(\w+)\('([A-Z_]+)\\`).FindAllStringSubmatch(withoutComments(files["shell/init.ps1"]), -1)
	if len(registry) != 3 {
		t.Errorf("%d registry calls in init.ps1, want the three reads of R28", len(registry))
	}
	for _, call := range registry {
		if call[1] != "GetValue" || call[2] != "HKEY_CURRENT_USER" {
			t.Errorf("init.ps1 calls Registry::%s on %s; only GetValue on HKEY_CURRENT_USER is allowed (R2)", call[1], call[2])
		}
	}

	shell := map[string]string{
		"a command substitution": "\\$\\([^(]|`",
		"a pipe":                 `[^|]\|[ \t]|[ \t]\|[^|]`,
		"a background job":       `[^&>]&([^&>]|$)`,
		"a process substitution or here-document":             `<\(|>\(|<<`,
		"eval, exec or a trap":                                `(^|[;{\s])(eval|exec|trap|command|coproc|nohup|sudo|su)\s`,
		"the source of another file":                          `(^|[;{\s])(source|\.)\s+["$/~A-Za-z]`,
		"a module or a function loaded from disk":             `\b(zmodload|autoload|enable)\b`,
		"a network device or tool":                            `/dev/(tcp|udp)|\b(curl|wget|nc|ssh|ztcp|zsocket)\b`,
		"a key binding, alias, completion or history setting": `(^|[;{\s])(bind|bindkey|alias|unalias|complete|compdef|compinit|history|fc|zle)\s|HIST[A-Z]*=|SAVEHIST|shopt -[su]|set [-+]o\b`,
		"a change of the directory or of PATH":                `(^|[;{\s])(cd|pushd|popd)\s|(^|[\s;])PATH=|\bpath=\(`,
		"a write to a file":                                   `>>|[^0-9&]>\s*[^\s&/]|>\s*/[^d]`,
	}
	// Redirections to /dev/null, the only ones there are: the look for sereno
	// on PATH; the three files of the user's that are read before a prompt
	// (the mode file, a .git file, HEAD), which another program may hold at
	// that moment; and in init.bash the one place where a variable of the
	// user is set, where a read-only one must not make bash speak.
	silenced := map[string]int{"shell/init.bash": 5, "shell/init.zsh": 4}
	for _, name := range []string{"shell/init.bash", "shell/init.zsh"} {
		code := singleQuoted.ReplaceAllString(withoutComments(files[name]), "''")
		for what, pattern := range shell {
			if found := regexp.MustCompile(`(?m)` + pattern).FindString(code); found != "" {
				t.Errorf("%s has %s: %q (R2)", name, what, found)
			}
		}
		if n := strings.Count(code, "> /dev/null"); n != silenced[name] {
			t.Errorf("%s redirects to /dev/null %d times, want %d", name, n, silenced[name])
		}
		for _, read := range []string{`} 2> /dev/null < "$__sereno_root/mode" || :`, `IFS= read -r line 2> /dev/null < "$git" || :`, `IFS= read -r line 2> /dev/null < "$head" || :`} {
			if !strings.Contains(code, read+"\n") {
				t.Errorf("%s does not read quietly with: %s", name, read)
			}
		}
		if name == "shell/init.bash" && !strings.Contains(code, "printf -v \"$1\" '' \"$2\" 2> /dev/null || failed=1\n") {
			t.Errorf("%s: __sereno_set does not set a variable quietly", name)
		}
		if !strings.Contains(code, "export LS_COLORS\n") || !strings.Contains(code, "export LSCOLORS\n") || strings.Count(code, "export ") != 2 {
			t.Errorf("%s must export LS_COLORS and LSCOLORS and nothing else (R33)", name)
		}
	}
}

// TestGeneratedShellFilesAreASCII is R23 on the rendered files: printable
// ASCII and line feeds, nothing else, and no trace of the machine that
// rendered them.
func TestGeneratedShellFilesAreASCII(t *testing.T) {
	for name, text := range generated(t) {
		for i := 0; i < len(text); i++ {
			if b := text[i]; b != '\n' && (b < 0x20 || b > 0x7e) {
				t.Errorf("%s: byte %#x at offset %d is not printable ASCII (R23)", name, b, i)
				break
			}
		}
		if homePath.MatchString(text) || email.MatchString(text) {
			t.Errorf("%s names a home folder or an address", name)
		}
	}
}

// TestScriptsReachBashByAnyPath keeps the two halves of one mistake apart:
// PowerShell names files with backslashes, and a bash script that finds the
// repository through its own name must cope with that. Where a PowerShell
// script hands a shell script to bash, it turns the path around; and the
// bash scripts do not rely on it.
func TestScriptsReachBashByAnyPath(t *testing.T) {
	root := testenv.RepoRoot(t)
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	shellFile := regexp.MustCompile(`Join-Path[^)]*\.(sh|bash|zsh)'`)
	handed := 0
	for _, name := range []string{"scripts/test.ps1", "scripts/bench.ps1", "test/shell/run.ps1"} {
		for number, line := range strings.Split(read(name), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") || !shellFile.MatchString(line) {
				continue
			}
			handed++
			if !strings.Contains(line, `.Replace('\', '/')`) && !strings.Contains(line, "& $posix ") {
				t.Errorf("%s:%d names a shell script by a path with backslashes:\n  %s", name, number+1, strings.TrimSpace(line))
			}
		}
	}
	if handed < 2 {
		t.Errorf("only %d lines found that hand a shell script to bash; the pattern of this test no longer matches the scripts", handed)
	}
	for _, name := range []string{"scripts/bench.sh", "test/shell/run.bash"} {
		text := read(name)
		if !strings.Contains(text, "here=${BASH_SOURCE[0]}\ncase ${OSTYPE-} in msys*|cygwin*) here=${here//\\\\//} ;; esac\ncase $here in */*) here=${here%/*} ;; *) here=. ;; esac\n") {
			t.Errorf("%s does not find its folder in a way that survives a path with backslashes", name)
		}
		if !strings.Contains(text, `--where) printf '%s\n' "$repo"; exit 0 ;;`) {
			t.Errorf("%s has no --where, which test/shell/run.bash uses to prove the above in a real bash", name)
		}
	}
}

// startRule is one way the scripts start a shell, and what must then be on
// the same line.
type startRule struct {
	what    string
	line    *regexp.Regexp
	needs   string
	atLeast int
}

// TestScriptsStartShellsWithoutProfiles is the safety rule of the shell tests
// made structural: wherever a script of the repository starts PowerShell, it
// says -NoProfile; wherever it starts bash, --noprofile --norc; zsh, -f. A
// test that loaded the developer's real profile would be testing the wrong
// thing and could change the wrong files.
func TestScriptsStartShellsWithoutProfiles(t *testing.T) {
	rules := []startRule{
		{"PowerShell through an argument string", regexp.MustCompile(`(?i)arguments\s*=\s*'-|arguments=\(-`), "-NoProfile", 6},
		{"PowerShell through the call operator", regexp.MustCompile(`&\s+\$(shell\[1\]|windowsPowerShell|exe)\s`), "-NoProfile", 2},
		{"PowerShell by name", regexp.MustCompile(`(^|[^d]\s)(pwsh|powershell)(\.exe)?\s+-`), "-NoProfile", 1},
		{"bash", regexp.MustCompile(`("\$BASH"|\$bash|(^|\s)bash)\s+-`), "--noprofile --norc", 8},
		{"zsh", regexp.MustCompile(`(\$zsh|(^|\s)zsh|command -v zsh\)")\s+-`), " -f", 4},
	}
	seen := make([]int, len(rules))
	root := testenv.RepoRoot(t)
	scripts := 0
	for _, dir := range []string{"scripts", "test/shell"} {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(dir)), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scripts++
			rel, _ := filepath.Rel(root, path)
			text := string(data)
			for number, line := range strings.Split(text, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "#") {
					continue
				}
				for i, rule := range rules {
					if !rule.line.MatchString(line) {
						continue
					}
					seen[i]++
					if !strings.Contains(line, rule.needs) {
						t.Errorf("%s:%d starts %s without %s:\n  %s", filepath.ToSlash(rel), number+1, rule.what, rule.needs, strings.TrimSpace(line))
					}
				}
			}
			// A child process of a test never gets a window, and PSReadLine
			// never saves what a test typed.
			if strings.Contains(text, "ProcessStartInfo") && !strings.Contains(text, "CreateNoWindow = $true") {
				t.Errorf("%s starts processes without CreateNoWindow", filepath.ToSlash(rel))
			}
			if strings.Contains(text, "Import-Module PSReadLine") && !strings.Contains(text, "-HistorySaveStyle SaveNothing") {
				t.Errorf("%s loads PSReadLine without turning its history file off", filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if scripts < 7 {
		t.Errorf("only %d scripts found under scripts/ and test/shell/", scripts)
	}
	for i, rule := range rules {
		if seen[i] < rule.atLeast {
			t.Errorf("%s is started on %d lines, want at least %d: the pattern of this test no longer matches the scripts", rule.what, seen[i], rule.atLeast)
		}
	}
}
