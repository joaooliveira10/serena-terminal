package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// The two READMEs show output of the program. These tests run the program
// again, in a test home, and look for what it prints in the README of each
// language, so that an example cannot drift away from the code.

// readme reads the README of a language.
func readme(t *testing.T, lang string) (name, text string) {
	t.Helper()
	name = "README.md"
	if lang == "pt-BR" {
		name = "README.pt-BR.md"
	}
	data, err := os.ReadFile(filepath.Join(testenv.RepoRoot(t), name))
	if err != nil {
		t.Fatal(err)
	}
	return name, strings.ReplaceAll(string(data), "\r\n", "\n")
}

// windowsHome is the name the READMEs give the test home of a Windows run.
const windowsHome = "C:\\Users\\user"

// asShown turns the output of a test run into what a README shows: the
// temporary home under a neutral name, and the version of a build that is
// not a release.
func asShown(w *world, out, home string) string {
	out = strings.ReplaceAll(out, w.home.Dir, home)
	return strings.ReplaceAll(out, Program+" "+testVersion, Program+" "+DevVersion)
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
			if name, text := readme(t, lang); !strings.Contains(text, "```text\n"+plan+"```\n") {
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
			if !strings.Contains(text, "```text\n"+out+"```\n") {
				t.Errorf("%s does not show what `sereno %s` prints today:\n%s", name, strings.Join(arguments, " "), out)
			}
		}
	})
}

// TestReadmesShowRealOutputOfWindows: the examples that were taken on a
// Windows home: a second apply, the plan of an uninstall, a change of theme.
func TestReadmesShowRealOutputOfWindows(t *testing.T) {
	if runtime.GOOS != platform.Windows {
		t.Skip("the paths of a Windows home can only be spelled on Windows")
	}
	bothLanguages(t, func(t *testing.T, lang string) {
		w := newWorld(t, platform.Windows, "wt-present")
		name, text := readme(t, lang)
		shown := func(what string, out string) {
			t.Helper()
			// The path of the program is the one a README names.
			out = strings.ReplaceAll(out, filepath.Join(w.home.Dir, "bin", Program), windowsHome+"\\go\\bin\\sereno.exe")
			out = asShown(w, out, windowsHome)
			if !strings.Contains(text, "```text\n"+out+"```\n") {
				t.Errorf("%s does not show what `sereno %s` prints today:\n%s", name, what, out)
			}
		}
		w.must(engine.ExitOK, "apply", "--lang", lang)
		shown("apply (a second time)", w.must(engine.ExitOK, "apply", "--lang", lang).out)
		shown("uninstall --dry-run", w.must(engine.ExitOK, "uninstall", "--dry-run", "--lang", lang).out)
		shown("theme light --flavor vivid", w.must(engine.ExitOK, "theme", "light", "--flavor", "vivid", "--lang", lang).out)
	})
}
