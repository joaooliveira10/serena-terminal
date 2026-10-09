package i18n

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestKeyParity is the test R5 asks for: it fails on any key that is missing
// from, or extra in, either catalogue.
func TestKeyParity(t *testing.T) {
	for key := range english {
		if _, ok := portuguese[key]; !ok {
			t.Errorf("key %q is missing from the pt-BR catalogue", key)
		}
	}
	for key := range portuguese {
		if _, ok := english[key]; !ok {
			t.Errorf("key %q is in the pt-BR catalogue but not in the English one", key)
		}
	}
	if len(english) == 0 {
		t.Error("the catalogues are empty")
	}
}

// verb matches a fmt verb with an optional explicit argument index.
var verb = regexp.MustCompile(`%(\[(\d+)\])?[-+# 0]*\d*(\.\d+)?([a-zA-Z%])`)

// arguments returns the arguments a format consumes as "index:verb", sorted,
// so that two texts may order them differently but must agree on what they
// take.
func arguments(t *testing.T, key, format string) []string {
	t.Helper()
	var args []string
	next := 1
	for _, match := range verb.FindAllStringSubmatch(format, -1) {
		if match[4] == "%" {
			continue
		}
		index := next
		if match[2] != "" {
			index = int(match[2][0] - '0')
			if len(match[2]) != 1 {
				t.Fatalf("%s: argument index %q is out of the supported range", key, match[2])
			}
		}
		next = index + 1
		args = append(args, string(rune('0'+index))+":"+match[4])
	}
	sort.Strings(args)
	// The same argument may be used twice (%[1]s ... %[1]s).
	unique := args[:0]
	for i, arg := range args {
		if i == 0 || arg != args[i-1] {
			unique = append(unique, arg)
		}
	}
	return unique
}

// TestSameArguments keeps a translation from asking for other arguments than
// the English text, which fmt would only reveal at run time.
func TestSameArguments(t *testing.T) {
	for key, en := range english {
		pt, ok := portuguese[key]
		if !ok {
			continue
		}
		a, b := arguments(t, key, en), arguments(t, key, pt)
		if strings.Join(a, " ") != strings.Join(b, " ") {
			t.Errorf("%s: English takes %v, pt-BR takes %v", key, a, b)
		}
		for i, arg := range a {
			if int(arg[0]-'0') != i+1 {
				t.Errorf("%s: arguments %v are not numbered 1..n without gaps", key, a)
				break
			}
		}
	}
}

func TestMessagesAreWellFormed(t *testing.T) {
	keyForm := regexp.MustCompile(`^[a-z0-9]+([.-][a-z0-9]+)*$`)
	for lang, messages := range catalogues {
		for key, text := range messages {
			if !keyForm.MatchString(key) {
				t.Errorf("%s: key %q is not lower-case words joined by dots and hyphens", lang, key)
			}
			if text == "" || text != strings.TrimSpace(text) || strings.ContainsAny(text, "\r\n\t") {
				t.Errorf("%s: %s is empty, padded or spans lines: %q", lang, key, text)
			}
			if strings.Contains(text, "%!") {
				t.Errorf("%s: %s contains a formatting error", lang, key)
			}
		}
	}
}

// TestFormattingEveryMessage formats each message with as many strings as it
// takes and looks for fmt's error markers.
func TestFormattingEveryMessage(t *testing.T) {
	for _, lang := range []Lang{English, Portuguese} {
		catalog := For(lang)
		for _, key := range Keys() {
			count := len(arguments(t, key, english[key]))
			args := make([]any, count)
			for i := range args {
				args[i] = "x"
			}
			if got := catalog.T(key, args...); strings.Contains(got, "%!") || strings.Contains(got, "!"+key+"!") {
				t.Errorf("%s: %s formats badly: %q", lang, key, got)
			}
		}
	}
}

// TestSentencesFixedByTheSpecification pins the texts R13 and R19 give word
// for word.
func TestSentencesFixedByTheSpecification(t *testing.T) {
	en, pt := For(English), For(Portuguese)
	fixed := []struct{ got, want string }{
		{en.T("plan.nothing-changed"), "Nothing was changed."},
		{pt.T("plan.nothing-changed"), "Nada foi alterado."},
		{en.T("wt.notice.stub"), "Windows Terminal adds a short entry for the Serena profile to its own settings.json the first time it sees it, as it does for every fragment profile. sereno does not write that file. Use --no-profile to install only the colour schemes."},
		{en.T("wt.notice.open"), "Close every Windows Terminal window, open Windows Terminal again and choose Serena in the new-tab menu. One more window of a Windows Terminal that is already running is not expected to show what this apply changed."},
	}
	for _, f := range fixed {
		if f.got != f.want {
			t.Errorf("got  %q\nwant %q", f.got, f.want)
		}
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want Lang
		ok   bool
	}{
		{"en", English, true}, {"EN", English, true},
		{"pt-BR", Portuguese, true}, {"pt-br", Portuguese, true}, {"PT-BR", Portuguese, true},
		{"", "", false}, {"pt", "", false}, {"pt_BR", "", false}, {"en-US", "", false}, {"fr", "", false},
	}
	for _, test := range tests {
		if got, ok := Parse(test.in); got != test.want || ok != test.ok {
			t.Errorf("Parse(%q) = %q, %v; want %q, %v", test.in, got, ok, test.want, test.ok)
		}
	}
}

// TestSelect walks the order of R5: flag, SERENO_LANG, system, English.
func TestSelect(t *testing.T) {
	tests := []struct {
		name              string
		flag, env, system string
		want              Lang
	}{
		{"nothing known", "", "", "", English},
		{"system only", "", "", "pt-BR", Portuguese},
		{"environment beats system", "", "en", "pt-BR", English},
		{"flag beats environment", "pt-BR", "en", "", Portuguese},
		{"flag beats everything", "en", "pt-BR", "pt-BR", English},
		{"unknown environment value is skipped", "", "klingon", "pt-BR", Portuguese},
		{"unknown system value means English", "", "", "de-DE", English},
		{"environment in another case", "", "PT-br", "", Portuguese},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Select(test.flag, test.env, test.system); got != test.want {
				t.Errorf("Select(%q, %q, %q) = %q, want %q", test.flag, test.env, test.system, got, test.want)
			}
		})
	}
}

func TestCatalog(t *testing.T) {
	pt := For(Portuguese)
	if pt.Lang() != Portuguese {
		t.Errorf("Lang = %q", pt.Lang())
	}
	if got := pt.T("refusal.shell-absent", "pwsh"); got != "pwsh não está no PATH" {
		t.Errorf("T with an argument = %q", got)
	}
	if got := For(English).T("refusal.hook-moved", "bash", "/home/u/.bashrc"); got != "the bash hook line is recorded in /home/u/.bashrc; run `sereno uninstall --hook bash` first" {
		t.Errorf("T with a repeated argument = %q", got)
	}
	if got := pt.T("no.such.key"); got != "!no.such.key!" {
		t.Errorf("a missing key gives %q", got)
	}
	if fallback := For("tlh"); fallback.Lang() != English || fallback.T("plan.nothing-changed") != "Nothing was changed." {
		t.Error("an unknown language does not fall back to English")
	}
	if !Has("plan.note.kept") || Has("no.such.key") {
		t.Error("Has is wrong")
	}
	keys := Keys()
	if len(keys) != len(english) || !sort.StringsAreSorted(keys) {
		t.Error("Keys is incomplete or unsorted")
	}
}
