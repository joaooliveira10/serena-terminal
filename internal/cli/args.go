package cli

import (
	"strings"

	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/render"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
)

// flagSpec describes one flag of a command.
type flagSpec struct {
	// name is the flag without its two dashes.
	name string
	// arg is how the value is shown in the usage line and in the help: a
	// placeholder such as LIST or DIR, or the allowed values joined by a bar.
	// Empty for a switch.
	arg string
	// values lists the allowed values when they form a closed set.
	values []string
	// list is true when the value is a comma-separated list of values.
	list bool
}

// syntax returns the flag as the help shows it: "--hook LIST".
func (f flagSpec) syntax() string {
	if f.arg == "" {
		return "--" + f.name
	}
	return "--" + f.name + " " + f.arg
}

// command is one command of R3.
type command struct {
	name string
	// usage is the command line of R3 without the program name. Command
	// names, flags and values are not translated (R5).
	usage string
	flags []flagSpec
	// positional says what may follow the command without a flag: the
	// placeholder of the help and, when they form a closed set, the values.
	// At most one positional argument is accepted.
	positional       string
	positionalValues []string
	run              func(*run, parsed) int
}

func (c *command) flag(name string) *flagSpec {
	for i := range c.flags {
		if c.flags[i].name == name {
			return &c.flags[i]
		}
	}
	return nil
}

// generalUsage is the usage line when no command is known.
const generalUsage = "<command> [flags]"

// Values of the flags, in the order R3 gives them.
var (
	shellValues  = []string{"auto", string(shells.PowerShell), string(shells.Pwsh)}
	scopeValues  = []string{"profile", "always", "off"}
	switchValues = []string{"on", "off"}
	modeValues   = []string{"auto", "dark", "light", "hc", "hc-light"}
	flavorValues = []string{"soft", "vivid"}
)

func hookValues() []string {
	var names []string
	for _, shell := range shells.All() {
		names = append(names, string(shell))
	}
	return names
}

func variantValues() []string {
	var ids []string
	for _, identity := range palette.Identities() {
		ids = append(ids, identity.ID)
	}
	return ids
}

func targetValues() []string {
	names := []string{"all"}
	for _, target := range render.Targets() {
		names = append(names, string(target))
	}
	return names
}

// commands is the table of R3, in the order the help lists it. It is filled
// by init because the commands refer to the table themselves (help).
var commands []*command

func init() {
	hook := flagSpec{name: "hook", arg: "LIST", values: hookValues(), list: true}
	dryRun := flagSpec{name: "dry-run"}
	commands = []*command{
		{
			name:  "apply",
			usage: "apply [--dry-run] [--hook LIST] [--shell auto|powershell|pwsh] [--no-profile] [--keys profile|always|off] [--prompt on|off]",
			flags: []flagSpec{
				dryRun, hook,
				{name: "shell", arg: strings.Join(shellValues, "|"), values: shellValues},
				{name: "no-profile"},
				{name: "keys", arg: strings.Join(scopeValues, "|"), values: scopeValues},
				{name: "prompt", arg: strings.Join(switchValues, "|"), values: switchValues},
			},
			run: runApply,
		},
		{
			name:  "uninstall",
			usage: "uninstall [--dry-run] [--hook LIST]",
			flags: []flagSpec{dryRun, hook},
			run:   runUninstall,
		},
		{
			name:  "theme",
			usage: "theme [auto|dark|light|hc|hc-light] [--flavor soft|vivid] [--inband profile|always|off] [--show] [--paint VARIANT] [--reset]",
			flags: []flagSpec{
				{name: "flavor", arg: strings.Join(flavorValues, "|"), values: flavorValues},
				{name: "inband", arg: strings.Join(scopeValues, "|"), values: scopeValues},
				{name: "show"},
				{name: "paint", arg: "VARIANT", values: variantValues()},
				{name: "reset"},
			},
			positional:       strings.Join(modeValues, "|"),
			positionalValues: modeValues,
			run:              runTheme,
		},
		{
			name:  "status",
			usage: "status [--json]",
			flags: []flagSpec{{name: "json"}},
			run:   runStatus,
		},
		{
			name:  "check",
			usage: "check [--json|--markdown]",
			flags: []flagSpec{{name: "json"}, {name: "markdown"}},
			run:   runCheck,
		},
		{
			name:  "export",
			usage: "export [--target all|ghostty|kitty|wezterm|alacritty] [--to DIR]",
			flags: []flagSpec{
				{name: "target", arg: strings.Join(targetValues(), "|"), values: targetValues()},
				{name: "to", arg: "DIR"},
			},
			run: runExport,
		},
		{
			name:  "version",
			usage: "version",
			run:   runVersion,
		},
		{
			name:       "help",
			usage:      "help [COMMAND]",
			positional: "COMMAND",
			run:        runHelp,
		},
	}
}

