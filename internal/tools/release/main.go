// Command release packs what a release of sereno consists of (requirement
// R41) from six programs that were built before it runs:
//
//	sereno_<version>_<os>_<arch>.zip      windows, amd64 and arm64
//	sereno_<version>_<os>_<arch>.tar.gz   darwin and linux, amd64 and arm64
//	checksums.txt                         SHA-256 of the six archives
//	install.ps1, install.sh               the install scripts of that version
//
// Each archive holds three files: the program (sereno, or sereno.exe on
// Windows), LICENSE and README.md. The install scripts are rendered from
// scripts/install.ps1.tmpl and scripts/install.sh.tmpl with the version, the
// repository the release belongs to and the lines of checksums.txt, so a
// script installs the archives of its own release and nothing else.
//
// Usage, from the root of the repository (scripts/release.sh builds the six
// programs and then runs this):
//
//	go run ./internal/tools/release --version v0.1.0 --binaries DIR --out DIR
//	go run ./internal/tools/release --version v0.1.0 --check
//
// --binaries names a folder with one sub-folder per target, <os>_<arch>,
// each holding the program built for it. --check only says whether the
// tag may name a release, and packs nothing. A tag may when it is a version
// the go command accepts for this module and, unless it has a pre-release
// part, when README.md no longer says that the version is unreleased and
// CHANGELOG.md has a section for it. Both runs check that first.
//
// The same input gives the same bytes: entries carry the time given with
// --epoch (seconds since 1970; default: SOURCE_DATE_EPOCH, else the first day
// of 1980) and no owner. This tool starts no program and opens no connection.
// Every write goes through the guarded file layer: with SERENO_SANDBOX set,
// --out must lie inside it.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// Target is one of the six systems a release is built for (R1).
type Target struct {
	OS, Arch string
}

// Targets lists them in the order the assets are written.
func Targets() []Target {
	return []Target{
		{"darwin", "amd64"}, {"darwin", "arm64"},
		{"linux", "amd64"}, {"linux", "arm64"},
		{"windows", "amd64"}, {"windows", "arm64"},
	}
}

// Dir is the name of the folder that holds the program of a target.
func (t Target) Dir() string { return t.OS + "_" + t.Arch }

// Program is the file name of the program on a target (R1).
func (t Target) Program() string {
	if t.OS == "windows" {
		return "sereno.exe"
	}
	return "sereno"
}

// Archive is the name of the archive of a target in a release (R41).
func (t Target) Archive(version string) string {
	name := "sereno_" + version + "_" + t.OS + "_" + t.Arch
	if t.OS == "windows" {
		return name + ".zip"
	}
	return name + ".tar.gz"
}

// Names of the other assets, and of what goes into every archive.
const (
	ChecksumsName = "checksums.txt"
	InstallPS1    = "install.ps1"
	InstallSh     = "install.sh"
	licenseName   = "LICENSE"
	readmeName    = "README.md"
	changelogName = "CHANGELOG.md"
)

// Where the templates of the install scripts are, below the repository root.
const (
	templatePS1 = "scripts/install.ps1.tmpl"
	templateSh  = "scripts/install.sh.tmpl"
)

// Placeholders of the templates. The first two may stand anywhere and any
// number of times; the third is one whole line, replaced by the lines of
// checksums.txt.
const (
	placeVersion    = "__VERSION__"
	placeRepository = "__REPOSITORY__"
	placeChecksums  = "__CHECKSUMS__"
)

// DefaultRepository is where releases of sereno are published (R1).
const DefaultRepository = "joaooliveira10/serena-terminal"

// defaultEpoch is the time of the entries when none is given: 1980-01-01,
// the earliest a zip archive can record.
const defaultEpoch = 315532800

// preRelease is one dot-separated part of what follows the hyphen.
var preRelease = regexp.MustCompile(`^(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)$`)

// CheckVersion says whether a tag may name a release. The rule follows from
// two requirements: a release is made from a tag v* (R41), and that tag is
// what `go install .../cmd/sereno@<tag>` is given (R40). So it must be a
// version the go command accepts for this module: v, then MAJOR.MINOR.PATCH
// in decimal without leading zeros, then optionally a hyphen and pre-release
// parts separated by dots. MAJOR is 0 or 1, because the module path has no
// /v2 suffix, and there is no build part (+...), which the go command does
// not take in a tag.
func CheckVersion(version string) error {
	fail := func(why string) error {
		return fmt.Errorf("%q cannot name a release: %s (wanted: vMAJOR.MINOR.PATCH, for example v0.1.0 or v0.1.0-rc.1)", version, why)
	}
	rest, ok := strings.CutPrefix(version, "v")
	if !ok {
		return fail("it does not start with v")
	}
	if strings.Contains(rest, "+") {
		return fail("it has a build part after +")
	}
	core, pre, hasPre := strings.Cut(rest, "-")
	numbers := strings.Split(core, ".")
	if len(numbers) != 3 {
		return fail("it does not have three numbers")
	}
	for _, number := range numbers {
		if number == "" || strings.Trim(number, "0123456789") != "" {
			return fail("its numbers are not decimal")
		}
		if len(number) > 1 && number[0] == '0' {
			return fail("a number has a leading zero")
		}
		if len(number) > 9 {
			return fail("a number is too long")
		}
	}
	if numbers[0] != "0" && numbers[0] != "1" {
		return fail("the module path has no /v" + numbers[0] + " suffix, so the go command could not install it")
	}
	if hasPre {
		for _, part := range strings.Split(pre, ".") {
			if !preRelease.MatchString(part) {
				return fail("its pre-release part is not letters, digits and hyphens separated by dots")
			}
		}
	}
	return nil
}

