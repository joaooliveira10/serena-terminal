package policy

import (
	"go/ast"
	"go/build"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/cli"
	"github.com/joaooliveira10/serena-terminal/internal/i18n"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// Where the command line lives.
const (
	cliDir   = "internal/cli/"
	mainFile = "cmd/sereno/main.go"
	module   = "github.com/joaooliveira10/serena-terminal"
)

// selectorsOf lists what a file selects from one imported package, sorted and
// without duplicates: "ReadFile", "Stat" for package os.
func selectorsOf(s source, importPath string) []string {
	names := map[string]bool{}
	for _, spec := range s.file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		if path != importPath {
			continue
		}
		name := importPath[strings.LastIndex(importPath, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names[name] = true
	}
	found := map[string]bool{}
	ast.Inspect(s.file, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if pkg, ok := selector.X.(*ast.Ident); ok && names[pkg.Name] {
				found[selector.Sel.Name] = true
			}
		}
		return true
	})
	var out []string
	for name := range found {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestCommandsStartNothingAndWriteThroughPlans is R2 for the command line,
// made structural. The commands of internal/cli
//
//   - use package os only to read (the list below is all they may call);
//   - never call the file layer of the engine themselves: every change of a
//     file is an operation of a plan, and plans are carried out in exactly
//     one place, which is what makes "the same code path for the dry run and
//     the real run" (R14) and the interrupt of R13 hold for every command;
//   - are handed their prober and their terminal, and so cannot start the one
//     process of R20 on their own or touch the console.
//
// Only cmd/sereno connects them to the real machine.
func TestCommandsStartNothingAndWriteThroughPlans(t *testing.T) {
	readOnly := map[string]bool{"ReadFile": true, "Stat": true}
	forbiddenImports := map[string]string{
		"os/exec": "starts processes", "os/signal": "belongs to cmd/sereno", "syscall": "reaches below the platform package",
		"unsafe": "reaches below the platform package", "flag": "would print its own English usage",
	}
	fileLayer := map[string]bool{"WriteFile": true, "Mkdir": true, "RemoveFile": true, "RemoveEmptyDir": true}
	applies, files := 0, 0
	for _, s := range goSources(t) {
		if !strings.HasPrefix(s.path, cliDir) {
			continue
		}
		files++
		for _, path := range imports(s) {
			if why, bad := forbiddenImports[path]; bad {
				t.Errorf("%s imports %s, which %s", s.path, path, why)
			}
		}
		for _, name := range selectorsOf(s, "os") {
			if !readOnly[name] {
				t.Errorf("%s uses os.%s: the commands only read through package os (R2)", s.path, name)
			}
		}
		for _, name := range selectorsOf(s, module+"/internal/engine") {
			if fileLayer[name] {
				t.Errorf("%s calls engine.%s: a command changes files through a plan only (R13)", s.path, name)
			}
		}
		for _, name := range selectorsOf(s, module+"/internal/target/shells") {
			if name == "NewProber" {
				t.Errorf("%s creates the prober itself: it is handed in through Env (R20)", s.path)
			}
		}
		for _, name := range selectorsOf(s, module+"/internal/platform") {
			if name == "EnableVT" || name == "IsTerminal" || name == "Current" {
				t.Errorf("%s calls platform.%s: the terminal and the host are handed in through Env", s.path, name)
			}
		}
		for _, name := range selectorsOf(s, module+"/internal/mode") {
			if name == "System" {
				t.Errorf("%s calls mode.System: the signal reader is handed in through Env", s.path)
			}
		}
		ast.Inspect(s.file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok && (selector.Sel.Name == "ApplyContext" || selector.Sel.Name == "Apply") {
				applies++
				if selector.Sel.Name == "Apply" {
					t.Errorf("%s: a plan is carried out with Apply, which cannot be interrupted (R13)", s.fset.Position(call.Pos()))
				}
			}
			return true
		})
	}
	if files < 8 {
		t.Fatalf("only %d files found in %s", files, cliDir)
	}
	if applies != 1 {
		t.Errorf("plans are carried out in %d places of %s, want exactly one", applies, cliDir)
	}
}

// TestMainOnlyConnects keeps cmd/sereno to what the repository layout gives
// it: it hands the real machine to the commands and stamps the version (R1).
func TestMainOnlyConnects(t *testing.T) {
	var main *source
	mainFiles := 0
	for _, s := range goSources(t) {
		if strings.HasPrefix(s.path, "cmd/") {
			mainFiles++
			if s.path == mainFile {
				found := s
				main = &found
			}
		}
	}
	if main == nil || mainFiles != 1 {
		t.Fatalf("cmd/ holds %d Go files, want only %s", mainFiles, mainFile)
	}
	allowed := map[string]bool{"Args": true, "Exit": true, "Stdout": true, "Stderr": true, "Executable": true, "Interrupt": true}
	for _, name := range selectorsOf(*main, "os") {
		if !allowed[name] {
			t.Errorf("%s uses os.%s", mainFile, name)
		}
	}
	// The version is a package variable of type string with a constant
	// initialiser: that is what -ldflags "-X main.version=..." can set.
	stamped := false
	declared := map[string]bool{}
	for _, decl := range main.file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			declared[d.Name.Name] = true
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				value := spec.(*ast.ValueSpec)
				for i, name := range value.Names {
					declared[name.Name] = true
					if name.Name != "version" || i >= len(value.Values) {
						continue
					}
					if selector, ok := value.Values[i].(*ast.SelectorExpr); ok && selector.Sel.Name == "DevVersion" {
						stamped = true
					}
				}
			}
		}
	}
	if !stamped || cli.DevVersion != "dev" {
		t.Errorf("%s must declare `var version = cli.DevVersion`, and DevVersion must be \"dev\" (R1)", mainFile)
	}
	for name := range declared {
		if name != "version" && name != "main" && name != "run" {
			t.Errorf("%s declares %s: commands, flags and texts belong to %s", mainFile, name, cliDir)
		}
	}
	position := main.fset.Position(main.file.End())
	if position.Line > 60 {
		t.Errorf("%s has %d lines; it only connects the machine to the commands", mainFile, position.Line)
	}
}

