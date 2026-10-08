package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/mode"
	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/render"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
	"github.com/joaooliveira10/serena-terminal/internal/target/winterminal"
)

// runTheme is `sereno theme` (R35, R36). It has three uses that do not mix:
// showing or changing the settings of the mode file, painting this terminal
// with `--paint`, and handing the terminal its own colours back with
// `--reset`.
func runTheme(r *run, p parsed) int {
	variant, painting := p.value("paint")
	resetting := p.has("reset")
	_, flavorGiven := p.value("flavor")
	_, inbandGiven := p.value("inband")
	changing := len(p.args) > 0 || flavorGiven || inbandGiven

	theme := findCommand("theme")
	switch {
	case painting && resetting:
		return r.usageError(theme, r.T("usage.exclusive", "--paint", "--reset"))
	case painting && (changing || p.has("show")):
		return r.usageError(theme, r.T("usage.stands-alone", "--paint"))
	case resetting && (changing || p.has("show")):
		return r.usageError(theme, r.T("usage.stands-alone", "--reset"))
	}

	pal, err := palette.Load()
	if err != nil {
		return r.failure(err)
	}
	session := r.env.Host.Session(winterminal.ProfileGUID())
	if painting || resetting {
		return r.paint(pal, session, variant, resetting)
	}

	s, ok := r.site()
	if !ok {
		return engine.ExitFailure
	}
	// The settings live in sereno's folder: without an apply there is none
	// to show or to change (R35). `--show` alone needs no file and works.
	if !s.applied && (changing || !p.has("show")) {
		return r.refuse(r.T("theme.not-applied"), r.T("theme.not-applied.next"), r.T("theme.not-applied.trial"))
	}

	settings := s.settings
	code := engine.ExitOK
	if changing {
		if len(p.args) > 0 {
			settings.Mode = mode.Mode(p.args[0])
		}
		if value, given := p.value("flavor"); given {
			settings.Flavor = mode.Flavor(value)
		}
		if value, given := p.value("inband"); given {
			settings.InBand = mode.Scope(value)
		}
		var stored bool
		if code, stored = r.storeSettings(s, pal, settings); code == engine.ExitFailure || code == engine.ExitRefused {
			return code
		}
		if !stored {
			// The mode file is as it was (it holds the setting already,
			// or the user edited it and it was left alone): what counts
			// is what it says.
			settings = readSettings(s.modePath)
		} else if r.env.Host.IsWindows() {
			r.say(r.T("theme.changed"))
		}
	}

	signals, known := mode.Current(r.getenv, r.env.Signals)
	resolved, _ := pal.Variant(settings.Variant(signals))
	r.settingsReport(settings, signals, known, resolved, session)
	if !r.env.Host.IsWindows() && settings.Mode == mode.Auto {
		r.say(r.T(WarnUnixAuto))
	}
	if p.has("show") {
		r.show(resolved)
	}

	// A session that follows the system is repainted at once (R35), which
	// also mends a tab whose palette Windows Terminal has reset.
	if r.terminal && session.PaintObstacle() == platform.NoObstacle && settings.Follows(session) {
		io.WriteString(r.env.Stdout, resolved.Sequence())
	}
	return code
}

