package cli

import (
	"os"
	"path/filepath"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
	"github.com/joaooliveira10/serena-terminal/internal/target/winterminal"
)

// runUninstall is `sereno uninstall` (R39), the undo of apply: it takes the
// hook lines out again, deletes the files sereno wrote that are still as it
// wrote them, and says what it cannot take back.
func runUninstall(r *run, p parsed) int {
	s, ok := r.site()
	if !ok {
		return engine.ExitFailure
	}
	dryRun := p.has("dry-run")
	if !s.applied {
		r.say(r.T("uninstall.nothing", s.root.Path))
		return engine.ExitOK
	}

	// `--hook LIST` takes out the lines of the named shells and nothing else.
	var only []string
	for _, list := range p.flags["hook"] {
		requested, err := shells.ParseList(list)
		if err != nil {
			return r.failure(err)
		}
		for _, shell := range requested {
			if _, recorded := s.manifest.Hook(string(shell)); !recorded {
				r.say(r.T("uninstall.no-hook", string(shell)))
				continue
			}
			only = append(only, string(shell))
		}
	}
	hooksOnly := p.has("hook")
	if hooksOnly && len(only) == 0 {
		return engine.ExitOK
	}

	// What Windows Terminal remembers is looked at before the fragment goes.
	remembered := !hooksOnly && r.terminalRemembers(s)

	plan, err := engine.PlanUninstall(engine.UninstallRequest{Root: s.root.Path, Version: r.env.Version, Shells: only})
	if err != nil {
		return r.failure(err)
	}
	r.printPlan(plan)
	code := plan.ExitCode()
	if !dryRun {
		if code = r.carryOut(plan); code == engine.ExitFailure {
			return code
		}
	}
	// A dry run ends with the sentence of R13; a real run says first that it
	// is done and then what it could not take back.
	if dryRun {
		if !hooksOnly {
			r.uninstallRemains(s, remembered)
		}
		r.closePlan(plan, true)
		return code
	}
	r.closePlan(plan, false)
	if !hooksOnly {
		r.say("")
		r.uninstallRemains(s, remembered)
	}
	return code
}

// terminalRemembers reports whether Windows Terminal will keep something of
// the Serena profile after the fragment is gone: the entry it wrote into its
// settings.json, or the GUID in its state.json (R39). Only a fragment with a
// profile leaves such traces, and only once Windows Terminal has loaded it.
// When the two files cannot be read, the answer is yes: better one sentence
// too many than a trace nobody mentioned.
func (r *run) terminalRemembers(s *site) bool {
	terminal, onWindows, err := r.windowsTerminal()
	if err != nil || !onWindows || !s.manifest.Options.Profile {
		return false
	}
	if _, owned := r.owns(s.manifest, terminal.fragmentPath); !owned {
		return false
	}
	installation := terminal.installation
	if installation.SettingsPath == "" {
		return false // Windows Terminal has never run for this user
	}
	settings, err := winterminal.ReadSettings(installation.SettingsPath)
	if err != nil {
		return true
	}
	if settings.HasProfile(winterminal.ProfileGUID()) {
		return true
	}
	if installation.StatePath == "" {
		return false
	}
	state, err := winterminal.ReadState(installation.StatePath)
	return err != nil || state.Knows(winterminal.ProfileGUID())
}

// uninstallRemains says what a complete uninstall does not take back (R39).
func (r *run) uninstallRemains(s *site, terminalRemembers bool) {
	if terminalRemembers {
		r.say(r.T("uninstall.remains.terminal"))
	}
	if bin := filepath.Join(s.root.Path, "bin"); isDir(bin) {
		r.say(r.T("uninstall.remains.bin", bin))
	}
	if r.env.Executable != "" {
		r.say(r.T("uninstall.remains.program", r.env.Executable))
	} else {
		r.say(r.T("uninstall.remains.program-unknown"))
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
