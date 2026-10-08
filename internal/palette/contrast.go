package palette

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// CursorFloor is the minimum contrast of the cursor against the background
// (R9 d). It is the same for every kind.
const CursorFloor = 3.0

// Floor returns the minimum contrast ratio for text of a variant of kind k:
// 4.5 for dark and light, 7.0 for the high-contrast kinds (R9).
func Floor(k Kind) float64 {
	if k == KindHC || k == KindHCLight {
		return 7.0
	}
	return 4.5
}

// Luminance returns the WCAG 2 relative luminance of a colour, from 0 (black)
// to 1 (white).
func Luminance(c Color) float64 {
	r, g, b := c.RGB()
	// The conversions keep the three products from being fused with the
	// additions, so every platform computes the same value.
	return float64(0.2126*linear(r)) + float64(0.7152*linear(g)) + float64(0.0722*linear(b))
}

// linear undoes the sRGB transfer function for one 8-bit channel, with the
// threshold WCAG 2 uses.
func linear(channel uint8) float64 {
	s := float64(channel) / 255
	if s <= 0.03928 {
		return s / 12.92
	}
	return math.Pow((s+0.055)/1.055, 2.4)
}

// Ratio returns the WCAG 2 contrast ratio of two colours, from 1 to 21. The
// order of the arguments does not matter.
func Ratio(a, b Color) float64 { return contrast(Luminance(a), Luminance(b)) }

// contrast is the contrast ratio of two relative luminances.
func contrast(a, b float64) float64 {
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}

// Round rounds a ratio to the two decimals reports show. Whether a pair passes
// is always decided on the unrounded ratio, so a value of 4.499 is reported as
// 4.50 and still fails a floor of 4.5.
func Round(ratio float64) float64 { return math.Round(ratio*100) / 100 }

// Relative luminance of black and of white, the two colours Windows Terminal
// 1.24 draws selected text in. They belong to rule R9 (e), not to the palette,
// and are written as luminances so that no hex value appears outside
// serena.json.
const (
	blackLuminance = 0.0
	whiteLuminance = 1.0
)

// The rules of R9 that produce measurements.
const (
	RuleSlots      = "a" // slots 1-15 against the background
	RuleSlotZero   = "b" // slot 0 against the background (light backgrounds only)
	RuleForeground = "c" // foreground against the background
	RuleCursor     = "d" // cursor against the background
	RuleSelection  = "e" // text against the selection background
)

// SubjectBlackOrWhite is the subject of the one measurement that is not about
// a colour of the palette: the better of black and white on the selection
// background (R9 e).
const SubjectBlackOrWhite = "blackOrWhite"

// Measurement is one contrast pair of the gate.
type Measurement struct {
	Rule    string  `json:"rule"`    // one of the Rule constants
	Subject string  `json:"subject"` // ansi[N], foreground, cursor, selectionForeground or blackOrWhite
	Against string  `json:"against"` // background or selectionBackground
	Ratio   float64 `json:"ratio"`   // rounded to two decimals (see Round)
	Floor   float64 `json:"floor"`
	Exempt  bool    `json:"exempt"` // measured for the record, not held to the floor
	Pass    bool    `json:"pass"`
}

// VariantReport is the result of the gate for one variant.
type VariantReport struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Kind         Kind          `json:"kind"`
	Floor        float64       `json:"floor"`
	Minimum      float64       `json:"minimum"`     // lowest ratio of rule (a), rounded to two decimals
	MinimumSlot  int           `json:"minimumSlot"` // the slot that has it
	OK           bool          `json:"ok"`
	Failures     []Measurement `json:"failures"`
	Measurements []Measurement `json:"measurements"`
}

// RoleReport is the result of rule (f) for one role.
type RoleReport struct {
	Role    Role   `json:"role"`
	SGR     string `json:"sgr"`
	Pass    bool   `json:"pass"`
	Problem string `json:"problem,omitempty"`
}

