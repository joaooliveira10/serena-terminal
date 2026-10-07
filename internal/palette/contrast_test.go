package palette

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadRoles(t *testing.T) *Roles {
	t.Helper()
	roles, err := LoadRoles()
	if err != nil {
		t.Fatal(err)
	}
	return roles
}

func near(a, b, tolerance float64) bool { return math.Abs(a-b) <= tolerance }

func TestLuminanceAndRatio(t *testing.T) {
	const black, white Color = "#000000", "#ffffff"
	if got := Luminance(black); got != 0 {
		t.Errorf("luminance of black = %v", got)
	}
	if got := Luminance(white); !near(got, 1, 1e-12) {
		t.Errorf("luminance of white = %v", got)
	}
	if got := Luminance("#808080"); !near(got, 0.21586, 1e-5) {
		t.Errorf("luminance of #808080 = %v, want 0.21586", got)
	}
	if got := Ratio(black, white); !near(got, 21, 1e-9) {
		t.Errorf("black on white = %v, want 21", got)
	}
	if Ratio(black, white) != Ratio(white, black) {
		t.Error("Ratio depends on the order of its arguments")
	}
	if got := Ratio("#123456", "#123456"); got != 1 {
		t.Errorf("a colour against itself = %v, want 1", got)
	}
	// The classic pair around the AA floor: #767676 passes, #777777 fails.
	if got := Ratio("#767676", white); !near(got, 4.54, 0.005) {
		t.Errorf("#767676 on white = %v, want 4.54", got)
	}
	if got := Ratio("#777777", white); !near(got, 4.48, 0.005) {
		t.Errorf("#777777 on white = %v, want 4.48", got)
	}
}

func TestRound(t *testing.T) {
	tests := []struct{ in, want float64 }{
		{4.519, 4.52}, {4.511, 4.51}, {4.4999, 4.5}, {4.5, 4.5}, {7.276, 7.28}, {21, 21},
	}
	for _, test := range tests {
		if got := Round(test.in); got != test.want {
			t.Errorf("Round(%v) = %v, want %v", test.in, got, test.want)
		}
	}
}

func TestFloors(t *testing.T) {
	for kind, want := range map[Kind]float64{KindDark: 4.5, KindLight: 4.5, KindHC: 7, KindHCLight: 7} {
		if got := Floor(kind); got != want {
			t.Errorf("Floor(%s) = %v, want %v", kind, got, want)
		}
	}
	if CursorFloor != 3 {
		t.Errorf("CursorFloor = %v", CursorFloor)
	}
}

// TestShippedPalettePassesTheGate checks R9 on the embedded palette, including
// the minimums of rule (a) the specification expects.
func TestShippedPalettePassesTheGate(t *testing.T) {
	report := Check(load(t), loadRoles(t))
	if !report.OK {
		t.Errorf("the shipped palette fails the gate")
	}
	minimums := map[string]float64{
		"dark": 4.52, "dark-vivid": 4.52, "light": 4.51, "light-vivid": 4.51, "hc": 7.82, "hc-light": 7.28,
	}
	if len(report.Variants) != len(minimums) {
		t.Fatalf("%d variants in the report", len(report.Variants))
	}
	for _, v := range report.Variants {
		if v.Minimum != minimums[v.ID] {
			t.Errorf("%s: minimum of slots 1-15 = %v, want %v", v.ID, v.Minimum, minimums[v.ID])
		}
		if v.MinimumSlot < 1 || v.MinimumSlot > 15 {
			t.Errorf("%s: minimum slot = %d", v.ID, v.MinimumSlot)
		}
		if !v.OK || len(v.Failures) != 0 {
			t.Errorf("%s: failures %+v", v.ID, v.Failures)
		}
		// 16 slots, foreground, cursor and the two selection pairs.
		if len(v.Measurements) != 20 {
			t.Errorf("%s: %d measurements, want 20", v.ID, len(v.Measurements))
		}
		zero := v.Measurements[0]
		if zero.Subject != "ansi[0]" || zero.Rule != RuleSlotZero {
			t.Fatalf("%s: first measurement is %+v", v.ID, zero)
		}
		if zero.Exempt != !v.Kind.LightBackground() {
			t.Errorf("%s: slot 0 exempt = %v", v.ID, zero.Exempt)
		}
		if zero.Exempt && zero.Ratio >= v.Floor {
			t.Errorf("%s: slot 0 is exempt but reaches the floor (%v); the exemption is no longer needed", v.ID, zero.Ratio)
		}
	}
	for _, role := range report.Roles {
		if !role.Pass {
			t.Errorf("role %s fails: %s", role.Role, role.Problem)
		}
	}
}

