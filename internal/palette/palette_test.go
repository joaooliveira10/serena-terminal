package palette

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func load(t *testing.T) *Palette {
	t.Helper()
	p, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestLoadVariants checks the table of R7 and the anchor values of R6.
func TestLoadVariants(t *testing.T) {
	p := load(t)
	want := []struct {
		id, name   string
		kind       Kind
		background Color
		foreground Color
	}{
		{"dark", "Serena Dark", KindDark, "#16171d", "#d4d4dc"},
		{"dark-vivid", "Serena Dark Vivid", KindDark, "#13141b", "#d4d4dc"},
		{"light", "Serena Light", KindLight, "#ecedf3", "#323445"},
		{"light-vivid", "Serena Light Vivid", KindLight, "#ecedf3", "#323445"},
		{"hc", "Serena High Contrast", KindHC, "#0b0c12", "#f2f2f8"},
		{"hc-light", "Serena High Contrast Light", KindHCLight, "#ffffff", "#0b0c12"},
	}
	if len(p.Variants) != len(want) {
		t.Fatalf("%d variants, want %d", len(p.Variants), len(want))
	}
	for i, w := range want {
		v := p.Variants[i]
		if v.ID != w.id || v.Name != w.name || v.Kind != w.kind {
			t.Errorf("variant %d = %q %q %q, want %q %q %q", i, v.ID, v.Name, v.Kind, w.id, w.name, w.kind)
		}
		if v.Background != w.background || v.Foreground != w.foreground {
			t.Errorf("%s: background/foreground = %s/%s, want %s/%s", v.ID, v.Background, v.Foreground, w.background, w.foreground)
		}
		got, ok := p.Variant(w.id)
		if !ok || got != v {
			t.Errorf("Variant(%q) does not return variant %d", w.id, i)
		}
	}
	if _, ok := p.Variant("solarized"); ok {
		t.Error("Variant found an id that does not exist")
	}
}

func TestSource(t *testing.T) {
	p := load(t)
	if p.Source.Repo != "github.com/joaooliveira10/serena-theme" || p.Source.Tag != "v1.1.0" {
		t.Errorf("source = %+v", p.Source)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(p.Source.Commit) {
		t.Errorf("commit %q is not a full hash", p.Source.Commit)
	}
}

// TestSelectionRules checks the two fallbacks of R6 on the imported data: the
// regular variants take the selection foreground from the foreground, the
// high-contrast ones carry their own.
func TestSelectionRules(t *testing.T) {
	p := load(t)
	for _, v := range p.Variants {
		own := v.Kind == KindHC || v.Kind == KindHCLight
		if !own && v.SelectionForeground != v.Foreground {
			t.Errorf("%s: selectionForeground = %s, want the foreground %s", v.ID, v.SelectionForeground, v.Foreground)
		}
	}
	hc, _ := p.Variant("hc")
	if hc.SelectionBackground != "#cbb8ff" || hc.SelectionForeground != "#0b0c12" {
		t.Errorf("hc selection = %s on %s", hc.SelectionForeground, hc.SelectionBackground)
	}
}

// TestEmbeddedFileIsCanonical proves that serena.json is exactly what Encode
// produces, which is what makes a second import byte-identical.
func TestEmbeddedFileIsCanonical(t *testing.T) {
	got, err := load(t).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, serenaJSON) {
		t.Error("serena.json differs from its canonical encoding; generate it with internal/tools/importpalette")
	}
	if bytes.Contains(serenaJSON, []byte("\r")) {
		t.Error("serena.json contains a carriage return")
	}
}

func TestParseRejects(t *testing.T) {
	valid := string(serenaJSON)
	tests := []struct {
		name string
		edit func(string) string
	}{
		{"upper-case hex", func(s string) string { return strings.Replace(s, "#16171d", "#16171D", 1) }},
		{"short colour", func(s string) string { return strings.Replace(s, `"#16171d"`, `"#161"`, 1) }},
		{"fifteen slots", func(s string) string { return strings.Replace(s, "        \"#262833\",\n", "", 1) }},
		{"seventeen slots", func(s string) string {
			return strings.Replace(s, "        \"#262833\",\n", "        \"#262833\",\n        \"#262833\",\n", 1)
		}},
		{"unknown field", func(s string) string { return strings.Replace(s, `"id": "dark",`, `"id": "dark", "extra": 1,`, 1) }},
		{"wrong name", func(s string) string { return strings.Replace(s, `"Serena Dark"`, `"Serena Night"`, 1) }},
		{"wrong kind", func(s string) string { return strings.Replace(s, `"kind": "hc-light"`, `"kind": "light"`, 1) }},
		{"missing colour", func(s string) string { return strings.Replace(s, "      \"cursor\": \"#c4a7e7\",\n", "", 1) }},
		{"no commit", func(s string) string {
			return regexp.MustCompile(`"commit": "[0-9a-f]+"`).ReplaceAllString(s, `"commit": ""`)
		}},
		{"trailing data", func(s string) string { return s + "{}" }},
		{"not json", func(string) string { return "palette" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			edited := test.edit(valid)
			if edited == valid {
				t.Fatal("the edit did not change the input")
			}
			if _, err := Parse([]byte(edited)); err == nil {
				t.Error("Parse accepted the file")
			}
		})
	}
}

func TestParseColor(t *testing.T) {
	c, err := ParseColor("#A1B2C3")
	if err != nil || c != "#a1b2c3" {
		t.Fatalf("ParseColor = %q, %v", c, err)
	}
	if r, g, b := c.RGB(); r != 0xa1 || g != 0xb2 || b != 0xc3 {
		t.Errorf("RGB = %x %x %x", r, g, b)
	}
	if got := c.XParse(); got != "rgb:a1/b2/c3" {
		t.Errorf("XParse = %q", got)
	}
	for _, bad := range []string{"", "a1b2c3", "#a1b2c", "#a1b2c3d4", "#g1b2c3", "#a1b2c3 "} {
		if _, err := ParseColor(bad); err == nil {
			t.Errorf("ParseColor(%q) succeeded", bad)
		}
	}
}

// goldenDir is testdata/golden/palette at the root of the repository.
const goldenDir = "../../testdata/golden/palette"

// TestSequenceGolden pins PALETTE for the six variants (R29). The golden files
// were produced from the theme files with shell tools, not with this package.
func TestSequenceGolden(t *testing.T) {
	for _, v := range load(t).Variants {
		want, err := os.ReadFile(filepath.Join(goldenDir, v.ID+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if got := v.Sequence(); got != string(want) {
			t.Errorf("%s: PALETTE differs from the golden file\n got %q\nwant %q", v.ID, got, want)
		}
	}
}

// TestSequenceShape checks the grammar of R29 without the golden files: twenty
// sequences and nothing else, in the fixed order, each ending in ST, hex in
// lower case.
func TestSequenceShape(t *testing.T) {
	const st = "\x1b\\"
	for _, v := range load(t).Variants {
		seq := v.Sequence()
		if !strings.HasSuffix(seq, st) {
			t.Fatalf("%s: PALETTE does not end in ST", v.ID)
		}
		parts := strings.Split(strings.TrimSuffix(seq, st), st)
		if len(parts) != 20 {
			t.Fatalf("%s: %d sequences, want 20", v.ID, len(parts))
		}
		colors := append([]Color{v.Foreground, v.Background, v.Cursor, v.SelectionBackground}, v.ANSI[:]...)
		prefixes := []string{"10;", "11;", "12;", "17;"}
		for n := 0; n < 16; n++ {
			prefixes = append(prefixes, "4;"+itoa(n)+";")
		}
		for i, part := range parts {
			c := colors[i]
			want := "\x1b]" + prefixes[i] + "rgb:" + string(c[1:3]) + "/" + string(c[3:5]) + "/" + string(c[5:7])
			if part != want {
				t.Errorf("%s: sequence %d = %q, want %q", v.ID, i, part, want)
			}
		}
		if seq != strings.ToLower(seq) {
			t.Errorf("%s: PALETTE contains upper-case characters", v.ID)
		}
	}
}

func itoa(n int) string {
	if n >= 10 {
		return string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	return string(rune('0' + n))
}

// TestSequenceDark spells out the first sequences of one variant by hand.
func TestSequenceDark(t *testing.T) {
	dark, _ := load(t).Variant("dark")
	want := "\x1b]10;rgb:d4/d4/dc\x1b\\" + "\x1b]11;rgb:16/17/1d\x1b\\" + "\x1b]12;rgb:c4/a7/e7\x1b\\" +
		"\x1b]17;rgb:3a/3f/5a\x1b\\" + "\x1b]4;0;rgb:26/28/33\x1b\\"
	if got := dark.Sequence(); !strings.HasPrefix(got, want) {
		t.Errorf("PALETTE of dark starts with %q, want %q", got[:len(want)], want)
	}
}

func TestResetSequence(t *testing.T) {
	want := "\x1b]104\x1b\\" + "\x1b]110\x1b\\" + "\x1b]111\x1b\\" + "\x1b]112\x1b\\" + "\x1b]117\x1b\\"
	if got := ResetSequence(); got != want {
		t.Errorf("RESET = %q, want %q", got, want)
	}
}

func TestIdentitiesIsACopy(t *testing.T) {
	list := Identities()
	list[0].ID = "changed"
	if Identities()[0].ID != "dark" {
		t.Error("Identities exposes the internal table")
	}
	for n := 0; n < 16; n++ {
		if SlotName(n) == "" {
			t.Errorf("slot %d has no name", n)
		}
	}
}
