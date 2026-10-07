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

// TestMessageKeysExist finds every string literal outside internal/i18n that
// has the form of a message key (a namespace of the catalogues, a dot, more
// words) and checks that the catalogues know it. A key that is misspelled in
// a Note would otherwise show up as "!key!" in front of the user.
func TestMessageKeysExist(t *testing.T) {
	namespaces := map[string]bool{}
	for _, key := range i18n.Keys() {
		namespaces[strings.SplitN(key, ".", 2)[0]] = true
	}
	keyForm := regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9-]+)+$`)
	found := 0
	for _, s := range goSources(t) {
		if strings.HasPrefix(s.path, i18nDir) {
			continue
		}
		ast.Inspect(s.file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil || !keyForm.MatchString(value) || !namespaces[strings.SplitN(value, ".", 2)[0]] {
				return true
			}
			found++
			if !i18n.Has(value) {
				t.Errorf("%s: message key %q is not in the catalogues", s.fset.Position(literal.Pos()), value)
			}
			return true
		})
	}
	if found < 20 {
		t.Errorf("only %d message keys found in the code; the scan is probably broken", found)
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
