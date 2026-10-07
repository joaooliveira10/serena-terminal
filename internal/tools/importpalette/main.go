// Command importpalette generates internal/palette/serena.json, the only file
// of the repository that contains hex values, from the built themes of Serena
// Theme (requirement R6).
//
// Usage, from the root of the repository:
//
//	go run ./internal/tools/importpalette --themes DIR --tag v1.1.0
//
// DIR must hold the six files serena-*-color-theme.json exactly as they are at
// that tag: a checkout of the tag, or an export made with `git archive`. The
// commit recorded next to the tag is read from the Git metadata above DIR
// (without starting git); an export has none, so pass --commit for it.
// Running the tool again on the same input reproduces the file byte for byte.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/palette"
)

const (
	defaultRepo = "github.com/joaooliveira10/serena-theme"
	defaultOut  = "internal/palette/serena.json"
	themeGlob   = "serena-*-color-theme.json"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main without the process: it returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("importpalette", flag.ContinueOnError)
	flags.SetOutput(stderr)
	themes := flags.String("themes", "", "directory with the six serena-*-color-theme.json files of the tag")
	tag := flags.String("tag", "", "Serena Theme tag the files belong to, for example v1.1.0")
	commit := flags.String("commit", "", "commit of the tag (default: read from the Git metadata above --themes)")
	repo := flags.String("repo", defaultRepo, "repository recorded as the source")
	out := flags.String("out", defaultOut, "file to write")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *themes == "" || *tag == "" || flags.NArg() > 0 {
		fmt.Fprintln(stderr, "importpalette: --themes DIR and --tag TAG are required")
		flags.Usage()
		return 2
	}

	if *commit == "" {
		found, err := tagCommit(*themes, *tag)
		if err != nil {
			fmt.Fprintf(stderr, "importpalette: %v\n", err)
			return 1
		}
		*commit = found
	}
	if !isObjectID(*commit) {
		fmt.Fprintf(stderr, "importpalette: %q is not a full commit hash\n", *commit)
		return 2
	}

	data, err := build(*themes, palette.Source{Repo: *repo, Tag: *tag, Commit: *commit})
	if err != nil {
		fmt.Fprintf(stderr, "importpalette: %v\n", err)
		return 1
	}
	if current, err := os.ReadFile(*out); err == nil && bytes.Equal(current, data) {
		fmt.Fprintf(stdout, "%s is up to date (%s at %s)\n", *out, *tag, (*commit)[:7])
		return 0
	}
	if err := engine.WriteFile(*out, data, engine.FilePerm); err != nil {
		fmt.Fprintf(stderr, "importpalette: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s (%s at %s)\n", *out, *tag, (*commit)[:7])
	return 0
}

// build reads the six theme files of dir and returns the bytes of serena.json.
func build(dir string, source palette.Source) ([]byte, error) {
	files, err := filepath.Glob(filepath.Join(dir, themeGlob))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	identities := palette.Identities()
	if len(files) != len(identities) {
		return nil, fmt.Errorf("%s holds %d files matching %s, want %d", dir, len(files), themeGlob, len(identities))
	}

	byName := map[string]palette.Variant{}
	for _, file := range files {
		variant, err := readTheme(file)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(file), err)
		}
		if _, dup := byName[variant.Name]; dup {
			return nil, fmt.Errorf("%s: a second theme is named %q", filepath.Base(file), variant.Name)
		}
		byName[variant.Name] = variant
	}

	p := palette.Palette{Source: source}
	for _, id := range identities {
		variant, ok := byName[id.Name]
		if !ok {
			return nil, fmt.Errorf("no theme is named %q", id.Name)
		}
		if variant.Kind != id.Kind {
			return nil, fmt.Errorf("theme %q is of kind %q, want %q", id.Name, variant.Kind, id.Kind)
		}
		variant.ID = id.ID
		p.Variants = append(p.Variants, variant)
	}
	return p.Encode()
}

// theme is the part of a VS Code colour theme the importer reads.
type theme struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Colors map[string]string `json:"colors"`
}

// kinds maps the "type" of a VS Code theme to the kind of R7.
var kinds = map[string]palette.Kind{
	"dark":    palette.KindDark,
	"light":   palette.KindLight,
	"hc":      palette.KindHC,
	"hcLight": palette.KindHCLight,
}

// ansiKeys are the theme keys of slots 0-7; the bright ones follow the same
// order with "Bright" inserted.
var ansiKeys = []string{"Black", "Red", "Green", "Yellow", "Blue", "Magenta", "Cyan", "White"}

// readTheme extracts one variant (without its id) from a built theme file.
func readTheme(path string) (palette.Variant, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return palette.Variant{}, err
	}
	var t theme
	if err := json.Unmarshal(dropCommentLines(raw), &t); err != nil {
		return palette.Variant{}, err
	}
	kind, ok := kinds[t.Type]
	if !ok {
		return palette.Variant{}, fmt.Errorf("unknown theme type %q", t.Type)
	}
	v := palette.Variant{Name: t.Name, Kind: kind}

	var problems []string
	color := func(key string, cut int) palette.Color {
		value, ok := t.Colors[key]
		if !ok {
			problems = append(problems, "missing "+key)
			return ""
		}
		if cut > 0 && len(value) > cut {
			value = value[:cut]
		}
		c, err := palette.ParseColor(value)
		if err != nil {
			problems = append(problems, key+": "+err.Error())
		}
		return c
	}

	v.Background = color("terminal.background", 0)
	v.Foreground = color("terminal.foreground", 0)
	v.Cursor = color("terminalCursor.foreground", 0)
	for n, name := range ansiKeys {
		v.ANSI[n] = color("terminal.ansi"+name, 0)
		v.ANSI[n+8] = color("terminal.ansiBright"+name, 0)
	}
	if _, ok := t.Colors["terminal.selectionBackground"]; ok {
		v.SelectionBackground = color("terminal.selectionBackground", 0)
	} else {
		// The editor colour may carry an alpha channel: keep "#rrggbb".
		v.SelectionBackground = color("editor.selectionBackground", 7)
	}
	if _, ok := t.Colors["terminal.selectionForeground"]; ok {
		v.SelectionForeground = color("terminal.selectionForeground", 0)
	} else {
		v.SelectionForeground = v.Foreground
	}
	if len(problems) > 0 {
		return palette.Variant{}, errors.New(strings.Join(problems, "; "))
	}
	return v, nil
}

// dropCommentLines removes a UTF-8 byte order mark and every line that begins
// with "//": the built themes start with such a line, which is not JSON.
func dropCommentLines(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var out bytes.Buffer
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimLeft(line, " \t"), []byte("//")) {
			continue
		}
		out.Write(line)
	}
	return out.Bytes()
}

// isObjectID reports whether s is a full SHA-1 or SHA-256 object name.
func isObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	return strings.Trim(s, "0123456789abcdef") == ""
}
