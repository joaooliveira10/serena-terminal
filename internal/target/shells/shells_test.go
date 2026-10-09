package shells

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/i18n"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

func host(goos string, vars map[string]string) platform.Host {
	return platform.Host{OS: goos, Getenv: testenv.Getenv(vars)}
}

// fakeProber answers from a table and records what it was asked.
type fakeProber struct {
	result ProbeResult
	err    error
	asked  []string
}

func (f *fakeProber) Probe(program string) (ProbeResult, error) {
	f.asked = append(f.asked, program)
	return f.result, f.err
}

// noProber fails the test when a process would have been started.
type noProber struct{ t *testing.T }

func (n noProber) Probe(program string) (ProbeResult, error) {
	n.t.Errorf("the prober was asked to run %s", program)
	return ProbeResult{}, errors.New("no process in this test")
}

func TestParseList(t *testing.T) {
	tests := []struct {
		in   string
		want []Shell
	}{
		{"bash", []Shell{Bash}},
		{"zsh,bash", []Shell{Bash, Zsh}},
		{"zsh,pwsh,bash,powershell", []Shell{PowerShell, Pwsh, Bash, Zsh}},
		{"pwsh,pwsh", []Shell{Pwsh}},
	}
	for _, test := range tests {
		got, err := ParseList(test.in)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Errorf("ParseList(%q) = %v, %v; want %v", test.in, got, err, test.want)
		}
	}
	for _, bad := range []string{"", "fish", "bash,", ",bash", "bash, zsh", "Bash", "bash;zsh", "all"} {
		if got, err := ParseList(bad); err == nil {
			t.Errorf("ParseList(%q) = %v, want an error", bad, got)
		}
	}
	if !reflect.DeepEqual(All(), []Shell{PowerShell, Pwsh, Bash, Zsh}) {
		t.Errorf("All = %v", All())
	}
}

func TestInitFileAndExecutable(t *testing.T) {
	windows, linux := host(platform.Windows, nil), host(platform.Linux, nil)
	tests := []struct {
		shell              Shell
		init               string
		onWindows, onLinux string
	}{
		{PowerShell, "shell/init.ps1", "powershell.exe", ""},
		{Pwsh, "shell/init.ps1", "pwsh.exe", "pwsh"},
		{Bash, "shell/init.bash", "", ""},
		{Zsh, "shell/init.zsh", "", ""},
	}
	for _, test := range tests {
		if got := test.shell.InitFile(); got != test.init {
			t.Errorf("%s: InitFile = %q", test.shell, got)
		}
		if got := test.shell.Executable(windows); got != test.onWindows {
			t.Errorf("%s: Executable on Windows = %q", test.shell, got)
		}
		if got := test.shell.Executable(linux); got != test.onLinux {
			t.Errorf("%s: Executable on Linux = %q", test.shell, got)
		}
	}
}

// TestHookLineDefaultRoot spells out the lines of R21 letter by letter.
func TestHookLineDefaultRoot(t *testing.T) {
	root := platform.Root{Path: filepath.Join(testenv.TempDir(t), "sereno"), Default: true}
	tests := []struct {
		os    string
		shell Shell
		want  string
	}{
		{platform.Windows, PowerShell, `if ($ExecutionContext.SessionState.LanguageMode -eq 'FullLanguage' -and [System.IO.File]::Exists("$env:LOCALAPPDATA\sereno\shell\init.ps1")) { . "$env:LOCALAPPDATA\sereno\shell\init.ps1" } # sereno:hook`},
		{platform.Windows, Pwsh, `if ($ExecutionContext.SessionState.LanguageMode -eq 'FullLanguage' -and [System.IO.File]::Exists("$env:LOCALAPPDATA\sereno\shell\init.ps1")) { . "$env:LOCALAPPDATA\sereno\shell\init.ps1" } # sereno:hook`},
		{platform.Windows, Bash, `[ -r "$LOCALAPPDATA/sereno/shell/init.bash" ] && . "$LOCALAPPDATA/sereno/shell/init.bash" # sereno:hook`},
		{platform.Windows, Zsh, `[ -r "$LOCALAPPDATA/sereno/shell/init.zsh" ] && . "$LOCALAPPDATA/sereno/shell/init.zsh" # sereno:hook`},
		{platform.Linux, Bash, `[ -r "$HOME/.local/share/sereno/shell/init.bash" ] && . "$HOME/.local/share/sereno/shell/init.bash" # sereno:hook`},
		{platform.Darwin, Zsh, `[ -r "$HOME/.local/share/sereno/shell/init.zsh" ] && . "$HOME/.local/share/sereno/shell/init.zsh" # sereno:hook`},
		{platform.Linux, Pwsh, `if ($ExecutionContext.SessionState.LanguageMode -eq 'FullLanguage' -and [System.IO.File]::Exists("$HOME/.local/share/sereno/shell/init.ps1")) { . "$HOME/.local/share/sereno/shell/init.ps1" } # sereno:hook`},
		{platform.Darwin, Pwsh, `if ($ExecutionContext.SessionState.LanguageMode -eq 'FullLanguage' -and [System.IO.File]::Exists("$HOME/.local/share/sereno/shell/init.ps1")) { . "$HOME/.local/share/sereno/shell/init.ps1" } # sereno:hook`},
	}
	for _, test := range tests {
		got, err := HookLine(host(test.os, nil), test.shell, root)
		if err != nil || got != test.want {
			t.Errorf("%s on %s:\n got %s (%v)\nwant %s", test.shell, test.os, got, err, test.want)
		}
	}
}

