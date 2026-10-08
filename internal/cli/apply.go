package cli

import (
	"path/filepath"
	"strings"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/mode"
	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/render"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
)

// runApply is `sereno apply` (R13): phase 1 computes every operation and
// every refusal, prints the plan, and only then phase 2 writes.
func runApply(r *run, p parsed) int {
	s, ok := r.site()
	if !ok {
		return engine.ExitFailure
	}
	host := r.env.Host
	dryRun := p.has("dry-run")

	// Options that are not on the command line keep the recorded values
	// (R13); a first apply starts from the defaults the empty manifest holds.
	options := engine.Options{Profile: s.manifest.Options.Profile, Shell: s.manifest.Options.Shell}
	if value, given := p.value("shell"); given {
		options.Shell = value
	}
	if p.has("no-profile") {
		options.Profile = false
	}
	settings := s.settings
	if value, given := p.value("keys"); given {
		settings.Keys = mode.Scope(value)
	}
	if value, given := p.value("prompt"); given {
		settings.Prompt = value == "on"
	}

	// `--hook` adds to the recorded hooks and never removes one (R13).
	recorded := map[shells.Shell]bool{}
	for _, name := range s.manifest.Options.Hooks {
		recorded[shells.Shell(name)] = true
	}
	for _, record := range s.manifest.Hooks {
		recorded[shells.Shell(record.Shell)] = true
	}
	wanted := map[shells.Shell]bool{}
	for shell := range recorded {
		wanted[shell] = true
	}
	for _, list := range p.flags["hook"] {
		requested, err := shells.ParseList(list)
		if err != nil {
			return r.failure(err) // the parser has checked the names already
		}
		for _, shell := range requested {
			wanted[shell] = true
		}
	}

	pal, err := palette.Load()
	if err != nil {
		return r.failure(err)
	}
	req := engine.Request{Root: s.root.Path, Version: r.env.Version, Options: &options}

	// Owned files, in the order they are written: the shell files first, the
	// fragment last, so that Windows Terminal never sees a profile whose
	// init file is not there yet.
	inits, err := render.InitFiles(pal)
	if err != nil {
		return r.failure(err)
	}
	for _, file := range inits {
		req.Files = append(req.Files, engine.File{Path: filepath.Join(s.root.Path, filepath.FromSlash(file.Name)), Data: file.Data})
	}
	req.Files = append(req.Files, engine.File{Path: s.modePath, Data: settings.Bytes()})

	terminal, onWindows, err := r.windowsTerminal()
	if err != nil {
		return r.failure(err)
	}
	switch {
	case !onWindows:
	case !terminal.installation.Present:
		// R16: no Windows Terminal, no fragment; the plan says so.
		req.Files = append(req.Files, engine.File{Path: terminal.fragmentPath, Skip: engine.NewNote("plan.note.wt-absent")})
	default:
		shell, found := r.profileShell(options.Shell)
		if options.Profile && !found {
			req.Refusals = append(req.Refusals, engine.Refusal{
				Path:   "--shell " + string(shells.Pwsh),
				Reason: engine.NewNote(shells.ReasonAbsent, shells.Pwsh.Executable(host)),
				Advice: engine.NewNote("refusal.advice.shell-auto"),
			})
			break
		}
		data, refusal, err := r.renderFragment(pal, render.FragmentOptions{
			Profile: options.Profile, Shell: shell, Mode: settings.Mode, Flavor: settings.Flavor, Root: s.root,
		})
		switch {
		case err != nil:
			return r.failure(err)
		case refusal != nil:
			req.Refusals = append(req.Refusals, *refusal)
		default:
			req.Files = append(req.Files, engine.File{Path: terminal.fragmentPath, Data: data})
		}
	}

	// Hook lines, only on request (R20-R22), and those of earlier requests.
	for _, shell := range shells.All() {
		if !wanted[shell] {
			continue
		}
		options.Hooks = append(options.Hooks, string(shell))
		hook, refusal, err := shells.Resolve(host, shell, s.root, r.env.Prober)
		switch {
		case err != nil:
			r.warn(Program + ": " + r.T("error.hook", string(shell), err))
			return engine.ExitFailure
		case refusal != nil:
			absent := refusal.Reason.Key == shells.ReasonAbsent || refusal.Reason.Key == shells.ReasonWindowsOnly
			if recorded[shell] && absent {
				// The hook is an earlier request: say how to withdraw it.
				refusal.Advice = engine.NewNote("refusal.advice.drop-hook")
				refusal.Text = Program + " uninstall --hook " + string(shell)
			}
			req.Refusals = append(req.Refusals, *refusal)
		default:
			req.Hooks = append(req.Hooks, hook)
		}
	}

	plan, err := engine.PlanApply(req)
	if err != nil {
		return r.failure(err)
	}
	if plan.Refused() {
		return r.printRefusals(plan.Refusals)
	}
	r.printPlan(plan)

	facts := applyFacts{first: !s.applied, onWindows: onWindows, terminalPresent: terminal.installation.Present, profile: options.Profile, hooks: len(options.Hooks)}
	for _, op := range plan.Ops {
		if !op.Changes() {
			continue
		}
		switch {
		case onWindows && r.samePath(op.Path, terminal.fragmentPath):
			facts.fragmentWritten = true
		case len(op.Added) > 0:
			facts.hooked = append(facts.hooked, op.Path)
		}
	}

	if dryRun {
		if facts.fragmentWritten && facts.profile {
			// Said before the apply, not only after it: Windows Terminal
			// will write to its own settings once it sees the profile.
			r.say(r.T("wt.notice.stub"))
		}
		r.closePlan(plan, true)
		return plan.ExitCode()
	}
	code := r.carryOut(plan)
	if code == engine.ExitFailure {
		return code
	}
	r.closePlan(plan, false)
	r.applyNotices(facts)
	return code
}

// applyFacts is what the sentences after an apply depend on.
type applyFacts struct {
	first           bool // no manifest before this run
	onWindows       bool
	terminalPresent bool // Windows Terminal was detected
	profile         bool // the fragment carries the Serena profile
	fragmentWritten bool // the fragment was created or replaced by this run
	hooks           int  // hook lines recorded or requested
	hooked          []string
}

// applyNotices says what an apply means for the user and what to do next:
// the two notices of R19 after a fragment with a profile was written, and
// otherwise how the files that were written come into use.
func (r *run) applyNotices(f applyFacts) {
	var lines []string
	switch {
	case f.fragmentWritten && f.profile:
		lines = append(lines, r.T("wt.notice.stub"), r.T("wt.notice.open"))
	case f.fragmentWritten:
		lines = append(lines, r.T("apply.note.schemes-only"))
	}
	if len(f.hooked) > 0 {
		lines = append(lines, r.T("apply.note.hooked", strings.Join(f.hooked, ", ")))
	}
	if f.first && f.hooks == 0 {
		switch {
		case !f.onWindows:
			lines = append(lines, r.T("apply.next.hook-unix"), r.T("apply.next.export"))
		case !f.terminalPresent:
			lines = append(lines, r.T("apply.next.hook-windows"))
		}
	}
	if f.first {
		lines = append(lines, r.T("apply.next.uninstall"))
	}
	if len(lines) == 0 {
		return
	}
	r.say("")
	for _, line := range lines {
		r.say(line)
	}
}
