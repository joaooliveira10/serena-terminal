// Command golden renders everything sereno generates into a directory, in the
// layout of testdata/golden:
//
//	fragment/<case>.json   the Windows Terminal fragment for each case of T4
//	init/init.ps1, init.bash, init.zsh
//	palette/<variant>.bin  PALETTE (R29) of the six variants
//	palette/reset.bin      RESET
//	export/<target>/...    the 24 theme files of `sereno export`
//
// It has two uses. The golden files of the repository are regenerated with it
// after a deliberate change to a template or to the palette:
//
//	go run ./internal/tools/golden --out testdata/golden
//
// and the shell tests are driven by a fresh rendering in a temporary folder:
//
//	go run ./internal/tools/golden --out "$TMP/rendered"
//
// The test next to this file compares testdata/golden with what the renderer
// produces, so a normal test run writes nothing. Every write here goes through
// the guarded file layer: with SERENO_SANDBOX set, --out must lie inside it.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/mode"
	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/render"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// fragmentCase is one rendering of the fragment that T4 asks for.
type fragmentCase struct {
	name    string
	options render.FragmentOptions
}

// customRoot stands for a ROOT that SERENO_HOME moved: the profile then names
// init.ps1 by its absolute path (R18). The space is deliberate.
const customRoot = `D:\Tools\sereno home`

// fragmentCases lists the fragments of T4: auto with both flavours, every
// fixed mode, no profile, PowerShell 7, and a ROOT that is not the default.
func fragmentCases() []fragmentCase {
	base := func(m mode.Mode, f mode.Flavor) render.FragmentOptions {
		return render.FragmentOptions{Profile: true, Shell: shells.PowerShell, Mode: m, Flavor: f, Root: platform.Root{Default: true}}
	}
	noProfile := base(mode.Auto, mode.Soft)
	noProfile.Profile = false
	pwsh := base(mode.Auto, mode.Soft)
	pwsh.Shell = shells.Pwsh
	custom := base(mode.Auto, mode.Soft)
	custom.Root = platform.Root{Path: customRoot}
	return []fragmentCase{
		{"auto-soft", base(mode.Auto, mode.Soft)},
		{"auto-vivid", base(mode.Auto, mode.Vivid)},
		{"dark-soft", base(mode.Dark, mode.Soft)},
		{"dark-vivid", base(mode.Dark, mode.Vivid)},
		{"light-soft", base(mode.Light, mode.Soft)},
		{"light-vivid", base(mode.Light, mode.Vivid)},
		{"hc", base(mode.HC, mode.Soft)},
		{"hc-light", base(mode.HCLight, mode.Soft)},
		{"no-profile", noProfile},
		{"pwsh", pwsh},
		{"custom-root", custom},
	}
}

// tree renders every golden file. Keys are paths below the output directory,
// with forward slashes.
func tree() (map[string][]byte, error) {
	p, err := palette.Load()
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}

	for _, c := range fragmentCases() {
		data, err := render.Fragment(p, c.options)
		if err != nil {
			return nil, fmt.Errorf("fragment %s: %w", c.name, err)
		}
		files["fragment/"+c.name+".json"] = data
	}

	inits, err := render.InitFiles(p)
	if err != nil {
		return nil, err
	}
	for _, f := range inits {
		// "shell/init.ps1" below ROOT is "init/init.ps1" here.
		files["init/"+filepath.Base(filepath.FromSlash(f.Name))] = f.Data
	}

	for _, v := range p.Variants {
		files["palette/"+v.ID+".bin"] = []byte(v.Sequence())
	}
	files["palette/reset.bin"] = []byte(palette.ResetSequence())

	for _, target := range render.Targets() {
		themes, err := render.Export(p, target)
		if err != nil {
			return nil, err
		}
		for _, f := range themes {
			files["export/"+f.Name] = f.Data
		}
	}
	return files, nil
}

// run is main without the process: it returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("golden", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", "", "directory to render into, for example testdata/golden")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *out == "" || flags.NArg() > 0 {
		fmt.Fprintln(stderr, "golden: --out DIR is required")
		flags.Usage()
		return 2
	}
	dir, err := filepath.Abs(*out)
	if err != nil {
		fmt.Fprintln(stderr, "golden:", err)
		return 1
	}
	files, err := tree()
	if err != nil {
		fmt.Fprintln(stderr, "golden:", err)
		return 1
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	written := 0
	for _, name := range names {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, files[name]) {
			continue
		}
		if err := mkdirAll(filepath.Dir(path)); err != nil {
			fmt.Fprintln(stderr, "golden:", err)
			return 1
		}
		if err := engine.WriteFile(path, files[name], engine.FilePerm); err != nil {
			fmt.Fprintln(stderr, "golden:", err)
			return 1
		}
		written++
	}
	fmt.Fprintf(stdout, "golden: %d files in %s, %d written\n", len(names), dir, written)
	return 0
}

// mkdirAll creates a directory and its missing parents through the guarded
// file layer, one level at a time.
func mkdirAll(dir string) error {
	info, err := os.Stat(dir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if parent := filepath.Dir(dir); parent != dir {
		if err := mkdirAll(parent); err != nil {
			return err
		}
	}
	return engine.Mkdir(dir)
}
