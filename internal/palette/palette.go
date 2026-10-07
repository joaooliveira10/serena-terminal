// Package palette holds the Serena colours and what is derived from them: the
// six variants (R7), the role table (R8), the in-band byte strings (R29) and
// the contrast gate (R9).
//
// serena.json is the only file of the repository that contains hex values. It
// is generated from Serena Theme by internal/tools/importpalette and embedded
// here; everything above the terminal emulator uses palette slots instead.
package palette

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed serena.json
var serenaJSON []byte

// Kind is the appearance class of a variant. It selects the contrast floor.
type Kind string

// The four kinds of R7.
const (
	KindDark    Kind = "dark"
	KindLight   Kind = "light"
	KindHC      Kind = "hc"
	KindHCLight Kind = "hc-light"
)

// LightBackground reports whether variants of this kind have a light
// background.
func (k Kind) LightBackground() bool { return k == KindLight || k == KindHCLight }

// Identity is one row of the variant table of R7.
type Identity struct {
	ID   string // used in commands and file names
	Name string // the scheme name, used verbatim in every emulator file
	Kind Kind
}

// identities is the table of R7, in its order.
var identities = []Identity{
	{"dark", "Serena Dark", KindDark},
	{"dark-vivid", "Serena Dark Vivid", KindDark},
	{"light", "Serena Light", KindLight},
	{"light-vivid", "Serena Light Vivid", KindLight},
	{"hc", "Serena High Contrast", KindHC},
	{"hc-light", "Serena High Contrast Light", KindHCLight},
}

// Identities returns the six variants of R7 in their fixed order.
func Identities() []Identity { return append([]Identity(nil), identities...) }

// Color is a colour written as "#rrggbb" with lower-case hex digits.
type Color string

// ParseColor accepts "#rrggbb" in either case and returns the lower-case form.
func ParseColor(s string) (Color, error) {
	c := Color(strings.ToLower(s))
	if !c.valid() {
		return "", fmt.Errorf("%q is not a colour of the form #rrggbb", s)
	}
	return c, nil
}

func (c Color) valid() bool {
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	for i := 1; i < len(c); i++ {
		if !strings.ContainsRune("0123456789abcdef", rune(c[i])) {
			return false
		}
	}
	return true
}

// RGB returns the three channels of a valid colour.
func (c Color) RGB() (r, g, b uint8) {
	return hexByte(c[1], c[2]), hexByte(c[3], c[4]), hexByte(c[5], c[6])
}

func hexByte(hi, lo byte) uint8 { return hexDigit(hi)<<4 | hexDigit(lo) }

func hexDigit(d byte) uint8 {
	if d >= 'a' {
		return d - 'a' + 10
	}
	return d - '0'
}

// XParse returns the colour in the "rgb:rr/gg/bb" form that OSC 4, 10, 11, 12
// and 17 take.
func (c Color) XParse() string {
	return "rgb:" + string(c[1:3]) + "/" + string(c[3:5]) + "/" + string(c[5:7])
}

// UnmarshalJSON accepts only the canonical lower-case form.
func (c *Color) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if !Color(s).valid() {
		return fmt.Errorf("%q is not a colour of the form #rrggbb in lower case", s)
	}
	*c = Color(s)
	return nil
}

// Slots is the 16-colour ANSI table: 0-7 black, red, green, yellow, blue,
// magenta, cyan, white and 8-15 their bright forms.
type Slots [16]Color

// UnmarshalJSON requires exactly sixteen colours; a plain array type would
// silently accept fewer or more.
func (s *Slots) UnmarshalJSON(data []byte) error {
	var list []Color
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	if len(list) != len(s) {
		return fmt.Errorf("ansi has %d colours, want %d", len(list), len(s))
	}
	copy(s[:], list)
	return nil
}

// slotNames holds the conventional names of the sixteen slots, in slot order.
var slotNames = [16]string{
	"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
	"bright black", "bright red", "bright green", "bright yellow",
	"bright blue", "bright magenta", "bright cyan", "bright white",
}