// TestHelpAndWarningsExistInBothLanguages is R5 for the texts whose keys are
// built at run time, which the scan of TestMessageKeysExist cannot see: the
// help of every command and of every flag, and the text of every warning id
// of `sereno status`. Key parity between the catalogues is tested in
// internal/i18n; here each key must exist and must actually be translated.
func TestHelpAndWarningsExistInBothLanguages(t *testing.T) {
	english, portuguese := i18n.For(i18n.English), i18n.For(i18n.Portuguese)
	check := func(key string) {
		t.Helper()
		if !i18n.Has(key) {
			t.Errorf("no text for %s in the catalogues", key)
			return
		}
		if english.T(key) == portuguese.T(key) {
			t.Errorf("%s has the same text in English and in pt-BR", key)
		}
	}
	commands := cli.Commands()
	if len(commands) != 8 {
		t.Errorf("%d commands, R3 has 8", len(commands))
	}
	keys := 0
	for _, name := range commands {
		for _, key := range cli.HelpKeys(name) {
			keys++
			check(key)
		}
	}
	if keys < 35 {
		t.Errorf("only %d help keys for %d commands", keys, len(commands))
	}
	ids := cli.WarningIDs()
	if len(ids) != 11 {
		t.Errorf("%d warning ids, R37 lists 11", len(ids))
	}
	for _, id := range ids {
		check(id)
	}
}

