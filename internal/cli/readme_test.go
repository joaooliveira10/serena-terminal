package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/i18n"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/target/winterminal"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// The two READMEs and docs/uninstall.md show output of the program. These
// tests run the program again, in a test home, and look for what it prints in
// the document, so that an example cannot drift away from the code.

// document reads a document of the repository, named with forward slashes.
func document(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testenv.RepoRoot(t), filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

// readme reads the README of a language.
func readme(t *testing.T, lang string) (name, text string) {
	t.Helper()
	name = "README.md"
	if lang == "pt-BR" {
		name = "README.pt-BR.md"
	}
	return name, document(t, name)
}

// windowsHome is the name the documents give the test home of a Windows run.
const windowsHome = "C:\\Users\\user"

// asShown turns the output of a test run into what a document shows: the
// temporary home under a neutral name, and the version of a build that is
// not a release.
func asShown(w *world, out, home string) string {
	out = strings.ReplaceAll(out, w.home.Dir, home)
	return strings.ReplaceAll(out, Program+" "+testVersion, Program+" "+DevVersion)
}

// asShownOnWindows is asShown for a Windows home, with the two things a
// document fixes besides: the program is where `go install` puts it, and the
// processor is the common one.
func asShownOnWindows(w *world, out string) string {
	out = strings.ReplaceAll(out, filepath.Join(w.home.Dir, "bin", Program), windowsHome+"\\go\\bin\\sereno.exe")
	out = strings.ReplaceAll(out, "(windows/"+runtime.GOARCH+")", "(windows/amd64)")
	return asShown(w, out, windowsHome)
}

// block is a piece of output the way a document shows it: a fenced block
// marked as text.
func block(out string) string { return "```text\n" + out + "```\n" }

// linesFrom returns the lines of a plan from the first one that starts with
// first up to and including the first one after it that starts with last,
// each with its line feed. It is empty when either is missing.
func linesFrom(out, first, last string) string {
	var picked strings.Builder
	started := false
	for _, line := range strings.SplitAfter(out, "\n") {
		if !started && !strings.HasPrefix(line, first) {
			continue
		}
		started = true
		picked.WriteString(line)
		if strings.HasPrefix(line, last) {
			return picked.String()
		}
	}
	return ""
}

// TestReadmesShowTheRealPlan: the plan of `sereno apply --dry-run` in the
// section about what is written where, for Windows with Windows Terminal and
// for Linux with an empty home. The paths of a Windows home can only be
// spelled on Windows and those of a Linux home only elsewhere, so each
// system checks the plan it can produce; CI runs both.
func TestReadmesShowTheRealPlan(t *testing.T) {
	cases := []struct {
		goos, fixture, home string
	}{
		{platform.Windows, "wt-present", windowsHome},
		{platform.Linux, "", "/home/user"},
	}
	for _, c := range cases {
		if (c.goos == platform.Windows) != (runtime.GOOS == platform.Windows) {
			continue
		}
		bothLanguages(t, func(t *testing.T, lang string) {
			w := newWorld(t, c.goos, c.fixture)
			plan := asShown(w, w.must(engine.ExitOK, "apply", "--dry-run", "--lang", lang).out, c.home)
			if name, text := readme(t, lang); !strings.Contains(text, block(plan)) {
				t.Errorf("%s does not show the plan `sereno apply --dry-run` prints on %s today:\n%s", name, c.goos, plan)
			}
		})
	}
}

// TestReadmesShowRealOutput: the examples of the commands whose output does
// not depend on the system: check, and the help of theme.
func TestReadmesShowRealOutput(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		w := newWorld(t, platform.Linux, "")
		name, text := readme(t, lang)
		for _, arguments := range [][]string{{"check"}, {"help", "theme"}} {
			out := w.must(engine.ExitOK, append(arguments, "--lang", lang)...).out
			if !strings.Contains(text, block(out)) {
				t.Errorf("%s does not show what `sereno %s` prints today:\n%s", name, strings.Join(arguments, " "), out)
			}
		}
	})
}

