package shells

// This is the only file of the module that imports os/exec, and Probe below
// is the only place that starts a process (R2, R20). A test in internal/policy
// fails if another file imports os/exec.

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// processProber is the Prober that really runs PowerShell.
type processProber struct {
	timeout time.Duration
}

// NewProber returns the Prober that starts the PowerShell it is given, once,
// with ProbeArgs(). The process reads the profile path and the execution
// policy; it loads no profile and changes nothing. It gets ProbeTimeout to
// answer and is stopped when it takes longer.
func NewProber() Prober { return processProber{timeout: ProbeTimeout} }

// Probe implements Prober.
func (p processProber) Probe(program string) (ProbeResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	command := exec.CommandContext(ctx, program, ProbeArgs()...)
	// Once the process is stopped, do not wait for ever on its output either.
	command.WaitDelay = time.Second
	output, err := command.Output()
	if ctx.Err() != nil {
		return ProbeResult{}, fmt.Errorf("run %s: %w", program, ErrProbeTimeout)
	}
	if err != nil {
		return ProbeResult{}, fmt.Errorf("run %s: %w", program, err)
	}
	return ParseProbe(decodeConsole(output))
}