// storeSettings writes the mode file and, when a fragment with the Serena
// profile is installed, the fragment with its colorScheme rendered again
// (R35). Everything else sereno owns stays as it is. The result is an exit
// code: 0, or 4 when a file the user edited was left alone; 1 and 3 after
// their messages. stored says whether the mode file was written, that is,
// whether there is a new setting.
//
// The fragment follows the mode file, never the command line. When the mode
// file is left as it is, because the user edited it or marked it read-only,
// the setting that was asked for is not stored, and the fragment is rendered
// from what the file says: that is what the shells go by and what the next
// apply renders.
func (r *run) storeSettings(s *site, pal *palette.Palette, settings mode.Settings) (code int, stored bool) {
	req := engine.Request{Root: s.root.Path, Version: r.env.Version, Manifest: s.manifest}
	req.Files = append(req.Files, engine.File{Path: s.modePath, Data: settings.Bytes()})

	// Without a place to look for Windows Terminal there is no fragment to
	// render again; the mode file is what counts.
	terminal, onWindows, err := r.windowsTerminal()
	if err != nil {
		onWindows = false
	}
	if recorded, owned := r.owns(s.manifest, terminal.fragmentPath); onWindows && owned && r.profileInstalled(s, recorded) {
		// What will the plan do with the mode file? A plan for that file
		// alone answers with the rules the real one goes by.
		alone, err := engine.PlanApply(req)
		if err != nil {
			return r.failure(err), false
		}
		if alone.Refused() {
			return r.printRefusals(alone.Refusals), false
		}
		inFile := settings
		if alone.Incomplete() > 0 {
			inFile = readSettings(s.modePath)
		}
		options := render.FragmentOptions{Profile: true, Shell: r.installedShell(s, pal, recorded), Mode: inFile.Mode, Flavor: inFile.Flavor, Root: s.root}
		data, refusal, err := r.renderFragment(pal, options)
		switch {
		case err != nil:
			return r.failure(err), false
		case refusal != nil:
			return r.printRefusals([]engine.Refusal{*refusal}), false
		}
		req.Files = append(req.Files, engine.File{Path: recorded, Data: data})
	}

	plan, err := engine.PlanApply(req)
	if err != nil {
		return r.failure(err), false
	}
	if plan.Refused() {
		return r.printRefusals(plan.Refusals), false
	}
	if code := r.carryOut(plan); code == engine.ExitFailure {
		return code, false
	}
	// Only what changed or was left alone is worth a line; the manifest is
	// sereno's own bookkeeping.
	for _, op := range plan.Ops {
		if op.Verb == engine.VerbKeep || r.samePath(op.Path, filepath.Join(s.root.Path, engine.ManifestName)) {
			continue
		}
		stored = stored || (op.Changes() && r.samePath(op.Path, s.modePath))
		for _, line := range op.Lines(r.catalog.T) {
			r.say(line)
		}
	}
	if plan.Incomplete() > 0 {
		r.say(r.T("plan.incomplete"))
	}
	return plan.ExitCode(), stored
}

// installedShell finds out which PowerShell the installed Serena profile
// starts, so that a change of theme changes the colours and nothing else:
// with `--shell auto` recorded, a PowerShell 7 that was installed after the
// apply must not slip into the profile through `sereno theme`. The installed
// fragment is compared with what sereno renders for each shell; when it is
// neither (the user edited it, and it will be left alone anyway), the
// recorded option decides as it does in apply.
func (r *run) installedShell(s *site, pal *palette.Palette, fragmentPath string) shells.Shell {
	fallback, _ := r.profileShell(s.manifest.Options.Shell)
	if s.manifest.Options.Shell != "auto" {
		return fallback
	}
	current, err := os.ReadFile(fragmentPath)
	if err != nil {
		return fallback
	}
	for _, shell := range []shells.Shell{shells.PowerShell, shells.Pwsh} {
		options := render.FragmentOptions{Profile: true, Shell: shell, Mode: s.settings.Mode, Flavor: s.settings.Flavor, Root: s.root}
		if data, err := render.Fragment(pal, options); err == nil && bytes.Equal(data, current) {
			return shell
		}
	}
	return fallback
}

// paint is `--paint VARIANT` and `--reset` (R36): the palette, or the
// sequences that hand it back, written to the terminal on standard output.
// No file is read or written.
func (r *run) paint(pal *palette.Palette, session platform.Session, id string, reset bool) int {
	switch obstacle := session.PaintObstacle(); {
	case !r.terminal:
		return r.refuse(r.T("theme.paint.not-terminal"), r.T("theme.paint.not-terminal.next"))
	case obstacle == platform.ObstacleVSCode:
		return r.refuse(r.T("theme.paint.vscode"), r.T("theme.paint.vscode.next"))
	case obstacle == platform.ObstacleMultiplexer:
		return r.refuse(r.T("theme.paint.multiplexer"), r.T("theme.paint.multiplexer.next"))
	}
	if reset {
		io.WriteString(r.env.Stdout, palette.ResetSequence())
		r.say(r.T("theme.reset.done"))
		return engine.ExitOK
	}
	variant, ok := pal.Variant(id)
	if !ok {
		return r.failure(fmt.Errorf("the palette has no variant %q", id))
	}
	io.WriteString(r.env.Stdout, variant.Sequence())
	r.say(r.T("theme.paint.done", variant.Name))
	return engine.ExitOK
}

// routeText explains a palette route in a few words.
func (r *run) routeText(route mode.Route) string {
	switch route {
	case mode.RouteInBand:
		return r.T("route.in-band")
	case mode.RouteHost:
		return r.T("route.host")
	}
	return r.T("route.emulator")
}

// signalsText describes the appearance the system reports.
func (r *run) signalsText(signals mode.Signals, known bool) string {
	switch {
	case !known:
		return r.T("signals.unknown")
	case signals.HighContrast && signals.HCLight:
		return r.T("signals.contrast-light")
	case signals.HighContrast:
		return r.T("signals.contrast-dark")
	case signals.AppsLight:
		return r.T("signals.light")
	}
	return r.T("signals.dark")
}

