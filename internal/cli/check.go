package cli

import (
	"fmt"
	"io"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/palette"
)

// runCheck is `sereno check`, the contrast gate of R9: one line per variant
// with the lowest contrast of slots 1 to 15 against the background, then
// every value under its floor. `--json` gives the same data for programs and
// `--markdown` the document committed as docs/contrast.md. The exit code is 1
// when any value fails.
func runCheck(r *run, p parsed) int {
	if p.has("json") && p.has("markdown") {
		return r.usageError(findCommand("check"), r.T("usage.exclusive", "--json", "--markdown"))
	}
	pal, err := palette.Load()
	if err != nil {
		return r.failure(err)
	}
	roles, err := palette.LoadRoles()
	if err != nil {
		return r.failure(err)
	}
	return r.checkReport(palette.Check(pal, roles), p)
}

// checkReport prints the result of the gate in the form that was asked for
// and returns the exit code: 1 when any value is under its floor.
func (r *run) checkReport(report palette.Report, p parsed) int {
	code := engine.ExitOK
	if !report.OK {
		code = engine.ExitFailure
	}

	switch {
	case p.has("json"):
		data, err := report.JSON()
		if err != nil {
			return r.failure(err)
		}
		r.env.Stdout.Write(data)
		return code
	case p.has("markdown"):
		// File contents are not translated (R5).
		io.WriteString(r.env.Stdout, report.Markdown())
		return code
	}

	failures := 0
	for _, variant := range report.Variants {
		verdict := r.styled(styleGood, r.T("check.pass"))
		if !variant.OK {
			verdict = r.styled(styleAlert, r.T("check.fail"))
		}
		r.say(fmt.Sprintf("%-12s %s", variant.ID, r.T("check.variant", ratio(variant.Minimum), fmt.Sprint(variant.MinimumSlot), floor(variant.Floor), verdict)))
	}
	for _, variant := range report.Variants {
		for _, m := range variant.Failures {
			failures++
			r.say("  " + r.T("check.failure", variant.ID, m.Subject, m.Against, ratio(m.Ratio), floor(m.Floor)))
		}
	}
	for _, role := range report.Roles {
		if !role.Pass {
			failures++
			r.say("  " + r.T("check.role", string(role.Role), role.SGR, role.Problem))
		}
	}
	if failures == 0 {
		r.say(r.T("check.all-pass"))
	} else {
		r.say(r.styled(styleAlert, r.T("check.some-fail", failures)))
	}
	return code
}
