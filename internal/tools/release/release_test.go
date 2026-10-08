package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// TestVersionRule is the rule a tag must meet before a release is built from
// it (R40, R41): what the go command takes as a version of this module.
func TestVersionRule(t *testing.T) {
	for _, version := range []string{"v0.1.0", "v0.1.1", "v0.10.3", "v1.0.0", "v0.1.0-rc.1", "v0.1.0-beta", "v0.2.0-alpha.2", "v0.1.0-0", "v0.1.0-x-y.7"} {
		if err := CheckVersion(version); err != nil {
			t.Errorf("CheckVersion(%q) = %v, want nil", version, err)
		}
	}
	for _, version := range []string{
		"", "v", "0.1.0", "V0.1.0", "v0.1", "v0.1.0.0", "v0.01.0", "v00.1.0", "v0.1.x", "v0.1.0+build", "v0.1.0-", "v0.1.0-rc..1",
		"v0.1.0-01", "v0.1.0-rc_1", "v2.0.0", "v10.0.0", "v0.1.0 ", " v0.1.0", "v0.1.0\n", "v-1.0.0", "vv0.1.0", "v0.1.0-rc.1+x",
		"latest", "v0.1.0/..", "v0.1.12345678901",
	} {
		if err := CheckVersion(version); err == nil {
			t.Errorf("CheckVersion(%q) = nil, want a refusal", version)
		}
	}
}

// timeOf is a moment given in seconds since 1970.
func timeOf(seconds int64) time.Time { return time.Unix(seconds, 0) }

// standIn is what the tests pack in the place of a program.
func standIn(target Target) []byte {
	return []byte("stand-in for " + target.Program() + " on " + target.OS + "/" + target.Arch + "\n")
}

// repositoryFile reads a file of the repository.
func repositoryFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testenv.RepoRoot(t), filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// input is a release made of stand-ins and of the real templates.
func input(t *testing.T) Input {
	t.Helper()
	in := Input{
		Version: "v0.1.0-test.1", Repository: "example/serena-terminal", When: timeOf(defaultEpoch + 86400),
		Programs:    map[string][]byte{},
		License:     repositoryFile(t, "LICENSE"),
		Readme:      []byte("# stand-in for README.md\n"),
		TemplatePS1: repositoryFile(t, templatePS1),
		TemplateSh:  repositoryFile(t, templateSh),
	}
	for _, target := range Targets() {
		in.Programs[target.Dir()] = standIn(target)
	}
	return in
}

// unpack lists the files of an archive with their contents and modes.
func unpack(t *testing.T, name string, data []byte) (map[string][]byte, map[string]int64) {
	t.Helper()
	files, modes := map[string][]byte{}, map[string]int64{}
	if strings.HasSuffix(name, ".zip") {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, file := range reader.File {
			stream, err := file.Open()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			content, err := io.ReadAll(stream)
			stream.Close()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			files[file.Name] = content
			modes[file.Name] = int64(file.Mode().Perm())
			if !file.Modified.Equal(timeOf(defaultEpoch + 86400)) {
				t.Errorf("%s: %s carries the time %v", name, file.Name, file.Modified)
			}
		}
		return files, modes
	}
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if header.Typeflag != tar.TypeReg || header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" {
			t.Errorf("%s: %s is not a plain file without an owner: %+v", name, header.Name, header)
		}
		if !header.ModTime.Equal(timeOf(defaultEpoch + 86400)) {
			t.Errorf("%s: %s carries the time %v", name, header.Name, header.ModTime)
		}
		files[header.Name] = content
		modes[header.Name] = header.Mode
	}
	return files, modes
}