// UnreleasedMark is the comment README.md carries, next to its status line,
// for as long as it says that the version it describes is not released.
const UnreleasedMark = "<!-- sereno:status unreleased -->"

// CheckDocuments refuses a tag that the documents contradict. README.md goes
// into every archive, and it must not tell the person who unpacks one that
// this version does not exist yet; CHANGELOG.md must have a section for the
// version. A tag with a pre-release part (v0.1.0-rc.1) is a trial, for which
// both documents may still speak of an unreleased version.
func CheckDocuments(version string, readme, changelog []byte) error {
	if strings.Contains(version, "-") {
		return nil
	}
	if bytes.Contains(readme, []byte(UnreleasedMark)) {
		return fmt.Errorf("README.md still says that this version is not released (it carries %s): rewrite its status section, and the one of README.pt-BR.md, before tagging %s", UnreleasedMark, version)
	}
	number := strings.TrimPrefix(version, "v")
	for _, line := range strings.Split(string(changelog), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "## "+number || strings.HasPrefix(line, "## "+number+" ") {
			return nil
		}
	}
	return fmt.Errorf("CHANGELOG.md has no section \"## %s\": move what is under \"## Unreleased\" there before tagging %s", number, version)
}

// repositoryName is OWNER/NAME as GitHub writes it.
var repositoryName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)

// Input is everything a release is packed from.
type Input struct {
	Version    string
	Repository string
	When       time.Time
	// Programs holds the program of each target, keyed by Target.Dir.
	Programs map[string][]byte
	License  []byte
	Readme   []byte
	// The two templates of the install scripts.
	TemplatePS1 []byte
	TemplateSh  []byte
}

// Build returns the assets of a release, keyed by file name.
func Build(in Input) (map[string][]byte, error) {
	if err := CheckVersion(in.Version); err != nil {
		return nil, err
	}
	if !repositoryName.MatchString(in.Repository) {
		return nil, fmt.Errorf("%q is not a repository in the form OWNER/NAME", in.Repository)
	}
	assets := map[string][]byte{}
	var sums bytes.Buffer
	for _, target := range Targets() {
		program, ok := in.Programs[target.Dir()]
		if !ok || len(program) == 0 {
			return nil, fmt.Errorf("no program for %s/%s", target.OS, target.Arch)
		}
		files := []member{
			{target.Program(), program, 0o755},
			{licenseName, in.License, 0o644},
			{readmeName, in.Readme, 0o644},
		}
		var data []byte
		var err error
		if target.OS == "windows" {
			data, err = packZip(files, in.When)
		} else {
			data, err = packTarGz(files, in.When)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", target.Archive(in.Version), err)
		}
		assets[target.Archive(in.Version)] = data
		// The format of sha256sum, so that `sha256sum -c checksums.txt`
		// checks a download.
		fmt.Fprintf(&sums, "%s  %s\n", engine.SHA256(data), target.Archive(in.Version))
	}
	assets[ChecksumsName] = sums.Bytes()

	for name, template := range map[string][]byte{InstallPS1: in.TemplatePS1, InstallSh: in.TemplateSh} {
		script, err := Render(template, in.Version, in.Repository, sums.Bytes())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		assets[name] = script
	}
	return assets, nil
}

// Render fills a template of an install script: the version and the
// repository wherever their placeholders stand, and the lines of
// checksums.txt in the place of the one line that is the third placeholder.
func Render(template []byte, version, repository string, checksums []byte) ([]byte, error) {
	text := string(template)
	if strings.Contains(text, "\r") {
		return nil, errors.New("the template has carriage returns; it must use line feeds only")
	}
	for _, place := range []string{placeVersion, placeRepository} {
		if !strings.Contains(text, place) {
			return nil, fmt.Errorf("the template has no %s", place)
		}
	}
	if n := strings.Count(text, "\n"+placeChecksums+"\n"); n != 1 || strings.Count(text, placeChecksums) != 1 {
		return nil, fmt.Errorf("the template must hold %s once, as a line of its own", placeChecksums)
	}
	text = strings.ReplaceAll(text, placeVersion, version)
	text = strings.ReplaceAll(text, placeRepository, repository)
	text = strings.Replace(text, "\n"+placeChecksums+"\n", "\n"+string(checksums), 1)
	return []byte(text), nil
}

