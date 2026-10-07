// Package shells knows the four shells a hook line can go to (R20): which file
// each one reads at start, what the line looks like there (R21) and when the
// hook must be refused. It produces requests for the engine; it never writes.
package shells

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
)

// Shell is a value of `--hook LIST`.
type Shell string

// The shells of R20, in the order of R3.
const (
	PowerShell Shell = "powershell" // Windows PowerShell 5.1, Windows only
	Pwsh       Shell = "pwsh"       // PowerShell 7
	Bash       Shell = "bash"
	Zsh        Shell = "zsh"
)

// all lists the shells in their canonical order.
var all = []Shell{PowerShell, Pwsh, Bash, Zsh}

// All returns the four shells in the order of R3.
func All() []Shell { return append([]Shell(nil), all...) }

// Parse recognises one shell name.
func Parse(name string) (Shell, bool) {
	for _, shell := range all {
		if string(shell) == name {
			return shell, true
		}
	}
	return "", false
}

// ParseList reads the comma-separated LIST of `--hook`. The result is in
// canonical order without duplicates, so `bash,zsh` and `zsh,bash,zsh` mean
// the same. An empty list or an unknown name is an error (a usage error for
// the command line).
func ParseList(list string) ([]Shell, error) {
	wanted := map[Shell]bool{}
	for _, name := range strings.Split(list, ",") {
		shell, ok := Parse(name)
		if !ok {
			return nil, fmt.Errorf("%q is not one of powershell, pwsh, bash, zsh", name)
		}
		wanted[shell] = true
	}
	var out []Shell
	for _, shell := range all {
		if wanted[shell] {
			out = append(out, shell)
		}
	}
	return out, nil
}

// isPowerShell reports whether the shell is one of the two PowerShells.
func (s Shell) isPowerShell() bool { return s == PowerShell || s == Pwsh }

// InitFile returns the path of the shell's init file below ROOT, with forward
// slashes: shell/init.ps1 for both PowerShells, shell/init.bash and
// shell/init.zsh (R10).
func (s Shell) InitFile() string {
	switch s {
	case Bash:
		return "shell/init.bash"
	case Zsh:
		return "shell/init.zsh"
	}
	return "shell/init.ps1"
}

// Executable returns the program name of a PowerShell on the host, as it is
// looked up on PATH: powershell.exe and pwsh.exe on Windows, pwsh elsewhere.
// bash and zsh are never looked up and have no entry.
func (s Shell) Executable(host platform.Host) string {
	switch {
	case s == PowerShell && host.IsWindows():
		return "powershell.exe"
	case s == Pwsh && host.IsWindows():
		return "pwsh.exe"
	case s == Pwsh:
		return "pwsh"
	}
	return ""
}

// profileEnv returns the variable that names a PowerShell profile directly, so
// that no process has to be started (R20). Tests always set both.
func (s Shell) profileEnv() string {
	if s == PowerShell {
		return "SERENO_PS_PROFILE_PATH"
	}
	return "SERENO_PWSH_PROFILE_PATH"
}

// Message keys of this package. The texts are in internal/i18n.
const (
	NoteBashProfile    = "plan.note.bash-profile"
	ReasonAbsent       = "refusal.shell-absent"
	ReasonWindowsOnly  = "refusal.shell-windows-only"
	ReasonPolicy       = "refusal.policy"
	ReasonRootNotASCII = "refusal.root-not-ascii"
	AdvicePolicy       = "refusal.advice.policy"
)

// PolicyCommand is what a user can run to let PowerShell load a profile.
// sereno prints it with the refusal and never runs it (R20).
const PolicyCommand = "Set-ExecutionPolicy -Scope CurrentUser RemoteSigned"

// ErrRootNotASCII is returned by HookLine when ROOT is not in its default
// place and its path has characters a one-line ASCII hook cannot carry.
var ErrRootNotASCII = errors.New("the path of sereno's folder is not printable ASCII")