// Report is the machine-readable result of `sereno check`.
type Report struct {
	OK       bool            `json:"ok"`
	Source   Source          `json:"source"`
	Variants []VariantReport `json:"variants"`
	Roles    []RoleReport    `json:"roles"`
}

// Check runs the contrast gate of R9 on a palette and a role table.
func Check(p *Palette, roles *Roles) Report {
	report := Report{OK: true, Source: p.Source}
	for _, v := range p.Variants {
		vr := checkVariant(v)
		report.OK = report.OK && vr.OK
		report.Variants = append(report.Variants, vr)
	}
	for _, role := range roleOrder {
		rr := RoleReport{Role: role, SGR: roles.SGR(role), Pass: true}
		if err := CheckSGR(rr.SGR); err != nil {
			rr.Pass, rr.Problem = false, err.Error()
			report.OK = false
		}
		report.Roles = append(report.Roles, rr)
	}
	return report
}

func checkVariant(v Variant) VariantReport {
	floor := Floor(v.Kind)
	vr := VariantReport{
		ID: v.ID, Name: v.Name, Kind: v.Kind, Floor: floor,
		OK: true, Failures: []Measurement{},
	}
	add := func(rule, subject, against string, ratio, floor float64, exempt bool) {
		m := Measurement{
			Rule: rule, Subject: subject, Against: against,
			Ratio: Round(ratio), Floor: floor, Exempt: exempt,
			Pass: exempt || ratio >= floor,
		}
		vr.Measurements = append(vr.Measurements, m)
		if !m.Pass {
			vr.Failures = append(vr.Failures, m)
			vr.OK = false
		}
	}

	// (b) Slot 0 is a surface colour on dark backgrounds and exempt there.
	add(RuleSlotZero, "ansi[0]", "background", Ratio(v.ANSI[0], v.Background), floor, !v.Kind.LightBackground())
	// (a) Slots 1-15.
	lowest := math.Inf(1)
	for n := 1; n < len(v.ANSI); n++ {
		ratio := Ratio(v.ANSI[n], v.Background)
		if ratio < lowest {
			lowest, vr.MinimumSlot = ratio, n
		}
		add(RuleSlots, fmt.Sprintf("ansi[%d]", n), "background", ratio, floor, false)
	}
	vr.Minimum = Round(lowest)
	// (c) and (d).
	add(RuleForeground, "foreground", "background", Ratio(v.Foreground, v.Background), floor, false)
	add(RuleCursor, "cursor", "background", Ratio(v.Cursor, v.Background), CursorFloor, false)
	// (e) Emulators that keep the text colour use selectionForeground;
	// Windows Terminal 1.24 draws selected text in black or white.
	add(RuleSelection, "selectionForeground", "selectionBackground", Ratio(v.SelectionForeground, v.SelectionBackground), floor, false)
	selection := Luminance(v.SelectionBackground)
	bw := math.Max(contrast(blackLuminance, selection), contrast(whiteLuminance, selection))
	add(RuleSelection, SubjectBlackOrWhite, "selectionBackground", bw, floor, false)
	return vr
}

