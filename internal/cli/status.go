package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/mode"
	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
	"github.com/joaooliveira10/serena-terminal/internal/target/winterminal"
)

// The warning ids of R37. They are not translated; each is also the key of
// its text in the catalogues.
const (
	WarnTerminalAbsent     = "wt.not-detected"
	WarnThemeNotSystem     = "wt.theme-not-system"
	WarnDefaultsOverride   = "wt.defaults-override"
	WarnProfileSuppressed  = "wt.profile-suppressed"
	WarnSettingsUnreadable = "wt.settings-unreadable"
	WarnOwnedModified      = "owned.modified"
	WarnOwnedMissing       = "owned.missing"
	WarnHookMissing        = "hook.missing"
	WarnVSCode             = "session.vscode"
	WarnPwshAbsent         = "pwsh.absent"
	WarnUnixAuto           = "unix.auto-unsupported"
)

// WarningIDs returns the warning ids of R37, in the order status lists them.
func WarningIDs() []string {
	return []string{
		WarnTerminalAbsent, WarnThemeNotSystem, WarnDefaultsOverride, WarnProfileSuppressed, WarnSettingsUnreadable,
		WarnOwnedModified, WarnOwnedMissing, WarnHookMissing, WarnVSCode, WarnPwshAbsent, WarnUnixAuto,
	}
}

// PwshInstallCommand is what installs PowerShell 7 through winget. status
// shows it with the pwsh.absent warning; sereno never runs it (R37, R40).
const PwshInstallCommand = "winget install -e --id Microsoft.PowerShell --source winget"

// statusReport is the output of `sereno status --json` (R37). The fields are
// declared in the order of the requirement, which is the order of the keys.
type statusReport struct {
	Version         string          `json:"version"`
	OS              string          `json:"os"`
	Root            string          `json:"root"`
	Applied         bool            `json:"applied"`
	Lang            string          `json:"lang"`
	Mode            statusMode      `json:"mode"`
	Signals         *statusSignals  `json:"signals"`
	Variant         string          `json:"variant"`
	Session         statusSession   `json:"session"`
	WindowsTerminal statusTerminal  `json:"windowsTerminal"`
	Hooks           []statusHook    `json:"hooks"`
	Owned           statusOwned     `json:"owned"`
	Pwsh            statusPwsh      `json:"pwsh"`
	Warnings        []statusWarning `json:"warnings"`
}

// statusMode is the mode file, value for value.
type statusMode struct {
	Mode   string `json:"mode"`
	Flavor string `json:"flavor"`
	InBand string `json:"inband"`
	Keys   string `json:"keys"`
	Prompt string `json:"prompt"`
}

// statusSignals is what the system says; the report holds null where the
// system offers no detection.
type statusSignals struct {
	AppsLight    bool `json:"appsLight"`
	HighContrast bool `json:"highContrast"`
	HCLight      bool `json:"hcLight"`
}

type statusSession struct {
	Emulator      string `json:"emulator"`
	SerenaProfile bool   `json:"serenaProfile"`
	PaletteRoute  string `json:"paletteRoute"`
}

type statusTerminal struct {
	Detected bool `json:"detected"`
	// Fragment is true when sereno's fragment is installed, Profile when it
	// carries the Serena profile.
	Fragment     bool   `json:"fragment"`
	Profile      bool   `json:"profile"`
	SettingsPath string `json:"settingsPath"`
	// Theme is Windows Terminal's own "theme" setting: a name, "" when it is
	// not set, or themePair when it names one theme for light and one for
	// dark.
	Theme            string   `json:"theme"`
	DefaultsOverride []string `json:"defaultsOverride"`
	// StubPresent: settings.json has an entry for the Serena profile.
	// GUIDKnown: state.json lists the profile among those it has seen.
	StubPresent bool `json:"stubPresent"`
	GUIDKnown   bool `json:"guidKnown"`
}

// themePair is the value of statusTerminal.Theme for a light/dark pair.
const themePair = "light/dark pair"

type statusHook struct {
	Shell   string `json:"shell"`
	Path    string `json:"path"`
	Present bool   `json:"present"`
}

type statusOwned struct {
	Total    int `json:"total"`
	Modified int `json:"modified"`
	Missing  int `json:"missing"`
}

type statusPwsh struct {
	OnPath bool `json:"onPath"`
}

type statusWarning struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// runStatus is `sereno status` (R37). It reads and reports; it writes
// nothing and starts nothing, and a file of Windows Terminal that cannot be
// read is a warning, not a failure.
func runStatus(r *run, p parsed) int {
	s, ok := r.site()
	if !ok {
		return engine.ExitFailure
	}
	report, variant, err := r.inspect(s)
	if err != nil {
		return r.failure(err)
	}
	if p.has("json") {
		var buf bytes.Buffer
		encoder := json.NewEncoder(&buf)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return r.failure(err)
		}
		r.env.Stdout.Write(buf.Bytes())
		return engine.ExitOK
	}
	r.statusText(s, report, variant)
	return engine.ExitOK
}

