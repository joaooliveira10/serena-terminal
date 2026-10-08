// Package cli is the command line of sereno: the eight commands of R3, their
// flags, their exit codes (R4) and everything they print, in English and in
// Brazilian Portuguese (R5).
//
// A command computes; it does not reach for the machine itself. Whatever it
// needs from outside comes in through Env: the two output streams, the host
// with its environment, the reader of the system's appearance signals, the
// prober that may ask a PowerShell for its profile, the terminal behind
// standard output and the context that ends on an interrupt. Production code
// fills Env in cmd/sereno; tests fill it by hand and call Run in the same
// process.
//
// The package starts no process and changes no file by itself: files are
// written by plans of package engine only, and package os is used here for
// reading (a test in internal/policy enforces both).
package cli

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/i18n"
	"github.com/joaooliveira10/serena-terminal/internal/mode"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
)

// Program is the name of the command (R1).
const Program = "sereno"

// DevVersion is the version of a build that was not stamped (R1).
const DevVersion = "dev"

// Env is the outside world as a command sees it.
type Env struct {
	// Version is the version stamped into the program; empty means DevVersion.
	Version string
	// Stdout receives results, Stderr errors, refusals and usage (R4).
	Stdout io.Writer
	Stderr io.Writer
	// Host is the machine: operating system, environment, folders.
	Host platform.Host
	// Signals reads the appearance signals of the system (R28); nil means the
	// system offers none.
	Signals mode.Reader
	// Prober asks a PowerShell for its profile path and execution policy
	// (R20). It is consulted only for `--hook powershell` or `--hook pwsh`,
	// and only when the SERENO_*_PROFILE_PATH variable of that shell is not
	// set. nil refuses to ask.
	Prober shells.Prober
	// Terminal prepares standard output for escape sequences and reports
	// whether it is a terminal that interprets them; restore puts back what
	// was changed. nil means standard output is not a terminal.
	Terminal func() (restore func(), ok bool)
	// Context ends when the user interrupts the program; a running apply or
	// uninstall then stops between two operations (R13). nil never ends.
	Context context.Context
	// Executable is the path of the running program, which uninstall names
	// as the one thing it leaves behind (R39); empty when unknown.
	Executable string
}

// run is one invocation: the environment, the language and the terminal
// state that every command shares.
type run struct {
	env     Env
	catalog *i18n.Catalog
	// terminal is true when standard output is a terminal that interprets
	// escape sequences; color is true when sereno's own output may be
	// coloured as well (R5).
	terminal bool
	color    bool
}

// T returns a sentence in the language of the invocation. Keys passed here as
// string literals are checked against the catalogues by internal/policy.
func (r *run) T(key string, args ...any) string { return r.catalog.T(key, args...) }

// getenv reads the host's environment; an unset variable is "".
func (r *run) getenv(key string) string {
	if r.env.Host.Getenv == nil {
		return ""
	}
	return r.env.Host.Getenv(key)
}

// noProber is the Prober of an Env that has none.
type noProber struct{}

func (noProber) Probe(program string) (shells.ProbeResult, error) {
	return shells.ProbeResult{}, fmt.Errorf("%s was not asked: no prober is available", program)
}

// Run carries out one command line (without the program name) and returns the
// exit code of R4.
func Run(env Env, args []string) int {
	if env.Version == "" {
		env.Version = DevVersion
	}
	if env.Stdout == nil {
		env.Stdout = io.Discard
	}
	if env.Stderr == nil {
		env.Stderr = io.Discard
	}
	if env.Signals == nil {
		env.Signals = mode.Unknown{}
	}
	if env.Prober == nil {
		env.Prober = noProber{}
	}
	if env.Context == nil {
		env.Context = context.Background()
	}

	global, rest := splitGlobal(args)
	r := &run{env: env}
	r.catalog = i18n.For(i18n.Select(global.lang, r.getenv("SERENO_LANG"), env.Host.UILanguage()))
	if env.Terminal != nil {
		restore, ok := env.Terminal()
		defer restore()
		r.terminal = ok
		r.color = ok && !global.noColor && !env.Host.NoColor()
	}

	// The language flag is checked after the language is chosen, so that the
	// complaint about it is at least in the language of the system.
	if global.langGiven {
		if _, ok := i18n.Parse(global.lang); !ok {
			return r.usageError(nil, r.T("usage.bad-value", global.lang, "--lang", "en, pt-BR"))
		}
	}
	if len(rest) == 0 {
		r.help(nil)
		return engine.ExitOK
	}
	name := rest[0]
	if strings.HasPrefix(name, "-") {
		return r.usageError(nil, r.T("usage.unknown-flag", name))
	}
	cmd := findCommand(name)
	if cmd == nil {
		return r.usageError(nil, r.T("usage.unknown-command", name))
	}
	if global.help {
		r.help(cmd)
		return engine.ExitOK
	}
	parsed, problem := r.parse(cmd, rest[1:])
	if problem != "" {
		return r.usageError(cmd, problem)
	}
	return cmd.run(r, parsed)
}

// global holds the flags every command accepts (R3).
type global struct {
	lang      string
	langGiven bool
	noColor   bool
	help      bool
}

// splitGlobal takes the global flags out of a command line, wherever they
// stand, and returns the rest in its order.
func splitGlobal(args []string) (global, []string) {
	var g global
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-color":
			g.noColor = true
		case arg == "-h" || arg == "--help":
			g.help = true
		case arg == "--lang":
			g.langGiven = true
			if i+1 < len(args) {
				i++
				g.lang = args[i]
			}
		case strings.HasPrefix(arg, "--lang="):
			g.langGiven = true
			g.lang = strings.TrimPrefix(arg, "--lang=")
		default:
			rest = append(rest, arg)
		}
	}
	return g, rest
}

// versionLine is what `sereno version` prints (R1).
func (r *run) versionLine() string {
	return fmt.Sprintf("%s %s (%s/%s)", Program, r.env.Version, runtime.GOOS, runtime.GOARCH)
}

func runVersion(r *run, _ parsed) int {
	r.say(r.versionLine())
	return engine.ExitOK
}

// usageError reports a command line that cannot be understood (R3): what is
// wrong, the usage of the command and where the details are, on standard
// error; the exit code is 2.
func (r *run) usageError(cmd *command, problem string) int {
	r.warn(Program + ": " + problem)
	if cmd == nil {
		r.warn(r.T("usage.label") + " " + Program + " " + generalUsage)
		r.warn(r.T("usage.hint.general"))
	} else {
		r.warn(r.T("usage.label") + " " + Program + " " + cmd.usage)
		r.warn(r.T("usage.hint.command", cmd.name))
	}
	return engine.ExitUsage
}
