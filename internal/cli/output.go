package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
)

// SGR parameters of sereno's own output. They are slot colours and
// attributes only (R5, R8); a test checks each of them against the list of
// what may appear above the emulator.
const (
	styleStrong = "1"  // headings
	styleGood   = "32" // something new, something that passed
	styleChange = "33" // something that changes or needs a look
	styleQuiet  = "90" // nothing happens here
	styleAlert  = "91" // something was left as it is, or failed
)

// styles lists every style, for the test.
var styles = []string{styleStrong, styleGood, styleChange, styleQuiet, styleAlert}

// Escape sequences of sereno's own output.
const (
	escape   = "\x1b"
	sgrReset = escape + "[0m"
)

// say prints one line of a result on standard output.
func (r *run) say(text string) { fmt.Fprintln(r.env.Stdout, text) }

// warn prints one line on standard error: errors, refusals and usage (R4).
func (r *run) warn(text string) { fmt.Fprintln(r.env.Stderr, text) }

// styled wraps text in an SGR sequence when sereno's output may be coloured.
func (r *run) styled(params, text string) string {
	if !r.color || text == "" {
		return text
	}
	return escape + "[" + params + "m" + text + sgrReset
}

// verbStyle chooses the colour of a plan verb.
func verbStyle(op engine.Op) string {
	switch op.Verb {
	case engine.VerbMkdir, engine.VerbCreate, engine.VerbAppend:
		return styleGood
	case engine.VerbReplace, engine.VerbRemove, engine.VerbRmdir:
		return styleChange
	case engine.VerbSkip:
		if op.Incomplete {
			return styleAlert
		}
	}
	return styleQuiet
}

// verbOrder is the order in which the summary of a plan counts the verbs:
// what changes something first, then what stays.
var verbOrder = []engine.Verb{
	engine.VerbMkdir, engine.VerbCreate, engine.VerbReplace, engine.VerbAppend,
	engine.VerbRemove, engine.VerbRmdir, engine.VerbKeep, engine.VerbSkip,
}

// printPlan prints a plan as R14 prescribes: the header, one line per
// operation with its details, and a one-line summary. A dry run and a real
// run both print through here, so they show the same lines.
func (r *run) printPlan(plan *engine.Plan) {
	r.say(r.styled(styleStrong, fmt.Sprintf("%s %s: plan", Program, r.env.Version)))
	for _, op := range plan.Ops {
		lines := op.Lines(r.catalog.T)
		// The first line starts with two spaces and the verb (R14).
		if prefix := "  " + string(op.Verb); strings.HasPrefix(lines[0], prefix) {
			lines[0] = "  " + r.styled(verbStyle(op), string(op.Verb)) + lines[0][len(prefix):]
		}
		for _, line := range lines {
			r.say(line)
		}
	}
	var counts []string
	for _, verb := range verbOrder {
		if n := plan.Count(verb); n > 0 {
			counts = append(counts, fmt.Sprintf("%d %s", n, verb))
		}
	}
	if len(counts) == 0 {
		r.say(r.T("plan.summary.empty"))
		return
	}
	r.say(r.T("plan.summary", strings.Join(counts, ", ")))
}

// closePlan prints what follows a plan: for a dry run the sentence R13 fixes,
// otherwise whether anything was done, and in both cases what the user can do
// about lines that report something left in place (exit code 4). left is that
// sentence; it depends on the command, because after an uninstall there is no
// record left to go on from.
func (r *run) closePlan(plan *engine.Plan, dryRun bool, left string) {
	if plan.Incomplete() > 0 {
		r.say(left)
	}
	switch {
	case dryRun && plan.Changes() > 0:
		r.say(r.T("plan.dry-run"))
		r.say(r.T("plan.nothing-changed"))
	case dryRun:
		r.say(r.T("plan.nothing-changed"))
	case plan.Changes() == 0:
		r.say(r.T("plan.nothing-to-do"))
	default:
		r.say(r.styled(styleGood, r.T("plan.done")))
	}
}

// carryOut is phase 2 for every command that writes: it performs the plan
// and turns a failure into a message and exit code 1. On success the exit
// code is that of the plan: 4 when something was left in place, otherwise 0.
func (r *run) carryOut(plan *engine.Plan) int {
	if err := plan.ApplyContext(r.env.Context); err != nil {
		// A plan without a folder of sereno's is the one of `export --to`.
		return r.writeFailure(err, plan.Root != "")
	}
	return plan.ExitCode()
}

