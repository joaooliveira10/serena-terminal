package cli

import (
	"path/filepath"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/render"
)

// themesDir is where `sereno export` writes below ROOT (R10).
const themesDir = "themes"

// runExport is `sereno export` (R38): six theme files per emulator, written
// to ROOT/themes, where they are owned and removed by uninstall, or to a
// folder the user names with `--to`, where sereno keeps no record of them.
// Nothing is ever written into an emulator's own folders; the command says
// where the files go and which line switches them on.
func runExport(r *run, p parsed) int {
	targets := render.Targets()
	if name, given := p.value("target"); given && name != "all" {
		target, _ := render.ParseTarget(name) // the parser has checked the name
		targets = []render.Target{target}
	}
	pal, err := palette.Load()
	if err != nil {
		return r.failure(err)
	}

	// With `--to` nothing of sereno's folder is read or written: whether
	// there is an installation, and in which state, does not matter.
	var s *site
	destination, custom := p.value("to")
	if custom {
		if destination, err = filepath.Abs(destination); err != nil {
			return r.failure(err)
		}
	} else {
		var ok bool
		if s, ok = r.site(); !ok {
			return engine.ExitFailure
		}
		if !s.applied {
			return r.refuse(r.T("export.not-applied", s.root.Path), r.T("export.not-applied.next"))
		}
		destination = filepath.Join(s.root.Path, themesDir)
	}

	var files []engine.File
	for _, target := range targets {
		themes, err := render.Export(pal, target)
		if err != nil {
			return r.failure(err)
		}
		for _, theme := range themes {
			files = append(files, engine.File{Path: filepath.Join(destination, filepath.FromSlash(theme.Name)), Data: theme.Data})
		}
	}

	var plan *engine.Plan
	if custom {
		plan, err = engine.PlanUntracked(files)
	} else {
		plan, err = engine.PlanApply(engine.Request{Root: s.root.Path, Version: r.env.Version, Files: files, Manifest: s.manifest})
	}
	if err != nil {
		return r.failure(err)
	}
	if plan.Refused() {
		return r.printRefusals(plan.Refusals)
	}
	r.printPlan(plan)
	code := r.carryOut(plan)
	if code == engine.ExitFailure {
		return code
	}
	r.closePlan(plan, false, r.T("plan.incomplete"))

	// Where the files go and how each emulator is told to use them.
	dark, _ := pal.Variant("dark")
	light, _ := pal.Variant("light")
	name := func(target render.Target, v palette.Variant) string {
		rel, _ := render.ExportName(target, v)
		return filepath.Join(destination, filepath.FromSlash(rel))
	}
	r.say("")
	for _, target := range targets {
		folder := filepath.Join(destination, string(target))
		switch target {
		case render.Ghostty:
			r.say(r.T("export.use.ghostty", folder))
			r.say("    theme = dark:" + dark.Name + ",light:" + light.Name)
		case render.Kitty:
			r.say(r.T("export.use.kitty", name(target, dark), name(target, light)))
		case render.WezTerm:
			r.say(r.T("export.use.wezterm", folder))
			r.say("    config.color_scheme = '" + dark.Name + "'")
		case render.Alacritty:
			r.say(r.T("export.use.alacritty"))
			r.say("    import = [\"" + filepath.ToSlash(name(target, dark)) + "\"]")
		}
	}
	r.say(r.T("export.unverified"))
	if custom {
		r.say(r.T("export.untracked"))
	} else {
		r.say(r.T("export.tracked"))
	}
	return code
}
