package shells

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ProbeArgs returns the arguments of the one child process sereno ever starts
// (R2, R20): they make PowerShell print the path of the current user's
// profile for the console host and the effective execution policy, without
// loading any profile and without waiting for input. Nothing is written.
func ProbeArgs() []string {
	return []string{
		"-NoProfile", "-NonInteractive", "-Command",
		"$PROFILE.CurrentUserCurrentHost; Get-ExecutionPolicy",
	}
}

// ProbeResult is what the probe reports.
type ProbeResult struct {
	ProfilePath string // $PROFILE.CurrentUserCurrentHost
	Policy      string // Get-ExecutionPolicy, for example RemoteSigned
}

// blocks reports whether the policy keeps a profile script from loading, in
// which case the hook is refused on Windows (R20).
func (r ProbeResult) blocks() bool {
	return strings.EqualFold(r.Policy, "Restricted") || strings.EqualFold(r.Policy, "AllSigned")
}

// Prober asks a PowerShell for its profile path and execution policy. It is an
// interface so that tests never start a process: they pass a fake, or set the
// SERENO_*_PROFILE_PATH variables, in which case no prober is consulted.
type Prober interface {
	// Probe runs the PowerShell at the given full path once.
	Probe(program string) (ProbeResult, error)
}

// policies are the values Get-ExecutionPolicy can print.
var policies = []string{"Restricted", "AllSigned", "RemoteSigned", "Unrestricted", "Bypass", "Undefined", "Default"}

// ParseProbe reads the two lines the probe prints: the profile path, then the
// policy. Anything else is an error, because a path that is guessed wrong
// would make sereno create a file in the wrong place. A question mark in the
// path is such a case: PowerShell prints one for every character the console
// code page cannot express.
func ParseProbe(output string) (ProbeResult, error) {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != 2 {
		return ProbeResult{}, fmt.Errorf("expected the profile path and the execution policy, got %d lines", len(lines))
	}
	result := ProbeResult{ProfilePath: lines[0]}
	for _, policy := range policies {
		if strings.EqualFold(policy, lines[1]) {
			result.Policy = policy
		}
	}
	if result.Policy == "" {
		return ProbeResult{}, fmt.Errorf("%q is not an execution policy", lines[1])
	}
	if !filepath.IsAbs(result.ProfilePath) {
		return ProbeResult{}, fmt.Errorf("the profile path %q is not absolute", result.ProfilePath)
	}
	for _, r := range result.ProfilePath {
		if r < 0x20 || r == '?' || r == utf8.RuneError {
			return ProbeResult{}, fmt.Errorf("the profile path %q has characters the console could not print; set the path in the environment instead", result.ProfilePath)
		}
	}
	result.ProfilePath = filepath.Clean(result.ProfilePath)
	return result, nil
}
