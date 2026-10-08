package policy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/i18n"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// Paths, relative to the root of the repository and with forward slashes,
// that the rules below name.
const (
	fsxFile     = "internal/engine/fsx.go"               // the only file that changes the file system
	probeFile   = "internal/target/shells/probe_exec.go" // the only file that starts a process
	paletteFile = "internal/palette/serena.json"         // the only source file with hex colours
	testenvDir  = "internal/testenv/"                    // test support, never linked into sereno
	i18nDir     = "internal/i18n/"                       // where the message texts live
)

// source is one Go file of the module that is not a test.
type source struct {
	path string // relative, forward slashes
	file *ast.File
	fset *token.FileSet
}

// goSources parses every non-test Go file below the root of the repository.
func goSources(t *testing.T) []source {
	t.Helper()
	root := testenv.RepoRoot(t)
	fset := token.NewFileSet()
	var sources []source
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "testdata" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sources = append(sources, source{path: filepath.ToSlash(rel), file: file, fset: fset})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) < 10 {
		t.Fatalf("only %d Go files found below %s", len(sources), root)
	}
	return sources
}

func imports(s source) []string {
	var paths []string
	for _, spec := range s.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err == nil {
			paths = append(paths, path)
		}
	}
	return paths
}

// TestNoNetworkAndOneProcess is R2 on imports: no file imports package net or
// anything below it, and os/exec appears in exactly one file, the PowerShell
// probe of R20. cgo and plugins are out as well: the build is CGO_ENABLED=0.
func TestNoNetworkAndOneProcess(t *testing.T) {
	probeSeen := false
	for _, s := range goSources(t) {
		for _, path := range imports(s) {
			switch {
			case path == "net" || strings.HasPrefix(path, "net/"):
				t.Errorf("%s imports %s: sereno has no network code (R2)", s.path, path)
			case path == "os/exec" && s.path == probeFile:
				probeSeen = true
			case path == "os/exec":
				t.Errorf("%s imports os/exec: only %s may start a process (R2, R20)", s.path, probeFile)
			case path == "C" || path == "plugin":
				t.Errorf("%s imports %s", s.path, path)
			case !strings.Contains(strings.Split(path, "/")[0], ".") || strings.HasPrefix(path, "github.com/joaooliveira10/serena-terminal/"):
				// The standard library or this module.
			default:
				t.Errorf("%s imports %s: the module has no dependencies (R1)", s.path, path)
			}
		}
	}
	if !probeSeen {
		t.Errorf("%s no longer imports os/exec; update this test if the probe moved", probeFile)
	}
}

// mutators are the functions of package os that change the file system. Only
// fsx.go may call them, so the SERENO_SANDBOX guard cannot be bypassed (R10).
var mutators = map[string]bool{
	"Chmod": true, "Chown": true, "Chtimes": true, "Create": true, "CreateTemp": true,
	"Lchown": true, "Link": true, "Mkdir": true, "MkdirAll": true, "MkdirTemp": true,
	"NewFile": true, "OpenFile": true, "OpenRoot": true, "Remove": true, "RemoveAll": true,
	"Rename": true, "Symlink": true, "Truncate": true, "WriteFile": true, "CopyFS": true,
}

// TestWritesGoThroughTheGuard looks for calls such as os.WriteFile outside
// fsx.go. Test support is exempt: it prepares temporary homes before the code
// under test runs.
func TestWritesGoThroughTheGuard(t *testing.T) {
	for _, s := range goSources(t) {
		if s.path == fsxFile || strings.HasPrefix(s.path, testenvDir) {
			continue
		}
		osNames := map[string]bool{}
		for _, spec := range s.file.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			switch {
			case path == "io/ioutil":
				t.Errorf("%s imports io/ioutil", s.path)
			case path != "os":
			case spec.Name != nil:
				osNames[spec.Name.Name] = true
			default:
				osNames["os"] = true
			}
		}
		ast.Inspect(s.file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := selector.X.(*ast.Ident); ok && osNames[pkg.Name] && mutators[selector.Sel.Name] {
				t.Errorf("%s: os.%s; every write goes through %s (R10)",
					s.fset.Position(selector.Pos()), selector.Sel.Name, fsxFile)
			}
			return true
		})
	}
}

