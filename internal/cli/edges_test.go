package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/i18n"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/target/winterminal"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// The tests of this file are the cases the independent review of the command
// line found: situations in which a sentence said something that was not so,
// or a command did something next to what it said.

// readOnly marks a file read-only and puts the mark back when the test ends,
// so that the temporary home can be removed.
func readOnly(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })
}

func writable(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestThemeLeavesFragmentAndModeFileInAgreement: when the mode file is left
// alone (the user edited it), the setting that was asked for is not stored,
// so the fragment must not get it either: it is rendered from what the file
// says, which is what the shells and the next apply go by (R35, R15). And
// nothing is said about sessions reading a new setting: there is none.
func TestThemeLeavesFragmentAndModeFileInAgreement(t *testing.T) {
	changed := i18n.For(i18n.English).T("theme.changed")

	w := newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply")
	edited := "mode=dark\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\n"
	w.write(w.modeFile(), edited)
	got := w.must(engine.ExitIncomplete, "theme", "light")
	if w.read(w.modeFile()) != edited {
		t.Error("an edited mode file was overwritten")
	}
	if w.read(w.fragment()) != golden(t, "fragment/dark-soft.json") {
		t.Errorf("the fragment is not the one of the mode file (dark, soft):\n%s", got.out)
	}
	if strings.Contains(got.out, changed) {
		t.Errorf("no setting was stored, and the command still talks about a new one:\n%s", got.out)
	}
	// The next apply agrees: it has nothing to write for the fragment.
	if again := w.must(engine.ExitIncomplete, "apply"); strings.Contains(again.out, "  replace "+w.fragment()) {
		t.Errorf("apply renders the fragment differently from theme:\n%s", again.out)
	}

	// A mode file that is not one (R11) means the defaults.
	w = newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply")
	w.write(w.modeFile(), "\x00\x01 not a mode file\n")
	got = w.must(engine.ExitIncomplete, "theme", "light", "--flavor", "vivid")
	if w.read(w.fragment()) != golden(t, "fragment/auto-soft.json") {
		t.Errorf("the fragment does not follow the defaults a malformed mode file stands for:\n%s", got.out)
	}
	if strings.Contains(got.out, changed) || strings.Contains(got.out, "  replace ") {
		t.Errorf("something was written or announced:\n%s", got.out)
	}
}

// TestReadOnlyOwnedFile: an owned file the user marked read-only is found
// before the first write, as a read-only profile is. It is left as it is,
// the command ends with exit code 4 and does not wait for anything.
func TestReadOnlyOwnedFile(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		w := newWorld(t, platform.Windows, "wt-present")
		w.must(0, "apply")
		readOnly(t, w.modeFile())
		tree := w.tree()

		start := time.Now()
		got := w.must(engine.ExitIncomplete, "theme", "light", "--lang", lang)
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("the command took %s: it waited for a file that is read-only", elapsed)
		}
		note := i18n.For(i18n.Lang(lang)).T(engine.NoteReadOnly)
		if !strings.Contains(got.out, "  skip    "+w.modeFile()+" ("+note+")") || got.err != "" {
			t.Errorf("no skip line that says the file is read-only:\n%s%s", got.out, got.err)
		}
		if !regexp.MustCompile(`(?m)^mode +auto$`).MatchString(got.out) {
			t.Errorf("the report does not show what the mode file says:\n%s", got.out)
		}
		sameTree(t, "theme with a read-only mode file", tree, w.tree())
		noMissingKey(t, got.out)

		// apply says the same about any owned file.
		init := filepath.Join(w.root(), "shell", "init.ps1")
		w.write(init, "# an older init file\n")
		// The record has to hold the hash of that content: an apply of an
		// older version wrote it, for this test.
		manifest := strings.Replace(w.read(w.manifest()), engine.SHA256([]byte(golden(t, "init/init.ps1"))), engine.SHA256([]byte("# an older init file\n")), 1)
		w.write(w.manifest(), manifest)
		readOnly(t, init)
		dry := w.must(engine.ExitIncomplete, "apply", "--dry-run", "--lang", lang)
		real := w.must(engine.ExitIncomplete, "apply", "--lang", lang)
		for _, out := range []string{dry.out, real.out} {
			if !strings.Contains(out, "  skip    "+init+" ("+note+")") {
				t.Errorf("apply does not skip the read-only init file:\n%s", out)
			}
		}
		if w.read(init) != "# an older init file\n" {
			t.Error("a read-only owned file was replaced")
		}

		// The way out: take the mark off. Then everything is written, and
		// uninstall takes everything back.
		writable(t, init)
		writable(t, w.modeFile())
		w.must(0, "apply", "--lang", lang)
		w.must(0, "theme", "light", "--lang", lang)
		if !strings.HasPrefix(w.read(w.modeFile()), "mode=light\n") || w.read(init) != golden(t, "init/init.ps1") {
			t.Error("the files were not written once they could be")
		}
	})
}

