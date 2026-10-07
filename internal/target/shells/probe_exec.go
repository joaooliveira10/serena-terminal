package shells

// This is the only file of the module that imports os/exec, and Probe below
// is the only place that starts a process (R2, R20). A test in internal/policy
// fails if another file imports os/exec.

import (
	"fmt"
	"os/exec"
)

// processProber is the Prober that really runs PowerShell.
type processProber struct{}

// NewProber returns the Prober that starts the PowerShell it is given, once,
// with ProbeArgs(). The process reads the profile path and the execution
// policy; it loads no profile and changes nothing.
func NewProber() Prober { return processProber{} }

// Probe implements Prober.
func (processProber) Probe(program string) (ProbeResult, error) {
	output, err := exec.Command(program, ProbeArgs()...).Output()
	if err != nil {
		return ProbeResult{}, fmt.Errorf("run %s: %w", program, err)
	}
	return ParseProbe(decodeConsole(output))
}
