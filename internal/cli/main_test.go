package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/mode"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
	"github.com/joaooliveira10/serena-terminal/internal/target/winterminal"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// TestMain gives every test of this package the hermetic home of rule T1.
func TestMain(m *testing.M) { testenv.Main(m) }

// testVersion is the version the commands under test believe they are.
const testVersion = "0.1.0-test"

// world is one machine as a command sees it: a temporary home, the
// environment that points into it, and everything else Env carries. Nothing
// here is the developer's: the host is built by hand, so the Windows rules
// and the rules of macOS and Linux are both tested on every system.
type world struct {
	t        *testing.T
	home     *testenv.Home
	os       string
	vars     map[string]string
	signals  mode.Reader
	prober   shells.Prober
	terminal bool            // standard output is a terminal
	ctx      context.Context // nil: never interrupted
	language uint16          // the Windows user-interface language id
}

// failProber fails the test when a command would have started a PowerShell.
type failProber struct{ t *testing.T }

func (f failProber) Probe(program string) (shells.ProbeResult, error) {
	f.t.Errorf("the prober was asked to run %s", program)
	return shells.ProbeResult{}, errors.New("no process in a test")
}

// fakeProber answers in a PowerShell's place.
type fakeProber struct {
	result shells.ProbeResult
	err    error
	asked  int
}

func (f *fakeProber) Probe(string) (shells.ProbeResult, error) {
	f.asked++
	return f.result, f.err
}

// newWorld builds a machine of the given system from a fixture home ("" for
// an empty one). A Windows machine has powershell.exe on PATH, as every
// Windows has; the file is a stand-in that is never started.
func newWorld(t *testing.T, goos, fixture string) *world {
	t.Helper()
	var home *testenv.Home
	if fixture == "" {
		home = testenv.New(t)
	} else {
		home = testenv.NewFrom(t, fixture)
	}
	w := &world{t: t, home: home, os: goos, vars: home.Env(), signals: mode.Unknown{}, prober: failProber{t}}
	if goos == platform.Windows {
		w.signals = mode.Fixed{}
		w.program("powershell.exe")
	}
	return w
}

// program puts a stand-in for a program on the PATH of the machine.
func (w *world) program(name string) {
	w.t.Helper()
	bin := filepath.Join(w.home.Dir, "bin")
	w.write(filepath.Join(bin, name), "")
	if err := os.Chmod(filepath.Join(bin, name), 0o755); err != nil {
		w.t.Fatal(err)
	}
	w.vars["PATH"] = bin
}

// result is what one command line produced.
type result struct {
	code     int
	out, err string
}

// run carries out one command line on the machine.
func (w *world) run(args ...string) result {
	w.t.Helper()
	var out, errs bytes.Buffer
	env := Env{
		Version: testVersion,
		Stdout:  &out,
		Stderr:  &errs,
		Host: platform.Host{
			OS:           w.os,
			Getenv:       testenv.Getenv(w.vars),
			UILanguageID: func() uint16 { return w.language },
		},
		Signals:    w.signals,
		Prober:     w.prober,
		Context:    w.ctx,
		Executable: filepath.Join(w.home.Dir, "bin", Program),
	}
	if w.terminal {
		env.Terminal = func() (func(), bool) { return func() {}, true }
	}
	code := Run(env, args)
	return result{code: code, out: out.String(), err: errs.String()}
}

// must runs a command line and fails the test unless it ends with the exit
// code that is wanted.
func (w *world) must(code int, args ...string) result {
	w.t.Helper()
	got := w.run(args...)
	if got.code != code {
		w.t.Fatalf("sereno %s: exit code %d, want %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), got.code, code, got.out, got.err)
	}
	return got
}

// root is sereno's folder on the machine.
func (w *world) root() string {
	if w.vars[platform.EnvHome] != "" {
		return w.vars[platform.EnvHome]
	}
	if w.os == platform.Windows {
		return filepath.Join(w.home.LocalAppData, "sereno")
	}
	if w.vars[platform.EnvDataHome] != "" {
		return filepath.Join(w.vars[platform.EnvDataHome], "sereno")
	}
	return filepath.Join(w.home.Dir, ".local", "share", "sereno")
}

// fragment is the file sereno writes for Windows Terminal.
func (w *world) fragment() string { return winterminal.FragmentPath(w.home.LocalAppData) }

// modeFile is ROOT/mode.
func (w *world) modeFile() string { return filepath.Join(w.root(), engine.ModeName) }

// manifest is ROOT/manifest.json.
func (w *world) manifest() string { return filepath.Join(w.root(), engine.ManifestName) }

// tree is a snapshot of the whole home.
func (w *world) tree() map[string]string { return testenv.Snapshot(w.t, w.home.Dir) }

func (w *world) read(path string) string {
	w.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		w.t.Fatal(err)
	}
	return string(data)
}

func (w *world) write(path, content string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

// golden reads a file of testdata/golden.
func golden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testenv.RepoRoot(t), "testdata", "golden", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// sameTree fails the test when two snapshots differ, naming the entries.
func sameTree(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	for path, state := range after {
		if old, ok := before[path]; !ok {
			t.Errorf("%s: %s appeared (%s)", what, path, state)
		} else if old != state {
			t.Errorf("%s: %s changed", what, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			t.Errorf("%s: %s disappeared", what, path)
		}
	}
}

// bothLanguages runs a test once in English and once in Brazilian
// Portuguese; lang is the value for --lang.
func bothLanguages(t *testing.T, test func(t *testing.T, lang string)) {
	t.Helper()
	for _, lang := range []string{"en", "pt-BR"} {
		t.Run(lang, func(t *testing.T) { test(t, lang) })
	}
}

// noMissingKey fails the test when a text shows the mark of a message key
// that is in no catalogue.
func noMissingKey(t *testing.T, texts ...string) {
	t.Helper()
	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "%!") || missingKey.MatchString(line) {
				t.Errorf("the output shows a missing message or a formatting mistake: %q", line)
			}
		}
	}
}