// TestStatusAndThemeReadTheFragment: whether the installed fragment carries
// the Serena profile is a fact of the file, not of the option the last apply
// recorded. After an apply with --no-profile that could not replace the
// fragment, the profile is still installed: status says so, and theme renders
// its colour scheme (R35, R37).
func TestStatusAndThemeReadTheFragment(t *testing.T) {
	w := newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply")
	readOnly(t, w.fragment())
	w.must(engine.ExitIncomplete, "apply", "--no-profile")
	if !strings.Contains(w.read(w.manifest()), `"profile": false`) || !strings.Contains(w.read(w.fragment()), `"commandline"`) {
		t.Fatal("the case needs a manifest that says no profile and a fragment that has one")
	}

	report, _ := statusOf(t, w)
	if !report.WindowsTerminal.Fragment || !report.WindowsTerminal.Profile {
		t.Errorf("status does not report the profile the fragment has: %+v", report.WindowsTerminal)
	}
	text := w.must(0, "status")
	if !strings.Contains(text.out, i18n.For(i18n.English).T("status.wt.fragment.profile")) {
		t.Errorf("the status text says something else than the fragment:\n%s", text.out)
	}

	writable(t, w.fragment())
	w.must(0, "theme", "light")
	if w.read(w.fragment()) != golden(t, "fragment/light-soft.json") {
		t.Error("theme did not render the colour scheme of the profile that is installed")
	}

	// And the other way round: a fragment without a profile gets none from
	// theme, whatever the option says. No command leaves that state behind;
	// the manifest is written by hand here.
	w = newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply", "--no-profile")
	w.write(w.manifest(), strings.Replace(w.read(w.manifest()), `"profile": false`, `"profile": true`, 1))
	if report, _ := statusOf(t, w); report.WindowsTerminal.Profile {
		t.Errorf("status reports a profile the fragment does not have: %+v", report.WindowsTerminal)
	}
	w.must(0, "theme", "light")
	if w.read(w.fragment()) != golden(t, "fragment/no-profile.json") {
		t.Error("theme wrote a profile into a fragment that had none")
	}
}