// writeFailure reports a plan that stopped half way. What was carried out
// until then is recorded in the manifest (R13), so the message says how to
// go on; for files sereno keeps no record of (tracked is false) it says
// that instead.
func (r *run) writeFailure(err error, tracked bool) int {
	var sandbox *engine.SandboxError
	switch {
	case errors.Is(err, engine.ErrBusy):
		return r.busy()
	case errors.As(err, &sandbox) && sandbox.Err != nil:
		// The check could not be made: where the path leads is not known.
		r.warn(Program + ": " + r.T("error.sandbox.unchecked", sandbox.Path, engine.SandboxEnv, sandbox.Sandbox, sandbox.Err))
	case errors.As(err, &sandbox):
		r.warn(Program + ": " + r.T("error.sandbox", sandbox.Path, engine.SandboxEnv, sandbox.Sandbox))
	case errors.Is(err, context.Canceled):
		r.warn(Program + ": " + r.T("error.interrupted"))
	default:
		r.warn(Program + ": " + r.T("error.write", err))
	}
	if tracked {
		r.warn(r.T("error.write.next"))
	} else {
		r.warn(r.T("error.write.next.untracked"))
	}
	return engine.ExitFailure
}

// busy reports a command that made way for another sereno (exit code 1). It
// wrote nothing, so there is nothing to finish or to take back: the one
// sentence says all there is to do.
func (r *run) busy() int {
	r.warn(Program + ": " + r.T("error.busy"))
	return engine.ExitFailure
}

// failure reports an error that happened before anything was written. The
// error itself is in the words of the system or of the engine, which are not
// translated; the sentence around it is.
func (r *run) failure(err error) int {
	if errors.Is(err, engine.ErrBusy) {
		return r.busy()
	}
	r.warn(Program + ": " + r.T("error.before", err))
	return engine.ExitFailure
}

// printRefusals prints the reasons a plan was not started (R13): for each,
// the path it is about, the reason, and the way out. Everything goes to
// standard error; nothing was written.
//
// One thing in the way of many files is one reason: a file where a folder
// should be is reported with the first path it stops, not with every one.
func (r *run) printRefusals(refusals []engine.Refusal) int {
	r.warn(Program + ": " + r.T("refusal.header"))
	blockers := map[string]bool{}
	for _, refusal := range refusals {
		if refusal.Reason.Key == engine.ReasonParent && len(refusal.Reason.Args) > 0 {
			blocker := fmt.Sprint(refusal.Reason.Args[0])
			if blockers[blocker] {
				continue
			}
			blockers[blocker] = true
		}
		r.warn("  " + refusal.Path + ": " + refusal.Reason.Text(r.catalog.T))
		advice := refusal.Advice.Text(r.catalog.T)
		if advice == "" {
			advice = r.wayOut(refusal.Reason.Key)
		}
		if advice != "" {
			r.warn("      " + advice)
		}
		if refusal.Text != "" {
			r.warn("      " + refusal.Text)
		}
	}
	return engine.ExitRefused
}

// wayOut returns what the user can do about a refusal that carries no advice
// of its own. Reasons whose sentence already says it get nothing more.
func (r *run) wayOut(reason string) string {
	switch reason {
	case engine.ReasonForeign, engine.ReasonNotRegular, engine.ReasonParent, engine.ReasonBackupBlocked:
		return r.T("refusal.way-out.move")
	case engine.ReasonDifferent:
		return r.T("refusal.way-out.other-folder")
	case engine.ReasonUnreadable:
		return r.T("refusal.way-out.permissions")
	case shells.ReasonAbsent:
		return r.T("refusal.way-out.leave-out")
	case shells.ReasonRootNotASCII:
		return r.T("refusal.way-out.ascii-root")
	}
	return ""
}

// refuse reports a command that will not start for a reason of its own (R4,
// exit code 3): the reason, then the way out, on standard error.
func (r *run) refuse(reason string, wayOut ...string) int {
	r.warn(Program + ": " + reason)
	for _, line := range wayOut {
		r.warn("  " + line)
	}
	return engine.ExitRefused
}

// yesNo spells a boolean in the user's language.
func (r *run) yesNo(value bool) string {
	if value {
		return r.T("word.yes")
	}
	return r.T("word.no")
}
