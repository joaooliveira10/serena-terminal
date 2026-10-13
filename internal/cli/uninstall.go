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
	remembered := !hooksOnly && r.terminalRemembers()

	plan, err := engine.PlanUninstall(engine.UninstallRequest{Root: s.root.Path, Version: r.env.Version, Shells: only, Manifest: s.manifest})
	if err != nil {
		return r.failure(err)
	}
	if plan.Refused() {
		return r.printRefusals(plan.Refusals)
	}
	r.printPlan(plan)
	code := plan.ExitCode()
	if !dryRun {
		if code = r.carryOut(plan); code == engine.ExitFailure {
			return code
		}
	}
	// What is said about a skip line. `uninstall --hook` keeps the record of
	// a line it could not take out, so the command can be run again. A whole
	// uninstall deletes the manifest: what it leaves is the user's to remove.
	left := r.T("plan.incomplete")
	if !hooksOnly {
		left = r.T("plan.incomplete.uninstall")
		if dryRun {
			left = r.T("plan.incomplete.uninstall.dry-run")
		}
	}
	// A dry run ends with the sentence of R13; a real run says first that it
	// is done and then what it could not take back.
	if dryRun {
		r.uninstallGoneHooks(plan)
		if !hooksOnly {
			r.uninstallRemains(s, remembered)
		}
		r.closePlan(plan, true, left)
		return code
	}
	r.closePlan(plan, false, left)
	if !hooksOnly || len(plan.GoneHooks) > 0 {
		r.say("")
	}
	r.uninstallGoneHooks(plan)
	if !hooksOnly {
		r.uninstallRemains(s, remembered)
	}
	return code
}

// uninstallGoneHooks says which hook lines an uninstall, whole or of hook
// lines only, could not look for: the file the line was recorded in is not
// there. A file the user deleted needs nothing more. One that was moved (a
// PowerShell profile goes with its Documents folder) has the line in it
// still, and sereno does not go looking for it: the sentence names the shell
// and the recorded place and leaves the line to the user (R39).
func (r *run) uninstallGoneHooks(plan *engine.Plan) {
	for _, record := range plan.GoneHooks {
		r.say(r.T("uninstall.remains.hook-file-gone", record.Shell, record.Path))
	}
}

// terminalRemembers reports whether Windows Terminal will keep something of
// the Serena profile after the fragment is gone: the entry it wrote into its
// settings.json, or the GUID in its state.json (R39). What counts is what
// those two files hold, not what sereno's fragment looks like today: an apply
// with --no-profile takes the profile out of the fragment and leaves the
// entry of Windows Terminal where it is. When the two files cannot be read,
// the answer is yes: better one sentence too many than a trace nobody
// mentioned.
func (r *run) terminalRemembers() bool {
	terminal, onWindows, err := r.windowsTerminal()
	if err != nil || !onWindows {
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