// TestUninstallAfterNoProfile: Windows Terminal keeps its entry for the
// Serena profile whatever sereno does afterwards. An apply with --no-profile
// takes the profile out of the fragment and not out of settings.json, so the
// uninstall that follows still has to say what remains (R39).
func TestUninstallAfterNoProfile(t *testing.T) {
	guid := winterminal.ProfileGUID()
	bothLanguages(t, func(t *testing.T, lang string) {
		remains := i18n.For(i18n.Lang(lang)).T("uninstall.remains.terminal")
		w := newWorld(t, platform.Windows, "wt-present")
		w.must(0, "apply")
		// What Windows Terminal does when it sees the profile.
		local := filepath.Join(w.home.LocalAppData, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState")
		w.write(filepath.Join(local, "settings.json"), `{"profiles": {"list": [{"guid": "`+guid+`", "hidden": false, "name": "Serena", "source": "SerenaTerminal"}]}}`)
		w.write(filepath.Join(local, "state.json"), `{"generatedProfiles": ["`+guid+`"]}`)
		w.must(0, "apply", "--no-profile")
		for _, args := range [][]string{{"uninstall", "--dry-run"}, {"uninstall"}} {
			if got := w.must(0, append(args, "--lang", lang)...); !strings.Contains(got.out, remains) {
				t.Errorf("sereno %v does not say that the entry of Windows Terminal remains:\n%s", args, got.out)
			}
		}
	})
}

// TestExportToAFolderNeedsNoInstallation: `export --to DIR` neither reads nor
// writes sereno's folder, so a manifest that cannot be read is none of its
// business; and when a write into DIR fails, it does not promise a record or
// an uninstall that do not exist for those files (R38).
func TestExportToAFolderNeedsNoInstallation(t *testing.T) {
	w := newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply")
	w.write(w.manifest(), "{ not json")
	destination := filepath.Join(w.home.Dir, "out")
	got := w.must(0, "export", "--to", destination, "--target", "kitty")
	if len(exported(t, destination)) != 6 || got.err != "" {
		t.Errorf("export --to with a broken manifest in sereno's folder:\n%s%s", got.out, got.err)
	}
	// Without --to the files would be recorded: that needs the manifest.
	w.must(engine.ExitFailure, "export")

	bothLanguages(t, func(t *testing.T, lang string) {
		catalog := i18n.For(i18n.Lang(lang))
		w := newWorld(t, platform.Linux, "lf")
		outside := testenv.TempDir(t)
		got := w.must(engine.ExitFailure, "export", "--to", filepath.Join(outside, "themes"), "--lang", lang)
		if !strings.Contains(got.err, catalog.T("error.write.next.untracked")) || strings.Contains(got.err, "`sereno uninstall`") {
			t.Errorf("a failed export --to promises what is not so:\n%s", got.err)
		}
		// A tracked plan keeps its sentence.
		w.vars["SERENO_HOME"] = filepath.Join(outside, "sereno")
		if got := w.must(engine.ExitFailure, "apply", "--lang", lang); !strings.Contains(got.err, catalog.T("error.write.next")) {
			t.Errorf("a failed apply does not say how to go on:\n%s", got.err)
		}
		noMissingKey(t, got.err)
	})
}

// TestUninstallThatLeavesSomething: after a complete uninstall the manifest
// is gone, so "run the command again" would be bad advice: the second run has
// nothing to remove. The text says what is so: the thing stays, nobody records
// it any more, and it is the user's to remove (R39, R5).
func TestUninstallThatLeavesSomething(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		catalog := i18n.For(i18n.Lang(lang))
		again := "run the command again"
		if lang != "en" {
			again = "rode o comando de novo"
		}

		// A profile that became read-only after the hook line went in.
		w := newWorld(t, platform.Linux, "lf")
		bashrc := filepath.Join(w.home.Dir, ".bashrc")
		w.must(0, "apply", "--hook", "bash")
		hooked := w.read(bashrc)
		readOnly(t, bashrc)

		dry := w.must(engine.ExitIncomplete, "uninstall", "--dry-run", "--lang", lang)
		if !strings.Contains(dry.out, catalog.T("plan.incomplete.uninstall.dry-run")) || strings.Contains(dry.out, catalog.T("plan.incomplete.uninstall")+"\n") {
			t.Errorf("the dry run does not say what would be left, in words for something that has not happened:\n%s", dry.out)
		}
		got := w.must(engine.ExitIncomplete, "uninstall", "--lang", lang)
		if !strings.Contains(got.out, catalog.T("plan.incomplete.uninstall")) || strings.Contains(got.out, again) {
			t.Errorf("the uninstall does not say that the rest is the user's to remove:\n%s", got.out)
		}
		// The backup stays because the line is still in the file, and that
		// is the reason given; the profile did not change.
		if !strings.Contains(got.out, "("+catalog.T(engine.NoteBackupLine, bashrc)+")") || strings.Contains(got.out, catalog.T(engine.NoteBackupKept, bashrc)) {
			t.Errorf("the backup line gives the wrong reason:\n%s", got.out)
		}
		if !strings.Contains(got.out, "          - "+strings.TrimSuffix(hooked[strings.LastIndex(strings.TrimSuffix(hooked, "\n"), "\n")+1:], "\n")) {
			t.Errorf("the line the user has to take out is not shown:\n%s", got.out)
		}
		if w.read(bashrc) != hooked {
			t.Error("a read-only profile was changed")
		}
		// And it is true: a second uninstall has nothing recorded.
		if second := w.must(0, "uninstall", "--lang", lang); !strings.Contains(second.out, w.root()) || strings.Contains(second.out, "plan") {
			t.Errorf("the second uninstall:\n%s", second.out)
		}
		noMissingKey(t, dry.out, got.out)

		// An owned file the user edited: the same sentence.
		w = newWorld(t, platform.Linux, "lf")
		w.must(0, "apply")
		w.write(filepath.Join(w.root(), "shell", "init.bash"), "# mine now\n")
		if got := w.must(engine.ExitIncomplete, "uninstall", "--lang", lang); !strings.Contains(got.out, catalog.T("plan.incomplete.uninstall")) || strings.Contains(got.out, again) {
			t.Errorf("uninstall with an edited file:\n%s", got.out)
		}

		// `uninstall --hook` keeps the record of a line it could not take
		// out, so there "run the command again" is right.
		w = newWorld(t, platform.Linux, "lf")
		bashrc = filepath.Join(w.home.Dir, ".bashrc")
		w.must(0, "apply", "--hook", "bash")
		readOnly(t, bashrc)
		if got := w.must(engine.ExitIncomplete, "uninstall", "--hook", "bash", "--lang", lang); !strings.Contains(got.out, catalog.T("plan.incomplete")) {
			t.Errorf("uninstall --hook with a read-only profile:\n%s", got.out)
		}
		writable(t, bashrc)
		w.must(0, "uninstall", "--hook", "bash", "--lang", lang)
		// apply keeps its sentence too.
		w.write(filepath.Join(w.root(), "shell", "init.bash"), "# mine now\n")
		if got := w.must(engine.ExitIncomplete, "apply", "--lang", lang); !strings.Contains(got.out, catalog.T("plan.incomplete")) {
			t.Errorf("apply with an edited file:\n%s", got.out)
		}
	})
}

