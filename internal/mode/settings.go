package mode

import (
	"fmt"
	"strings"

	"github.com/joaooliveira10/serena-terminal/internal/platform"
)

// Scope says in which sessions a feature of the init files is active.
type Scope string

// The scopes of R11, used by the inband and keys settings.
const (
	ScopeProfile Scope = "profile" // only in sessions started from the Serena profile
	ScopeAlways  Scope = "always"  // in every session that sources the init file
	ScopeOff     Scope = "off"     // never
)

// Settings is the content of the mode file ROOT/mode (R11).
type Settings struct {
	Mode   Mode
	Flavor Flavor
	InBand Scope // where the palette may be painted in-band (R30)
	Keys   Scope // where the PSReadLine comfort set is applied (R27)
	Prompt bool  // whether the Serena prompt is installed (R26)
}

// Defaults returns the values of a first apply: auto, soft, profile, profile,
// on. A missing or malformed mode file means these.
func Defaults() Settings {
	return Settings{Mode: Auto, Flavor: Soft, InBand: ScopeProfile, Keys: ScopeProfile, Prompt: true}
}

// ParseMode recognises auto, dark, light, hc and hc-light.
func ParseMode(value string) (Mode, error) {
	switch m := Mode(value); m {
	case Auto, Dark, Light, HC, HCLight:
		return m, nil
	}
	return "", fmt.Errorf("%q is not one of auto, dark, light, hc, hc-light", value)
}

// ParseFlavor recognises soft and vivid.
func ParseFlavor(value string) (Flavor, error) {
	switch f := Flavor(value); f {
	case Soft, Vivid:
		return f, nil
	}
	return "", fmt.Errorf("%q is not one of soft, vivid", value)
}

// ParseScope recognises profile, always and off.
func ParseScope(value string) (Scope, error) {
	switch s := Scope(value); s {
	case ScopeProfile, ScopeAlways, ScopeOff:
		return s, nil
	}
	return "", fmt.Errorf("%q is not one of profile, always, off", value)
}

// ParseSwitch recognises on and off.
func ParseSwitch(value string) (bool, error) {
	switch value {
	case "on":
		return true, nil
	case "off":
		return false, nil
	}
	return false, fmt.Errorf("%q is not one of on, off", value)
}

// Validate reports the first field that holds a value R11 does not allow.
func (s Settings) Validate() error {
	if _, err := ParseMode(string(s.Mode)); err != nil {
		return fmt.Errorf("mode: %w", err)
	}
	if _, err := ParseFlavor(string(s.Flavor)); err != nil {
		return fmt.Errorf("flavor: %w", err)
	}
	if _, err := ParseScope(string(s.InBand)); err != nil {
		return fmt.Errorf("inband: %w", err)
	}
	if _, err := ParseScope(string(s.Keys)); err != nil {
		return fmt.Errorf("keys: %w", err)
	}
	return nil
}

// Bytes returns the content of the mode file: ASCII, LF, exactly five lines
// in the order mode, flavor, inband, keys, prompt.
func (s Settings) Bytes() []byte {
	prompt := "off"
	if s.Prompt {
		prompt = "on"
	}
	return []byte(fmt.Sprintf("mode=%s\nflavor=%s\ninband=%s\nkeys=%s\nprompt=%s\n", s.Mode, s.Flavor, s.InBand, s.Keys, prompt))
}

// Parse reads the content of a mode file. It accepts exactly what Bytes
// writes: five LF-terminated lines, in order, with allowed values. Anything
// else is an error, and the caller falls back to Defaults as the shell hooks
// do.
func Parse(data []byte) (Settings, error) {
	text := string(data)
	if !strings.HasSuffix(text, "\n") {
		return Settings{}, fmt.Errorf("mode file: the last line is not terminated")
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	keys := []string{"mode", "flavor", "inband", "keys", "prompt"}
	if len(lines) != len(keys) {
		return Settings{}, fmt.Errorf("mode file: %d lines, want %d", len(lines), len(keys))
	}
	values := make([]string, len(keys))
	for i, key := range keys {
		value, ok := strings.CutPrefix(lines[i], key+"=")
		if !ok {
			return Settings{}, fmt.Errorf("mode file: line %d does not start with %q", i+1, key+"=")
		}
		values[i] = value
	}
	var s Settings
	var err error
	if s.Mode, err = ParseMode(values[0]); err != nil {
		return Settings{}, fmt.Errorf("mode file: mode: %w", err)
	}
	if s.Flavor, err = ParseFlavor(values[1]); err != nil {
		return Settings{}, fmt.Errorf("mode file: flavor: %w", err)
	}
	if s.InBand, err = ParseScope(values[2]); err != nil {
		return Settings{}, fmt.Errorf("mode file: inband: %w", err)
	}
	if s.Keys, err = ParseScope(values[3]); err != nil {
		return Settings{}, fmt.Errorf("mode file: keys: %w", err)
	}
	if s.Prompt, err = ParseSwitch(values[4]); err != nil {
		return Settings{}, fmt.Errorf("mode file: prompt: %w", err)
	}
	return s, nil
}

// Variant returns the id of the variant these settings select under the given
// signals (R28).
func (s Settings) Variant(signals Signals) string {
	return Resolve(s.Mode, s.Flavor, signals)
}

// Route is how the palette reaches the screen in a session (R37).
type Route string

// The three palette routes.
const (
	RouteInBand   Route = "in-band"  // sereno paints it with OSC sequences at the prompt
	RouteHost     Route = "host"     // the host application owns it (VS Code with Serena Theme)
	RouteEmulator Route = "emulator" // the emulator's own scheme: the fragment or an exported theme
)

// Follows reports whether the follow check of R30 is active in a session: the
// session is in Windows Terminal (and not in VS Code's terminal), inband is
// not off, and inband is always or the session was started from the Serena
// profile.
func (s Settings) Follows(session platform.Session) bool {
	if session.Emulator != platform.WindowsTerminal {
		return false
	}
	switch s.InBand {
	case ScopeAlways:
		return true
	case ScopeProfile:
		return session.SerenaProfile
	}
	return false
}

// Route returns the palette route of a session under these settings.
func (s Settings) Route(session platform.Session) Route {
	switch {
	case session.Emulator == platform.VSCode:
		return RouteHost
	case s.Follows(session):
		return RouteInBand
	}
	return RouteEmulator
}