// The imports above are not the only way to a child process, a socket or the
// registry: package os can start a process, and package syscall can do all
// three. The lists below are everything sereno uses of package syscall and of
// the Windows DLLs today. Each name was read and found harmless (reading a
// registry value, asking the console for its mode and code page, converting
// text, one ioctl that reads the terminal settings); anything that is not on
// a list fails the test, and so does a call that could do more than the name
// says.
var (
	allowedSyscall = map[string]bool{
		// Errors and plain types.
		"Errno": true, "ENOTDIR": true, "ERROR_ACCESS_DENIED": true, "ERROR_MORE_DATA": true, "Handle": true,
		// The lock of sereno's folder, on a file the engine has opened itself:
		// flock on macOS and Linux, and on Windows the structure LockFileEx
		// wants (see allowedProcedures). Neither opens or creates a file.
		"Flock": true, "LOCK_EX": true, "LOCK_SH": true, "LOCK_NB": true, "LOCK_UN": true, "EWOULDBLOCK": true, "Overlapped": true,
		// Text for the Windows API.
		"UTF16PtrFromString": true,
		// Reading values under HKEY_CURRENT_USER (R28). There is no name
		// here that writes one.
		"HKEY_CURRENT_USER": true, "KEY_QUERY_VALUE": true, "RegOpenKeyEx": true, "RegQueryValueEx": true, "RegCloseKey": true,
		"REG_SZ": true, "REG_EXPAND_SZ": true, "REG_DWORD": true,
		// Is this a terminal?
		"GetConsoleMode": true, "Syscall": true, "SYS_IOCTL": true, "Termios": true, "TCGETS": true, "TIOCGETA": true,
		// Procedures of kernel32.dll, see allowedProcedures.
		"NewLazyDLL": true,
	}
	allowedLibraries  = map[string]bool{"kernel32.dll": true}
	allowedProcedures = map[string]bool{
		"GetConsoleOutputCP": true, "GetOEMCP": true, "MultiByteToWideChar": true, // decode what PowerShell printed
		"SetConsoleMode":           true, // switch escape sequences on for sereno's own output
		"GetUserDefaultUILanguage": true, // which language to speak (R5)
		"LockFileEx":               true, // one sereno at a time writes the folder (R13) ...
		"UnlockFileEx":             true, // ... and lets go of it again
	}
)

// lowLevel returns what a file does below the level the import rules see:
// starting a process through package os, and every use of package syscall or
// of a DLL that is not on the lists above.
func lowLevel(file *ast.File) []string {
	names := map[string]string{} // local name -> import path
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		if path != "os" && path != "syscall" {
			continue
		}
		name := path
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names[name] = path
	}
	// from returns the name selected from package os or syscall, if any.
	from := func(expr ast.Expr, pkg string) string {
		selector, ok := expr.(*ast.SelectorExpr)
		if !ok {
			return ""
		}
		if ident, ok := selector.X.(*ast.Ident); ok && names[ident.Name] == pkg {
			return selector.Sel.Name
		}
		return ""
	}
	text := func(expr ast.Expr) (string, bool) {
		lit, ok := expr.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(lit.Value)
		return value, err == nil
	}

	var found []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.SelectorExpr:
			if name := from(n, "os"); name == "StartProcess" {
				found = append(found, "os.StartProcess starts a process")
			}
			if name := from(n, "syscall"); name != "" && !allowedSyscall[name] {
				found = append(found, "syscall."+name+" is not on the list of what sereno uses")
			}
		case *ast.CallExpr:
			switch from(n.Fun, "syscall") {
			case "Syscall":
				// The one system call is the ioctl that reads the terminal
				// settings; the number must say so in words.
				if len(n.Args) == 0 || from(n.Args[0], "syscall") != "SYS_IOCTL" {
					found = append(found, "syscall.Syscall with a number other than syscall.SYS_IOCTL")
				}
			case "RegOpenKeyEx":
				if len(n.Args) < 4 || from(n.Args[3], "syscall") != "KEY_QUERY_VALUE" {
					found = append(found, "syscall.RegOpenKeyEx with access other than syscall.KEY_QUERY_VALUE")
				}
			case "NewLazyDLL":
				if name, ok := text(firstArg(n)); !ok || !allowedLibraries[strings.ToLower(name)] {
					found = append(found, "syscall.NewLazyDLL of a library that is not on the list")
				}
			}
			if selector, ok := n.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "NewProc" {
				if name, ok := text(firstArg(n)); !ok || !allowedProcedures[name] {
					found = append(found, "NewProc of a procedure that is not on the list: "+name)
				}
			}
		}
		return true
	})
	return found
}

// firstArg returns the first argument of a call, or nil.
func firstArg(call *ast.CallExpr) ast.Expr {
	if len(call.Args) == 0 {
		return nil
	}
	return call.Args[0]
}

