// Command sereno makes the command line follow the system's dark, light and
// high-contrast setting, and removes everything it wrote with one command.
//
// This file only connects the real machine to the commands: the commands
// themselves, their flags and their texts are in internal/cli.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/joaooliveira10/serena-terminal/internal/cli"
	"github.com/joaooliveira10/serena-terminal/internal/mode"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
)

// version is stamped by the release build with
// -ldflags "-X main.version=<tag>" (R1).
var version = cli.DevVersion

func main() {
	os.Exit(run())
}

func run() int {
	// Ctrl+C ends the context; a running apply then stops between two
	// operations and leaves a manifest that is true (R13).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	executable, err := os.Executable()
	if err != nil {
		executable = ""
	}
	return cli.Run(cli.Env{
		Version:    version,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Host:       platform.Current(),
		Signals:    mode.System(),
		Prober:     shells.NewProber(),
		Terminal:   func() (func(), bool) { return platform.EnableVT(os.Stdout) },
		Context:    ctx,
		Executable: executable,
	}, os.Args[1:])
}