// member is one file of an archive.
type member struct {
	name string
	data []byte
	mode fs.FileMode
}

// packZip writes the files into a zip archive, at its top level.
func packZip(files []member, when time.Time) ([]byte, error) {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate, Modified: when.UTC()}
		header.SetMode(file.mode)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(file.data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// packTarGz writes the files into a gzip-compressed tar archive, at its top
// level, owned by nobody in particular.
func packTarGz(files []member, when time.Time) ([]byte, error) {
	var buf bytes.Buffer
	compressor, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	writer := tar.NewWriter(compressor)
	for _, file := range files {
		header := &tar.Header{
			Typeflag: tar.TypeReg, Name: file.name, Size: int64(len(file.data)),
			Mode: int64(file.mode.Perm()), ModTime: when.UTC(), Format: tar.FormatUSTAR,
		}
		if err := writer.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := writer.Write(file.data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if err := compressor.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// run is main without the process: it returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("release", flag.ContinueOnError)
	flags.SetOutput(stderr)
	version := flags.String("version", "", "the tag of the release, for example v0.1.0")
	check := flags.Bool("check", false, "only say whether --version may name a release")
	binaries := flags.String("binaries", "", "folder with one sub-folder <os>_<arch> per target, each holding the program")
	out := flags.String("out", "", "folder to write the assets into")
	root := flags.String("root", ".", "root of the repository (LICENSE, README.md, scripts/)")
	repository := flags.String("repository", DefaultRepository, "OWNER/NAME of the repository the release belongs to")
	epoch := flags.String("epoch", os.Getenv("SOURCE_DATE_EPOCH"), "time of the archive entries, in seconds since 1970")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	fail := func(code int, err error) int {
		fmt.Fprintln(stderr, "release:", err)
		return code
	}
	if *version == "" || flags.NArg() > 0 {
		fmt.Fprintln(stderr, "release: --version TAG is required")
		flags.Usage()
		return 2
	}
	if err := CheckVersion(*version); err != nil {
		return fail(1, err)
	}
	readme, err := os.ReadFile(filepath.Join(*root, readmeName))
	if err != nil {
		return fail(1, err)
	}
	changelog, err := os.ReadFile(filepath.Join(*root, changelogName))
	if err != nil {
		return fail(1, err)
	}
	if err := CheckDocuments(*version, readme, changelog); err != nil {
		return fail(1, err)
	}
	if *check {
		fmt.Fprintf(stdout, "release: %s may name a release\n", *version)
		return 0
	}
	if *binaries == "" || *out == "" {
		fmt.Fprintln(stderr, "release: --binaries DIR and --out DIR are required")
		flags.Usage()
		return 2
	}
	seconds := int64(defaultEpoch)
	if *epoch != "" {
		value, err := strconv.ParseInt(*epoch, 10, 64)
		if err != nil || value < defaultEpoch {
			return fail(2, fmt.Errorf("--epoch %q is not a number of seconds from 1980 on", *epoch))
		}
		seconds = value
	}

	in := Input{Version: *version, Repository: *repository, When: time.Unix(seconds, 0), Programs: map[string][]byte{}, Readme: readme}
	for _, target := range Targets() {
		data, err := os.ReadFile(filepath.Join(*binaries, target.Dir(), target.Program()))
		if err != nil {
			return fail(1, err)
		}
		in.Programs[target.Dir()] = data
	}
	for name, into := range map[string]*[]byte{licenseName: &in.License, templatePS1: &in.TemplatePS1, templateSh: &in.TemplateSh} {
		data, err := os.ReadFile(filepath.Join(*root, filepath.FromSlash(name)))
		if err != nil {
			return fail(1, err)
		}
		*into = data
	}
	assets, err := Build(in)
	if err != nil {
		return fail(1, err)
	}

	dir, err := filepath.Abs(*out)
	if err != nil {
		return fail(1, err)
	}
	if err := mkdirAll(dir); err != nil {
		return fail(1, err)
	}
	names := make([]string, 0, len(assets))
	for name := range assets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		mode := engine.FilePerm
		if name == InstallSh {
			mode = 0o755
		}
		if err := engine.WriteFile(filepath.Join(dir, name), assets[name], mode); err != nil {
			return fail(1, err)
		}
		fmt.Fprintf(stdout, "%s  %s\n", engine.SHA256(assets[name]), name)
	}
	fmt.Fprintf(stdout, "release: %d assets of %s in %s\n", len(names), *version, dir)
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