// TestHookLineCustomRoot covers "the absolute path is used in single quotes
// with quotes escaped for that shell".
func TestHookLineCustomRoot(t *testing.T) {
	base := testenv.TempDir(t)
	plain := filepath.Join(base, "custom root", "sereno")
	quoted := filepath.Join(base, "it's", "sereno")
	slashes := func(path string) string { return strings.ReplaceAll(path, `\`, "/") }

	tests := []struct {
		name  string
		os    string
		shell Shell
		root  string
		path  string // the quoted path expected inside the line
	}{
		{"powershell", platform.Windows, PowerShell, plain, "'" + filepath.Join(plain, "shell", "init.ps1") + "'"},
		{"pwsh on linux", platform.Linux, Pwsh, plain, "'" + filepath.Join(plain, "shell", "init.ps1") + "'"},
		{"powershell with a quote", platform.Windows, Pwsh, quoted,
			"'" + strings.ReplaceAll(filepath.Join(quoted, "shell", "init.ps1"), "'", "''") + "'"},
		{"bash on linux", platform.Linux, Bash, plain, "'" + filepath.Join(plain, "shell", "init.bash") + "'"},
		{"zsh with a quote", platform.Darwin, Zsh, quoted,
			"'" + strings.ReplaceAll(filepath.Join(quoted, "shell", "init.zsh"), "'", `'\''`) + "'"},
		{"git bash gets forward slashes", platform.Windows, Bash, plain, "'" + slashes(filepath.Join(plain, "shell", "init.bash")) + "'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := HookLine(host(test.os, nil), test.shell, platform.Root{Path: test.root})
			if err != nil {
				t.Fatal(err)
			}
			want := "[ -r " + test.path + " ] && . " + test.path + " # sereno:hook"
			if test.shell.isPowerShell() {
				want = "if ($ExecutionContext.SessionState.LanguageMode -eq 'FullLanguage' -and [System.IO.File]::Exists(" + test.path + ")) { . " + test.path + " } # sereno:hook"
			}
			if got != want {
				t.Errorf("\n got %s\nwant %s", got, want)
			}
			if strings.Contains(got, `"`) {
				t.Error("a custom root must be in single quotes, where nothing is expanded")
			}
		})
	}

	for _, bad := range []string{filepath.Join(base, "Jo\u00e3o", "sereno"), filepath.Join(base, "tab\there"), filepath.Join(base, "line\nbreak")} {
		if _, err := HookLine(host(platform.Linux, nil), Bash, platform.Root{Path: bad}); !errors.Is(err, ErrRootNotASCII) {
			t.Errorf("HookLine with root %q: %v, want ErrRootNotASCII", bad, err)
		}
	}
}