// TestEditedModeFileAndUninstall: status says of an edited file what apply
// and uninstall do with it, and the mode file is the exception: uninstall
// deletes it whatever it holds (R39, step 3). The warning has to say so.
func TestEditedModeFileAndUninstall(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		exception := "except the mode file, which uninstall always deletes"
		if lang != "en" {
			exception = "menos o arquivo mode, que o uninstall sempre apaga"
		}
		w := newWorld(t, platform.Windows, "wt-present")
		w.must(0, "apply")
		init := filepath.Join(w.root(), "shell", "init.ps1")
		w.write(w.modeFile(), "mode=dark\nflavor=soft\ninband=profile\nkeys=profile\nprompt=on\n")
		w.write(init, "# mine now\n")

		report, _ := statusOf(t, w, "--lang", lang)
		text := ""
		for _, warning := range report.Warnings {
			if warning.ID == WarnOwnedModified {
				text = warning.Text
			}
		}
		if !strings.Contains(text, w.modeFile()) || !strings.Contains(text, init) || !strings.Contains(text, exception) {
			t.Errorf("owned.modified does not say what happens to the mode file: %q", text)
		}
		// What the warning says is what happens.
		got := w.must(engine.ExitIncomplete, "uninstall", "--lang", lang)
		if !strings.Contains(got.out, "  remove  "+w.modeFile()+"\n") || !strings.Contains(got.out, "  skip    "+init+" (") {
			t.Errorf("uninstall with an edited mode file and an edited init file:\n%s", got.out)
		}
		if _, err := os.Stat(w.modeFile()); !os.IsNotExist(err) {
			t.Error("the mode file is still there")
		}
		if w.read(init) != "# mine now\n" {
			t.Error("the edited init file was touched")
		}
	})
}

