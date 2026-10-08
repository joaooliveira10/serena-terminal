package shells

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/i18n"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// TestParseProbeWithoutAProfilePath: when the Documents folder is missing or
// out of reach, PowerShell prints an empty line for the profile path, so only
// the policy arrives. That has an error of its own.
func TestParseProbeWithoutAProfilePath(t *testing.T) {
	for _, output := range []string{"RemoteSigned\r\n", "\r\nRestricted\r\n", "bypass\n", "\n\nUndefined"} {
		if _, err := ParseProbe(output); !errors.Is(err, ErrNoProfilePath) {
			t.Errorf("ParseProbe(%q) = %v, want ErrNoProfilePath", output, err)
		}
	}
	// One line that is not a policy is something else gone wrong.
	for _, output := range []string{filepath.Join(testenv.TempDir(t), "profile.ps1") + "\n", "Access is denied.\n", ""} {
		if _, err := ParseProbe(output); err == nil || errors.Is(err, ErrNoProfilePath) {
			t.Errorf("ParseProbe(%q) = %v, want another error", output, err)
		}
	}
}

// TestResolveWhenPowerShellHasNoAnswer: no profile path and no answer in time
// are refusals (exit 3, nothing written), and each names the variable that
// lets the user answer in PowerShell's place.
func TestResolveWhenPowerShellHasNoAnswer(t *testing.T) {
	home := testenv.New(t)
	bin := programs(t, home, "powershell.exe", "pwsh.exe")
	root := platform.Root{Path: filepath.Join(home.LocalAppData, "sereno"), Default: true}
	h := host(platform.Windows, map[string]string{"PATH": bin, "LOCALAPPDATA": home.LocalAppData})

	tests := []struct {
		name  string
		shell Shell
		err   error
		want  engine.Refusal
	}{
		{"no profile path, Windows PowerShell", PowerShell, ErrNoProfilePath,
			engine.Refusal{Path: "powershell", Reason: engine.NewNote(ReasonNoProfilePath, "powershell.exe", "SERENO_PS_PROFILE_PATH")}},
		{"no profile path, PowerShell 7", Pwsh, ErrNoProfilePath,
			engine.Refusal{Path: "pwsh", Reason: engine.NewNote(ReasonNoProfilePath, "pwsh.exe", "SERENO_PWSH_PROFILE_PATH")}},
		{"no answer in time", PowerShell, errors.Join(errors.New("run powershell.exe"), ErrProbeTimeout),
			engine.Refusal{Path: "powershell", Reason: engine.NewNote(ReasonProbeTimeout, "powershell.exe", "15", "SERENO_PS_PROFILE_PATH")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, refusal, err := Resolve(h, test.shell, root, &fakeProber{err: test.err})
			if err != nil || refusal == nil {
				t.Fatalf("refusal %+v, err %v", refusal, err)
			}
			if !reflect.DeepEqual(*refusal, test.want) {
				t.Errorf("refusal = %+v\n   want %+v", *refusal, test.want)
			}
			for _, lang := range []i18n.Lang{i18n.English, i18n.Portuguese} {
				text := refusal.Reason.Text(i18n.For(lang).T)
				if !strings.Contains(text, test.want.Reason.Args[len(test.want.Reason.Args)-1].(string)) || strings.Contains(text, "%!") {
					t.Errorf("%s: the text does not name the variable: %q", lang, text)
				}
			}
		})
	}
	if ProbeTimeout != 15*time.Second {
		t.Errorf("ProbeTimeout = %s; the refusal text above assumes 15 seconds", ProbeTimeout)
	}
}

// standIn returns this test program and arranges for it to answer as told
// when the prober starts it (see TestMain).
func standIn(t *testing.T, answer string) string {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(standInEnv, answer)
	return program
}

// TestProbeStopsAProcessThatHangs runs the real prober against a child that
// never answers: it must give up at its deadline, stop the child and say
// ErrProbeTimeout.
func TestProbeStopsAProcessThatHangs(t *testing.T) {
	testenv.New(t)
	program := standIn(t, "hang")
	start := time.Now()
	_, err := processProber{timeout: 300 * time.Millisecond}.Probe(program)
	elapsed := time.Since(start)
	if !errors.Is(err, ErrProbeTimeout) {
		t.Errorf("Probe = %v, want ErrProbeTimeout", err)
	}
	if elapsed > 20*time.Second {
		t.Errorf("Probe returned after %s; the child was not stopped", elapsed)
	}
}

// TestProbeReadsARealProcess takes the whole way through the prober with a
// child that answers like PowerShell: start, read the pipe, decode, parse.
func TestProbeReadsARealProcess(t *testing.T) {
	testenv.New(t)
	profile := `/home/u/.config/powershell/Microsoft.PowerShell_profile.ps1`
	if runtime.GOOS == "windows" {
		profile = `C:\Users\u\Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1`
	}
	prober := NewProber()

	result, err := prober.Probe(standIn(t, profile+"\r\nRemoteSigned\r\n"))
	if err != nil || result.ProfilePath != profile || result.Policy != "RemoteSigned" {
		t.Errorf("Probe = %+v, %v", result, err)
	}
	if _, err := prober.Probe(standIn(t, "\r\nRemoteSigned\r\n")); !errors.Is(err, ErrNoProfilePath) {
		t.Errorf("a child that prints only the policy: Probe = %v, want ErrNoProfilePath", err)
	}
	if _, err := prober.Probe(filepath.Join(testenv.TempDir(t), "no-such-program")); err == nil || errors.Is(err, ErrProbeTimeout) || errors.Is(err, ErrNoProfilePath) {
		t.Errorf("a program that does not exist: Probe = %v", err)
	}
}