// TestNoProcessSocketOrRegistryWriteBySyscall is R2 below the imports: no
// file starts a process through package os, and package syscall is used for
// the listed names only. Test support is exempt from the second rule (it
// creates junctions for the sandbox tests) and never reaches the program.
func TestNoProcessSocketOrRegistryWriteBySyscall(t *testing.T) {
	used := map[string]bool{}
	for _, s := range goSources(t) {
		if strings.HasPrefix(s.path, testenvDir) {
			for _, problem := range lowLevel(s.file) {
				if strings.HasPrefix(problem, "os.") {
					t.Errorf("%s: %s (R2)", s.path, problem)
				}
			}
			continue
		}
		for _, problem := range lowLevel(s.file) {
			t.Errorf("%s: %s (R2)", s.path, problem)
		}
		ast.Inspect(s.file, func(node ast.Node) bool {
			if selector, ok := node.(*ast.SelectorExpr); ok {
				if ident, ok := selector.X.(*ast.Ident); ok && ident.Name == "syscall" {
					used[selector.Sel.Name] = true
				}
			}
			return true
		})
	}
	// A list that has grown stale would let a forgotten name back in.
	for name := range allowedSyscall {
		if !used[name] {
			t.Errorf("syscall.%s is on the list and no longer used: take it off", name)
		}
	}
}