// inspect gathers everything status reports.
func (r *run) inspect(s *site) (statusReport, palette.Variant, error) {
	host := r.env.Host
	pal, err := palette.Load()
	if err != nil {
		return statusReport{}, palette.Variant{}, err
	}
	inspection, err := engine.Inspect(s.root.Path)
	if err != nil {
		return statusReport{}, palette.Variant{}, err
	}
	guid := winterminal.ProfileGUID()
	session := host.Session(guid)
	signals, known := mode.Current(r.getenv, r.env.Signals)
	variant, _ := pal.Variant(s.settings.Variant(signals))

	report := statusReport{
		Version: r.env.Version,
		OS:      host.OS,
		Root:    s.root.Path,
		Applied: s.applied,
		Lang:    string(r.catalog.Lang()),
		Mode: statusMode{
			Mode: string(s.settings.Mode), Flavor: string(s.settings.Flavor), InBand: string(s.settings.InBand),
			Keys: string(s.settings.Keys), Prompt: onOff(s.settings.Prompt),
		},
		Variant: variant.ID,
		Session: statusSession{
			Emulator: string(session.Emulator), SerenaProfile: session.SerenaProfile,
			PaletteRoute: string(s.settings.Route(session)),
		},
		WindowsTerminal: statusTerminal{DefaultsOverride: []string{}},
		Hooks:           []statusHook{},
		Owned: statusOwned{
			Total: len(inspection.Owned), Modified: inspection.Count(engine.OwnedModified), Missing: inspection.Count(engine.OwnedMissing),
		},
		Warnings: []statusWarning{},
	}
	if known {
		report.Signals = &statusSignals{AppsLight: signals.AppsLight, HighContrast: signals.HighContrast, HCLight: signals.HCLight}
	}
	_, report.Pwsh.OnPath = host.LookPath(shells.Pwsh.Executable(host))
	warn := func(id, text string) {
		report.Warnings = append(report.Warnings, statusWarning{ID: id, Text: text})
	}

	// Windows Terminal: detection, sereno's fragment, and what its two files
	// say. They are only read (R2).
	terminal, onWindows, err := r.windowsTerminal()
	if err != nil {
		// A Windows without LOCALAPPDATA has no place to look in: nothing
		// is detected, and status says so instead of failing.
		terminal, onWindows = terminalSite{}, true
	}
	wt := &report.WindowsTerminal
	if onWindows {
		installation := terminal.installation
		wt.Detected = installation.Present
		if recorded, owned := r.owns(s.manifest, terminal.fragmentPath); owned && isFile(recorded) {
			wt.Fragment = true
			wt.Profile = r.profileInstalled(s, recorded)
		}
		if !wt.Detected {
			warn(WarnTerminalAbsent, r.T(WarnTerminalAbsent))
		}
		wt.SettingsPath = installation.SettingsPath
		settingsRead := false
		if installation.SettingsPath != "" {
			settings, err := winterminal.ReadSettings(installation.SettingsPath)
			if err != nil {
				warn(WarnSettingsUnreadable, r.T(WarnSettingsUnreadable, installation.SettingsPath, err))
			} else {
				settingsRead = true
				wt.Theme = settings.Theme
				if settings.ThemePair {
					wt.Theme = themePair
				}
				wt.DefaultsOverride = append(wt.DefaultsOverride, settings.DefaultsOverride...)
				wt.StubPresent = settings.HasProfile(guid)
				if wt.Profile && s.settings.Mode == mode.Auto && !settings.FollowsSystem() {
					theme := wt.Theme
					if theme == "" {
						theme = r.T("wt.theme.default-name")
					}
					warn(WarnThemeNotSystem, r.T(WarnThemeNotSystem, theme))
				}
				if wt.Profile && len(settings.DefaultsOverride) > 0 {
					warn(WarnDefaultsOverride, r.T(WarnDefaultsOverride, strings.Join(settings.DefaultsOverride, ", ")))
				}
			}
		}
		if installation.StatePath != "" {
			state, err := winterminal.ReadState(installation.StatePath)
			if err != nil {
				warn(WarnSettingsUnreadable, r.T(WarnSettingsUnreadable, installation.StatePath, err))
			} else {
				wt.GUIDKnown = state.Knows(guid)
			}
		}
		if wt.Profile && settingsRead && wt.GUIDKnown && !wt.StubPresent {
			warn(WarnProfileSuppressed, r.T(WarnProfileSuppressed, installation.SettingsPath, stubObject(guid)))
		}
	}

	if modified := inspection.Count(engine.OwnedModified); modified > 0 {
		warn(WarnOwnedModified, r.T(WarnOwnedModified, ownedPaths(inspection, engine.OwnedModified)))
	}
	if missing := inspection.Count(engine.OwnedMissing); missing > 0 {
		warn(WarnOwnedMissing, r.T(WarnOwnedMissing, ownedPaths(inspection, engine.OwnedMissing)))
	}
	for _, hook := range inspection.Hooks {
		report.Hooks = append(report.Hooks, statusHook{Shell: hook.Shell, Path: hook.Path, Present: hook.Present})
		switch {
		case hook.Present:
		case !hook.Problem.IsZero():
			warn(WarnHookMissing, r.T("hook.missing.unreadable", hook.Shell, hook.Path, hook.Problem.Text(r.catalog.T)))
		default:
			warn(WarnHookMissing, r.T(WarnHookMissing, hook.Shell, hook.Path))
		}
	}
	if session.Emulator == platform.VSCode {
		warn(WarnVSCode, r.T(WarnVSCode))
	}
	if onWindows && !report.Pwsh.OnPath {
		warn(WarnPwshAbsent, r.T(WarnPwshAbsent, PwshInstallCommand))
	}
	if !onWindows && s.settings.Mode == mode.Auto {
		warn(WarnUnixAuto, r.T(WarnUnixAuto))
	}
	return report, variant, nil
}

