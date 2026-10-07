package mode

import (
	"strconv"
	"strings"
)

// OSModeEnv names the variable that replaces the system signals (R28). Its
// values are dark, light, hc and hc-light.
const OSModeEnv = "SERENO_OS_MODE"

// Reader reads the appearance signals of the operating system. It is an
// interface so that tests supply signals instead of reading the machine's.
type Reader interface {
	// Signals returns the current signals. known is false where the system
	// offers no detection (macOS and Linux in 0.1): the signals are then all
	// off, and auto resolves to dark.
	Signals() (signals Signals, known bool)
}

// Fixed is a Reader that always reports the same signals as known.
type Fixed Signals

// Signals implements Reader.
func (f Fixed) Signals() (Signals, bool) { return Signals(f), true }

// Unknown is the Reader of systems without detection.
type Unknown struct{}

// Signals implements Reader.
func (Unknown) Signals() (Signals, bool) { return Signals{}, false }

// Current returns the signals in effect: those SERENO_OS_MODE names when it
// holds one of its four values, otherwise what the reader reports.
func Current(getenv func(string) string, reader Reader) (signals Signals, known bool) {
	if signals, ok := SignalsFor(getenv(OSModeEnv)); ok {
		return signals, true
	}
	return reader.Signals()
}

// SignalsFor translates a value of SERENO_OS_MODE into the signals a system
// in that state would report.
func SignalsFor(osMode string) (Signals, bool) {
	switch osMode {
	case "dark":
		return Signals{}, true
	case "light":
		return Signals{AppsLight: true}, true
	case "hc":
		return Signals{HighContrast: true}, true
	case "hc-light":
		return Signals{HighContrast: true, HCLight: true}, true
	}
	return Signals{}, false
}

// Registry reads values under HKEY_CURRENT_USER. The Windows implementation
// uses package syscall; tests use a map.
type Registry interface {
	// String returns a REG_SZ value.
	String(key, name string) (string, bool)
	// Integer returns a REG_DWORD value.
	Integer(key, name string) (uint32, bool)
}

// The registry values of R28, all under HKEY_CURRENT_USER.
const (
	keyHighContrast = `Control Panel\Accessibility\HighContrast`
	keyColors       = `Control Panel\Colors`
	keyPersonalize  = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
)

// FromRegistry interprets the three registry values of R28. A value that
// cannot be read, or does not have the expected form, counts as off or dark.
func FromRegistry(registry Registry) Signals {
	var signals Signals

	// Flags is stored as text ("126"); bit 0 is "high contrast on".
	if text, ok := registry.String(keyHighContrast, "Flags"); ok {
		if flags, err := strconv.ParseUint(strings.TrimSpace(text), 10, 32); err == nil {
			signals.HighContrast = flags&1 == 1
		}
	} else if flags, ok := registry.Integer(keyHighContrast, "Flags"); ok {
		signals.HighContrast = flags&1 == 1
	}

	// Window is "R G B" in decimal: a light window colour means the contrast
	// theme is a light one.
	if text, ok := registry.String(keyColors, "Window"); ok {
		signals.HCLight = lightWindow(text)
	}

	if value, ok := registry.Integer(keyPersonalize, "AppsUseLightTheme"); ok {
		signals.AppsLight = value == 1
	}
	return signals
}

// lightWindow reports whether 0.2126 R + 0.7152 G + 0.0722 B of an "R G B"
// string is above 127.
func lightWindow(value string) bool {
	fields := strings.Fields(value)
	if len(fields) != 3 {
		return false
	}
	weights := [3]float64{0.2126, 0.7152, 0.0722}
	sum := 0.0
	for i, field := range fields {
		channel, err := strconv.ParseUint(field, 10, 8)
		if err != nil {
			return false
		}
		sum += float64(weights[i] * float64(channel))
	}
	return sum > 127
}