// TestUsageOfEmptyValues: a value that is empty is a value that is missing,
// and the complaint says so; it does not print a sentence with a hole where
// the value should be (R3).
func TestUsageOfEmptyValues(t *testing.T) {
	tests := []struct {
		args  []string
		names []string
	}{
		{[]string{"--lang"}, []string{"--lang", "en|pt-BR"}},
		{[]string{"--lang="}, []string{"--lang", "en|pt-BR"}},
		{[]string{"version", "--lang", ""}, []string{"--lang", "en|pt-BR"}},
		{[]string{"apply", "--hook", "bash,,zsh"}, []string{"--hook", "LIST"}},
		{[]string{"apply", "--hook", "bash,"}, []string{"--hook", "LIST"}},
		{[]string{"apply", "--hook", ""}, []string{"--hook", "LIST"}},
		{[]string{"apply", "--hook="}, []string{"--hook", "LIST"}},
		{[]string{"apply", "--shell="}, []string{"--shell", "auto|powershell|pwsh"}},
		{[]string{"theme", "--paint", ""}, []string{"--paint", "VARIANT"}},
		{[]string{"export", "--to="}, []string{"--to", "DIR"}},
	}
	bothLanguages(t, func(t *testing.T, lang string) {
		needs := map[string]string{"en": " needs a value: ", "pt-BR": " precisa de um valor: "}[lang]
		w := newWorld(t, platform.Linux, "")
		w.vars["SERENO_LANG"] = lang
		for _, test := range tests {
			got := w.must(engine.ExitUsage, test.args...)
			first := strings.SplitN(got.err, "\n", 2)[0]
			if !strings.Contains(first, needs) || strings.Contains(first, "  ") {
				t.Errorf("sereno %q: %q", test.args, first)
			}
			for _, name := range test.names {
				if !strings.Contains(first, name) {
					t.Errorf("sereno %q: the complaint does not name %s: %q", test.args, name, first)
				}
			}
			if got.out != "" || strings.Count(got.err, "\n") != 3 {
				t.Errorf("sereno %q: want three lines on standard error and nothing else:\n%s%s", test.args, got.out, got.err)
			}
		}
	})
}

// TestFailuresSayItOnce: a failure names its path once, in the words of the
// system, and never the temporary file sereno writes through; and one cause
// of a refusal is printed once, however many files it stands in the way of.
func TestFailuresSayItOnce(t *testing.T) {
	// SERENO_HOME is a file: every path below it has the same thing in the way.
	w := newWorld(t, platform.Linux, "")
	blocker := filepath.Join(w.home.Dir, "not-a-folder")
	w.write(blocker, "a file\n")
	w.vars["SERENO_HOME"] = filepath.Join(blocker, "sereno")
	got := w.must(engine.ExitRefused, "apply")
	if n := strings.Count(got.err, blocker+" is in the way and is not a directory"); n != 1 {
		t.Errorf("the one cause is printed %d times:\n%s", n, got.err)
	}
	if n := strings.Count(got.err, "Move it away or delete it"); n != 1 {
		t.Errorf("the way out is printed %d times:\n%s", n, got.err)
	}

	// A write that is stopped: the message names the path once. (The words
	// of a write that fails in the system are tested with the engine.)
	w = newWorld(t, platform.Linux, "")
	target := filepath.Join(testenv.TempDir(t), "themes")
	got = w.must(engine.ExitFailure, "export", "--to", target)
	if first := strings.SplitN(got.err, "\n", 2)[0]; strings.Count(first, target) != 1 || strings.Contains(got.err, "sereno-tmp") {
		t.Errorf("the message of a write that was stopped:\n%s", got.err)
	}
}

// TestSandboxMessageWhenTheCheckFails: a junction on the way to a file makes
// the sandbox check itself fail. The message says that, not that the file is
// outside the sandbox, which nobody knows (R10).
func TestSandboxMessageWhenTheCheckFails(t *testing.T) {
	w := newWorld(t, platform.Windows, "")
	elsewhere := filepath.Join(w.home.Dir, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(w.home.LocalAppData), 0o755); err != nil {
		t.Fatal(err)
	}
	testenv.Junction(t, elsewhere, w.home.LocalAppData)
	t.Cleanup(func() { os.Remove(w.home.LocalAppData) })
	bothLanguages(t, func(t *testing.T, lang string) {
		got := w.must(engine.ExitFailure, "apply", "--lang", lang)
		outside := map[string]string{"en": "it is outside", "pt-BR": "fica fora de"}[lang]
		unchecked := map[string]string{"en": "could not be checked", "pt-BR": "não foi possível conferir"}[lang]
		if strings.Contains(got.err, outside) || !strings.Contains(got.err, unchecked) || !strings.Contains(got.err, "junction") {
			t.Errorf("the message of a check that failed:\n%s", got.err)
		}
		noMissingKey(t, got.err)
	})
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Errorf("something was written through the junction: %v", entries)
	}
}