// TestReadmesSayWhatComesAfterApply: the section about apply tells a reader
// what to do once the files are written, in the words apply itself ends
// with: on Windows the Serena profile is in the new-tab menu of Windows
// Terminal, and on macOS and Linux nothing loads the files until a hook line
// is asked for. The sentences are quoted, so they are looked for letter for
// letter, in the output and in the README.
func TestReadmesSayWhatComesAfterApply(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		catalog := i18n.For(i18n.Lang(lang))
		name, text := readme(t, lang)
		for _, c := range []struct {
			goos, fixture, key string
		}{
			{platform.Windows, "wt-present", "wt.notice.open"},
			{platform.Linux, "", "apply.next.hook-unix"},
		} {
			sentence := catalog.T(c.key)
			w := newWorld(t, c.goos, c.fixture)
			if out := w.must(engine.ExitOK, "apply", "--lang", lang).out; !strings.Contains(out, sentence+"\n") {
				t.Errorf("the first apply on %s no longer ends with the sentence %s:\n%s", c.goos, c.key, out)
			}
			if !strings.Contains(text, `"`+sentence+`"`) {
				t.Errorf("%s does not quote what apply says next on %s (%s): %s", name, c.goos, c.key, sentence)
			}
		}
	})
}

// TestReadmesSayWhatWindowsTerminalKeeps: what an uninstall leaves in the
// files of Windows Terminal is said by the program, in the sentence of
// uninstall.remains.terminal, and by the section about uninstalling of each
// README, in the same words: the entry is to be left, and what Windows
// Terminal is expected to do with it once it has been closed and opened
// again. Neither says again that the entry is inactive and hidden, which is
// not what the source code of Windows Terminal 1.24 says. docs/uninstall.md
// quotes the English sentence whole
// (TestUninstallDocumentQuotesRealSentences); the Portuguese one, which is
// what a Windows in Brazilian Portuguese prints, is held here.
func TestReadmesSayWhatWindowsTerminalKeeps(t *testing.T) {
	expected := map[string]string{
		"en":    "once Windows Terminal has been closed and opened again, it is expected to leave Serena out of its new-tab menu and to go on listing it in its settings, as a profile that is no longer detected",
		"pt-BR": "depois que o Windows Terminal for fechado e aberto de novo, o esperado é que ele tire o Serena do menu de nova aba e continue a listá-lo nas configurações, como um perfil que não é mais detectado",
	}
	bothLanguages(t, func(t *testing.T, lang string) {
		sentence := i18n.For(i18n.Lang(lang)).T("uninstall.remains.terminal")
		name, text := readme(t, lang)
		if !strings.Contains(sentence, expected[lang]) {
			t.Errorf("uninstall.remains.terminal (%s) no longer says what Windows Terminal is expected to do with the entry:\n%s", lang, sentence)
		}
		if !strings.Contains(text, expected[lang]) {
			t.Errorf("%s does not say what the program says about the entry Windows Terminal keeps: %s", name, expected[lang])
		}
		for _, old := range []string{"inactive and hidden", "inativa e oculta"} {
			if strings.Contains(sentence, old) {
				t.Errorf("uninstall.remains.terminal (%s) calls the entry %q again", lang, old)
			}
			if strings.Contains(text, old) {
				t.Errorf("%s calls the entry %q again", name, old)
			}
		}
	})
}

// TestReadmesShowRealOutputOfWindows: the examples that were taken on a
// Windows home, in the order a reader meets the commands: the hook lines of
// a dry run, a second apply, status, the plan of an uninstall, a change of
// theme, an export, the version.
func TestReadmesShowRealOutputOfWindows(t *testing.T) {
	if runtime.GOOS != platform.Windows {
		t.Skip("the paths of a Windows home can only be spelled on Windows")
	}
	bothLanguages(t, func(t *testing.T, lang string) {
		w := newWorld(t, platform.Windows, "wt-present")
		name, text := readme(t, lang)
		shown := func(what string, out string) {
			t.Helper()
			out = asShownOnWindows(w, out)
			if out == "" || !strings.Contains(text, block(out)) {
				t.Errorf("%s does not show what `sereno %s` prints today:\n%s", name, what, out)
			}
		}
		// The two hook lines: of the plan the README shows the four lines
		// that are about them. Nothing is applied yet, and a dry run leaves
		// it so.
		dry := w.must(engine.ExitOK, "apply", "--hook", "powershell,bash", "--dry-run", "--lang", lang).out
		shown("apply --hook powershell,bash --dry-run (its append lines)", linesFrom(dry, "  append  ", "          + [ -r "))

		w.must(engine.ExitOK, "apply", "--lang", lang)
		shown("apply (a second time)", w.must(engine.ExitOK, "apply", "--lang", lang).out)
		// The machine of the example has no hook line and no PowerShell 7:
		// status says both, and warns about the second.
		shown("status", w.must(engine.ExitOK, "status", "--lang", lang).out)
		shown("uninstall --dry-run", w.must(engine.ExitOK, "uninstall", "--dry-run", "--lang", lang).out)
		shown("theme light --flavor vivid", w.must(engine.ExitOK, "theme", "light", "--flavor", "vivid", "--lang", lang).out)
		shown("export --target kitty", w.must(engine.ExitOK, "export", "--target", "kitty", "--lang", lang).out)
		shown("version", w.must(engine.ExitOK, "version", "--lang", lang).out)
	})
}