// field prints one "label  value" line of a report.
func (r *run) field(label, value string) {
	r.say(fmt.Sprintf("%-*s  %s", labelWidth, label, value))
}

// labelWidth is the width of the label column of the reports; the longest
// label of either language fits (a test checks that).
const labelWidth = 18

// onOff spells the prompt setting as the mode file does.
func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

// settingsReport prints the settings of the mode file, what the system says
// and what follows from both: the variant and the palette route (R35, R37).
// The five settings keep the names they have in the mode file and on the
// command line.
func (r *run) settingsReport(settings mode.Settings, signals mode.Signals, known bool, variant palette.Variant, session platform.Session) {
	r.field("mode", string(settings.Mode))
	r.field("flavor", string(settings.Flavor))
	r.field("inband", string(settings.InBand))
	r.field("keys", string(settings.Keys))
	r.field("prompt", onOff(settings.Prompt))
	r.field(r.T("label.system"), r.signalsText(signals, known))
	r.field(r.T("label.variant"), variant.ID+" ("+variant.Name+")")
	route := settings.Route(session)
	r.field(r.T("label.route"), string(route)+": "+r.routeText(route))
}

// roleSamples gives every role of R8 a few characters to show it with.
var roleSamples = map[palette.Role]string{
	palette.RoleFunction: "Get-ChildItem", palette.RoleKeyword: "foreach", palette.RoleString: "'text'",
	palette.RoleType: "[string]", palette.RoleProperty: ".Length", palette.RoleConstant: "42",
	palette.RoleParameter: "-Path", palette.RoleOperator: "-eq", palette.RoleVariable: "$name",
	palette.RoleDefault: "text", palette.RoleComment: "# comment", palette.RoleError: "error",
	palette.RoleEmphasis: "match", palette.RoleSelection: "selected", palette.RolePromptPath: "~/project",
	palette.RolePromptBranch: "main", palette.RolePromptOK: ">", palette.RolePromptFailed: ">",
	palette.RolePromptMeta: "2.3s",
}

// show is `--show` (R36): the sixteen slots as swatches, one sample per role
// and the contrast the gate measured for the variant. Swatches and samples
// are drawn with slot colours and reverse video, so they show what this
// terminal makes of the slots; the values next to them are the variant's.
func (r *run) show(variant palette.Variant) {
	r.say("")
	r.say(r.styled(styleStrong, r.T("show.slots", variant.Name)))
	for n, colour := range variant.ANSI {
		foreground := 30 + n
		if n >= 8 {
			foreground = 90 + n - 8
		}
		swatch := r.styled(fmt.Sprintf("7;%d", foreground), "    ")
		sample := r.styled(fmt.Sprint(foreground), "sereno")
		r.say(fmt.Sprintf("  %2d  %-15s %s  %s  %s", n, palette.SlotName(n), colour, swatch, sample))
	}

	r.say("")
	r.say(r.styled(styleStrong, r.T("show.roles")))
	roles, err := palette.LoadRoles()
	if err != nil {
		r.say("  " + err.Error())
		return
	}
	for _, role := range palette.RoleList() {
		r.say(fmt.Sprintf("  %-14s %-5s %s", role, roles.SGR(role), r.styled(roles.SGR(role), roleSamples[role])))
	}

	r.say("")
	r.say(r.styled(styleStrong, r.T("show.contrast")))
	for _, report := range palette.Check(&palette.Palette{Source: palette.Source{}, Variants: []palette.Variant{variant}}, roles).Variants {
		r.say("  " + r.T("show.contrast.slots", ratio(report.Minimum), fmt.Sprint(report.MinimumSlot), floor(report.Floor)))
		for _, m := range report.Measurements {
			if m.Rule == palette.RuleForeground || m.Rule == palette.RuleCursor || m.Rule == palette.RuleSelection {
				r.say("  " + r.T("show.contrast.pair", r.subject(m.Subject), m.Against, ratio(m.Ratio), floor(m.Floor)))
			}
		}
	}
}

// subject names what a measurement of the contrast gate is about. The names
// are those of the palette file, which are not translated; the one that is
// not a colour of the palette, the better of black and white on the selection
// (R9), is said in words.
func (r *run) subject(name string) string {
	if name == palette.SubjectBlackOrWhite {
		return r.T("show.contrast.black-or-white")
	}
	return name
}

// ratio and floor print contrast figures the way docs/contrast.md does.
func ratio(value float64) string { return fmt.Sprintf("%.2f", value) }
func floor(value float64) string { return fmt.Sprintf("%.1f", value) }