// dependencies returns every package the program is linked from on one
// system: what `go list -deps ./cmd/sereno` prints there. It is computed
// from the sources with go/build, without starting the go command.
func dependencies(t *testing.T, goos, goarch string) map[string][]string {
	t.Helper()
	root := testenv.RepoRoot(t)
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = goos, goarch, false
	importedBy := map[string][]string{}
	var visit func(path, from, fromDir string)
	visit = func(path, from, fromDir string) {
		if path == "unsafe" || path == "C" {
			return
		}
		importedBy[path] = append(importedBy[path], from)
		if len(importedBy[path]) > 1 {
			return
		}
		var pkg *build.Package
		var err error
		if path == module || strings.HasPrefix(path, module+"/") {
			pkg, err = ctx.ImportDir(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, module))), 0)
		} else if pkg, err = ctx.Import(path, fromDir, 0); err != nil {
			// A package the standard library vendors.
			pkg, err = ctx.Import("vendor/"+path, fromDir, 0)
		}
		if err != nil {
			t.Skipf("the sources of %s (imported by %s) cannot be read here: %v; `go list -deps ./cmd/sereno` in CI covers this", path, from, err)
		}
		for _, imported := range pkg.Imports {
			visit(imported, path, pkg.Dir)
		}
	}
	visit(module+"/cmd/sereno", "", "")
	return importedBy
}

// pureNet lists the two packages below net/ that the program may link. They
// come with text/template, hold no socket and import neither net nor
// net/http: net/url takes addresses apart as text, and net/netip is the
// address type it uses for that.
var pureNet = map[string]bool{"net/url": true, "net/netip": true}

// TestProgramDependencies is the dependency rule of R1 and the privacy
// promise behind it: the program links neither `net` nor `net/http`, on any
// of the three systems. Also: nothing outside the standard library and this
// module, no test support, and os/exec through the probe only.
func TestProgramDependencies(t *testing.T) {
	for _, target := range [][2]string{{"windows", "amd64"}, {"windows", "arm64"}, {"darwin", "arm64"}, {"darwin", "amd64"}, {"linux", "amd64"}, {"linux", "arm64"}} {
		deps := dependencies(t, target[0], target[1])
		if len(deps) < 60 {
			t.Fatalf("%s/%s: only %d packages found; the walk is probably broken", target[0], target[1], len(deps))
		}
		t.Logf("%s/%s: %d packages", target[0], target[1], len(deps))
		for path, from := range deps {
			first := strings.Split(strings.TrimPrefix(path, "vendor/"), "/")[0]
			switch {
			case path == "net" || path == "net/http" || (strings.HasPrefix(path, "net/") && !pureNet[path]):
				t.Errorf("%s/%s: the program links %s (through %s): sereno has no network code (R1, R2)", target[0], target[1], path, from[0])
			case path == "os/exec":
				for _, importer := range from {
					if importer != module+"/internal/target/shells" {
						t.Errorf("%s/%s: os/exec is imported by %s", target[0], target[1], importer)
					}
				}
			case path == "testing" || path == module+"/internal/testenv" || strings.HasPrefix(path, module+"/internal/tools"):
				t.Errorf("%s/%s: the program links %s (through %s)", target[0], target[1], path, from[0])
			case strings.Contains(first, ".") && !strings.HasPrefix(path, module) && !strings.HasPrefix(path, "vendor/") && !strings.HasPrefix(path, "golang.org/x/"):
				t.Errorf("%s/%s: the program depends on %s (R1: no require lines)", target[0], target[1], path)
			}
		}
		for _, wanted := range []string{module + "/internal/cli", module + "/internal/engine", module + "/internal/render", "os/exec", "os/signal"} {
			if _, ok := deps[wanted]; !ok {
				t.Errorf("%s/%s: %s is not among the dependencies; the walk is probably broken", target[0], target[1], wanted)
			}
		}
	}
}

// sandboxVariables are what must point into a temporary folder before a
// built sereno is started by a script: the sandbox itself, and every variable
// a home directory, a Windows Terminal folder or a profile is found through.
var sandboxVariables = []string{
	"SERENO_SANDBOX", "HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA",
	"SERENO_PS_PROFILE_PATH", "SERENO_PWSH_PROFILE_PATH",
}