// TestHookLinesPassTheEngine makes sure every line this package can produce
// is one the engine accepts: one line of printable ASCII ending in the marker.
func TestHookLinesPassTheEngine(t *testing.T) {
	home := testenv.New(t)
	roots := []platform.Root{
		{Path: filepath.Join(home.LocalAppData, "sereno"), Default: true},
		{Path: filepath.Join(home.Dir, "it's custom", "sereno")},
	}
	for _, goos := range []string{platform.Windows, platform.Linux, platform.Darwin} {
		for _, shell := range All() {
			for _, root := range roots {
				line, err := HookLine(host(goos, nil), shell, root)
				if err != nil {
					t.Fatal(err)
				}
				req := engine.Request{Root: root.Path, Version: "test", Hooks: []engine.Hook{
					{Shell: string(shell), Path: filepath.Join(home.Dir, "rc"), Line: line},
				}}
				if _, err := engine.PlanApply(req); err != nil {
					t.Errorf("%s on %s: the engine rejects %q: %v", shell, goos, line, err)
				}
			}
		}
	}
}

func TestResolveBashAndZsh(t *testing.T) {
	home := testenv.New(t)
	other := filepath.Join(home.Dir, "profile")
	zdot := filepath.Join(home.Dir, "zdot")
	root := platform.Root{Path: filepath.Join(home.Dir, "root"), Default: true}
	tests := []struct {
		name  string
		os    string
		vars  map[string]string
		shell Shell
		path  string
		note  string
	}{
		{"bash on linux", platform.Linux, map[string]string{"HOME": home.Dir}, Bash, filepath.Join(home.Dir, ".bashrc"), ""},
		{"bash on windows uses HOME", platform.Windows, map[string]string{"HOME": home.Dir, "USERPROFILE": other}, Bash, filepath.Join(home.Dir, ".bashrc"), NoteBashProfile},
		{"bash on windows falls back to USERPROFILE", platform.Windows, map[string]string{"USERPROFILE": other}, Bash, filepath.Join(other, ".bashrc"), NoteBashProfile},
		{"zsh", platform.Darwin, map[string]string{"HOME": home.Dir}, Zsh, filepath.Join(home.Dir, ".zshrc"), ""},
		{"zsh with ZDOTDIR", platform.Linux, map[string]string{"HOME": home.Dir, "ZDOTDIR": zdot}, Zsh, filepath.Join(zdot, ".zshrc"), ""},
		{"zsh with ZDOTDIR and no HOME", platform.Linux, map[string]string{"ZDOTDIR": zdot}, Zsh, filepath.Join(zdot, ".zshrc"), ""},
		{"zsh ignores a relative ZDOTDIR", platform.Linux, map[string]string{"HOME": home.Dir, "ZDOTDIR": "zdot"}, Zsh, filepath.Join(home.Dir, ".zshrc"), ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := host(test.os, test.vars)
			hook, refusal, err := Resolve(h, test.shell, root, noProber{t})
			if err != nil || refusal != nil {
				t.Fatalf("Resolve: refusal %+v, err %v", refusal, err)
			}
			line, _ := HookLine(h, test.shell, root)
			want := engine.Hook{Shell: string(test.shell), Path: test.path, Line: line}
			if test.note != "" {
				want.CreateNote = engine.NewNote(test.note)
			}
			if !reflect.DeepEqual(hook, want) {
				t.Errorf("hook = %+v\nwant %+v", hook, want)
			}
		})
	}

	// Any login file means Git for Windows will not create one: no note.
	for _, login := range []string{".bash_profile", ".bash_login", ".profile"} {
		t.Run("note with "+login, func(t *testing.T) {
			h := testenv.New(t)
			if err := os.WriteFile(filepath.Join(h.Dir, login), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			hook, _, err := Resolve(host(platform.Windows, map[string]string{"HOME": h.Dir}), Bash, root, noProber{t})
			if err != nil || !hook.CreateNote.IsZero() {
				t.Errorf("note = %q, err %v", hook.CreateNote.Key, err)
			}
		})
	}

	for _, shell := range []Shell{Bash, Zsh} {
		if _, _, err := Resolve(host(platform.Linux, nil), shell, root, noProber{t}); err == nil {
			t.Errorf("%s without a home directory was resolved", shell)
		}
	}
	if _, _, err := Resolve(host(platform.Linux, map[string]string{"HOME": home.Dir}), "fish", root, noProber{t}); err == nil {
		t.Error("an unknown shell was resolved")
	}
}