// TestAssets is R41 on the bytes: six archives under the names of the
// requirement, each with exactly the program, LICENSE and README.md at its
// top level; checksums.txt with the SHA-256 of each archive; and both install
// scripts carrying the version, the repository and those checksums.
func TestAssets(t *testing.T) {
	in := input(t)
	assets, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{
		"checksums.txt", "install.ps1", "install.sh",
		"sereno_v0.1.0-test.1_darwin_amd64.tar.gz", "sereno_v0.1.0-test.1_darwin_arm64.tar.gz",
		"sereno_v0.1.0-test.1_linux_amd64.tar.gz", "sereno_v0.1.0-test.1_linux_arm64.tar.gz",
		"sereno_v0.1.0-test.1_windows_amd64.zip", "sereno_v0.1.0-test.1_windows_arm64.zip",
	}
	var names []string
	for name := range assets {
		names = append(names, name)
	}
	sort.Strings(names)
	if strings.Join(names, " ") != strings.Join(wantNames, " ") {
		t.Fatalf("assets:\n  %v\nwant\n  %v", names, wantNames)
	}

	var sums strings.Builder
	for _, target := range Targets() {
		name := target.Archive(in.Version)
		files, modes := unpack(t, name, assets[name])
		if len(files) != 3 {
			t.Errorf("%s holds %d files, want the program, LICENSE and README.md", name, len(files))
		}
		if !bytes.Equal(files[target.Program()], standIn(target)) {
			t.Errorf("%s: %s is not the program of %s/%s", name, target.Program(), target.OS, target.Arch)
		}
		if !bytes.Equal(files["LICENSE"], in.License) || !bytes.Equal(files["README.md"], in.Readme) {
			t.Errorf("%s: LICENSE or README.md is not the file of the repository", name)
		}
		if modes[target.Program()] != 0o755 || modes["LICENSE"] != 0o644 || modes["README.md"] != 0o644 {
			t.Errorf("%s: modes %v, want 755 for the program and 644 for the texts", name, modes)
		}
		sums.WriteString(engine.SHA256(assets[name]) + "  " + name + "\n")
	}
	if string(assets["checksums.txt"]) != sums.String() {
		t.Errorf("checksums.txt is\n%s\nwant\n%s", assets["checksums.txt"], sums.String())
	}

	for _, name := range []string{"install.ps1", "install.sh"} {
		script := string(assets[name])
		for _, place := range []string{placeVersion, placeRepository, placeChecksums} {
			if strings.Contains(script, place) {
				t.Errorf("%s still holds the placeholder %s", name, place)
			}
		}
		if !strings.Contains(script, "\n"+sums.String()) {
			t.Errorf("%s does not carry the lines of checksums.txt", name)
		}
		if !strings.Contains(script, "'v0.1.0-test.1'") || !strings.Contains(script, "'example/serena-terminal'") {
			t.Errorf("%s does not carry the version and the repository as quoted values", name)
		}
		for i := 0; i < len(script); i++ {
			if b := script[i]; b != '\n' && (b < 0x20 || b > 0x7e) {
				t.Errorf("%s: byte %#x at offset %d is not printable ASCII", name, b, i)
				break
			}
		}
	}
}

