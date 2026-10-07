// Package mode decides which Serena variant applies: it holds the settings of
// the mode file (R11), the operating-system signals and the resolution table
// (R28), and the rule for following the system in a session (R30).
//
// The table is implemented three times, here and in the PowerShell and
// bash/zsh init files, and all three are tested against testdata/modes.tsv.
package mode

// Mode is the first setting of the mode file: follow the system, or one fixed
// appearance.
type Mode string

// The modes of R11.
const (
	Auto    Mode = "auto"
	Dark    Mode = "dark"
	Light   Mode = "light"
	HC      Mode = "hc"
	HCLight Mode = "hc-light"
)

// Flavor chooses between the soft and the vivid colours of the dark and light
// variants. The high-contrast variants have no flavours.
type Flavor string

// The flavours of R11.
const (
	Soft  Flavor = "soft"
	Vivid Flavor = "vivid"
)

// Signals are the three appearance facts of the operating system (R28).
type Signals struct {
	HighContrast bool // hc_on: a contrast theme is active
	HCLight      bool // hc_light: the contrast theme has a light window colour
	AppsLight    bool // apps_light: applications use the light theme
}

// Resolve returns the id of the variant that a mode, a flavour and the system
// signals select (R28). The ids are those of the palette (R7).
//
//   - auto with a contrast theme: hc-light or hc, by the window colour;
//   - auto otherwise: light or dark, by the application theme, with the
//     flavour;
//   - dark and light: that variant, with the flavour;
//   - hc and hc-light: that variant.
//
// An unknown mode resolves like auto and an unknown flavour like soft: the
// defaults of R11.
func Resolve(mode Mode, flavor Flavor, signals Signals) string {
	switch mode {
	case HC:
		return "hc"
	case HCLight:
		return "hc-light"
	case Dark:
		return withFlavor("dark", flavor)
	case Light:
		return withFlavor("light", flavor)
	}
	switch {
	case signals.HighContrast && signals.HCLight:
		return "hc-light"
	case signals.HighContrast:
		return "hc"
	case signals.AppsLight:
		return withFlavor("light", flavor)
	}
	return withFlavor("dark", flavor)
}

func withFlavor(variant string, flavor Flavor) string {
	if flavor == Vivid {
		return variant + "-vivid"
	}
	return variant
}