// uninstallDocument is the page about the way back. It is in English only.
const uninstallDocument = "docs/uninstall.md"

// TestUninstallDocumentQuotesRealSentences: the sentences the page quotes
// from the program are the ones of the catalogue, and the object it tells a
// reader to add back to Windows Terminal's settings names the profile sereno
// writes. That the program prints them in those situations is tested where
// the situations are (TestUninstallAfterNoProfile, TestCommandsMakeWay).
func TestUninstallDocumentQuotesRealSentences(t *testing.T) {
	catalog := i18n.For(i18n.English)
	text := document(t, uninstallDocument)
	for what, quoted := range map[string]string{
		"the sentence about the entry of Windows Terminal": block(catalog.T("uninstall.remains.terminal") + "\n"),
		"what a command says when it makes way":            block(Program + ": " + catalog.T("error.busy") + "\n"),
		"the profile of the object to add back":            `{ "guid": "` + winterminal.ProfileGUID() + `", "hidden": false, "name": "Serena", "source": "SerenaTerminal" }`,
	} {
		if !strings.Contains(text, quoted) {
			t.Errorf("%s does not have %s as the program has it today:\n%s", uninstallDocument, what, quoted)
		}
	}
	// The same sentence, quoted in the page about how sereno works.
	if busy := `"` + catalog.T("error.busy") + `"`; !strings.Contains(document(t, "docs/how-it-works.md"), busy) {
		t.Errorf("docs/how-it-works.md does not quote what a command says when it makes way: %s", busy)
	}
}

// TestUninstallDocumentShowsRealOutput: the three runs the page shows, taken
// again on the same Windows home: the plan of an uninstall after two hook
// lines and an export for kitty, an uninstall that meets a file the user
// edited, and one that keeps the backup of a start-up file the user changed.
func TestUninstallDocumentShowsRealOutput(t *testing.T) {
	if runtime.GOOS != platform.Windows {
		t.Skip("the paths of a Windows home can only be spelled on Windows")
	}
	text := document(t, uninstallDocument)
	shown := func(w *world, what, out string) {
		t.Helper()
		out = asShownOnWindows(w, out)
		if out == "" || !strings.Contains(text, block(out)) {
			t.Errorf("%s does not show %s as the program prints it today:\n%s", uninstallDocument, what, out)
		}
	}

	w := newWorld(t, platform.Windows, "wt-present")
	w.must(engine.ExitOK, "apply", "--hook", "powershell,bash", "--lang", "en")
	w.must(engine.ExitOK, "export", "--target", "kitty", "--lang", "en")
	tree := w.tree()
	shown(w, "the dry run of an uninstall after two hook lines and an export for kitty", w.must(engine.ExitOK, "uninstall", "--dry-run", "--lang", "en").out)
	sameTree(t, "uninstall --dry-run", tree, w.tree())

	// A file sereno wrote and the user changed: it stays, the run ends with
	// exit code 4, and the page shows the run up to its "Done."; the sentence
	// about the program follows after an empty line.
	w = newWorld(t, platform.Windows, "wt-present")
	w.must(engine.ExitOK, "apply", "--lang", "en")
	edited := filepath.Join(w.root(), "shell", "init.bash")
	w.write(edited, "# mine now\n")
	run, _, _ := strings.Cut(w.must(engine.ExitIncomplete, "uninstall", "--lang", "en").out, "\n\n")
	shown(w, "an uninstall that meets a file the user edited", run+"\n")
	if w.read(edited) != "# mine now\n" {
		t.Error("the edited file was touched")
	}

	// A start-up file the user changed after the hook line went in: the line
	// is taken out, and the backup stays. The page shows those three lines.
	w = newWorld(t, platform.Windows, "wt-present")
	w.must(engine.ExitOK, "apply", "--hook", "bash", "--lang", "en")
	bashrc := filepath.Join(w.home.Dir, ".bashrc")
	w.write(bashrc, w.read(bashrc)+"# a line of mine\n")
	out := w.must(engine.ExitOK, "uninstall", "--lang", "en").out
	shown(w, "the lines of an uninstall that keeps a backup", linesFrom(out, "  replace ", "  keep    "))
}