// stubObject is the entry Windows Terminal needs back in profiles.list to
// show a profile it has marked as deleted (R37).
func stubObject(guid string) string {
	return fmt.Sprintf(`{ "guid": "%s", "hidden": false, "name": "%s", "source": "%s" }`, guid, winterminal.ProfileName, winterminal.FragmentApp)
}

// ownedPaths lists the owned files in a state, for a warning text.
func ownedPaths(inspection *engine.Inspection, state engine.OwnedState) string {
	var paths []string
	for _, file := range inspection.Owned {
		if file.State == state {
			paths = append(paths, file.Path)
		}
	}
	return strings.Join(paths, ", ")
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// statusText prints the report for people. It mirrors the JSON: the same
// facts in the same order.
func (r *run) statusText(s *site, report statusReport, variant palette.Variant) {
	r.field(r.T("label.version"), r.versionLine())
	r.field(r.T("label.root"), report.Root)
	if report.Applied {
		r.field(r.T("label.applied"), r.T("word.yes"))
	} else {
		r.field(r.T("label.applied"), r.T("status.applied.no"))
	}
	r.field(r.T("label.lang"), report.Lang)

	session := r.env.Host.Session(winterminal.ProfileGUID())
	signals, known := mode.Current(r.getenv, r.env.Signals)
	r.settingsReport(s.settings, signals, known, variant, session)
	emulator := r.T("status.session.other")
	switch session.Emulator {
	case platform.WindowsTerminal:
		emulator = "Windows Terminal"
	case platform.VSCode:
		emulator = "VS Code"
	}
	r.field(r.T("label.session"), r.T("status.session", emulator, r.yesNo(report.Session.SerenaProfile)))

	if r.env.Host.IsWindows() {
		wt := report.WindowsTerminal
		detected := r.T("status.wt.absent")
		if wt.Detected {
			detected = r.T("status.wt.detected")
		}
		fragment := r.T("status.wt.fragment.none")
		switch {
		case wt.Fragment && wt.Profile:
			fragment = r.T("status.wt.fragment.profile")
		case wt.Fragment:
			fragment = r.T("status.wt.fragment.schemes")
		}
		r.field("Windows Terminal", detected+"; "+fragment)
		if wt.SettingsPath != "" {
			theme := wt.Theme
			if theme == "" {
				theme = r.T("status.wt.theme.default")
			}
			r.field("", r.T("status.wt.settings", wt.SettingsPath))
			r.field("", r.T("status.wt.theme", theme))
			r.field("", r.T("status.wt.entry", r.yesNo(wt.StubPresent), r.yesNo(wt.GUIDKnown)))
		}
	}

	if len(report.Hooks) == 0 {
		r.field(r.T("label.hooks"), r.T("status.hooks.none"))
	}
	for i, hook := range report.Hooks {
		label := ""
		if i == 0 {
			label = r.T("label.hooks")
		}
		state := r.styled(styleAlert, r.T("word.missing"))
		if hook.Present {
			state = r.styled(styleGood, r.T("word.present"))
		}
		r.field(label, fmt.Sprintf("%s  %s  %s", hook.Shell, hook.Path, state))
	}
	r.field(r.T("label.owned"), r.T("status.owned", report.Owned.Total, report.Owned.Modified, report.Owned.Missing))
	if report.Pwsh.OnPath {
		r.field("PowerShell 7", r.T("status.pwsh.on"))
	} else {
		r.field("PowerShell 7", r.T("status.pwsh.off"))
	}

	if len(report.Warnings) == 0 {
		r.field(r.T("label.warnings"), r.T("status.warnings.none"))
		return
	}
	r.say("")
	r.say(r.styled(styleStrong, r.T("label.warnings")))
	for _, warning := range report.Warnings {
		r.say("  " + r.styled(styleChange, warning.ID))
		r.say("      " + warning.Text)
	}
}