// TestWording pins sentences the review found awkward, ambiguous or not
// exact, in both languages (R5, R37).
func TestWording(t *testing.T) {
	english, portuguese := i18n.For(i18n.English), i18n.For(i18n.Portuguese)

	// status: the session line is two facts, and reads as two.
	w := newWorld(t, platform.Windows, "wt-present")
	w.must(0, "apply")
	if got := w.must(0, "status"); !regexp.MustCompile(`(?m)^session +other terminal \(not Windows Terminal, not VS Code\); started from the Serena profile: no$`).MatchString(got.out) {
		t.Errorf("the session line in English:\n%s", got.out)
	}
	if got := w.must(0, "status", "--lang", "pt-BR"); !regexp.MustCompile(`(?m)^sessão +outro terminal \(nem Windows Terminal nem VS Code\); aberta pelo perfil Serena: não$`).MatchString(got.out) {
		t.Errorf("the session line in pt-BR:\n%s", got.out)
	}
	w.vars["WT_SESSION"], w.vars["SERENO_SESSION"] = "1", "wt-profile"
	if got := w.must(0, "status"); !regexp.MustCompile(`(?m)^session +Windows Terminal; started from the Serena profile: yes$`).MatchString(got.out) {
		t.Errorf("the session line in Windows Terminal:\n%s", got.out)
	}

	// The notice of R19 in pt-BR: whose settings.json, and who sees what.
	if stub := portuguese.T("wt.notice.stub"); !strings.Contains(stub, "ao settings.json dele mesmo") || strings.Contains(stub, "ao seu próprio") {
		t.Errorf("wt.notice.stub in pt-BR: %s", stub)
	}
	// The English wording of R19 is fixed there, word for word.
	if stub := english.T("wt.notice.stub"); stub != "Windows Terminal adds a short entry for the Serena profile to its own settings.json the first time it sees it, as it does for every fragment profile. sereno does not write that file. Use --no-profile to install only the colour schemes." {
		t.Errorf("wt.notice.stub in English is not the text of R19: %s", stub)
	}

	// Numbers are written as every output prints them: 4.5, 7.0.
	help := w.must(0, "help", "check", "--lang", "pt-BR")
	if !strings.Contains(help.out, "4.5") || !strings.Contains(help.out, "7.0") || strings.Contains(help.out, "4,5") || strings.Contains(help.out, "7,0") {
		t.Errorf("the help of check in pt-BR writes the floors another way than check prints them:\n%s", help.out)
	}

	// theme --show names what is measured, not a name from the code.
	show := w.must(0, "theme", "--show")
	if strings.Contains(show.out, "blackOrWhite") || !strings.Contains(show.out, "  black or white text on selectionBackground: ") {
		t.Errorf("theme --show:\n%s", show.out)
	}
	if show := w.must(0, "theme", "--show", "--lang", "pt-BR"); strings.Contains(show.out, "blackOrWhite") || !strings.Contains(show.out, "  texto preto ou branco sobre selectionBackground: ") {
		t.Errorf("theme --show in pt-BR:\n%s", show.out)
	}

	// The sentence after a change of theme holds for every Windows.
	if changed := english.T("theme.changed"); strings.Contains(changed, "Windows Terminal") || !strings.Contains(changed, "follow the system") {
		t.Errorf("theme.changed: %s", changed)
	}

	// check counts what failed without "(s)".
	for _, catalog := range []*i18n.Catalog{english, portuguese} {
		if text := catalog.T("check.some-fail", 2); strings.Contains(text, "(") || !strings.HasSuffix(text, ": 2.") {
			t.Errorf("check.some-fail: %s", text)
		}
	}
}