// TestLowLevelScan proves that the scan above catches what it is for.
func TestLowLevelScan(t *testing.T) {
	const sample = `package sample

import (
	"os"
	sys "syscall"
)

var (
	kernel32 = sys.NewLazyDLL("kernel32.dll")
	advapi32 = sys.NewLazyDLL("advapi32.dll")
	fine     = kernel32.NewProc("GetOEMCP")
	write    = advapi32.NewProc("RegSetValueExW")
	spawn    = kernel32.NewProc("CreateProcessW")
	hidden   = kernel32.NewProc(name)
)

func bad() {
	os.StartProcess("x", nil, nil)
	sys.StartProcess("x", nil, nil)
	sys.ForkExec("x", nil, nil)
	sys.Socket(2, 1, 0)
	sys.Syscall(41, 0, 0, 0)
	sys.Syscall(sys.SYS_IOCTL, 0, 0, 0)
	sys.RegOpenKeyEx(sys.HKEY_CURRENT_USER, nil, 0, 0xF003F, nil)
	sys.RegOpenKeyEx(sys.HKEY_CURRENT_USER, nil, 0, sys.KEY_QUERY_VALUE, nil)
	_ = os.Getenv("HOME")
}
`
	file, err := parser.ParseFile(token.NewFileSet(), "sample.go", sample, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(lowLevel(file), "\n")
	for _, want := range []string{
		"os.StartProcess starts a process",
		"syscall.StartProcess is not on the list",
		"syscall.ForkExec is not on the list",
		"syscall.Socket is not on the list",
		"syscall.Syscall with a number other than syscall.SYS_IOCTL",
		"syscall.RegOpenKeyEx with access other than syscall.KEY_QUERY_VALUE",
		"syscall.NewLazyDLL of a library that is not on the list",
		"NewProc of a procedure that is not on the list: RegSetValueExW",
		"NewProc of a procedure that is not on the list: CreateProcessW",
		"NewProc of a procedure that is not on the list: \n",
	} {
		if !strings.Contains(got+"\n", want) {
			t.Errorf("the scan did not report %q; it reported:\n%s", want, got)
		}
	}
	if count := len(lowLevel(file)); count != 10 {
		t.Errorf("the scan reported %d problems, want 10 (the allowed calls must pass):\n%s", count, got)
	}
}

// TestTestSupportStaysOutOfTheProgram keeps package testing and the test
// helpers out of everything that is linked into sereno.
func TestTestSupportStaysOutOfTheProgram(t *testing.T) {
	for _, s := range goSources(t) {
		if strings.HasPrefix(s.path, testenvDir) {
			continue
		}
		for _, path := range imports(s) {
			if path == "testing" || strings.HasSuffix(path, "/internal/testenv") {
				t.Errorf("%s imports %s outside a test", s.path, path)
			}
		}
	}
}

// hexColour matches the two ways a colour value is written in this project:
// #rrggbb and the rgb:rr/gg/bb form of the OSC sequences.
var hexColour = regexp.MustCompile(`#[0-9a-fA-F]{6}\b|rgb:[0-9a-fA-F]{2}/[0-9a-fA-F]{2}/[0-9a-fA-F]{2}`)

// TestOnlyThePaletteFileHoldsHexValues is the first sentence of R6: serena.json
// is the only source file that contains hex values. Sources are the Go files
// that are not tests and the templates later stages add; tests, fixtures,
// golden files and documentation may quote colours.
func TestOnlyThePaletteFileHoldsHexValues(t *testing.T) {
	root := testenv.RepoRoot(t)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "testdata" || name == "docs" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		isSource := (strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")) || strings.HasSuffix(rel, ".tmpl")
		if !isSource || rel == paletteFile {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if found := hexColour.Find(data); found != nil {
			t.Errorf("%s contains the colour value %s; colours live in %s only (R6)", rel, found, paletteFile)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(paletteFile)))
	if err != nil || !hexColour.Match(data) {
		t.Errorf("%s is missing or holds no colours (%v)", paletteFile, err)
	}
}

// maxPathLength bounds every path of the repository, counted from its root.
// Git for Windows cannot create a path beyond 260 characters unless
// core.longpaths is set, and then leaves a half-empty work tree behind. With
// this bound a plain clone works in any directory whose own path is up to
// about 150 characters long. Fixtures that need longer names inside a home
// use the aliases of internal/testenv.
const maxPathLength = 100

// TestPathsStayShort keeps the repository clonable on Windows without any
// Git setting.
func TestPathsStayShort(t *testing.T) {
	root := testenv.RepoRoot(t)
	longest := ""
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "dist", ".claude", ".vscode": // not part of a clone
				return filepath.SkipDir
			}
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if len([]rune(rel)) > len([]rune(longest)) {
			longest = rel
		}
		if length := len([]rune(rel)); length > maxPathLength {
			t.Errorf("%s is %d characters long; the limit is %d", rel, length, maxPathLength)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("the longest path has %d characters: %s", len([]rune(longest)), longest)
}

// TestModuleHasNoDependencies is R1 on go.mod: `go 1.24` and no require line.
func TestModuleHasNoDependencies(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testenv.RepoRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "module github.com/joaooliveira10/serena-terminal\n") {
		t.Error("go.mod does not declare the module of R1")
	}
	if !regexp.MustCompile(`(?m)^go 1\.24$`).MatchString(text) {
		t.Error("go.mod does not declare go 1.24")
	}
	for _, directive := range []string{"require", "replace", "toolchain"} {
		if regexp.MustCompile(`(?m)^` + directive + `\b`).MatchString(text) {
			t.Errorf("go.mod has a %s directive", directive)
		}
	}
}

// keyUse is a place in the code that names a message key.
type keyUse struct {
	key string
	pos token.Pos
}

// keyConstant matches the names of the constants that hold message keys:
// NoteKept, ReasonForeign, AdviceAddByHand, and the Warn... constants of the
// status warnings, whose ids are also the keys of their texts.
var keyConstant = regexp.MustCompile(`^(Note|Reason|Advice|Warn)[A-Z0-9]`)

// messageKeys finds the places where a file names a message key. The scan
// goes by the shape of the code, not by the look of a string, so that a file
// name such as "pwsh.exe" is never taken for a key:
//
//   - the first argument of a call to NewNote, when it is a string literal;
//   - the first argument of a call to a method named T, which is how a
//     catalogue is asked for a text, when it is a string literal;
//   - the value of a string constant whose name matches keyConstant.
func messageKeys(file *ast.File) []keyUse {
	var uses []keyUse
	literal := func(expr ast.Expr) (keyUse, bool) {
		lit, ok := expr.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return keyUse{}, false
		}
		value, err := strconv.Unquote(lit.Value)
		return keyUse{key: value, pos: lit.Pos()}, err == nil
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			if len(n.Args) == 0 {
				return true
			}
			name, method := "", false
			switch callee := n.Fun.(type) {
			case *ast.Ident:
				name = callee.Name
			case *ast.SelectorExpr:
				name, method = callee.Sel.Name, true
			}
			if name == "NewNote" || (method && name == "T") {
				if use, ok := literal(n.Args[0]); ok {
					uses = append(uses, use)
				}
			}
		case *ast.GenDecl:
			if n.Tok != token.CONST {
				return true
			}
			for _, spec := range n.Specs {
				values := spec.(*ast.ValueSpec)
				for i, name := range values.Names {
					if i >= len(values.Values) || !keyConstant.MatchString(name.Name) {
						continue
					}
					if use, ok := literal(values.Values[i]); ok {
						uses = append(uses, use)
					}
				}
			}
		}
		return true
	})
	return uses
}