// rootVariables move sereno's folder away from its default place below
// LOCALAPPDATA or HOME. A script that starts the program either points them
// into the temporary folder as well, or makes sure they are not set: the
// default folder then lies in the temporary folder, because LOCALAPPDATA and
// HOME do. The first pass of each smoke script does the second, so that the
// program is run the way a machine without those variables runs it (the
// forms of R18 and R21 that name the folder through a variable).
var rootVariables = []string{"SERENO_HOME", "XDG_DATA_HOME"}

// TestTheBuiltProgramOnlyRunsInASandbox is the safety rule of the scripts
// made structural: the program that `go build` produces is never started
// against the real home. Only the two smoke scripts start it, and they set
// the whole sandbox first: scripts/smoke.sh through `env -i` on every start,
// scripts\smoke.ps1 in its own environment before the first start. The test
// wrappers hand the program to those scripts and never run it themselves.
func TestTheBuiltProgramOnlyRunsInASandbox(t *testing.T) {
	root := testenv.RepoRoot(t)
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	code := func(text string) []string { // the lines that are not comments
		var lines []string
		for _, line := range strings.Split(text, "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				lines = append(lines, line)
			}
		}
		return lines
	}

	// scripts/smoke.sh: `clean` is env -i with every variable, and "$exe" is
	// started through it only.
	shell := read("scripts/smoke.sh")
	start := strings.Index(shell, "\nclean() {\n")
	end := strings.Index(shell, "\nsereno() {")
	if end < 0 {
		end = strings.Index(shell, "\nrun() {")
	}
	if start < 0 || end < start {
		t.Fatal("scripts/smoke.sh has no clean() in front of run()")
	}
	clean := shell[start:end]
	// The command itself: from `env -i` to the program it starts. Nothing of
	// the caller reaches the program, so what is not named here is not set.
	command := ""
	if at := strings.Index(clean, "    env -i "); at >= 0 {
		command = clean[at:]
	} else {
		t.Error("clean() of scripts/smoke.sh does not start from an empty environment (env -i)")
	}
	for _, name := range sandboxVariables {
		if !regexp.MustCompile(`[ \t]` + name + `="\$(home|tmp)\b`).MatchString(command) {
			t.Errorf("the env -i of scripts/smoke.sh does not point %s into the temporary folder", name)
		}
	}
	// The variables that move sereno's folder: not in the command (the first
	// pass runs without them), and into the temporary folder where they are
	// put in front of it.
	for _, name := range rootVariables {
		if strings.Contains(command, name+"=") {
			t.Errorf("the env -i of scripts/smoke.sh sets %s on every start; the first pass must run with the default folder", name)
		}
		set := regexp.MustCompile(`\b`+name+`=("?)([^ "]*)`).FindAllStringSubmatch(clean, -1)
		if len(set) != 1 || !regexp.MustCompile(`^\$(home|root)\b`).MatchString(set[0][2]) {
			t.Errorf("clean() of scripts/smoke.sh sets %s %d times, want once and into the temporary folder: %v", name, len(set), set)
		}
	}
	if !strings.Contains(clean, `if [ -n "$moved" ]; then set -- `) || !regexp.MustCompile(`(?m)^root="?\$home/`).MatchString(shell) || regexp.MustCompile(`(?m)^root=["']?/`).MatchString(shell) {
		t.Error("scripts/smoke.sh must set the folder variables only for a moved folder, and move the folder into the temporary home")
	}
	starts := 0
	for _, line := range code(shell) {
		if !strings.Contains(line, `"$exe"`) && !strings.Contains(line, "$tmp/bin/sereno") {
			continue
		}
		switch {
		case strings.Contains(line, `clean "$exe" "$@"`):
			starts++
		case strings.HasPrefix(line, "exe=$tmp/bin/sereno"), strings.HasPrefix(line, `cp "$program" "$tmp/bin/sereno"`), strings.HasPrefix(line, `cp "$exe" `):
		default:
			t.Errorf("scripts/smoke.sh uses the program outside clean():\n  %s", strings.TrimSpace(line))
		}
	}
	if starts != 1 {
		t.Errorf("scripts/smoke.sh starts the program in %d places, want the one inside run()", starts)
	}
	// The sessions that load a hooked start-up file go through clean() too.
	for _, line := range code(shell) {
		if regexp.MustCompile(`(^|\s)(bash|zsh|pwsh)\s+-`).MatchString(line) && !strings.Contains(line, "clean ") {
			t.Errorf("scripts/smoke.sh starts a shell outside clean():\n  %s", strings.TrimSpace(line))
		}
	}

	// scripts\smoke.ps1: the table has every variable, it is applied before
	// the program or a shell is started, and the program is started in one
	// place.
	power := read("scripts/smoke.ps1")
	table := regexp.MustCompile(`(?s)\$sandbox = @\{.*?\n\}`).FindString(power)
	inside := `\(?(\$userHome|\$local|\$tmp|Join-Path \$(userHome|local|tmp))`
	for _, name := range sandboxVariables {
		if !regexp.MustCompile(`(^|[\s;{])` + name + ` = ` + inside).MatchString(table) {
			t.Errorf("the sandbox of scripts/smoke.ps1 does not point %s into the temporary folder", name)
		}
	}
	// The variables that move sereno's folder are in the table as well, so
	// that a value of the caller cannot reach the program. SERENO_HOME is
	// taken out of the environment: the first pass runs with the default
	// folder. XDG_DATA_HOME does not move the folder on Windows.
	if !regexp.MustCompile(`(^|[\s;{])SERENO_HOME = \$null\b`).MatchString(table) {
		t.Error("the sandbox of scripts/smoke.ps1 does not take SERENO_HOME out of the environment; the first pass must run with the default folder")
	}
	if !regexp.MustCompile(`(^|[\s;{])XDG_DATA_HOME = (\$null|` + inside + `)`).MatchString(table) {
		t.Error("the sandbox of scripts/smoke.ps1 neither clears XDG_DATA_HOME nor points it into the temporary folder")
	}
	// Later the folder is moved once, with SERENO_HOME, into the temporary home.
	moves := regexp.MustCompile(`(?m)^.*SetEnvironmentVariable\('SERENO_HOME'.*$`).FindAllString(power, -1)
	if len(moves) != 1 || strings.TrimSpace(moves[0]) != "[Environment]::SetEnvironmentVariable('SERENO_HOME', $root)" {
		t.Errorf("scripts/smoke.ps1 sets SERENO_HOME outside the table in another way than the one expected: %v", moves)
	}
	for _, assignment := range regexp.MustCompile(`(?m)^\s*\$root = .*$`).FindAllString(power, -1) {
		if !regexp.MustCompile(`^\s*\$root = Join-Path \$(userHome|local) `).MatchString(assignment) {
			t.Errorf("scripts/smoke.ps1 puts sereno's folder somewhere else than into the temporary home: %s", strings.TrimSpace(assignment))
		}
	}
	applied := strings.Index(power, "foreach ($key in $sandbox.Keys) {")
	firstRun := strings.Index(power, "Invoke-Sereno @(")
	firstShell := regexp.MustCompile(`&\s+\$(windowsPowerShell|bash)\s`).FindStringIndex(power)
	if applied < 0 || firstRun < applied || firstShell == nil || firstShell[0] < applied {
		t.Error("scripts/smoke.ps1 starts the program or a shell before the sandbox is in the environment")
	}
	if n := strings.Count(power, "& $binary "); n != 1 {
		t.Errorf("scripts/smoke.ps1 starts the program in %d places, want the one inside Invoke-Sereno", n)
	}
	// The second way: several one right after the other, as processes of
	// their own that inherit the environment the sandbox was put into. Like
	// Invoke-Sereno it is a function, defined before the sandbox and called
	// after it.
	if n := strings.Count(power, "$start.FileName = $binary"); n != 1 || strings.Count(power, "FileName") != 1 {
		t.Errorf("scripts/smoke.ps1 hands the program to a process in %d places, want the one inside Start-Together", n)
	}
	if strings.Contains(power, ".EnvironmentVariables") || strings.Contains(power, ".Environment[") || strings.Contains(power, "Start-Process") || strings.Contains(power, "Start-Job") {
		t.Error("scripts/smoke.ps1 gives a process an environment of its own, or starts one in a way this test does not know")
	}
	if together := strings.Index(power, "Start-Together @("); applied < 0 || together < applied {
		t.Error("scripts/smoke.ps1 starts the program several times before the sandbox is in the environment")
	}
	if strings.Contains(power, "& $program") || strings.Contains(power, "& $Sereno") {
		t.Error("scripts/smoke.ps1 starts the program it was given instead of its copy")
	}

	// What running with the default folder is for: the forms of R18 and R21
	// that name the folder through a variable are produced by the built
	// program and compared, letter for letter. The fragment is one of two
	// files: `--shell auto` takes PowerShell 7 where pwsh.exe is on PATH, as
	// it is on the Windows runners of CI, so the script has to ask the same
	// question and must know both.
	for script, lines := range map[string][]string{
		"scripts/smoke.ps1": {
			`'if (Test-Path -LiteralPath "$env:LOCALAPPDATA\sereno\shell\init.ps1") { . "$env:LOCALAPPDATA\sereno\shell\init.ps1" } # sereno:hook'`,
			`'[ -r "$LOCALAPPDATA/sereno/shell/init.bash" ] && . "$LOCALAPPDATA/sereno/shell/init.bash" # sereno:hook'`,
			`'fragment\auto-soft.json'`,
			`if (Get-Command pwsh.exe -CommandType Application -ErrorAction SilentlyContinue) { $wantedFragment = 'fragment\pwsh.json' }`,
		},
		"scripts/smoke.sh": {
			`'[ -r "$HOME/.local/share/sereno/shell/init.bash" ] && . "$HOME/.local/share/sereno/shell/init.bash" # sereno:hook'`,
		},
	} {
		text := read(script)
		for _, line := range lines {
			if !strings.Contains(text, line) {
				t.Errorf("%s does not compare what the program writes for the default folder with %s", script, line)
			}
		}
	}

	// The wrappers build the program and hand it over; they never run it.
	for _, line := range code(read("scripts/test.ps1")) {
		if !strings.Contains(strings.ToLower(line), "$sereno") {
			continue
		}
		allowed := strings.Contains(line, "$sereno = Join-Path $tmp ") || strings.Contains(line, "'-o', $sereno,") ||
			strings.Contains(line, "Test-Path -LiteralPath $sereno") || strings.Contains(line, `scripts\smoke.ps1') -Sereno $sereno `)
		if !allowed {
			t.Errorf("scripts/test.ps1 uses the built program other than to build it and to hand it to smoke.ps1:\n  %s", strings.TrimSpace(line))
		}
	}
	for _, line := range code(read("scripts/test.sh")) {
		if !strings.Contains(line, "/bin/sereno") {
			continue
		}
		allowed := strings.Contains(line, `-o "$tmp/bin/sereno" ./cmd/sereno`) || strings.Contains(line, `sh scripts/smoke.sh "$tmp/bin/sereno" `)
		if !allowed {
			t.Errorf("scripts/test.sh uses the built program other than to build it and to hand it to smoke.sh:\n  %s", strings.TrimSpace(line))
		}
	}
	// No other script of the repository knows the program at all. The tests
	// of the install scripts work on stand-ins, and the install scripts
	// themselves place a program without starting it (see install_test.go,
	// which also keeps scripts/release.sh to building).
	for _, name := range []string{
		"scripts/bench.ps1", "scripts/bench.sh", "test/shell/run.ps1", "test/shell/run.bash", "test/shell/run.zsh",
		"test/install/run.ps1", "test/install/run.sh", "scripts/install.ps1.tmpl", "scripts/install.sh.tmpl",
	} {
		if regexp.MustCompile(`go build[^\n]*cmd/sereno|cmd[/\\]sereno`).MatchString(read(name)) {
			t.Errorf("%s builds or runs the program; only the smoke scripts may", name)
		}
	}
}