// TestManifestOfANewerVersion: a manifest whose format number is above the
// one this version reads was written by a newer sereno. The advice is to use
// that version; "restore the file" or "delete the folder" would be wrong.
func TestManifestOfANewerVersion(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		catalog := i18n.For(i18n.Lang(lang))
		w := newWorld(t, platform.Windows, "wt-present")
		w.must(0, "apply")
		w.write(w.manifest(), strings.Replace(w.read(w.manifest()), `"schema": 1`, `"schema": 2`, 1))
		tree := w.tree()
		for _, args := range [][]string{{"apply"}, {"status"}, {"theme", "dark"}, {"uninstall"}} {
			got := w.must(engine.ExitFailure, append(args, "--lang", lang)...)
			if !strings.Contains(got.err, catalog.T("error.manifest.newer")) || strings.Contains(got.err, catalog.T("error.manifest.next")) || got.out != "" {
				t.Errorf("sereno %v with a manifest of schema 2:\n%s%s", args, got.out, got.err)
			}
		}
		sameTree(t, "a manifest of a newer version", tree, w.tree())

		// A manifest that is broken keeps the other advice.
		w.write(w.manifest(), `{"schema": 0}`)
		if got := w.must(engine.ExitFailure, "status", "--lang", lang); !strings.Contains(got.err, catalog.T("error.manifest.next")) {
			t.Errorf("a broken manifest:\n%s", got.err)
		}
	})
}

// TestForeignFileInThePlaceOfTheLock: the lock of a command that writes is
// the file `lock` in sereno's folder. Something of the user's under that name
// is in the way of every such command, and is reported like any foreign file
// (R15): exit code 3 before anything is written, with the path and the way
// out, also by uninstall. It is never locked, emptied or deleted.
func TestForeignFileInThePlaceOfTheLock(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		catalog := i18n.For(i18n.Lang(lang))
		w := newWorld(t, platform.Windows, "wt-present")
		w.must(0, "apply")
		lock := filepath.Join(w.root(), engine.LockName)
		w.write(lock, "notes of mine\n")
		tree := w.tree()
		for _, args := range [][]string{{"apply", "--keys", "always"}, {"apply", "--dry-run"}, {"theme", "light"}, {"export"}, {"uninstall"}, {"uninstall", "--dry-run"}} {
			got := w.must(engine.ExitRefused, append(args, "--lang", lang)...)
			if got.out != "" || !strings.Contains(got.err, "  "+lock+": "+catalog.T(engine.ReasonForeign)+"\n") || !strings.Contains(got.err, catalog.T("refusal.way-out.move")) {
				t.Errorf("sereno %v with a foreign file in the place of the lock:\nstdout: %q\nstderr: %q", args, got.out, got.err)
			}
		}
		sameTree(t, "commands that were refused", tree, w.tree())
		// Reading goes on, and the way out works.
		w.must(0, "status", "--lang", lang)
		if err := os.Remove(lock); err != nil {
			t.Fatal(err)
		}
		w.must(0, "theme", "light", "--lang", lang)
		w.must(0, "uninstall", "--lang", lang)
	})
}

// TestCommandsMakeWay: what a command prints when another sereno is at work
// on the folder. Here the other one has finished between the moment this one
// read the manifest and the moment it would have written; the tests in
// test/e2e run commands side by side.
func TestCommandsMakeWay(t *testing.T) {
	bothLanguages(t, func(t *testing.T, lang string) {
		busy := i18n.For(i18n.Lang(lang)).T("error.busy")
		w := newWorld(t, platform.Windows, "wt-present")
		w.must(0, "apply")
		// The lock of the folder, held as a running sereno holds it.
		release, err := engine.Lock(w.root())
		if err != nil {
			t.Fatal(err)
		}
		tree := w.tree()
		for _, args := range [][]string{{"apply", "--keys", "always"}, {"apply", "--dry-run"}, {"theme", "light"}, {"export"}, {"uninstall"}, {"uninstall", "--dry-run"}} {
			got := w.must(engine.ExitFailure, append(args, "--lang", lang)...)
			if got.err != "sereno: "+busy+"\n" || got.out != "" {
				t.Errorf("sereno %v while another sereno holds the lock:\nstdout: %q\nstderr: %q", args, got.out, got.err)
			}
		}
		// Commands that only read go on.
		for _, args := range [][]string{{"status"}, {"theme"}, {"check"}, {"version"}, {"help"}} {
			w.must(0, append(args, "--lang", lang)...)
		}
		sameTree(t, "commands that made way", tree, w.tree())
		release()
		w.must(0, "apply", "--keys", "always", "--lang", lang)
		w.must(0, "uninstall", "--lang", lang)
		if _, err := os.Stat(w.root()); !os.IsNotExist(err) {
			t.Errorf("uninstall left %s (%v)", w.root(), err)
		}
	})
}
