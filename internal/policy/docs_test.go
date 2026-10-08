package policy

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/cli"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// The documents a reader starts from. README.pt-BR.md is README.md in
// Brazilian Portuguese and is kept in step with it, section by section.
const (
	readmeEnglish    = "README.md"
	readmePortuguese = "README.pt-BR.md"
)

// unreleasedMark is the comment both READMEs carry next to their status
// line for as long as they say that the version is not released. The release
// tool refuses to pack a version while README.md has it.
const unreleasedMark = "<!-- sereno:status unreleased -->"

// repositoryText reads a file of the repository as text with line feeds.
func repositoryText(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testenv.RepoRoot(t), filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

// documents lists the Markdown files written for readers: the two READMEs,
// CHANGELOG.md, SECURITY.md and everything below docs, with forward slashes.
// The specification is a working contract, not a document for users, and
// docs/contrast.md is generated; both are left to their own tests.
func documents(t *testing.T) []string {
	t.Helper()
	root := testenv.RepoRoot(t)
	names := []string{readmeEnglish, readmePortuguese, "CHANGELOG.md", "SECURITY.md"}
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if name := filepath.ToSlash(rel); name != "docs/spec-0.1.md" && name != "docs/contrast.md" {
			names = append(names, name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	return names
}

// codeBlock is a fenced block of a Markdown file.
type codeBlock struct {
	info  string // what follows the opening fence: sh, powershell, text
	lines []string
}

// section is a heading and what stands below it, up to the next heading.
type section struct {
	level   int
	title   string
	blocks  []codeBlock
	rows    int      // lines of a table
	bullets int      // list items
	spans   []string // inline code outside fenced blocks
}

var inlineCode = regexp.MustCompile("`([^`\n]+)`")

// parseMarkdown splits a document into its sections. What stands in front of
// the first heading is a section of level 0.
func parseMarkdown(text string) []section {
	sections := []section{{}}
	var block *codeBlock
	for _, line := range strings.Split(text, "\n") {
		current := &sections[len(sections)-1]
		switch {
		case strings.HasPrefix(line, "```"):
			if block == nil {
				block = &codeBlock{info: strings.TrimSpace(strings.TrimPrefix(line, "```"))}
			} else {
				current.blocks = append(current.blocks, *block)
				block = nil
			}
		case block != nil:
			block.lines = append(block.lines, line)
		case strings.HasPrefix(line, "#"):
			level := len(line) - len(strings.TrimLeft(line, "#"))
			sections = append(sections, section{level: level, title: strings.TrimSpace(line[level:])})
		default:
			if strings.HasPrefix(line, "|") {
				current.rows++
			}
			if strings.HasPrefix(line, "- ") {
				current.bullets++
			}
			for _, match := range inlineCode.FindAllStringSubmatch(line, -1) {
				current.spans = append(current.spans, match[1])
			}
		}
	}
	return sections
}

// TestRepositoryLayout: the documents and workflows the specification lists
// exist. An empty file would pass the other tests of this file unnoticed.
func TestRepositoryLayout(t *testing.T) {
	for _, name := range []string{
		readmeEnglish, readmePortuguese, "LICENSE", "CHANGELOG.md", "SECURITY.md",
		"docs/how-it-works.md", "docs/contrast.md", "docs/support-matrix.md", "docs/uninstall.md", "docs/privacy.md",
		"docs/adr/0001-not-an-emulator.md", "docs/adr/0002-slots-not-hex.md", "docs/adr/0003-in-band-palette.md",
		".github/workflows/ci.yml", ".github/workflows/release.yml", ".github/dependabot.yml",
		"scripts/install.ps1.tmpl", "scripts/install.sh.tmpl", "scripts/release.sh",
	} {
		if text := repositoryText(t, name); len(strings.TrimSpace(text)) < 200 {
			t.Errorf("%s is nearly empty", name)
		}
	}
}

// TestReadmesSayFirstWhatTheProjectIsNot is the one sentence the
// specification fixes: Serena Terminal is not a terminal emulator and is not
// related to the Oraios tool of a similar name. It is the first sentence
// after the title, in both languages.
func TestReadmesSayFirstWhatTheProjectIsNot(t *testing.T) {
	for name, wanted := range map[string][]string{
		readmeEnglish:    {"is not a terminal emulator", "not related to the Oraios"},
		readmePortuguese: {"não é um emulador de terminal", "não tem relação com", "Oraios"},
	} {
		lines := strings.Split(repositoryText(t, name), "\n")
		if len(lines) < 3 || lines[0] != "# Serena Terminal" || lines[1] != "" {
			t.Errorf("%s does not start with the title and an empty line", name)
			continue
		}
		first := lines[2]
		if end := strings.Index(first, ". "); end >= 0 {
			first = first[:end+1]
		}
		for _, piece := range wanted {
			if !strings.Contains(first, piece) {
				t.Errorf("the first sentence of %s does not say %q: %s", name, piece, first)
			}
		}
	}
}

// TestReadmesAreInStep compares the two READMEs section by section: the same
// headings at the same levels, the same number of code blocks, table lines
// and list items in each, and the same commands. What a person types is not
// translated, so every block that is not program output must be the same
// text in both files.
func TestReadmesAreInStep(t *testing.T) {
	english := parseMarkdown(repositoryText(t, readmeEnglish))
	portuguese := parseMarkdown(repositoryText(t, readmePortuguese))
	if len(english) != len(portuguese) {
		t.Fatalf("%s has %d sections, %s has %d", readmeEnglish, len(english)-1, readmePortuguese, len(portuguese)-1)
	}
	if len(english) < 15 {
		t.Fatalf("only %d sections found in %s; the parsing is probably broken", len(english)-1, readmeEnglish)
	}
	blocks := 0
	for i := range english {
		en, pt := english[i], portuguese[i]
		where := "section " + en.title
		if en.level != pt.level {
			t.Errorf("%s is a heading of level %d, its counterpart %q of level %d", where, en.level, pt.title, pt.level)
		}
		// The names of the commands are headings, and are not translated.
		if contains(cli.Commands(), en.title) && en.title != pt.title {
			t.Errorf("%s is the command %q in English and %q in Portuguese", where, en.title, pt.title)
		}
		if en.rows != pt.rows || en.bullets != pt.bullets {
			t.Errorf("%s has %d table lines and %d list items, its counterpart %q has %d and %d", where, en.rows, en.bullets, pt.title, pt.rows, pt.bullets)
		}
		if len(en.blocks) != len(pt.blocks) {
			t.Errorf("%s has %d code blocks, its counterpart %q has %d", where, len(en.blocks), pt.title, len(pt.blocks))
			continue
		}
		for j := range en.blocks {
			blocks++
			a, b := en.blocks[j], pt.blocks[j]
			if a.info != b.info {
				t.Errorf("%s: code block %d is %q in English and %q in Portuguese", where, j+1, a.info, b.info)
			}
			if a.info == "" {
				t.Errorf("%s: code block %d says neither what language it is in nor that it is output (text)", where, j+1)
			}
			if a.info != "text" && strings.Join(a.lines, "\n") != strings.Join(b.lines, "\n") {
				t.Errorf("%s: the commands of code block %d differ between the two languages:\n%s\n---\n%s", where, j+1, strings.Join(a.lines, "\n"), strings.Join(b.lines, "\n"))
			}
			if a.info == "text" && len(a.lines) != len(b.lines) {
				t.Errorf("%s: the output in code block %d has %d lines in English and %d in Portuguese", where, j+1, len(a.lines), len(b.lines))
			}
		}
	}
	if blocks < 20 {
		t.Errorf("only %d code blocks compared; the parsing is probably broken", blocks)
	}
}

func contains(list []string, item string) bool {
	for _, known := range list {
		if known == item {
			return true
		}
	}
	return false
}

// commandFlags returns the flags of every command, from the keys of its help.
func commandFlags() map[string]map[string]bool {
	flags := map[string]map[string]bool{}
	for _, name := range cli.Commands() {
		flags[name] = map[string]bool{}
		prefix := "help." + name + ".flag."
		for _, key := range cli.HelpKeys(name) {
			if strings.HasPrefix(key, prefix) {
				flags[name][strings.TrimPrefix(key, prefix)] = true
			}
		}
	}
	return flags
}

// globalFlags are the flags every command takes (R3).
var globalFlags = map[string]bool{"lang": true, "no-color": true, "help": true}

// foreignFlags are flags of other programs that the documents name in running
// text: the install script for macOS and Linux.
var foreignFlags = map[string]bool{"uninstall": true}

// flagName returns the name of a flag written as --name, --name=value or
// --name value, and false for anything else.
func flagName(token string) (string, bool) {
	if !strings.HasPrefix(token, "--") {
		return "", false
	}
	name := strings.TrimPrefix(token, "--")
	if cut := strings.IndexAny(name, "= "); cut >= 0 {
		name = name[:cut]
	}
	return name, regexp.MustCompile(`^[a-z][a-z-]*$`).MatchString(name)
}

// TestDocumentsNameRealCommandsAndFlags reads every command line of sereno
// the documents show, in a code block or in inline code, and every flag they
// name on its own, and checks them against the table of commands. A document
// that tells a reader to type something the program does not take is worse
// than no document.
func TestDocumentsNameRealCommandsAndFlags(t *testing.T) {
	flags := commandFlags()
	anyFlag := map[string]bool{}
	for _, set := range flags {
		for name := range set {
			anyFlag[name] = true
		}
	}
	checked := 0
	commandLine := func(where, line string) {
		t.Helper()
		words := strings.Fields(line)
		if len(words) < 2 || words[0] != cli.Program {
			return
		}
		checked++
		command := ""
		for _, word := range words[1:] {
			name, isFlag := flagName(word)
			switch {
			case word == "-h":
			case strings.HasPrefix(word, "-") && !isFlag:
				t.Errorf("%s: %q in `%s` is not written like a flag of sereno", where, word, line)
			case isFlag && !globalFlags[name] && (command == "" || !flags[command][name]):
				t.Errorf("%s: `%s` uses --%s, which `sereno %s` does not have", where, line, name, command)
			case !isFlag && command == "":
				if !contains(cli.Commands(), word) {
					t.Errorf("%s: `%s` names the command %q, which sereno does not have", where, line, word)
					return
				}
				command = word
			}
		}
	}
	for _, name := range documents(t) {
		for _, part := range parseMarkdown(repositoryText(t, name)) {
			where := name + ", section " + part.title
			for _, block := range part.blocks {
				if block.info == "text" {
					continue // output of a program, not something to type
				}
				for _, line := range block.lines {
					commandLine(where, strings.TrimPrefix(strings.TrimSpace(line), "$ "))
				}
			}
			for _, span := range part.spans {
				span = strings.ReplaceAll(span, `\|`, "|") // a bar inside a table cell
				commandLine(where, span)
				if name, isFlag := flagName(span); isFlag && !anyFlag[name] && !globalFlags[name] && !foreignFlags[name] {
					t.Errorf("%s names the flag --%s, which no command of sereno has", where, name)
				}
			}
		}
	}
	if checked < 40 {
		t.Errorf("only %d command lines of sereno found in the documents; the parsing is probably broken", checked)
	}
}

// TestDocumentsNameRealWarnings: the ids of the warnings of `sereno status`
// that the documents quote are ids the command has.
func TestDocumentsNameRealWarnings(t *testing.T) {
	looksLikeID := regexp.MustCompile(`^(wt|owned|hook|session|pwsh|unix)\.[a-z][a-z-]*$`)
	found := 0
	for _, name := range documents(t) {
		for _, part := range parseMarkdown(repositoryText(t, name)) {
			for _, span := range part.spans {
				if !looksLikeID.MatchString(span) || span == "pwsh.exe" {
					continue
				}
				found++
				if !contains(cli.WarningIDs(), span) {
					t.Errorf("%s, section %s quotes the warning %q, which `sereno status` does not have", name, part.title, span)
				}
			}
		}
	}
	if found < 5 {
		t.Errorf("only %d warning ids found in the documents; the parsing is probably broken", found)
	}
}

// link matches a Markdown link and captures its target without the anchor.
var link = regexp.MustCompile(`\]\(([^)#\s]+)(#[^)\s]*)?\)`)

// TestDocumentLinksResolve: a link from one document of the repository to
// another file of it names a file that exists.
func TestDocumentLinksResolve(t *testing.T) {
	root := testenv.RepoRoot(t)
	local := 0
	for _, name := range documents(t) {
		for _, match := range link.FindAllStringSubmatch(repositoryText(t, name), -1) {
			target := match[1]
			if strings.HasPrefix(target, "https://") {
				continue
			}
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				t.Errorf("%s links to %s: only https links leave the repository", name, target)
				continue
			}
			local++
			path := filepath.Join(root, filepath.FromSlash(filepath.ToSlash(filepath.Dir(name))), filepath.FromSlash(target))
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s links to %s, which does not exist", name, target)
			}
		}
	}
	if local < 15 {
		t.Errorf("only %d links between documents found; the parsing is probably broken", local)
	}
}

// TestDocumentsAgreeOnTheStatus: while a version is not released, both
// READMEs say so next to the mark the release tool looks for, and
// CHANGELOG.md describes it under "Unreleased". Once the mark is taken out of
// one README it must be out of both.
func TestDocumentsAgreeOnTheStatus(t *testing.T) {
	english := strings.Contains(repositoryText(t, readmeEnglish), unreleasedMark)
	portuguese := strings.Contains(repositoryText(t, readmePortuguese), unreleasedMark)
	if english != portuguese {
		t.Errorf("%s carries the mark %s: %v; %s: %v", readmeEnglish, unreleasedMark, english, readmePortuguese, portuguese)
	}
	if english && !strings.Contains(repositoryText(t, "CHANGELOG.md"), "\n## Unreleased\n") {
		t.Error("README.md says the version is not released, and CHANGELOG.md has no section \"## Unreleased\"")
	}
	if english {
		for name, sentence := range map[string]string{readmeEnglish: "is not released yet", readmePortuguese: "ainda não foi lançada"} {
			if !strings.Contains(repositoryText(t, name), sentence) {
				t.Errorf("%s carries the mark %s and does not say that the version %s", name, unreleasedMark, sentence)
			}
		}
	}
}

// TestDocumentsNeverPipeADownloadIntoAShell: the documents tell a reader to
// download an install script, read it and run it, never to hand a download
// to a shell unseen.
func TestDocumentsNeverPipeADownloadIntoAShell(t *testing.T) {
	piped := regexp.MustCompile(`(?i)\b(curl|wget|iwr|irm|invoke-webrequest|invoke-restmethod)\b[^|\n]*\|\s*(sudo\s+)?(sh|bash|zsh|pwsh|powershell|iex|invoke-expression)\b`)
	for _, name := range documents(t) {
		for number, line := range strings.Split(repositoryText(t, name), "\n") {
			if found := piped.FindString(line); found != "" {
				t.Errorf("%s:%d hands something to a shell through a pipe (%s)", name, number+1, strings.TrimSpace(found))
			}
		}
	}
}