// Commands returns the names of the commands of R3 in the order the help
// lists them.
func Commands() []string {
	names := make([]string, len(commands))
	for i, cmd := range commands {
		names[i] = cmd.name
	}
	return names
}

func findCommand(name string) *command {
	for _, cmd := range commands {
		if cmd.name == name {
			return cmd
		}
	}
	return nil
}

// parsed is a command line after its command name: the flags that were given
// with their values, and the arguments that are not flags.
type parsed struct {
	flags map[string][]string
	args  []string
}

// has reports whether a flag was given.
func (p parsed) has(name string) bool { return len(p.flags[name]) > 0 }

// value returns the value of a flag that takes one; given twice, the last
// one counts.
func (p parsed) value(name string) (string, bool) {
	values := p.flags[name]
	if len(values) == 0 {
		return "", false
	}
	return values[len(values)-1], true
}

// parse reads the flags and arguments of a command. The second result is the
// complaint, in the user's language, when the line cannot be understood: an
// unknown flag, a missing or unknown value, an argument too many (R3).
func (r *run) parse(cmd *command, args []string) (parsed, string) {
	p := parsed{flags: map[string][]string{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			p.args = append(p.args, arg)
			continue
		}
		bare := strings.TrimLeft(arg, "-")
		name, value, hasValue := strings.Cut(bare, "=")
		spec := cmd.flag(name)
		switch {
		case spec != nil && len(arg)-len(bare) != 2:
			// One dash where two are needed: say which flag was meant.
			return p, r.T("usage.did-you-mean", strings.SplitN(arg, "=", 2)[0], cmd.name, "--"+name)
		case spec == nil:
			return p, r.T("usage.unknown-flag-for", strings.SplitN(arg, "=", 2)[0], cmd.name)
		case spec.arg == "" && hasValue:
			return p, r.T("usage.no-value", "--"+name)
		case spec.arg == "":
			p.flags[name] = []string{"true"}
			continue
		}
		if !hasValue {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return p, r.T("usage.needs-value", "--"+name, spec.arg)
			}
			i++
			value = args[i]
		}
		if problem := r.checkValue(spec, value); problem != "" {
			return p, problem
		}
		p.flags[name] = append(p.flags[name], value)
	}

	switch {
	case len(p.args) > 1 || (len(p.args) == 1 && cmd.positional == ""):
		extra := p.args[len(p.args)-1]
		return p, r.T("usage.unexpected", extra, cmd.name)
	case len(p.args) == 1 && cmd.positionalValues != nil && !contains(cmd.positionalValues, p.args[0]):
		return p, r.T("usage.bad-positional", p.args[0], strings.Join(cmd.positionalValues, ", "))
	}
	return p, ""
}

// checkValue compares the value of a flag with the values it allows.
func (r *run) checkValue(spec *flagSpec, value string) string {
	if spec.values == nil {
		if value == "" {
			return r.T("usage.needs-value", "--"+spec.name, spec.arg)
		}
		return ""
	}
	items := []string{value}
	if spec.list {
		items = strings.Split(value, ",")
	}
	for _, item := range items {
		if !contains(spec.values, item) {
			return r.T("usage.bad-value", item, "--"+spec.name, strings.Join(spec.values, ", "))
		}
	}
	return ""
}

func contains(list []string, item string) bool {
	for _, known := range list {
		if known == item {
			return true
		}
	}
	return false
}
