package cli

import (
	"fmt"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
)

// The help texts are in the catalogues under keys that are built from the
// names of the commands and of their flags, so that nothing can be added to
// the table of commands without its sentences in both languages:
//
//	help.<command>.summary       one line for the list of commands
//	help.<command>.about         what the command does, in a sentence or two
//	help.<command>.arg           what the argument without a flag means
//	help.<command>.flag.<flag>   what a flag does
//
// HelpKeys lists them; tests in this package and in internal/policy check the
// catalogues against that list.

// HelpKeys returns the message keys the help of a command is made of.
func HelpKeys(name string) []string {
	cmd := findCommand(name)
	if cmd == nil {
		return nil
	}
	keys := []string{"help." + name + ".summary", "help." + name + ".about"}
	if cmd.positional != "" {
		keys = append(keys, "help."+name+".arg")
	}
	for _, flag := range cmd.flags {
		keys = append(keys, "help."+name+".flag."+flag.name)
	}
	return keys
}

// syntaxWidth is the widest flag that still has its explanation on the same
// line; a longer one gets it on the next line.
const syntaxWidth = 28

// entry prints one line of a help list: what to type and what it does.
func (r *run) entry(syntax, text string) {
	if len(syntax) > syntaxWidth {
		r.say("  " + syntax)
		r.say(fmt.Sprintf("  %-*s  %s", syntaxWidth, "", text))
		return
	}
	r.say(fmt.Sprintf("  %-*s  %s", syntaxWidth, syntax, text))
}

// help prints the help of one command, or the general help for nil, on
// standard output.
func (r *run) help(cmd *command) {
	if cmd == nil {
		r.say(Program + " " + r.env.Version + ": " + r.T("help.tagline"))
		r.say("")
		r.say(r.T("usage.label") + " " + Program + " " + generalUsage)
		r.say("")
		r.say(r.styled(styleStrong, r.T("help.commands")))
		for _, each := range commands {
			r.say(fmt.Sprintf("  %-10s %s", each.name, r.catalog.T("help."+each.name+".summary")))
		}
		r.say("")
		r.say(r.styled(styleStrong, r.T("help.global")))
		r.entry("--lang en|pt-BR", r.T("help.global.lang"))
		r.entry("--no-color", r.T("help.global.no-color"))
		r.entry("-h, --help", r.T("help.global.help"))
		r.say("")
		r.say(r.T("help.start"))
		r.say(r.T("help.more"))
		return
	}
	r.say(r.T("usage.label") + " " + Program + " " + cmd.usage)
	r.say("")
	r.say(r.catalog.T("help." + cmd.name + ".about"))
	if cmd.positional == "" && len(cmd.flags) == 0 {
		return
	}
	r.say("")
	if cmd.positional != "" {
		r.entry(cmd.positional, r.catalog.T("help."+cmd.name+".arg"))
	}
	for _, flag := range cmd.flags {
		r.entry(flag.syntax(), r.catalog.T("help."+cmd.name+".flag."+flag.name))
	}
}

// runHelp is `sereno help [COMMAND]`.
func runHelp(r *run, p parsed) int {
	if len(p.args) == 0 {
		r.help(nil)
		return engine.ExitOK
	}
	cmd := findCommand(p.args[0])
	if cmd == nil {
		return r.usageError(nil, r.T("usage.unknown-command", p.args[0]))
	}
	r.help(cmd)
	return engine.ExitOK
}