// JSON returns the report as indented JSON with a final newline.
func (r Report) JSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Markdown returns the document committed as docs/contrast.md. It is English
// only (file contents are not translated, R5) and depends on nothing but the
// palette and the role table, so it can be compared byte for byte in CI.
func (r Report) Markdown() string {
	var b strings.Builder
	commit := r.Source.Commit
	if len(commit) > 7 {
		commit = commit[:7]
	}
	b.WriteString("# Contrast\n\n")
	b.WriteString("<!-- Generated by `sereno check --markdown`. Do not edit: change the palette in Serena Theme, import it, and generate this file again. -->\n\n")
	fmt.Fprintf(&b, "Contrast ratios (WCAG 2 relative luminance) of the Serena palette, imported from %s at %s (commit %s).\n", r.Source.Repo, r.Source.Tag, commit)
	b.WriteString("Ratios are rounded to two decimals; whether a pair passes is decided on the exact ratio.\n\n")

	header := func(first ...string) {
		cells := append([]string(nil), first...)
		rule := make([]string, 0, len(first)+len(r.Variants))
		for range first {
			rule = append(rule, ":---")
		}
		for _, v := range r.Variants {
			cells = append(cells, v.Name)
			rule = append(rule, "---:")
		}
		writeRow(&b, cells)
		writeRow(&b, rule)
	}
	// row writes one table line with the measurement of every variant that
	// matches subject.
	row := func(subject string, first ...string) {
		cells := append([]string(nil), first...)
		for _, v := range r.Variants {
			cells = append(cells, cell(v, subject))
		}
		writeRow(&b, cells)
	}

	b.WriteString("## Slots against the background\n\n")
	b.WriteString("Floor: 4.5 for the dark and light variants, 7.0 for the high-contrast ones.\n")
	b.WriteString("Slot 0 is a surface colour on dark backgrounds and is exempt there.\n\n")
	header("Slot", "Name")
	for n := 0; n < 16; n++ {
		row(fmt.Sprintf("ansi[%d]", n), fmt.Sprint(n), SlotName(n))
	}
	minimum, floor := []string{"", "minimum of slots 1-15"}, []string{"", "floor"}
	for _, v := range r.Variants {
		minimum = append(minimum, ratioText(v.Minimum))
		floor = append(floor, floorText(v.Floor))
	}
	writeRow(&b, minimum)
	writeRow(&b, floor)

	b.WriteString("\n## Other pairs\n\n")
	fmt.Fprintf(&b, "The cursor needs %s; the other pairs need the floor of their variant.\n", floorText(CursorFloor))
	b.WriteString("Windows Terminal draws selected text in black or white, whichever contrasts more; other emulators use the selection foreground.\n\n")
	header("Pair")
	row("foreground", "foreground on background")
	row("cursor", "cursor on background")
	row("selectionForeground", "selection foreground on selection background")
	row(SubjectBlackOrWhite, "black or white on selection background")

	b.WriteString("\n## Roles\n\n")
	b.WriteString("Everything above the emulator uses SGR parameters for the sixteen slots, never hex values and never a painted background.\n\n")
	writeRow(&b, []string{"Role", "SGR parameters", "Allowed"})
	writeRow(&b, []string{":---", ":---", ":---"})
	for _, role := range r.Roles {
		allowed := "yes"
		if !role.Pass {
			allowed = "**no**"
		}
		writeRow(&b, []string{string(role.Role), "`" + role.SGR + "`", allowed})
	}

	b.WriteString("\n## Result\n\n")
	if failures := r.failureCount(); failures == 0 {
		b.WriteString("Every value is at or above its floor.\n")
	} else {
		fmt.Fprintf(&b, "**%d value(s) below the floor or not allowed.**\n", failures)
	}
	return b.String()
}

func (r Report) failureCount() int {
	count := 0
	for _, v := range r.Variants {
		count += len(v.Failures)
	}
	for _, role := range r.Roles {
		if !role.Pass {
			count++
		}
	}
	return count
}

// cell renders the measurement of one variant for one subject.
func cell(v VariantReport, subject string) string {
	for _, m := range v.Measurements {
		if m.Subject != subject {
			continue
		}
		switch {
		case m.Exempt:
			return ratioText(m.Ratio) + " (exempt)"
		case !m.Pass:
			return "**" + ratioText(m.Ratio) + " (fails)**"
		default:
			return ratioText(m.Ratio)
		}
	}
	return ""
}

func ratioText(ratio float64) string { return fmt.Sprintf("%.2f", ratio) }

func floorText(floor float64) string { return fmt.Sprintf("%.1f", floor) }

func writeRow(b *strings.Builder, cells []string) {
	b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
}