// TestSameInputSameBytes: a release can be packed again and compared.
func TestSameInputSameBytes(t *testing.T) {
	first, err := Build(input(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(input(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range first {
		if !bytes.Equal(data, second[name]) {
			t.Errorf("%s differs between two runs on the same input", name)
		}
	}
}

// TestRefusals: nothing is packed from half an input.
func TestRefusals(t *testing.T) {
	cases := map[string]func(*Input){
		"a version that is not one":    func(in *Input) { in.Version = "0.1.0" },
		"a repository that is not one": func(in *Input) { in.Repository = "https://example.org/x" },
		"a missing program":            func(in *Input) { delete(in.Programs, "linux_arm64") },
		"an empty program":             func(in *Input) { in.Programs["windows_amd64"] = nil },
		"a template without a version": func(in *Input) {
			in.TemplateSh = bytes.ReplaceAll(in.TemplateSh, []byte(placeVersion), []byte("x"))
		},
		"a template without the checksums line": func(in *Input) {
			in.TemplatePS1 = bytes.ReplaceAll(in.TemplatePS1, []byte("\n"+placeChecksums+"\n"), []byte("\n"))
		},
		"a template with the checksums inside a line": func(in *Input) {
			in.TemplateSh = bytes.ReplaceAll(in.TemplateSh, []byte("\n"+placeChecksums+"\n"), []byte("\nx "+placeChecksums+"\n"))
		},
		"a template with carriage returns": func(in *Input) {
			in.TemplatePS1 = bytes.ReplaceAll(in.TemplatePS1, []byte("\n"), []byte("\r\n"))
		},
	}
	for name, change := range cases {
		in := input(t)
		change(&in)
		if _, err := Build(in); err == nil {
			t.Errorf("%s was packed", name)
		}
	}
}

// TestTemplatesNeverCompareWithAPlaceholder keeps a mistake out that would
// only show in a release: a template that tests whether it was rendered by
// comparing with the placeholder would, once rendered, compare the version
// with itself. The placeholders therefore stand only where a value is given
// or shown: in comments, and once each as the value of the variable.
func TestTemplatesNeverCompareWithAPlaceholder(t *testing.T) {
	for _, name := range []string{templatePS1, templateSh} {
		for number, line := range strings.Split(string(repositoryFile(t, name)), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || !strings.Contains(line, "__") {
				continue
			}
			switch trimmed {
			case "version='" + placeVersion + "'", "repository='" + placeRepository + "'",
				"$version = '" + placeVersion + "'", "$repository = '" + placeRepository + "'", placeChecksums:
			default:
				if strings.Contains(line, placeVersion) || strings.Contains(line, placeRepository) || strings.Contains(line, placeChecksums) {
					t.Errorf("%s:%d uses a placeholder outside a comment and outside the three assignments:\n  %s", name, number+1, trimmed)
				}
			}
		}
	}
}

// TestRun packs into a folder through the command line, as scripts/release.sh
// does, and refuses a tag before anything is read.
func TestRun(t *testing.T) {
	home := testenv.New(t)
	binaries := filepath.Join(home.Dir, "bin")
	for _, target := range Targets() {
		dir := filepath.Join(binaries, target.Dir())
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, target.Program()), standIn(target), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A repository as it is on the day of a release: the real LICENSE and
	// templates, and documents that speak of the version as released. The
	// test does not depend on what the documents of this repository say today.
	root := filepath.Join(home.Dir, "repository")
	files := map[string][]byte{
		"LICENSE":      repositoryFile(t, "LICENSE"),
		templatePS1:    repositoryFile(t, templatePS1),
		templateSh:     repositoryFile(t, templateSh),
		"README.md":    []byte("# Serena Terminal\n\nVersion 0.1.0.\n"),
		"CHANGELOG.md": []byte("# Changelog\n\n## Unreleased\n\n## 0.1.0 - 2026-11-02\n\nThe first release.\n"),
	}
	for name, data := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := filepath.Join(home.Dir, "dist")
	var stdout, stderr bytes.Buffer
	arguments := []string{"--version", "v0.1.0", "--binaries", binaries, "--out", out, "--root", root, "--epoch", "1760000000"}
	if code := run(arguments, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d: %s", code, stderr.String())
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 9 {
		t.Errorf("%d files in the folder, want 9", len(entries))
	}
	sums, err := os.ReadFile(filepath.Join(out, ChecksumsName))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		sum, name, ok := strings.Cut(line, "  ")
		if !ok {
			t.Fatalf("checksums.txt has the line %q", line)
		}
		if got, err := engine.HashFile(filepath.Join(out, name)); err != nil || got != sum {
			t.Errorf("%s: SHA-256 %s (%v), checksums.txt says %s", name, got, err, sum)
		}
	}
	// The default repository is the one of R1.
	script, err := os.ReadFile(filepath.Join(out, InstallSh))
	if err != nil || !strings.Contains(string(script), "repository='"+DefaultRepository+"'") {
		t.Errorf("install.sh does not name %s (%v)", DefaultRepository, err)
	}

	stdout.Reset()
	if code := run([]string{"--version", "v0.1.0", "--check", "--root", root}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "v0.1.0 may name a release") {
		t.Errorf("--check on v0.1.0: exit code %d, %q", code, stdout.String())
	}
	stderr.Reset()
	if code := run([]string{"--version", "nightly", "--check", "--root", root}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "cannot name a release") {
		t.Errorf("--check on a tag that is no version: exit code %d, %q", code, stderr.String())
	}
	stderr.Reset()
	if code := run([]string{"--version", "v0.1.0", "--binaries", filepath.Join(home.Dir, "nowhere"), "--out", out, "--root", root}, &stdout, &stderr); code != 1 {
		t.Errorf("a folder without programs: exit code %d", code)
	}
	if code := run([]string{"--binaries", binaries, "--out", out, "--root", root}, &stdout, &stderr); code != 2 {
		t.Errorf("no version: exit code %d, want 2", code)
	}

	// A version the documents do not speak of yet is refused before anything
	// is packed, by --check and by a run alike; a trial tag is not.
	later := filepath.Join(home.Dir, "later")
	for _, arguments := range [][]string{
		{"--version", "v0.2.0", "--check", "--root", root},
		{"--version", "v0.2.0", "--binaries", binaries, "--out", later, "--root", root},
	} {
		stderr.Reset()
		if code := run(arguments, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), `CHANGELOG.md has no section "## 0.2.0"`) {
			t.Errorf("a version without a section in CHANGELOG.md: exit code %d, %q", code, stderr.String())
		}
	}
	if _, err := os.Stat(later); err == nil {
		t.Error("a folder was written for a version that was refused")
	}
	if code := run([]string{"--version", "v0.2.0-rc.1", "--check", "--root", root}, &stdout, &stderr); code != 0 {
		t.Errorf("a trial tag was refused for what the documents say: exit code %d, %q", code, stderr.String())
	}
}

// TestDocumentsMustSpeakOfTheVersion: README.md is packed into every archive,
// so a version cannot be tagged while it says that the version is not
// released, or before CHANGELOG.md has a section for it.
func TestDocumentsMustSpeakOfTheVersion(t *testing.T) {
	released := []byte("# Serena Terminal\n\nVersion 0.1.0.\n")
	unreleased := []byte("# Serena Terminal\n\n" + UnreleasedMark + "\n**Not released yet.**\n")
	changelog := []byte("# Changelog\n\n## Unreleased\n\n## 0.1.0 - 2026-11-02\r\n\n## 0.1\n")
	cases := []struct {
		version   string
		readme    []byte
		changelog []byte
		refusal   string
	}{
		{"v0.1.0", released, changelog, ""},
		{"v0.1.0", unreleased, changelog, "README.md still says"},
		{"v0.1.1", released, changelog, `no section "## 0.1.1"`},
		{"v0.1.0", released, []byte("# Changelog\n\n## Unreleased\n\n## 0.1.00\n\n## 0.1.0-rc.1\n"), `no section "## 0.1.0"`},
		{"v0.1.0", released, []byte("## 0.1.0\n"), ""},
		{"v0.1.0-rc.1", unreleased, []byte("# Changelog\n\n## Unreleased\n"), ""},
	}
	for _, c := range cases {
		err := CheckDocuments(c.version, c.readme, c.changelog)
		switch {
		case c.refusal == "" && err != nil:
			t.Errorf("%s: refused: %v", c.version, err)
		case c.refusal != "" && (err == nil || !strings.Contains(err.Error(), c.refusal)):
			t.Errorf("%s: got %v, want a refusal with %q", c.version, err, c.refusal)
		}
	}
}