// HookLine returns the hook line of R21 for a shell: one ASCII line that tests
// for the init file, sources it, and ends with the marker.
//
// With ROOT in its default place the line names it through the environment
// ($env:LOCALAPPDATA, $LOCALAPPDATA or $HOME), so it is the same for every
// user. Otherwise it carries the absolute path in single quotes, escaped for
// the shell; under Git Bash that path is written with forward slashes.
func HookLine(host platform.Host, shell Shell, root platform.Root) (string, error) {
	var test, source string
	switch {
	case root.Default && shell.isPowerShell():
		path := `"$HOME/.local/share/sereno/` + shell.InitFile() + `"`
		if host.IsWindows() {
			path = `"$env:LOCALAPPDATA\sereno\` + strings.ReplaceAll(shell.InitFile(), "/", `\`) + `"`
		}
		test, source = path, path
	case root.Default:
		path := `"$HOME/.local/share/sereno/` + shell.InitFile() + `"`
		if host.IsWindows() {
			path = `"$LOCALAPPDATA/sereno/` + shell.InitFile() + `"`
		}
		test, source = path, path
	default:
		path := filepath.Join(root.Path, filepath.FromSlash(shell.InitFile()))
		if !shell.isPowerShell() && host.IsWindows() {
			path = strings.ReplaceAll(path, `\`, "/")
		}
		for i := 0; i < len(path); i++ {
			if path[i] < 0x20 || path[i] > 0x7E {
				return "", ErrRootNotASCII
			}
		}
		quoted := "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
		if shell.isPowerShell() {
			quoted = "'" + strings.ReplaceAll(path, "'", "''") + "'"
		}
		test, source = quoted, quoted
	}
	if shell.isPowerShell() {
		return "if (Test-Path -LiteralPath " + test + ") { . " + source + " } " + engine.HookMarker, nil
	}
	return "[ -r " + test + " ] && . " + source + " " + engine.HookMarker, nil
}

// Resolve works out the hook of one shell on this host: the file, the line
// and the line ending for a new file. It returns a refusal instead when the
// hook must not be attempted (R20): Windows PowerShell outside Windows, a
// PowerShell that is not on PATH, an execution policy that would keep the
// profile from loading, or a ROOT the line cannot carry. An error means the
// question could not be answered at all (no home directory, a failed probe).
//
// The prober is consulted for a PowerShell only when its SERENO_*_PROFILE_PATH
// variable is not set; that is the one child process of R2.
func Resolve(host platform.Host, shell Shell, root platform.Root, prober Prober) (engine.Hook, *engine.Refusal, error) {
	hook := engine.Hook{Shell: string(shell)}
	var refusal *engine.Refusal
	var err error
	switch shell {
	case PowerShell, Pwsh:
		hook.Path, refusal, err = profilePath(host, shell, prober)
		hook.NewFileCRLF = host.IsWindows()
	case Bash:
		var home string
		if home, err = host.Home(); err == nil {
			hook.Path = filepath.Join(home, ".bashrc")
			hook.CreateNote = bashProfileNote(host, home)
		}
	case Zsh:
		// ${ZDOTDIR:-$HOME}/.zshrc
		dir := host.Getenv("ZDOTDIR")
		if dir == "" || !filepath.IsAbs(dir) {
			dir, err = host.Home()
		}
		if err == nil {
			hook.Path = filepath.Join(dir, ".zshrc")
		}
	default:
		err = fmt.Errorf("unknown shell %q", shell)
	}
	if err != nil || refusal != nil {
		return engine.Hook{}, refusal, err
	}

	hook.Line, err = HookLine(host, shell, root)
	if errors.Is(err, ErrRootNotASCII) {
		return engine.Hook{}, &engine.Refusal{Path: hook.Path, Reason: engine.NewNote(ReasonRootNotASCII, root.Path)}, nil
	}
	if err != nil {
		return engine.Hook{}, nil, err
	}
	return hook, nil, nil
}

// profilePath finds $PROFILE.CurrentUserCurrentHost of a PowerShell, or the
// reason the hook is refused.
func profilePath(host platform.Host, shell Shell, prober Prober) (string, *engine.Refusal, error) {
	exe := shell.Executable(host)
	if exe == "" {
		return "", &engine.Refusal{Path: string(shell), Reason: engine.NewNote(ReasonWindowsOnly)}, nil
	}
	program, ok := host.LookPath(exe)
	if !ok {
		return "", &engine.Refusal{Path: string(shell), Reason: engine.NewNote(ReasonAbsent, exe)}, nil
	}
	if path := host.Getenv(shell.profileEnv()); path != "" {
		if !filepath.IsAbs(path) {
			return "", nil, fmt.Errorf("%s must be an absolute path, got %q", shell.profileEnv(), path)
		}
		return filepath.Clean(path), nil, nil
	}
	result, err := prober.Probe(program)
	if err != nil {
		return "", nil, fmt.Errorf("ask %s for its profile: %w", exe, err)
	}
	if host.IsWindows() && result.blocks() {
		return "", &engine.Refusal{
			Path:   result.ProfilePath,
			Reason: engine.NewNote(ReasonPolicy, result.Policy),
			Advice: engine.NewNote(AdvicePolicy),
			Text:   PolicyCommand,
		}, nil
	}
	return result.ProfilePath, nil, nil
}

// bashProfileNote returns the note of R22 for Git Bash: when ~/.bashrc is
// about to be created and no login file exists, Git for Windows will create
// ~/.bash_profile on its own the next time it starts.
func bashProfileNote(host platform.Host, home string) engine.Note {
	if !host.IsWindows() {
		return engine.Note{}
	}
	for _, name := range []string{".bash_profile", ".bash_login", ".profile"} {
		if _, err := os.Lstat(filepath.Join(home, name)); err == nil {
			return engine.Note{}
		}
	}
	return engine.NewNote(NoteBashProfile)
}