// programs creates empty files that stand for PowerShell on a PATH of the
// test, spelled so that LookPath finds them on every system.
func programs(t *testing.T, home *testenv.Home, names ...string) string {
	t.Helper()
	bin := filepath.Join(home.Dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(bin, name), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

func TestResolvePowerShellOnWindows(t *testing.T) {
	home := testenv.New(t)
	bin := programs(t, home, "powershell.exe", "pwsh.exe")
	root := platform.Root{Path: filepath.Join(home.LocalAppData, "sereno"), Default: true}
	profile := filepath.Join(home.Dir, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
	base := map[string]string{"PATH": bin, "LOCALAPPDATA": home.LocalAppData}
	with := func(extra map[string]string) map[string]string {
		vars := map[string]string{}
		for k, v := range base {
			vars[k] = v
		}
		for k, v := range extra {
			vars[k] = v
		}
		return vars
	}

	t.Run("the variable names the profile and no process starts", func(t *testing.T) {
		for shell, variable := range map[Shell]string{PowerShell: "SERENO_PS_PROFILE_PATH", Pwsh: "SERENO_PWSH_PROFILE_PATH"} {
			h := host(platform.Windows, with(map[string]string{variable: profile}))
			hook, refusal, err := Resolve(h, shell, root, noProber{t})
			if err != nil || refusal != nil {
				t.Fatalf("%s: refusal %+v, err %v", shell, refusal, err)
			}
			if hook.Path != profile || !hook.NewFileCRLF || hook.Shell != string(shell) || !hook.CreateNote.IsZero() {
				t.Errorf("%s: hook = %+v", shell, hook)
			}
			if !strings.HasPrefix(hook.Line, `if ($ExecutionContext.SessionState.LanguageMode -eq 'FullLanguage' -and [System.IO.File]::Exists("$env:LOCALAPPDATA\sereno\shell\init.ps1"))`) {
				t.Errorf("%s: line = %s", shell, hook.Line)
			}
		}
	})

	t.Run("a recorded path is the answer and no process starts", func(t *testing.T) {
		recorded := filepath.Join(home.Dir, "OneDrive", "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
		// The path and nothing else: no policy, so none is judged. It is
		// handed a program that is there and would answer, as the real
		// prober shows: the stand-in process of this package, with another
		// profile and a policy that refuses. Had it been started, the answer
		// would be that one.
		program := standIn(t, profile+"\r\nRestricted\r\n")
		if started, err := NewProber().Probe(program); err != nil || started != (ProbeResult{ProfilePath: profile, Policy: "Restricted"}) {
			t.Fatalf("the stand-in process does not answer when it is started: %+v, %v", started, err)
		}
		result, err := Recorded(recorded).Probe(program)
		if err != nil || result != (ProbeResult{ProfilePath: recorded}) {
			t.Errorf("Recorded(%q).Probe = %+v, %v", recorded, result, err)
		}
		for _, shell := range []Shell{PowerShell, Pwsh} {
			hook, refusal, err := Resolve(host(platform.Windows, base), shell, root, Recorded(recorded))
			if err != nil || refusal != nil {
				t.Fatalf("%s: refusal %+v, err %v", shell, refusal, err)
			}
			if hook.Path != recorded || !hook.NewFileCRLF || hook.Shell != string(shell) {
				t.Errorf("%s: hook = %+v", shell, hook)
			}
		}
		// What comes before any prober still does: the variable that names
		// the profile, and the look for the shell on PATH.
		h := host(platform.Windows, with(map[string]string{"SERENO_PS_PROFILE_PATH": profile}))
		if hook, refusal, err := Resolve(h, PowerShell, root, Recorded(recorded)); err != nil || refusal != nil || hook.Path != profile {
			t.Errorf("with the variable set: hook %+v, refusal %+v, err %v", hook, refusal, err)
		}
		h = host(platform.Windows, with(map[string]string{"PATH": filepath.Join(home.Dir, "no-such-folder")}))
		if _, refusal, err := Resolve(h, Pwsh, root, Recorded(recorded)); err != nil || refusal == nil || refusal.Reason.Key != ReasonAbsent {
			t.Errorf("with the shell gone: refusal %+v, err %v", refusal, err)
		}
	})

	t.Run("the other shell's variable does not count", func(t *testing.T) {
		prober := &fakeProber{result: ProbeResult{ProfilePath: profile, Policy: "RemoteSigned"}}
		h := host(platform.Windows, with(map[string]string{"SERENO_PWSH_PROFILE_PATH": filepath.Join(home.Dir, "other.ps1")}))
		hook, refusal, err := Resolve(h, PowerShell, root, prober)
		if err != nil || refusal != nil || hook.Path != profile {
			t.Fatalf("hook %+v, refusal %+v, err %v", hook, refusal, err)
		}
		if want := []string{filepath.Join(bin, "powershell.exe")}; !reflect.DeepEqual(prober.asked, want) {
			t.Errorf("the prober was asked %v, want %v (the full path, once)", prober.asked, want)
		}
	})

	for _, policy := range []string{"Restricted", "AllSigned", "allsigned"} {
		t.Run("policy "+policy+" is a refusal", func(t *testing.T) {
			prober := &fakeProber{result: ProbeResult{ProfilePath: profile, Policy: policy}}
			_, refusal, err := Resolve(host(platform.Windows, base), Pwsh, root, prober)
			if err != nil || refusal == nil {
				t.Fatalf("refusal %+v, err %v", refusal, err)
			}
			want := engine.Refusal{
				Path:   profile,
				Reason: engine.NewNote(ReasonPolicy, policy),
				Advice: engine.NewNote(AdvicePolicy),
				Text:   "Set-ExecutionPolicy -Scope CurrentUser RemoteSigned",
			}
			if !reflect.DeepEqual(*refusal, want) {
				t.Errorf("refusal = %+v\nwant %+v", *refusal, want)
			}
		})
	}
	for _, policy := range []string{"RemoteSigned", "Unrestricted", "Bypass"} {
		t.Run("policy "+policy+" is fine", func(t *testing.T) {
			prober := &fakeProber{result: ProbeResult{ProfilePath: profile, Policy: policy}}
			hook, refusal, err := Resolve(host(platform.Windows, base), PowerShell, root, prober)
			if err != nil || refusal != nil || hook.Path != profile {
				t.Errorf("hook %+v, refusal %+v, err %v", hook, refusal, err)
			}
		})
	}

	t.Run("not on PATH", func(t *testing.T) {
		empty := filepath.Join(home.Dir, "empty")
		if err := os.Mkdir(empty, 0o755); err != nil {
			t.Fatal(err)
		}
		// The variable is set: the shell must still exist to get a hook.
		h := host(platform.Windows, with(map[string]string{"PATH": empty, "SERENO_PWSH_PROFILE_PATH": profile}))
		_, refusal, err := Resolve(h, Pwsh, root, noProber{t})
		if err != nil || refusal == nil {
			t.Fatalf("refusal %+v, err %v", refusal, err)
		}
		if refusal.Path != "pwsh" || !reflect.DeepEqual(refusal.Reason, engine.NewNote(ReasonAbsent, "pwsh.exe")) {
			t.Errorf("refusal = %+v", *refusal)
		}
	})

	t.Run("a failed probe is an error", func(t *testing.T) {
		prober := &fakeProber{err: errors.New("exit status 1")}
		if _, refusal, err := Resolve(host(platform.Windows, base), PowerShell, root, prober); err == nil || refusal != nil {
			t.Errorf("refusal %+v, err %v", refusal, err)
		}
	})

	t.Run("a relative path in the variable is an error", func(t *testing.T) {
		h := host(platform.Windows, with(map[string]string{"SERENO_PS_PROFILE_PATH": "profile.ps1"}))
		if _, _, err := Resolve(h, PowerShell, root, noProber{t}); err == nil {
			t.Error("a relative profile path was accepted")
		}
	})

	t.Run("a root the line cannot carry", func(t *testing.T) {
		h := host(platform.Windows, with(map[string]string{"SERENO_PS_PROFILE_PATH": profile}))
		odd := platform.Root{Path: filepath.Join(home.Dir, "Jo\u00e3o", "sereno")}
		_, refusal, err := Resolve(h, PowerShell, odd, noProber{t})
		if err != nil || refusal == nil || refusal.Reason.Key != ReasonRootNotASCII || refusal.Path != profile {
			t.Errorf("refusal %+v, err %v", refusal, err)
		}
	})
}

func TestResolvePowerShellElsewhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Files have no executable bit here, so a Unix PATH lookup finds
		// nothing; the rules that do not need the lookup are still tested.
		t.Log("the lookup of pwsh on a Unix PATH is tested on macOS and Linux")
	}
	home := testenv.New(t)
	bin := programs(t, home, "pwsh")
	root := platform.Root{Path: filepath.Join(home.DataHome, "sereno"), Default: true}
	profile := filepath.Join(home.Dir, ".config", "powershell", "Microsoft.PowerShell_profile.ps1")
	vars := map[string]string{"PATH": bin, "HOME": home.Dir}

	t.Run("Windows PowerShell exists only on Windows", func(t *testing.T) {
		_, refusal, err := Resolve(host(platform.Linux, vars), PowerShell, root, noProber{t})
		if err != nil || refusal == nil || refusal.Reason.Key != ReasonWindowsOnly || refusal.Path != "powershell" {
			t.Errorf("refusal %+v, err %v", refusal, err)
		}
	})

	t.Run("pwsh that is not installed", func(t *testing.T) {
		_, refusal, err := Resolve(host(platform.Darwin, map[string]string{"HOME": home.Dir}), Pwsh, root, noProber{t})
		if err != nil || refusal == nil || !reflect.DeepEqual(refusal.Reason, engine.NewNote(ReasonAbsent, "pwsh")) {
			t.Errorf("refusal %+v, err %v", refusal, err)
		}
	})

	if runtime.GOOS == "windows" {
		return
	}
	t.Run("pwsh with a restrictive policy is not refused outside Windows", func(t *testing.T) {
		prober := &fakeProber{result: ProbeResult{ProfilePath: profile, Policy: "Restricted"}}
		hook, refusal, err := Resolve(host(platform.Linux, vars), Pwsh, root, prober)
		if err != nil || refusal != nil {
			t.Fatalf("refusal %+v, err %v", refusal, err)
		}
		if hook.Path != profile || hook.NewFileCRLF {
			t.Errorf("hook = %+v (a new profile gets LF outside Windows)", hook)
		}
		if !reflect.DeepEqual(prober.asked, []string{filepath.Join(bin, "pwsh")}) {
			t.Errorf("the prober was asked %v", prober.asked)
		}
	})
}

func TestProbeArgs(t *testing.T) {
	want := []string{"-NoProfile", "-NonInteractive", "-Command", "$PROFILE.CurrentUserCurrentHost; Get-ExecutionPolicy"}
	if !reflect.DeepEqual(ProbeArgs(), want) {
		t.Errorf("ProbeArgs = %q, want %q", ProbeArgs(), want)
	}
	ProbeArgs()[0] = "changed"
	if ProbeArgs()[0] != "-NoProfile" {
		t.Error("ProbeArgs hands out shared state")
	}
}

func TestParseProbe(t *testing.T) {
	profile := filepath.Join(testenv.TempDir(t), "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
	tests := []struct {
		name   string
		output string
		policy string // "" means an error is expected
	}{
		{"as Windows PowerShell prints it", profile + "\r\nRemoteSigned\r\n", "RemoteSigned"},
		{"as pwsh prints it elsewhere", profile + "\nUnrestricted\n", "Unrestricted"},
		{"blank lines around", "\r\n" + profile + "\r\n\r\nRestricted\r\n\r\n", "Restricted"},
		{"policy in another case", profile + "\nallsigned\n", "AllSigned"},
		{"only the path", profile + "\n", ""},
		{"three lines", profile + "\nRemoteSigned\nextra\n", ""},
		{"nothing", "", ""},
		{"an unknown policy", profile + "\nMaybe\n", ""},
		{"a relative path", "Microsoft.PowerShell_profile.ps1\nRemoteSigned\n", ""},
		{"a character the code page lost", strings.Replace(profile, "Documents", "Documents?", 1) + "\nRemoteSigned\n", ""},
		{"a replacement character", strings.Replace(profile, "Documents", "Docu\uFFFDments", 1) + "\nRemoteSigned\n", ""},
		{"an error message instead", "Get-ExecutionPolicy : The term is not recognized\nat line:1 char:1\n", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := ParseProbe(test.output)
			if test.policy == "" {
				if err == nil {
					t.Errorf("ParseProbe = %+v, want an error", result)
				}
				return
			}
			if err != nil || result.ProfilePath != profile || result.Policy != test.policy {
				t.Errorf("ParseProbe = %+v, %v", result, err)
			}
		})
	}
}

func TestMessageKeysExist(t *testing.T) {
	for _, key := range []string{NoteBashProfile, ReasonAbsent, ReasonWindowsOnly, ReasonPolicy, ReasonNoProfilePath, ReasonProbeTimeout, ReasonRootNotASCII, AdvicePolicy} {
		if !i18n.Has(key) {
			t.Errorf("message key %q is not in the catalogues", key)
		}
	}
}

// TestHookEndToEnd goes from the real host of the test machine, pointed at a
// temporary home, through Resolve and the engine to the bytes on disk, and
// back. No process is started: the profile path comes from the environment.
func TestHookEndToEnd(t *testing.T) {
	home := testenv.NewFrom(t, "wt-present")
	names := []string{"pwsh"}
	if runtime.GOOS == "windows" {
		names = []string{"pwsh.exe", "powershell.exe"}
	}
	bin := programs(t, home, names...)
	before := testenv.Snapshot(t, home.Dir)
	t.Setenv("PATH", bin)
	t.Setenv("ZDOTDIR", filepath.Join(home.Dir, "zsh config"))

	h := platform.Current()
	root, err := h.Root()
	if err != nil {
		t.Fatal(err)
	}
	wanted := []Shell{Pwsh, Bash, Zsh}
	if runtime.GOOS == "windows" {
		wanted = All()
	}
	req := engine.Request{Root: root.Path, Version: "test"}
	for _, shell := range wanted {
		hook, refusal, err := Resolve(h, shell, root, noProber{t})
		if err != nil || refusal != nil {
			t.Fatalf("%s: refusal %+v, err %v", shell, refusal, err)
		}
		req.Hooks = append(req.Hooks, hook)
	}
	plan, err := engine.PlanApply(req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Refused() {
		t.Fatalf("refusals: %+v", plan.Refusals)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}

	eol := "\n"
	if runtime.GOOS == "windows" {
		eol = "\r\n"
	}
	pwshLine, _ := HookLine(h, Pwsh, root)
	if got, err := os.ReadFile(home.PwshProfile); err != nil || string(got) != pwshLine+eol {
		t.Errorf("the new pwsh profile holds %q (%v), want the line and %q", got, err, eol)
	}
	zshLine, _ := HookLine(h, Zsh, root)
	if got, err := os.ReadFile(filepath.Join(home.Dir, "zsh config", ".zshrc")); err != nil || string(got) != zshLine+"\n" {
		t.Errorf("the new .zshrc holds %q (%v)", got, err)
	}
	bashLine, _ := HookLine(h, Bash, root)
	if got, err := os.ReadFile(filepath.Join(home.Dir, ".bashrc")); err != nil || !strings.HasSuffix(string(got), "export EDITOR=vi\n"+bashLine+"\n") {
		t.Errorf(".bashrc holds %q (%v)", got, err)
	}
	if runtime.GOOS == "windows" {
		psLine, _ := HookLine(h, PowerShell, root)
		if got, err := os.ReadFile(home.PSProfile); err != nil || !strings.HasSuffix(string(got), "Set-Alias ll Get-ChildItem\r\n"+psLine+"\r\n") {
			t.Errorf("the Windows PowerShell profile holds %q (%v)", got, err)
		}
	}

	removal, err := engine.PlanUninstall(engine.UninstallRequest{Root: root.Path})
	if err != nil {
		t.Fatal(err)
	}
	if err := removal.Apply(); err != nil {
		t.Fatal(err)
	}
	if removal.ExitCode() != engine.ExitOK {
		t.Errorf("uninstall: exit %d", removal.ExitCode())
	}
	after := testenv.Snapshot(t, home.Dir)
	for key, value := range after {
		if before[key] != value {
			t.Errorf("after uninstall %s is new or changed", key)
		}
	}
	for key := range before {
		if _, ok := after[key]; !ok {
			t.Errorf("after uninstall %s is gone", key)
		}
	}
}