// TestMessageKeysExist checks every message key the code names against the
// catalogues. A key that is misspelled in a Note would otherwise show up as
// "!key!" in front of the user.
func TestMessageKeysExist(t *testing.T) {
	found := 0
	for _, s := range goSources(t) {
		if strings.HasPrefix(s.path, i18nDir) {
			continue
		}
		for _, use := range messageKeys(s.file) {
			found++
			if !i18n.Has(use.key) {
				t.Errorf("%s: message key %q is not in the catalogues", s.fset.Position(use.pos), use.key)
			}
		}
	}
	if found < 20 {
		t.Errorf("only %d message keys found in the code; the scan is probably broken", found)
	}
}

// TestMessageKeyScan pins what the scan takes for a key and what it leaves
// alone. The strings that look like keys and are not (a program, a file)
// matter: stage 1 took "pwsh.exe" for a key as soon as a key "pwsh.absent"
// existed.
func TestMessageKeyScan(t *testing.T) {
	const sample = `package sample

const (
	NoteFine                = "plan.note.kept"
	ReasonMissing           = "refusal.no-such-thing"
	AdviceMissing, fileName = "refusal.advice.nope", "plan.json"
	WarnPwshAbsent          = "pwsh.absent"
	Noteworthy              = "plan.note.lower-case-after-the-prefix"
	program                 = "pwsh.exe"
)

const WarnTyped ID = "wt.not-detected"

var ReasonVariable = "refusal.in-a-variable"

func use(catalog *Catalog, key string) {
	_ = NewNote("plan.note.typo", 1)
	_ = engine.NewNote(ReasonMissing)
	_ = engine.NewNote("refusal.other-typo", "x")
	_ = catalog.T("wt.notice.nope")
	_ = catalog.T(key)
	_ = T("refusal.a-function-named-t")
	settings, state := "settings.json", "state.json"
	_ = lookPath("powershell.exe") + settings + state
	t.Run("plan.note.kept", nil)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", sample, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, use := range messageKeys(file) {
		got = append(got, use.key)
	}
	want := []string{
		"plan.note.kept", "refusal.no-such-thing", "refusal.advice.nope", "pwsh.absent", "wt.not-detected",
		"plan.note.typo", "refusal.other-typo", "wt.notice.nope",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the scan found\n  %v\nwant\n  %v", got, want)
	}
}

// personal matches what must never be committed: a path inside somebody's
// home directory, or an e-mail address.
var (
	homePath = regexp.MustCompile(`(?i)[a-z]:\\{1,2}Users\\{1,2}([^\\/\s"'<>%$]+)|/(?:home|Users)/([^/\s"'<>%$]+)/`)
	email    = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
)

// TestNoPersonalData scans every file of the repository for home directory
// paths and e-mail addresses. Fixtures use the neutral user names listed
// here; no address is allowed at all.
func TestNoPersonalData(t *testing.T) {
	neutralUsers := map[string]bool{"u": true, "x": true, "user": true, "name": true}
	root := testenv.RepoRoot(t)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if rel == "spike.ps1" || rel == "spike-result.txt" || rel == "sereno" || rel == "sereno.exe" {
			return nil // listed in .gitignore
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		if strings.HasPrefix(text, "\xff\xfe") {
			text = strings.ReplaceAll(text, "\x00", "") // the UTF-16 fixture
		}
		for _, match := range homePath.FindAllStringSubmatch(text, -1) {
			user := match[1] + match[2]
			if !neutralUsers[strings.ToLower(user)] {
				t.Errorf("%s contains a path into a home directory: %s", rel, match[0])
			}
		}
		for _, address := range email.FindAllString(text, -1) {
			t.Errorf("%s contains the e-mail address %s", rel, address)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestEveryTestPackageIsHermetic is rule T1 made structural: each directory
// with tests has a TestMain that hands over to testenv.Main, so no test of the
// module starts with the developer's real home in its environment.
func TestEveryTestPackageIsHermetic(t *testing.T) {
	root := testenv.RepoRoot(t)
	withTests := map[string]bool{}
	hermetic := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "testdata" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir := filepath.Dir(path)
		withTests[dir] = true
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Name.Name != "TestMain" || function.Recv != nil || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch callee := call.Fun.(type) {
				case *ast.SelectorExpr:
					if pkg, ok := callee.X.(*ast.Ident); ok && pkg.Name == "testenv" && callee.Sel.Name == "Main" {
						hermetic[dir] = true
					}
				case *ast.Ident:
					if callee.Name == "Main" && file.Name.Name == "testenv" {
						hermetic[dir] = true
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(withTests) < 8 {
		t.Fatalf("only %d test packages found", len(withTests))
	}
	for dir := range withTests {
		if !hermetic[dir] {
			rel, _ := filepath.Rel(root, dir)
			t.Errorf("%s has tests but no TestMain that calls testenv.Main (T1)", filepath.ToSlash(rel))
		}
	}
}