// TestGateFindsEachRule breaks one colour at a time and expects exactly the
// matching rule to fail.
func TestGateFindsEachRule(t *testing.T) {
	tests := []struct {
		name    string
		variant string
		edit    func(*Variant)
		rule    string
		subject string
	}{
		{"slot on background", "dark", func(v *Variant) { v.ANSI[3] = v.Background }, RuleSlots, "ansi[3]"},
		{"slot 15 on background", "hc", func(v *Variant) { v.ANSI[15] = v.Background }, RuleSlots, "ansi[15]"},
		{"slot 0 on a light background", "light", func(v *Variant) { v.ANSI[0] = v.Background }, RuleSlotZero, "ansi[0]"},
		{"foreground", "light-vivid", func(v *Variant) { v.Foreground = v.Background }, RuleForeground, "foreground"},
		{"cursor", "dark-vivid", func(v *Variant) { v.Cursor = v.Background }, RuleCursor, "cursor"},
		{"selection foreground", "hc-light", func(v *Variant) { v.SelectionForeground = v.SelectionBackground }, RuleSelection, "selectionForeground"},
		// Mid grey: neither black nor white reaches 7:1 on it.
		{"black or white on selection", "hc", func(v *Variant) { v.SelectionBackground = "#808080"; v.SelectionForeground = "#000000" }, RuleSelection, "blackOrWhite"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := load(t)
			for i := range p.Variants {
				if p.Variants[i].ID == test.variant {
					test.edit(&p.Variants[i])
				}
			}
			report := Check(p, loadRoles(t))
			if report.OK {
				t.Fatal("the gate passed a broken palette")
			}
			for _, v := range report.Variants {
				if v.ID != test.variant {
					if !v.OK {
						t.Errorf("untouched variant %s fails", v.ID)
					}
					continue
				}
				found := false
				for _, f := range v.Failures {
					if f.Rule == test.rule && f.Subject == test.subject {
						found = true
					}
					if f.Pass || f.Exempt {
						t.Errorf("failure %+v is marked as passing", f)
					}
				}
				if !found {
					t.Errorf("failures %+v do not include rule %s for %s", v.Failures, test.rule, test.subject)
				}
			}
		})
	}
}

// TestSlotZeroIsExemptOnlyOnDarkBackgrounds changes slot 0 to the background
// on a dark variant: rule (b) must not fail there.
func TestSlotZeroIsExemptOnlyOnDarkBackgrounds(t *testing.T) {
	p := load(t)
	p.Variants[0].ANSI[0] = p.Variants[0].Background
	if report := Check(p, loadRoles(t)); !report.OK {
		t.Errorf("slot 0 on a dark background failed the gate: %+v", report.Variants[0].Failures)
	}
}

func TestGateFindsForbiddenRoleParameters(t *testing.T) {
	roles, err := ParseRoles([]byte(strings.Replace(string(rolesJSON), `"selection": "7"`, `"selection": "44;97"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	report := Check(load(t), roles)
	if report.OK {
		t.Fatal("the gate passed a role that paints a background")
	}
	for _, role := range report.Roles {
		if failed := role.Role == RoleSelection; role.Pass == failed {
			t.Errorf("role %s: pass = %v", role.Role, role.Pass)
		}
	}
	if !strings.Contains(report.Markdown(), "**no**") {
		t.Error("the Markdown report does not show the failing role")
	}
}

func TestReportJSON(t *testing.T) {
	report := Check(load(t), loadRoles(t))
	data, err := report.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		OK     bool `json:"ok"`
		Source struct {
			Tag string `json:"tag"`
		} `json:"source"`
		Variants []struct {
			ID           string            `json:"id"`
			Minimum      float64           `json:"minimum"`
			Failures     []json.RawMessage `json:"failures"`
			Measurements []struct {
				Rule, Subject, Against string
				Ratio, Floor           float64
				Exempt, Pass           bool
			} `json:"measurements"`
		} `json:"variants"`
		Roles []struct {
			Role, SGR string
			Pass      bool
		} `json:"roles"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.OK || decoded.Source.Tag != "v1.1.0" || len(decoded.Variants) != 6 || len(decoded.Roles) != len(roleOrder) {
		t.Errorf("unexpected JSON: %s", data)
	}
	if decoded.Variants[5].ID != "hc-light" || decoded.Variants[5].Minimum != 7.28 {
		t.Errorf("hc-light in JSON = %+v", decoded.Variants[5])
	}
	if !strings.Contains(string(data), `"failures": []`) {
		t.Error("an empty failure list must be encoded as [] and not as null")
	}
	if !strings.HasSuffix(string(data), "}\n") {
		t.Error("the JSON report does not end with a newline")
	}
}

func TestMarkdown(t *testing.T) {
	report := Check(load(t), loadRoles(t))
	md := report.Markdown()
	if md != Check(load(t), loadRoles(t)).Markdown() {
		t.Fatal("Markdown is not deterministic")
	}
	for _, want := range []string{
		"# Contrast\n",
		"| Slot | Name | Serena Dark | Serena Dark Vivid | Serena Light | Serena Light Vivid | Serena High Contrast | Serena High Contrast Light |\n",
		"| 15 | bright white |",
		"|  | minimum of slots 1-15 | 4.52 | 4.52 | 4.51 | 4.51 | 7.82 | 7.28 |\n",
		"|  | floor | 4.5 | 4.5 | 4.5 | 4.5 | 7.0 | 7.0 |\n",
		"(exempt)",
		"| parameter | `3;37` | yes |\n",
		"Every value is at or above its floor.\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown lacks %q", want)
		}
	}
	if strings.Contains(md, "\r") || !strings.HasSuffix(md, "\n") || strings.Contains(md, "(fails)") {
		t.Error("Markdown has a carriage return, lacks the final newline or reports a failure")
	}
	for _, r := range md {
		if r > 126 {
			t.Fatalf("Markdown contains the non-ASCII character %q", r)
		}
	}
}

// TestContrastDocumentIsCurrent keeps docs/contrast.md, which is generated
// from Markdown, in step with the palette (R9): after a palette import the
// file has to be generated again with `sereno check --markdown`.
func TestContrastDocumentIsCurrent(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join("..", "..", "docs", "contrast.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := Check(load(t), loadRoles(t)).Markdown(); string(committed) != want {
		t.Errorf("docs/contrast.md is not what `sereno check --markdown` prints now:\n%s", want)
	}
}