// SlotName returns the conventional name of slot n (0-15).
func SlotName(n int) string { return slotNames[n] }

// Variant is one of the six colour schemes.
type Variant struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Kind                Kind   `json:"kind"`
	Background          Color  `json:"background"`
	Foreground          Color  `json:"foreground"`
	Cursor              Color  `json:"cursor"`
	ANSI                Slots  `json:"ansi"`
	SelectionBackground Color  `json:"selectionBackground"`
	SelectionForeground Color  `json:"selectionForeground"`
}

// Source records where the colours were imported from.
type Source struct {
	Repo   string `json:"repo"`
	Tag    string `json:"tag"`
	Commit string `json:"commit"`
}

// Palette is the content of serena.json.
type Palette struct {
	Source   Source    `json:"source"`
	Variants []Variant `json:"variants"`
}

// Load returns the embedded palette.
func Load() (*Palette, error) {
	p, err := Parse(serenaJSON)
	if err != nil {
		return nil, fmt.Errorf("embedded serena.json: %w", err)
	}
	return p, nil
}

// Parse decodes and validates the content of a serena.json file.
func Parse(data []byte) (*Palette, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p Palette
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("unexpected data after the palette object")
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate checks that the palette holds exactly the six variants of R7, in
// order, with every colour present.
func (p *Palette) Validate() error {
	if p.Source.Repo == "" || p.Source.Tag == "" || p.Source.Commit == "" {
		return fmt.Errorf("source needs repo, tag and commit")
	}
	if len(p.Variants) != len(identities) {
		return fmt.Errorf("%d variants, want %d", len(p.Variants), len(identities))
	}
	for i, want := range identities {
		v := p.Variants[i]
		if v.ID != want.ID || v.Name != want.Name || v.Kind != want.Kind {
			return fmt.Errorf("variant %d is %q (%q, %s), want %q (%q, %s)",
				i, v.ID, v.Name, v.Kind, want.ID, want.Name, want.Kind)
		}
		colors := append([]Color{v.Background, v.Foreground, v.Cursor, v.SelectionBackground, v.SelectionForeground}, v.ANSI[:]...)
		for _, c := range colors {
			if !c.valid() {
				return fmt.Errorf("variant %q has a missing or malformed colour %q", v.ID, c)
			}
		}
	}
	return nil
}

// Encode returns the canonical bytes of serena.json: two-space indent, fixed
// key order, LF, final newline. Encoding a parsed file reproduces it byte for
// byte.
func (p *Palette) Encode() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Variant returns the variant with the given id.
func (p *Palette) Variant(id string) (Variant, bool) {
	for _, v := range p.Variants {
		if v.ID == id {
			return v, true
		}
	}
	return Variant{}, false
}

// Control bytes of R1.
const (
	esc = "\x1b"
	st  = esc + `\`
)

// osc is the one function that builds the in-band byte strings of R29: each
// command becomes ESC ] command ST, and nothing else is emitted.
func osc(commands []string) string {
	var b strings.Builder
	for _, command := range commands {
		b.WriteString(esc + "]" + command + st)
	}
	return b.String()
}

// Sequence returns PALETTE for the variant (R29): twenty OSC sequences that
// set foreground (10), background (11), cursor (12), selection background
// (17) and the sixteen slots (4), each ending in ST. The result is raw bytes
// meant for the terminal.
func (v Variant) Sequence() string {
	commands := []string{
		"10;" + v.Foreground.XParse(),
		"11;" + v.Background.XParse(),
		"12;" + v.Cursor.XParse(),
		"17;" + v.SelectionBackground.XParse(),
	}
	for n, c := range v.ANSI {
		commands = append(commands, fmt.Sprintf("4;%d;%s", n, c.XParse()))
	}
	return osc(commands)
}

// ResetSequence returns RESET (R29): the five sequences that hand the sixteen
// slots, foreground, background, cursor and selection background back to the
// emulator's own scheme.
func ResetSequence() string {
	return osc([]string{"104", "110", "111", "112", "117"})
}
